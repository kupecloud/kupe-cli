package app

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func TestDeployAppOnlyMissingParentList404StartsCreation(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		fake := flowFake()
		refusal := &client.APIError{StatusCode: 404, Message: `app "newapp" not found`}
		if disabled {
			refusal.Code = "apps_unavailable"
			refusal.Message = "apps are not available on this platform"
		}
		fake.ListAppEnvironmentsErr = refusal
		_, _, err := run(factoryWith(fake), "deploy", "newapp", "--image", nextDigest, "--yes", "--wait=false")
		if disabled {
			if cli.ExitCode(err) != cli.ExitUnavailable || len(writes(fake)) != 0 {
				t.Fatalf("err=%v calls=%v", err, fake.Calls)
			}
		} else if err != nil || len(fake.AppCreates) != 1 || fake.AppCreates[0].Name != "newapp" || len(fake.AppEnvironmentCreates) != 1 || fake.AppEnvironmentCreates[0].Environment != "production" {
			t.Fatalf("err=%v calls=%v", err, fake.Calls)
		}
	}
}

func TestDeployResumeUsesPortableParametersForAcceptedVariableNames(t *testing.T) {
	fake := flowFake()
	fake.CreateAppEnvironmentErr = errors.New("synthetic failure")
	_, notes, _ := run(factoryWith(fake), "deploy", "--image", nextDigest, "--yes", "--wait=false", "--set-env", "APP.CONFIG=private-dot-value", "--set-env", "APP-CONFIG=private-hyphen-value")
	_, resume, ok := strings.Cut(notes, "Resume: ")
	if !ok || strings.Contains(notes, "private-dot-value") || strings.Contains(notes, "private-hyphen-value") || !strings.Contains(notes, "Set KUPE_DEPLOY_VALUE_1 locally for app variable APP.CONFIG") || !strings.Contains(notes, "Set KUPE_DEPLOY_VALUE_2 locally for app variable APP-CONFIG") {
		t.Fatal(notes)
	}
	for _, tc := range []struct {
		name    string
		env     []string
		success bool
	}{
		{"both-unset", nil, false},
		{"dot-set-hyphen-unset", []string{"KUPE_DEPLOY_VALUE_1="}, false},
		{"hyphen-set-dot-unset", []string{"KUPE_DEPLOY_VALUE_2="}, false},
		{"both-empty", []string{"KUPE_DEPLOY_VALUE_1=", "KUPE_DEPLOY_VALUE_2="}, true},
		{"quoted-values", []string{"KUPE_DEPLOY_VALUE_1=space ' $(no-command)", "KUPE_DEPLOY_VALUE_2=two words"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "/bin/sh", "-c", "kupe() { printf '%s\\n' \"$@\"; }; "+strings.TrimSpace(resume))
			command.Env = append([]string{}, tc.env...)
			out, err := command.Output()
			if !tc.success {
				if err == nil || len(out) != 0 {
					t.Fatalf("err=%v out=%s", err, out)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "both-empty" && (!strings.Contains(string(out), "APP.CONFIG=\n") || !strings.Contains(string(out), "APP-CONFIG=\n")) {
				t.Fatal(string(out))
			}
			if tc.name == "quoted-values" && (!strings.Contains(string(out), "APP.CONFIG=space ' $(no-command)\n") || !strings.Contains(string(out), "APP-CONFIG=two words\n")) {
				t.Fatal(string(out))
			}
		})
	}
}

func TestDeployResumePreservesOnlyExplicitSafeTargetFlags(t *testing.T) {
	fake := flowFake()
	fake.CreateAppEnvironmentErr = errors.New("synthetic failure")
	f := factoryWith(fake)
	cmd := NewCmd(f)
	f.Flags.Bind(cmd)
	cmd.SetOut(f.IOStreams.Out)
	cmd.SetErr(f.IOStreams.ErrOut)
	cmd.SetArgs([]string{"deploy", "--image", nextDigest, "--yes", "--wait=false", "--context", "selected context", "--tenant", "selected-tenant", "--api-url", "https://api.synthetic.invalid", "--config", "/private/tmp/synthetic config", "--token", "private-token-sentinel"})
	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected partial failure")
	}
	notes := f.IOStreams.ErrOut.(interface{ String() string }).String()
	for _, want := range []string{"--context 'selected context'", "--tenant selected-tenant", "--api-url https://api.synthetic.invalid", "--config '/private/tmp/synthetic config'"} {
		if !strings.Contains(notes, want) {
			t.Errorf("missing %s in %s", want, notes)
		}
	}
	if strings.Contains(notes, "private-token-sentinel") || strings.Contains(notes, "--token") {
		t.Fatal(notes)
	}
	// A default targeting flag is not made explicit in a resume command.
	fake = flowFake()
	fake.CreateAppEnvironmentErr = errors.New("synthetic failure")
	_, notes, _ = run(factoryWith(fake), "deploy", "--image", nextDigest, "--yes", "--wait=false")
	for _, flag := range []string{"--context", "--tenant", "--api-url", "--config", "--token"} {
		if strings.Contains(notes, flag) {
			t.Fatal(notes)
		}
	}
}
