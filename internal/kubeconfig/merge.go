package kubeconfig

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ErrCollision is returned when Merge encounters an existing entry of the
// same name with different contents and force=false. Commands should
// surface this as a cli.ConflictError (exit 5) with a hint about --force.
var ErrCollision = errors.New("existing kubeconfig entry with the same name but different contents; pass --force to overwrite")

// ErrCorrupt is returned by Merge when the target file exists but cannot
// be parsed. Refusing to proceed here is a deliberate safety choice —
// silently starting from an empty config would overwrite every other
// context the user has accumulated. Commands should surface this as a
// cli.ConflictError directing the user to inspect / back up the file
// manually before re-running with --force-overwrite.
var ErrCorrupt = errors.New("existing kubeconfig is corrupt or unparsable; refusing to merge on top (use --force-overwrite to start from empty)")

// TargetPath returns the file Merge will write to. Precedence:
//
//  1. explicit argument (from --kubeconfig or similar if ever added).
//  2. First entry in $KUBECONFIG (colon-separated on Unix, semicolon on
//     Windows). This matches kubectl's "new context goes here" rule.
//  3. ~/.kube/config.
//
// Never returns "" on success; callers always get an absolute path.
func TargetPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if env := os.Getenv("KUBECONFIG"); env != "" {
		sep := ":"
		if runtime.GOOS == "windows" {
			sep = ";"
		}
		first := strings.SplitN(env, sep, 2)[0]
		if first != "" {
			return first, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".kube", "config"), nil
}

// MergeOptions controls Merge behaviour. All fields have safe zero-values.
type MergeOptions struct {
	// Force silences per-entry collision detection (ErrCollision) — an
	// existing cluster/user/context with the same name but different
	// contents is overwritten. Equivalent to kubectl's --force.
	Force bool

	// ForceOverwrite acknowledges that the existing file is corrupt and
	// authorises Merge to start from an empty config. Without this, a
	// parse failure is fatal and nothing is written. Distinct from Force
	// because the risk profile is different: Force loses a single entry;
	// ForceOverwrite loses every other context in the file.
	ForceOverwrite bool

	// Warn is where merge-time warnings (currently only the
	// ForceOverwrite-on-corrupt path) are written. Pass IOStreams.ErrOut
	// from the calling command so the warning is unit-testable. nil
	// silently drops the warning — only set in tests that don't care
	// about it; production callers should always set this.
	Warn io.Writer
}

// Merge inserts the cluster/authinfo/context entries from `incoming` into
// the kubeconfig at path, writing atomically. An absent file is treated as
// empty and will be created (with parent directories) at mode 0600.
//
// Errors:
//
//   - ErrCorrupt if the target file exists but can't be parsed and
//     opts.ForceOverwrite is false. This is a DATA-LOSS GATE — returning
//     an error here prevents silently clobbering every other context.
//   - ErrCollision if an entry of the same name already exists with
//     different contents and opts.Force is false.
func Merge(path string, incoming *clientcmdapi.Config, opts MergeOptions) error {
	if path == "" {
		return errors.New("merge target path is empty")
	}

	base, err := loadExisting(path)
	if err != nil {
		if !opts.ForceOverwrite {
			return fmt.Errorf("%w: %w", ErrCorrupt, err)
		}
		if opts.Warn != nil {
			fmt.Fprintf(opts.Warn, "warning: existing kubeconfig at %s could not be parsed (%v); --force-overwrite set, starting from empty\n", path, err)
		}
		base = clientcmdapi.NewConfig()
	}
	if base == nil {
		// No existing file — start fresh.
		base = clientcmdapi.NewConfig()
	}

	// Detect collisions before mutating.
	if !opts.Force {
		for name, in := range incoming.Clusters {
			if existing, ok := base.Clusters[name]; ok && !clusterEqual(existing, in) {
				return fmt.Errorf("cluster %q: %w", name, ErrCollision)
			}
		}
		for name, in := range incoming.AuthInfos {
			if existing, ok := base.AuthInfos[name]; ok && !authInfoEqual(existing, in) {
				return fmt.Errorf("user %q: %w", name, ErrCollision)
			}
		}
		for name, in := range incoming.Contexts {
			if existing, ok := base.Contexts[name]; ok && !contextEqual(existing, in) {
				return fmt.Errorf("context %q: %w", name, ErrCollision)
			}
		}
	}

	for name, c := range incoming.Clusters {
		base.Clusters[name] = c
	}
	for name, u := range incoming.AuthInfos {
		base.AuthInfos[name] = u
	}
	for name, c := range incoming.Contexts {
		base.Contexts[name] = c
	}

	// Promote the new context to current. Matches kubectl convention — the
	// thing you just added is usually the thing you want to use.
	if incoming.CurrentContext != "" {
		base.CurrentContext = incoming.CurrentContext
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	// clientcmd.WriteToFile writes atomically via tempfile+rename and sets
	// mode 0600 on new files.
	if err := clientcmd.WriteToFile(*base, path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// loadExisting returns:
//   - (nil, nil) if the file does not exist (fresh-install case — caller
//     should start from an empty config).
//   - (*Config, nil) if the file parses cleanly.
//   - (nil, error) if the file exists but cannot be parsed. This is a
//     data-loss signal — callers must either refuse to merge or ask the
//     user to force-overwrite explicitly.
func loadExisting(path string) (*clientcmdapi.Config, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, errors.New("kubeconfig parsed to nil — treating as corrupt")
	}
	return cfg, nil
}

// clusterEqual reports whether two *clientcmdapi.Cluster entries describe
// the same connection. Compares the fields we actually set; ignores the
// Extensions map.
func clusterEqual(a, b *clientcmdapi.Cluster) bool {
	return a != nil && b != nil &&
		a.Server == b.Server &&
		bytesEqual(a.CertificateAuthorityData, b.CertificateAuthorityData) &&
		a.CertificateAuthority == b.CertificateAuthority
}

// authInfoEqual reports whether two AuthInfos carry the same bearer token
// or exec config. For exec, reflect.DeepEqual is fine because ExecConfig's
// slices are always small.
func authInfoEqual(a, b *clientcmdapi.AuthInfo) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Token != b.Token {
		return false
	}
	if (a.Exec == nil) != (b.Exec == nil) {
		return false
	}
	if a.Exec != nil && !reflect.DeepEqual(*a.Exec, *b.Exec) {
		return false
	}
	return true
}

// contextEqual reports whether two contexts point at the same cluster+user.
func contextEqual(a, b *clientcmdapi.Context) bool {
	return a != nil && b != nil &&
		a.Cluster == b.Cluster &&
		a.AuthInfo == b.AuthInfo &&
		a.Namespace == b.Namespace
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
