// Package update implements the CLI's "a newer version exists" notice and the
// alpha notice that sits beside it (review M-11).
//
// Three rules shape everything here, and they are all about staying out of the
// way:
//
//  1. It never blocks a command. The check runs after the command has done its
//     work, against a short timeout, and any failure — no network, a captive
//     portal, GitHub down, a corrupt cache — is silently dropped. A version
//     notice that can make `kupe cluster create` hang is worse than no notice.
//
//  2. It is quiet by default in anything automated. No TTY, CI, --quiet, or
//     a machine-readable output format means no notice and no request:
//     scripts parsing stdout must never see this, and stderr noise in CI logs
//     is its own kind of cost.
//
//  3. It asks at most once a day, from a cache file next to the config. The
//     notice itself is then printed from cache, so the common case makes no
//     network request at all.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// latestReleaseURL is GitHub's unauthenticated "latest release" endpoint for
// the public kupe-cli repository. Unauthenticated calls are rate-limited per
// IP (60/hour), which the once-a-day cache keeps us far inside.
const latestReleaseURL = "https://api.github.com/repos/kupecloud/kupe-cli/releases/latest"

// checkInterval is how long a cached answer is reused before asking again.
const checkInterval = 24 * time.Hour

// requestTimeout bounds the whole check. Deliberately short: this runs after
// the user's command has finished, so every millisecond is pure latency added
// to a command that already did its job.
const requestTimeout = 2 * time.Second

// docsURL is where the upgrade instructions live, per platform.
const docsURL = "https://docs.kupe.cloud/platform/kupe-cli/"

// cache is the on-disk record of the last check.
type cache struct {
	CheckedAt     time.Time `json:"checkedAt"`
	LatestVersion string    `json:"latestVersion"`
}

// Checker performs the version check. The zero value is usable: it uses the
// real HTTP client, the real clock and the real cache path.
type Checker struct {
	// CachePath is the file holding the last result. Empty means DefaultCachePath.
	CachePath string
	// URL is the release endpoint. Empty means latestReleaseURL.
	URL string
	// Client is the HTTP client. Nil means a client with requestTimeout.
	Client *http.Client
	// Now is the clock. Nil means time.Now.
	Now func() time.Time
}

// DefaultCachePath returns the cache file path, which sits beside the config
// file so one directory holds everything the CLI writes.
func DefaultCachePath(configPath string) string {
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), "update-check.json")
}

// Notice returns the message to show the user, or "" when there is nothing to
// say. Callers print it to stderr.
//
// current is the running version (build.Version). A non-release version —
// "dev", or anything that is not a vN.N.N-shaped string — returns "" without
// asking anything: a developer running a local build does not need telling
// that the released version differs.
func (c *Checker) Notice(ctx context.Context, current string) string {
	if !isReleaseVersion(current) {
		return ""
	}

	latest := c.latest(ctx)
	if latest == "" || !isReleaseVersion(latest) {
		return ""
	}
	if !isNewer(latest, current) {
		return ""
	}

	return fmt.Sprintf("A newer kupe is available: %s (you have %s).\nUpgrade instructions: %s",
		latest, normalise(current), docsURL)
}

// latest returns the latest released version, from cache when it is fresh and
// from the network otherwise. Every failure path returns "".
func (c *Checker) latest(ctx context.Context) string {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}

	path := c.CachePath
	if cached, ok := readCache(path); ok && now().Sub(cached.CheckedAt) < checkInterval {
		return cached.LatestVersion
	}

	latest, err := c.fetch(ctx)
	if err != nil {
		// Record the attempt even on failure, so an offline machine retries
		// once a day rather than on every single command.
		writeCache(path, cache{CheckedAt: now()})
		return ""
	}
	writeCache(path, cache{CheckedAt: now(), LatestVersion: latest})
	return latest
}

func (c *Checker) fetch(ctx context.Context) (string, error) {
	url := c.URL
	if url == "" {
		url = latestReleaseURL
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release lookup returned %s", resp.Status)
	}

	// Bound the read: this is a third-party endpoint on the end of a pipe we
	// do not control, and we want one short string out of it.
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return "", err
	}
	if payload.TagName == "" {
		return "", errors.New("release lookup returned no tag_name")
	}
	return payload.TagName, nil
}

func readCache(path string) (cache, bool) {
	if path == "" {
		return cache{}, false
	}
	data, err := os.ReadFile(path) //#nosec G304 -- path derives from the resolved config location
	if err != nil {
		return cache{}, false
	}
	var c cache
	if err := json.Unmarshal(data, &c); err != nil {
		return cache{}, false
	}
	return c, true
}

func writeCache(path string, c cache) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	// Best-effort: a cache we cannot write costs one HTTP request per command
	// at worst, and failing the user's command over it would be absurd.
	_ = os.WriteFile(path, data, 0o600)
}

// normalise strips a leading "v" so versions compare and print consistently
// whichever form they arrived in (git tags carry it, --short does not).
func normalise(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// isReleaseVersion reports whether v looks like a released semver — three
// dot-separated numeric components, optionally "v"-prefixed and optionally
// with a pre-release or build suffix. "dev" (the un-stamped default) is not.
func isReleaseVersion(v string) bool {
	core, _, _ := strings.Cut(normalise(v), "-")
	core, _, _ = strings.Cut(core, "+")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// isNewer reports whether latest is a higher version than current, comparing
// the three numeric components. A pre-release suffix is ignored rather than
// ordered: this decides whether to print a sentence, and getting "1.2.0-rc.1
// versus 1.2.0" subtly wrong is not worth a semver dependency.
func isNewer(latest, current string) bool {
	l := components(latest)
	c := components(current)
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func components(v string) [3]int {
	var out [3]int
	core, _, _ := strings.Cut(normalise(v), "-")
	core, _, _ = strings.Cut(core, "+")
	for i, p := range strings.SplitN(core, ".", 3) {
		if i > 2 {
			break
		}
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				return out
			}
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out
}
