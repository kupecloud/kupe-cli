//go:build darwin || linux

package secret

import (
	"context"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

func TestValuesDirectoryInputIsBoundedMisuse(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	calls := 0
	f.Client = func() (client.Interface, error) { calls++; return clienttest.New(), nil }
	cmd := newValuesSetCmd(f)
	cmd.SetArgs([]string{"--path", "team/db", "--expected-version", "0", "--file", t.TempDir()})
	cmd.SetOut(f.IOStreams.Out)
	cmd.SetErr(f.IOStreams.ErrOut)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("directory values input panicked: %v", recovered)
		}
		if calls != 0 {
			t.Errorf("client calls=%d; want zero for unreadable values input", calls)
		}
	}()
	err := cmd.ExecuteContext(ctx)
	if cli.ExitCode(err) != cli.ExitMisuse || err.Error() != "could not read values input" {
		t.Fatalf("exit=%d err=%v", cli.ExitCode(err), err)
	}
	if ctx.Err() != nil {
		t.Fatalf("directory input reached its context bound: %v", ctx.Err())
	}
}
