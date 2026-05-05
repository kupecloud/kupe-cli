//go:build live

// Live integration tests for the kupe CLI, run by exec'ing the compiled
// binary against a deployed kupe-api (default: dev). Unlike the unit-test
// layer (which uses fakes via clienttest), this suite drives the real
// binary end-to-end so the noun/verb surface, output formats, exit codes,
// and token handling all see real network and a real API.
//
// Prerequisites
//
//  1. A long-lived testing tenant exists on the target cluster. The
//     fixture is the same one kupe-api's live tests use — see
//     kupe-api/test/live/suite_test.go for fixture provisioning.
//  2. An admin token on that tenant is exported as KUPE_API_TOKEN. This
//     can be either:
//       - A long-lived API key (format kupe_…) — exercises the
//         apikey auth path.
//       - An OIDC JWT bearer (e.g. minted by `kupe auth login --method
//         oidc` once and read out of the keyring) — exercises the same
//         CLI surface but with the OIDC token validation path on the
//         server. The CLI itself doesn't care which is set.
//  3. The runner has network reach to the API — usually via WireGuard
//     to the private dev API (api.dev.int.kupe.cloud).
//
// Environment variables
//
//	KUPE_API_URL         Base URL of the deployed API.
//	                     Default: https://api.dev.int.kupe.cloud
//	KUPE_API_TOKEN       Required. API key or OIDC bearer.
//	KUPE_TEST_TENANT     Tenant slug the tests target.
//	                     Default: kupe-test
//	KUPE_LIVE_CLUSTER    Set to "1" to enable the cluster lifecycle test
//	                     (5-8 minutes per run; off by default).
//
// Run with:
//
//	make test-live
//
// Per-test resources are suffixed with Unix nanoseconds so concurrent runs
// don't collide. Tests register cleanup via t.Cleanup; the testing tenant
// itself is never deleted.

package live

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	defaultAPIURL = "https://api.dev.int.kupe.cloud"
	defaultTenant = "kupe-test"
)

var (
	apiBaseURL string
	apiToken   string
	testTenant string
	binaryPath string
)

func TestMain(m *testing.M) {
	apiBaseURL = envOrDefault("KUPE_API_URL", defaultAPIURL)
	testTenant = envOrDefault("KUPE_TEST_TENANT", defaultTenant)
	apiToken = os.Getenv("KUPE_API_TOKEN")

	if apiToken == "" {
		fmt.Fprintln(os.Stderr, "KUPE_API_TOKEN not set — see test/live/suite_test.go header for setup")
		os.Exit(2)
	}

	bin, err := buildBinary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build kupe binary: %v\n", err)
		os.Exit(2)
	}
	binaryPath = bin
	defer os.Remove(binaryPath)

	fmt.Fprintf(os.Stderr, "live test target: %s (tenant=%s, binary=%s)\n", apiBaseURL, testTenant, binaryPath)
	os.Exit(m.Run())
}

// buildBinary compiles `cmd/kupe` into a temp file and returns its path.
// We build once per `go test` invocation; reusing across subtests keeps
// the suite fast. Build flags match the production Makefile loosely
// (-mod=vendor, no ldflags — the live tests don't care about Version
// strings).
func buildBinary() (string, error) {
	dir, err := os.MkdirTemp("", "kupe-cli-live-")
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, "kupe")

	// Walk up from the test/live directory to find the module root.
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root := wd
	for filepath.Base(root) != "kupe-cli" && root != "/" {
		root = filepath.Dir(root)
	}
	if root == "/" {
		return "", fmt.Errorf("could not locate kupe-cli module root from %s", wd)
	}

	cmd := exec.Command("go", "build", "-mod=vendor", "-o", out, "./cmd/kupe") //#nosec G204 -- fixed args under test
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out, nil
}

// cliResult is the captured outcome of a single binary invocation.
type cliResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// runCLI invokes the freshly-built binary with args and the standard live
// env (token, URL, tenant). Returns stdout, stderr, and the exit code.
// Tests assert on the result directly rather than through t.Fatalf so they
// can verify both success and failure paths cleanly.
func runCLI(t *testing.T, args ...string) cliResult {
	t.Helper()

	cmd := exec.Command(binaryPath, args...) //#nosec G204 -- args are fixed test inputs
	cmd.Env = append(os.Environ(),
		"KUPE_API_URL="+apiBaseURL,
		"KUPE_API_TOKEN="+apiToken,
		"KUPE_TENANT="+testTenant,
		// Force plain stdout/no-color even if the runner has TTY.
		"NO_COLOR=1",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exit := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
		} else {
			t.Fatalf("running kupe %v: %v", args, err)
		}
	}
	return cliResult{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		exitCode: exit,
	}
}

// runCLIExpectOK invokes runCLI and fails the test on a non-zero exit.
// Convenience wrapper for the common happy-path case. Returns the stdout.
func runCLIExpectOK(t *testing.T, args ...string) string {
	t.Helper()
	r := runCLI(t, args...)
	if r.exitCode != 0 {
		t.Fatalf("kupe %v exited %d\nstdout:\n%s\nstderr:\n%s", args, r.exitCode, r.stdout, r.stderr)
	}
	return r.stdout
}

// runCLIJSON invokes the binary with -o json appended, fails on non-zero
// exit, and unmarshals stdout into out. The token leak guard is also
// applied — sensitive substrings should never appear in stdout.
func runCLIJSON(t *testing.T, out any, args ...string) {
	t.Helper()
	withFmt := append([]string{}, args...)
	withFmt = append(withFmt, "-o", "json")
	stdout := runCLIExpectOK(t, withFmt...)
	assertNoTokenLeak(t, stdout)
	if err := json.Unmarshal([]byte(stdout), out); err != nil {
		t.Fatalf("unmarshal %s output: %v\nstdout:\n%s", strings.Join(args, " "), err, stdout)
	}
}

// assertNoTokenLeak checks that the bearer token didn't end up in CLI
// output. A common regression — covered here once and reused by every
// JSON-parsing test.
func assertNoTokenLeak(t *testing.T, output string) {
	t.Helper()
	if apiToken != "" && strings.Contains(output, apiToken) {
		t.Fatal("bearer token leaked into CLI stdout — failing test to surface the regression")
	}
}

// uniqueName returns a name for transient test resources. Includes a
// Unix-nanosecond suffix so reruns and concurrent invocations don't
// collide on resource names within the testing tenant.
func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
