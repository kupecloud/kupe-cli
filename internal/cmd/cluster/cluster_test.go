package cluster

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/kupecloud/kupe-cli/internal/config"
)

// factoryWith wires a Factory whose Client returns the given Fake. The
// config file is empty but valid, so Resolved() works and the factory
// doesn't try to load a token.
func factoryWith(t *testing.T, fake *clienttest.Fake) *cli.Factory {
	t.Helper()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{
		APIVersion:     config.APIVersion,
		Kind:           config.Kind,
		CurrentContext: "acme",
		Contexts: []config.Context{
			{Name: "acme", APIURL: "https://test", Tenant: "acme", TokenRef: "plaintext"},
		},
	}
	if err := cfg.Save(cfgPath); err != nil {
		t.Fatal(err)
	}

	io, _, _ := cli.Test()
	flags := &cli.GlobalFlags{ConfigPath: cfgPath}
	f := cli.NewFactory(io, flags)
	f.Client = func() (client.Interface, error) { return fake, nil }
	return f
}

// executeCmd runs the command with a background context and captured
// stdout/stderr.
func executeCmd(cmd *cobra.Command, args ...string) error {
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestListEmpty(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	cmd := newListCmd(f)
	if err := executeCmd(cmd); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := f.IOStreams.Out.(interface{ String() string }).String()
	if !strings.Contains(out, "NAME") {
		t.Fatalf("expected header-only output, got:\n%s", out)
	}
}

func TestListTableAndJSON(t *testing.T) {
	fake := clienttest.New()
	fake.Clusters["prod"] = &client.Cluster{
		Name: "prod", Type: "shared", Version: "1.32",
		Status:    &client.ClusterStatus{Phase: client.PhaseRunning},
		Resources: &client.ClusterResource{CPU: "2", Memory: "8Gi"},
	}

	// Table
	f := factoryWith(t, fake)
	if err := executeCmd(newListCmd(f)); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := f.IOStreams.Out.(interface{ String() string }).String()
	for _, s := range []string{"prod", "shared", "1.32", "Running"} {
		if !strings.Contains(out, s) {
			t.Errorf("table missing %q:\n%s", s, out)
		}
	}

	// JSON
	f = factoryWith(t, fake)
	if err := executeCmd(newListCmd(f), "-o", "json"); err != nil {
		t.Fatalf("list -o json: %v", err)
	}
	var got []client.Cluster
	if err := json.Unmarshal([]byte(f.IOStreams.Out.(interface{ String() string }).String()), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 1 || got[0].Name != "prod" {
		t.Fatalf("unexpected JSON: %+v", got)
	}
}

func TestGetNotFoundMapsToExit4(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	err := executeCmd(newGetCmd(f), "ghost")
	if err == nil {
		t.Fatal("expected error")
	}
	if code := cli.ExitCode(err); code != cli.ExitNotFound {
		t.Fatalf("exit = %d; want %d", code, cli.ExitNotFound)
	}
}

func TestCreateNoWaitReturnsImmediately(t *testing.T) {
	fake := clienttest.New()
	f := factoryWith(t, fake)

	err := executeCmd(newCreateCmd(f), "prod", "--type", "shared",
		"--cpu-limit", "2", "--memory-limit", "8Gi", "--storage-limit", "50Gi",
		"--wait=false")
	if err != nil {
		t.Fatalf("create --wait=false: %v", err)
	}
	if _, ok := fake.Clusters["prod"]; !ok {
		t.Fatal("cluster not persisted in fake")
	}
}

func TestCreateWaitWalksPhasesToRunning(t *testing.T) {
	fake := clienttest.New()
	fake.GetClusterSeq["prod"] = []*client.Cluster{
		{Name: "prod", Status: &client.ClusterStatus{Phase: client.PhasePending}},
		{Name: "prod", Status: &client.ClusterStatus{Phase: client.PhaseProvisioning}},
		{Name: "prod", Status: &client.ClusterStatus{Phase: client.PhaseRunning}},
	}

	f := factoryWith(t, fake)
	// Plain progress path (Test streams have SpinnersEnabled=false).
	if err := executeCmd(newCreateCmd(f), "prod", "--type", "shared",
		"--cpu-limit", "2", "--memory-limit", "8Gi", "--storage-limit", "50Gi"); err != nil {
		t.Fatalf("create: %v", err)
	}
	out := f.IOStreams.ErrOut.(interface{ String() string }).String()
	for _, phase := range []string{"Pending", "Provisioning", "Running"} {
		if !strings.Contains(out, phase) {
			t.Errorf("progress missing %q:\n%s", phase, out)
		}
	}
}

func TestCreateRejectsConflictWithExit5(t *testing.T) {
	fake := clienttest.New()
	fake.Clusters["prod"] = &client.Cluster{Name: "prod", Type: "shared"}

	f := factoryWith(t, fake)
	err := executeCmd(newCreateCmd(f), "prod", "--type", "shared",
		"--cpu-limit", "2", "--memory-limit", "8Gi", "--storage-limit", "50Gi",
		"--wait=false")
	if err == nil {
		t.Fatal("expected conflict")
	}
	if code := cli.ExitCode(err); code != cli.ExitConflict {
		t.Fatalf("exit = %d; want %d", code, cli.ExitConflict)
	}
}

func TestDeleteNonInteractiveRequiresYes(t *testing.T) {
	fake := clienttest.New()
	fake.Clusters["prod"] = &client.Cluster{Name: "prod"}

	f := factoryWith(t, fake)
	err := executeCmd(newDeleteCmd(f), "prod")
	if err == nil {
		t.Fatal("expected misuse error")
	}
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d; want %d", code, cli.ExitMisuse)
	}
	if _, still := fake.Clusters["prod"]; !still {
		t.Fatal("cluster deleted despite non-interactive refusal")
	}
}

func TestDeleteYesRemovesCluster(t *testing.T) {
	fake := clienttest.New()
	fake.Clusters["prod"] = &client.Cluster{Name: "prod"}

	f := factoryWith(t, fake)
	err := executeCmd(newDeleteCmd(f), "prod", "--yes", "--wait=false")
	if err != nil {
		t.Fatalf("delete --yes: %v", err)
	}
	if _, still := fake.Clusters["prod"]; still {
		t.Fatal("cluster still present after delete")
	}
}

func TestUpdateUsesRMW(t *testing.T) {
	fake := clienttest.New()
	fake.Clusters["prod"] = &client.Cluster{
		Name:    "prod",
		Type:    "shared",
		Version: "1.32",
		Status:  &client.ClusterStatus{Phase: client.PhaseRunning},
	}

	f := factoryWith(t, fake)
	if err := executeCmd(newUpdateCmd(f), "prod", "--version", "1.33", "--wait=false"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if v := fake.Clusters["prod"].Version; v != "1.33" {
		t.Fatalf("version not updated: %s", v)
	}
}

func TestWaitForRunningExits0WhenAlreadyThere(t *testing.T) {
	fake := clienttest.New()
	fake.Clusters["prod"] = &client.Cluster{
		Name:   "prod",
		Status: &client.ClusterStatus{Phase: client.PhaseRunning},
	}
	f := factoryWith(t, fake)
	if err := executeCmd(newWaitCmd(f), "prod", "--for", "running", "--timeout", "5s"); err != nil {
		t.Fatalf("wait running: %v", err)
	}
}

func TestWaitForDeleted(t *testing.T) {
	fake := clienttest.New()
	// Cluster not present → GetCluster returns 404 from the fake.
	f := factoryWith(t, fake)
	if err := executeCmd(newWaitCmd(f), "prod", "--for", "deleted", "--timeout", "5s"); err != nil {
		t.Fatalf("wait deleted: %v", err)
	}
}

func TestWaitRejectsUnknownPhase(t *testing.T) {
	f := factoryWith(t, clienttest.New())
	err := executeCmd(newWaitCmd(f), "prod", "--for", "bogus", "--timeout", "5s")
	if err == nil {
		t.Fatal("expected misuse error")
	}
	if code := cli.ExitCode(err); code != cli.ExitMisuse {
		t.Fatalf("exit = %d; want %d", code, cli.ExitMisuse)
	}
}
