package cmd

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

// runRoot executes the real command tree with args and returns what it
// wrote to stderr and the error cobra hands back. extra commands are added to
// the root first, for flag types no shipped command uses yet. The config path
// points into a temp dir so nothing reads the user's own config.
func runRoot(t *testing.T, extra []*cobra.Command, args ...string) (string, error) {
	t.Helper()
	t.Setenv("KUPE_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	io, _, _ := cli.Test()
	root := newRootCmd(io, &cli.GlobalFlags{})
	for _, c := range extra {
		root.AddCommand(c)
	}
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return errOut.String(), err
}

// int32Cmd stands in for an int-valued flag (no shipped command has one yet)
// so the malformed and out-of-range int shapes run through the same tree.
func int32Cmd() *cobra.Command {
	var n int32
	c := &cobra.Command{
		Use:    "int32-test",
		Hidden: true,
		RunE:   func(*cobra.Command, []string) error { return nil },
	}
	c.Flags().Int32Var(&n, "count", 0, "a count")
	return c
}

// Issue #6: a flag that fails to parse is misuse, exit 2 (docs/design.md,
// Exit codes), not a general error. The message is pflag's own, unchanged.
func TestFlagParseErrorsExitMisuse(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{
			name:    "malformed bool",
			args:    []string{"database", "create", "x", "--ha=maybe"},
			wantMsg: `invalid argument "maybe" for "--ha" flag: strconv.ParseBool: parsing "maybe": invalid syntax`,
		},
		{
			name:    "malformed bool on cluster create",
			args:    []string{"cluster", "create", "x", "--ha=maybe"},
			wantMsg: `invalid argument "maybe" for "--ha" flag: strconv.ParseBool: parsing "maybe": invalid syntax`,
		},
		{
			name:    "malformed duration",
			args:    []string{"database", "create", "x", "--wait-timeout", "two"},
			wantMsg: `invalid argument "two" for "--wait-timeout" flag: time: invalid duration "two"`,
		},
		{
			name:    "missing flag value",
			args:    []string{"database", "create", "x", "--wait-timeout"},
			wantMsg: `flag needs an argument: --wait-timeout`,
		},
		{
			name:    "malformed int",
			args:    []string{"int32-test", "--count", "two"},
			wantMsg: `invalid argument "two" for "--count" flag: strconv.ParseInt: parsing "two": invalid syntax`,
		},
		{
			name:    "out-of-range int",
			args:    []string{"int32-test", "--count", "2147483648"},
			wantMsg: `invalid argument "2147483648" for "--count" flag: strconv.ParseInt: parsing "2147483648": value out of range`,
		},
		{
			name:    "unknown flag",
			args:    []string{"database", "delete", "x", "--backup-retention", "3"},
			wantMsg: `unknown flag: --backup-retention`,
		},
		{
			name:    "unknown shorthand on root",
			args:    []string{"-x"},
			wantMsg: `unknown shorthand flag: 'x' in -x`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stderr, err := runRoot(t, []*cobra.Command{int32Cmd()}, tt.args...)
			if code := cli.ExitCode(err); code != cli.ExitMisuse {
				t.Fatalf("exit = %d; want %d (%v)", code, cli.ExitMisuse, err)
			}
			if err.Error() != tt.wantMsg {
				t.Fatalf("message changed:\n got %q\nwant %q", err.Error(), tt.wantMsg)
			}
			if want := "Error: " + tt.wantMsg + "\n"; !strings.Contains(stderr, want) {
				t.Fatalf("stderr = %q; want it to contain %q", stderr, want)
			}
		})
	}
}

// Controls: flags that parse still reach RunE, and an error RunE returns
// keeps its own exit code, so the misuse mapping is limited to parsing.
func TestFlagParseMisuseOnlyForParseErrors(t *testing.T) {
	if _, err := runRoot(t, []*cobra.Command{int32Cmd()}, "int32-test", "--count", "2147483647"); err != nil {
		t.Fatalf("valid int32 flag: err = %v; want nil", err)
	}
	failing := &cobra.Command{
		Use:  "fail-test",
		RunE: func(*cobra.Command, []string) error { return errors.New("boom") },
	}
	_, err := runRoot(t, []*cobra.Command{failing}, "fail-test")
	if code := cli.ExitCode(err); code != cli.ExitGeneral {
		t.Fatalf("RunE error: exit = %d; want %d (%v)", code, cli.ExitGeneral, err)
	}
}
