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

// Issue #6, review F1: cobra's own validation errors are misuse too. A
// positional-argument count error (including a bool flag written as
// `--ha true`, where pflag leaves `true` as an extra argument) and a required
// flag left unset exit 2, with cobra's message unchanged.
func TestValidationErrorsExitMisuse(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{
			name:    "required flag on apikey create",
			args:    []string{"apikey", "create"},
			wantMsg: `required flag(s) "name" not set`,
		},
		{
			name:    "required flag on member update",
			args:    []string{"member", "update", "alice"},
			wantMsg: `required flag(s) "role" not set`,
		},
		{
			name:    "missing arg on cluster create",
			args:    []string{"cluster", "create"},
			wantMsg: `accepts 1 arg(s), received 0`,
		},
		{
			name:    "missing arg on database create",
			args:    []string{"database", "create"},
			wantMsg: `accepts 1 arg(s), received 0`,
		},
		{
			name:    "bool flag value without equals",
			args:    []string{"database", "create", "x", "--ha", "true"},
			wantMsg: `accepts 1 arg(s), received 2`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stderr, err := runRoot(t, nil, tt.args...)
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

// Controls: an unknown command stays a general error (exit 1), and a group
// command run bare or with an unknown subcommand still prints help, exit 0.
func TestMisuseLeavesUnknownCommandAndGroupHelp(t *testing.T) {
	stderr, err := runRoot(t, nil, "bogus-cmd")
	if code := cli.ExitCode(err); code != cli.ExitGeneral {
		t.Fatalf("unknown command: exit = %d; want %d (%v)", code, cli.ExitGeneral, err)
	}
	if want := `Error: unknown command "bogus-cmd" for "kupe"` + "\n"; !strings.Contains(stderr, want) {
		t.Fatalf("stderr = %q; want it to contain %q", stderr, want)
	}
	for _, args := range [][]string{{"database"}, {"database", "bogus-sub"}} {
		if _, err := runRoot(t, nil, args...); err != nil {
			t.Fatalf("%v: err = %v; want nil (help, exit 0)", args, err)
		}
	}
}

// markMisuse on shapes no shipped command has yet: flag groups, an existing
// PreRunE or PreRun, and an Args validator that already returns a typed error.
func TestMarkMisuseHooks(t *testing.T) {
	run := func(t *testing.T, c *cobra.Command, args ...string) error {
		t.Helper()
		root := &cobra.Command{Use: "root", SilenceUsage: true, SilenceErrors: true}
		root.AddCommand(c)
		markMisuse(root)
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(args)
		return root.Execute()
	}
	noop := func(*cobra.Command, []string) error { return nil }

	t.Run("flag group", func(t *testing.T) {
		c := &cobra.Command{Use: "group", RunE: noop}
		c.Flags().String("a", "", "")
		c.Flags().String("b", "", "")
		c.MarkFlagsRequiredTogether("a", "b")
		err := run(t, c, "group", "--a", "1")
		if code := cli.ExitCode(err); code != cli.ExitMisuse {
			t.Fatalf("exit = %d; want %d (%v)", code, cli.ExitMisuse, err)
		}
		want := `if any flags in the group [a b] are set they must all be set; missing [b]`
		if err.Error() != want {
			t.Fatalf("message changed:\n got %q\nwant %q", err.Error(), want)
		}
	})

	t.Run("existing PreRunE runs first and keeps its error", func(t *testing.T) {
		c := &cobra.Command{
			Use:     "pre",
			PreRunE: func(*cobra.Command, []string) error { return cli.AuthError("not logged in") },
			RunE:    noop,
		}
		c.Flags().String("name", "", "")
		_ = c.MarkFlagRequired("name")
		err := run(t, c, "pre")
		if code := cli.ExitCode(err); code != cli.ExitAuth || err.Error() != "not logged in" {
			t.Fatalf("err = %v (exit %d); want the PreRunE error, exit %d", err, code, cli.ExitAuth)
		}
	})

	t.Run("existing PreRun still runs", func(t *testing.T) {
		ran := false
		c := &cobra.Command{Use: "pre", PreRun: func(*cobra.Command, []string) { ran = true }, RunE: noop}
		if err := run(t, c, "pre"); err != nil || !ran {
			t.Fatalf("err = %v, PreRun ran = %v; want nil, true", err, ran)
		}
	})

	t.Run("typed Args error keeps its code", func(t *testing.T) {
		c := &cobra.Command{
			Use:  "typed",
			Args: func(*cobra.Command, []string) error { return cli.NotFoundError("no such thing") },
			RunE: noop,
		}
		err := run(t, c, "typed")
		if code := cli.ExitCode(err); code != cli.ExitNotFound {
			t.Fatalf("exit = %d; want %d (%v)", code, cli.ExitNotFound, err)
		}
	})
}
