package registry

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func readyConnection(cluster string, push ...string) *client.RegistryBinding {
	b := &client.RegistryBinding{Name: cluster, Registry: "registry", Cluster: cluster, PushNamespaces: push, SecretName: "kupe-registry-push",
		Status: &client.RegistryBindingStatus{Phase: client.PhaseReady, PullUsername: "kupe+acme+pull-" + cluster}}
	for _, ns := range push {
		b.Status.Namespaces = append(b.Status.Namespaces, client.RegistryBindingNamespaceStatus{Name: ns, Phase: client.PhaseReady})
	}
	return b
}

func TestConnect_PrintsRestartNote(t *testing.T) {
	t.Run("pull only", func(t *testing.T) {
		fake := seeded()
		fake.GetRegistryBindingSeq["staging"] = []*client.RegistryBinding{readyConnection("staging")}
		f := factoryWith(t, fake)
		_, errOut, err := run(f, "connect", "staging")
		if err != nil {
			t.Fatal(err)
		}
		want := `Workloads in staging can now pull from registry.kupe.cloud/acme. Pods that were running before this connection was made keep their old settings: restart them to pull from the registry, for example "kubectl rollout restart deployment -n NAMESPACE NAME".`
		if !strings.Contains(errOut, want) {
			t.Fatalf("stderr missing the restart note:\n%s", errOut)
		}
		if strings.Contains(errOut, "push namespace") {
			t.Fatalf("pull-only connection mentions push:\n%s", errOut)
		}
		req := fake.RegistryBindingCreates[0]
		if req.Name != "staging" || req.Cluster != "staging" || req.PushNamespaces != nil || req.SecretName != "" {
			t.Fatalf("request = %+v", req)
		}
	})
	t.Run("with push namespaces", func(t *testing.T) {
		fake := seeded()
		fake.GetRegistryBindingSeq["build"] = []*client.RegistryBinding{readyConnection("build", "ci")}
		f := factoryWith(t, fake)
		_, errOut, err := run(f, "connect", "build", "--push-namespace", "ci")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(errOut, "Each push namespace has a Secret named kupe-registry-push that can pull from and push to every repository in your registry, including replacing existing tags.") {
			t.Fatalf("stderr missing the push note:\n%s", errOut)
		}
	})
	t.Run("without waiting", func(t *testing.T) {
		f := factoryWith(t, seeded())
		_, errOut, err := run(f, "connect", "staging", "--wait=false")
		if err != nil || !strings.Contains(errOut, "Once the connection is Ready, workloads in staging can pull") {
			t.Fatalf("err = %v, stderr:\n%s", err, errOut)
		}
	})
	t.Run("local checks", func(t *testing.T) {
		for _, args := range [][]string{
			{"connect", "Prod"}, {"connect", "a--b"},
			{"connect", "prod", "--push-namespace", "kube-system"}, {"connect", "prod", "--push-namespace", "argocd"},
			{"connect", "prod", "--push-namespace", "ci", "--push-namespace", "ci"},
			{"connect", "prod", "--push-namespace", "CI"},
			{"connect", "prod", "--secret-name", "kupe-registry-pull"},
			{"connect", "prod", "--secret-name", "Bad_Name"},
		} {
			fake := seeded()
			_, _, err := run(factoryWith(t, fake), args...)
			assertExit(t, err, cli.ExitMisuse)
			assertNoCall(t, fake, "CreateRegistryBinding")
		}
		eleven := []string{"connect", "prod"}
		for i := range 11 {
			eleven = append(eleven, "--push-namespace", fmt.Sprintf("ns%d", i))
		}
		_, _, err := run(factoryWith(t, seeded()), eleven...)
		assertExit(t, err, cli.ExitMisuse)
	})
	t.Run("a failed connection with a Secret clash exits 5", func(t *testing.T) {
		fake := seeded()
		failed := readyConnection("build", "ci")
		failed.Status.Phase = client.PhaseFailed
		failed.Status.Message = `a Secret named "kupe-registry-push" already exists in namespace "ci"`
		failed.Status.Namespaces[0].Phase = client.PhaseFailed
		failed.Status.Conditions = []client.Condition{{Type: "Conflict", Status: "True"}}
		fake.GetRegistryBindingSeq["build"] = []*client.RegistryBinding{failed}
		_, _, err := run(factoryWith(t, fake), "connect", "build", "--push-namespace", "ci")
		assertExit(t, err, cli.ExitConflict)
		if !strings.Contains(err.Error(), "already exists") || !strings.Contains(hint(err), "kupe registry connection get build") {
			t.Fatalf("err = %v, hint %q", err, hint(err))
		}
	})
}

func TestConnectionPhase(t *testing.T) {
	ready := readyConnection("p", "a")
	withdrawing := readyConnection("p", "a")
	withdrawing.Status.Namespaces = append(withdrawing.Status.Namespaces, client.RegistryBindingNamespaceStatus{Name: "old", Phase: client.PhaseReady})
	unreported := readyConnection("p", "a", "b")
	unreported.Status.Namespaces = unreported.Status.Namespaces[:1]
	failedNS := readyConnection("p", "a")
	failedNS.Status.Phase = client.PhasePending
	failedNS.Status.Namespaces[0].Phase = client.PhaseFailed
	pullFailed := readyConnection("p")
	pullFailed.Status.Phase = client.PhaseFailed
	for _, tt := range []struct {
		name string
		b    *client.RegistryBinding
		want string
	}{
		{"ready", ready, client.PhaseReady},
		{"a removed namespace still withdrawn", withdrawing, client.PhasePending},
		{"a listed namespace not reported yet", unreported, client.PhasePending},
		{"a listed namespace failed", failedNS, client.PhaseFailed},
		{"pull failed", pullFailed, client.PhaseFailed},
		{"no status", &client.RegistryBinding{}, ""},
	} {
		if got := connectionPhase(tt.b); got != tt.want {
			t.Errorf("%s: phase = %q; want %q", tt.name, got, tt.want)
		}
	}
}

func TestConnectionUpdate_AddRemove(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"add appends", []string{"--add-push-namespace", "release"}, []string{"ci", "release"}},
		{"remove takes out", []string{"--remove-push-namespace", "ci"}, []string{}},
		{"replace", []string{"--push-namespace", "a", "--push-namespace", "b"}, []string{"a", "b"}},
		{"clear sends []", []string{"--clear-push-namespaces"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := seeded()
			f := factoryWith(t, fake)
			if _, _, err := run(f, append([]string{"connection", "update", "prod", "--wait=false"}, tt.args...)...); err != nil {
				t.Fatal(err)
			}
			p := fake.RegistryBindingPatches[0]
			if p.PushNamespaces == nil || !slices.Equal(*p.PushNamespaces, tt.want) {
				t.Fatalf("patch = %v; want %v", p.PushNamespaces, tt.want)
			}
			body, _ := json.Marshal(p)
			if len(tt.want) == 0 && string(body) != `{"pushNamespaces":[]}` {
				t.Fatalf("body = %s; want an empty list sent", body)
			}
		})
	}
	t.Run("an 11th namespace is refused locally", func(t *testing.T) {
		fake := seeded()
		ten := []string{}
		for i := range 10 {
			ten = append(ten, fmt.Sprintf("ns%d", i))
		}
		fake.RegistryBindingsByName["prod"].PushNamespaces = ten
		_, _, err := run(factoryWith(t, fake), "connection", "update", "prod", "--add-push-namespace", "extra")
		assertExit(t, err, cli.ExitMisuse)
		if len(fake.RegistryBindingPatches) != 0 {
			t.Fatal("a patch was sent")
		}
	})
	t.Run("kube-system is refused locally", func(t *testing.T) {
		fake := seeded()
		_, _, err := run(factoryWith(t, fake), "connection", "update", "prod", "--add-push-namespace", "kube-system")
		assertExit(t, err, cli.ExitMisuse)
		assertNoCall(t, fake, "UpdateRegistryBinding")
	})
	t.Run("nothing changes, nothing is sent", func(t *testing.T) {
		fake := seeded()
		_, errOut, err := run(factoryWith(t, fake), "connection", "update", "prod", "--add-push-namespace", "ci")
		if err != nil || len(fake.RegistryBindingPatches) != 0 || !strings.Contains(errOut, "is unchanged: its push namespaces are ci") {
			t.Fatalf("err = %v, patches %v, stderr %q", err, fake.RegistryBindingPatches, errOut)
		}
	})
	t.Run("mixed modes are misuse", func(t *testing.T) {
		for _, args := range [][]string{
			{"--push-namespace", "a", "--add-push-namespace", "b"},
			{"--clear-push-namespaces", "--remove-push-namespace", "ci"},
			{"--add-push-namespace", "a", "--remove-push-namespace", "a"},
			{},
		} {
			_, _, err := run(factoryWith(t, seeded()), append([]string{"connection", "update", "prod"}, args...)...)
			assertExit(t, err, cli.ExitMisuse)
		}
	})
}

func TestRotate_WaitsForApplied(t *testing.T) {
	fake := seeded()
	fake.NextRotation = "t1"
	pending := readyConnection("prod", "ci")
	pending.Status.Rotation = &client.RegistryBindingRotationStatus{RequestedAt: "t1", AppliedAt: "t0", Pending: true}
	applied := readyConnection("prod", "ci")
	applied.Status.Rotation = &client.RegistryBindingRotationStatus{RequestedAt: "t1", AppliedAt: "t1"}
	fake.GetRegistryBindingSeq["prod"] = []*client.RegistryBinding{pending, pending, applied}
	f := factoryWith(t, fake)
	out, _, err := run(f, "connection", "rotate", "prod", "--yes", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var got rotation
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got.RequestedAt != "t1" || got.AppliedAt != "t1" || got.Pending {
		t.Fatalf("rotation = %+v", got)
	}
	if !hasCall(fake, "RotateRegistryBinding:registry:prod") {
		t.Fatalf("calls = %v", fake.Calls)
	}
	t.Run("a newer rotation replaces it", func(t *testing.T) {
		fake := seeded()
		fake.NextRotation = "t1"
		newer := readyConnection("prod")
		newer.Status.Rotation = &client.RegistryBindingRotationStatus{RequestedAt: "t2", AppliedAt: "t1", Pending: true}
		done := readyConnection("prod")
		done.Status.Rotation = &client.RegistryBindingRotationStatus{RequestedAt: "t2", AppliedAt: "t2"}
		fake.GetRegistryBindingSeq["prod"] = []*client.RegistryBinding{newer, newer, done}
		f := factoryWith(t, fake)
		out, errOut, err := run(f, "connection", "rotate", "prod", "--yes")
		if err != nil || out != "connection/prod credentials rotated\n" || !strings.Contains(errOut, "A newer rotation (t2) replaced this one") {
			t.Fatalf("err = %v, out %q, stderr %q", err, out, errOut)
		}
	})
	t.Run("refused without --yes when not interactive", func(t *testing.T) {
		fake := seeded()
		_, _, err := run(factoryWith(t, fake), "connection", "rotate", "prod")
		assertExit(t, err, cli.ExitMisuse)
		assertNoCall(t, fake, "RotateRegistryBinding")
	})
}

func TestDisconnect_WarnsAndConfirms(t *testing.T) {
	fake := seeded()
	f := factoryWith(t, fake)
	interactive(f, "no\n")
	_, errOut, err := run(f, "disconnect", "prod")
	assertExit(t, err, cli.ExitMisuse)
	if !strings.Contains(errOut, "Pods in prod that restart after this cannot pull images from your registry.") {
		t.Fatalf("stderr = %q", errOut)
	}
	assertNoCall(t, fake, "DeleteRegistryBinding")
	interactive(f, "yes\n")
	out, _, err := run(f, "disconnect", "prod")
	if err != nil || !strings.Contains(out, "connection/prod delete requested") {
		t.Fatalf("err = %v, out %q", err, out)
	}
}
