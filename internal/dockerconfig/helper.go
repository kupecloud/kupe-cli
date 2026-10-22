package dockerconfig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// ErrInvalidHelper is a credential helper name that is not a plain program
// name suffix, so it is never looked up or run.
var ErrInvalidHelper = errors.New("invalid credential helper name")

// ErrHelperNotFound is a credential helper the config names that is not on
// PATH.
var ErrHelperNotFound = errors.New("credential helper not found")

// helperName is what may follow "docker-credential-": no path separator, no
// shell syntax, no leading dot or dash.
var helperName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// helperTimeout bounds one helper run; a keychain prompt the user ignores
// must not hang the command.
const helperTimeout = 30 * time.Second

// maxHelperOutput caps what is kept of a helper's output for messages.
const maxHelperOutput = 4 << 10

// notFoundMessage is what every helper of the docker-credential-helpers
// project prints when it holds nothing for the server.
const notFoundMessage = "credentials not found in native keychain"

// Program is the helper's executable name: docker-credential-NAME, with .exe
// on Windows.
func (h Helper) Program() string {
	name := "docker-credential-" + h.Name
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// limitedBuffer keeps the first maxHelperOutput bytes written to it.
type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := maxHelperOutput - b.Len(); room > 0 {
		b.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// Check reports whether the helper can be run: a valid name, and its
// program on PATH. Store and Erase check again; this lets a command refuse
// before it asks kupe-api for a credential it could not store.
func (h Helper) Check() error {
	if !helperName.MatchString(h.Name) {
		return fmt.Errorf("%w %q (from %s): it may contain only letters, digits, '.', '_' and '-'", ErrInvalidHelper, h.Name, h.Key)
	}
	if _, err := exec.LookPath(h.Program()); err != nil {
		return fmt.Errorf("%w: %s is not on PATH (chosen by %s in the Docker config); install it or change %s", ErrHelperNotFound, h.Program(), h.Key, h.Key)
	}
	return nil
}

// runHelper runs the helper's action with stdin and returns its stdout. The
// program is looked up on PATH by its fixed prefix and a checked name, and
// run without a shell; the secret only ever travels on stdin, never in
// arguments. A failure's message carries the helper's own output with every
// occurrence of secret replaced by ***.
func runHelper(ctx context.Context, h Helper, action string, stdin []byte, secret string) ([]byte, error) {
	if err := h.Check(); err != nil {
		return nil, err
	}
	path, err := exec.LookPath(h.Program())
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrHelperNotFound, h.Program(), err)
	}
	ctx, cancel := context.WithTimeout(ctx, helperTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, action) // #nosec G204 -- docker-credential-<checked name> from PATH, fixed action, no shell
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(strings.TrimSpace(stdout.String()) + " " + strings.TrimSpace(stderr.String()))
		if secret != "" {
			detail = strings.ReplaceAll(detail, secret, "***")
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("%s %s did not finish within %s", h.Program(), action, helperTimeout)
		}
		if detail == "" {
			return nil, fmt.Errorf("%s %s failed: %w", h.Program(), action, err)
		}
		return stdout.Bytes(), fmt.Errorf("%s %s failed: %s: %w", h.Program(), action, detail, err)
	}
	return stdout.Bytes(), nil
}

// eraseFromHelper asks the helper to erase host's credential. found is false
// when the helper held none.
func eraseFromHelper(ctx context.Context, h Helper, host string) (found bool, err error) {
	out, err := runHelper(ctx, h, "erase", []byte(host), "")
	if err != nil {
		if strings.Contains(string(out), notFoundMessage) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// limitedBuffer takes a command's output.
var _ io.Writer = (*limitedBuffer)(nil)
