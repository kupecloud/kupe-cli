package secret

import (
	"context"
	"os"
	osexec "os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
)

func TestValuesInputDescriptorExhaustion(t *testing.T) {
	if os.Getenv("KUPE_VALUES_DESCRIPTOR_CHILD") != "1" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		child := osexec.CommandContext(ctx, executable, "-test.run=^TestValuesInputDescriptorExhaustion$") // #nosec G204 -- only this test executable
		child.Env = []string{"GOMAXPROCS=2", "KUPE_VALUES_DESCRIPTOR_CHILD=1"}
		if out, err := child.CombinedOutput(); err != nil {
			t.Fatalf("descriptor exhaustion child: %v\n%s", err, out)
		}
		return
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if _, err := writer.WriteString(`{"A":"synthetic"}`); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	conn, err := reader.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var fd int
	var dupErr error
	if err := conn.Control(func(original uintptr) { fd, dupErr = unix.FcntlInt(original, unix.F_DUPFD_CLOEXEC, 1100) }); err != nil {
		t.Fatal(err)
	}
	if dupErr != nil {
		t.Fatal(dupErr)
	}
	high := os.NewFile(uintptr(fd), "high-input")
	defer high.Close()
	f := factoryWith(t, clienttest.New())
	f.IOStreams.In = high
	calls := 0
	f.Client = func() (client.Interface, error) { calls++; return clienttest.New(), nil }
	// Isolate the host-wide descriptor pressure to this joined test child.
	var owned []int
	defer func() {
		for _, duplicate := range owned {
			_ = unix.Close(duplicate)
		}
	}()
	for {
		duplicate, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		owned = append(owned, duplicate)
		if duplicate >= 1024 {
			break
		}
	}
	started := time.Now()
	err = exec(newValuesSetCmd(f), "--path", "team/db", "--expected-version", "0", "--stdin")
	if cli.ExitCode(err) != cli.ExitMisuse || err.Error() != "could not read values input" || calls != 0 || time.Since(started) > time.Second {
		t.Fatalf("err=%v exit=%d calls=%d elapsed=%s", err, cli.ExitCode(err), calls, time.Since(started))
	}
}
