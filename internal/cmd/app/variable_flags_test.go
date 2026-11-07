package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client/clienttest"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestVariableFlagCommandTree(t *testing.T) {
	root := NewCmd(factoryWith(clienttest.New()))
	expected := map[string]map[string]string{
		"app deploy":     {"set-env": setEnvHelp, "secret-env": envReferenceHelp},
		"app env create": {"set-env": setEnvHelp, "secret-env": envReferenceHelp},
		"app env update": {"set-env": setEnvHelp, "secret-env": envReferenceHelp, "unset-env": unsetEnvHelp},
	}
	// Keep the non-variable flags explicit too: an invented variable flag must
	// fail this check even when its name and help never mention "env".
	nonVariable := map[string]bool{}
	for _, name := range strings.Fields("cluster cpu cpu-limit create-cluster deletion-protection domain dry-run file for help image memory memory-limit no-deletion-protection number output phase port readiness remove-domain remove-secret replicas secret timeout wait wait-timeout yes") {
		nonVariable[name] = true
	}
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		flags := expected[cmd.CommandPath()]
		for name, help := range flags {
			flag := cmd.Flags().Lookup(name)
			if flag == nil || flag.Usage != help {
				t.Errorf("%s --%s: %v", cmd.CommandPath(), name, flag)
			}
		}
		// LocalFlags includes this command's persistent declarations; walking
		// every parent checks those inherited by its descendants as well.
		cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
			if nonVariable[flag.Name] {
				return
			}
			if _, ok := flags[flag.Name]; !ok {
				t.Errorf("unexpected variable flag %s --%s", cmd.CommandPath(), flag.Name)
			}
		})
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func TestSetEnvSetsVariableAndReportsTypedFlag(t *testing.T) {
	for _, verb := range []string{"create", "update"} {
		for _, flag := range []string{"--set-env"} {
			t.Run(verb+flag, func(t *testing.T) {
				fake := releaseFake()
				if verb == "create" {
					delete(fake.AppEnvironmentsByApp["shop"], "production")
				}
				args := []string{"env", verb, "shop", "production", flag, "LOG_LEVEL=info", "--wait=false", "-o", "json"}
				if verb == "create" {
					args = append(args, "--cluster", "europe", "--image", digest, "--port", "8080")
				}
				out, notes, err := run(factoryWith(fake), args...)
				if err != nil {
					t.Fatal(err)
				}
				if !json.Valid([]byte(out)) {
					t.Fatalf("structured stdout polluted: %s", out)
				}
				if strings.Contains(notes, "deprecated") {
					t.Fatal(notes)
				}
				var value string
				if verb == "create" {
					value = *fake.AppEnvironmentCreates[0].Web.Env[0].Value
				} else {
					for _, v := range fake.AppEnvironmentPatches[0].Web.Env {
						if v.Name == "LOG_LEVEL" {
							value = *v.Value
						}
					}
				}
				if value != "info" {
					t.Fatal(value)
				}
				fake = releaseFake()
				_, _, err = run(factoryWith(fake), "env", verb, "shop", "production", flag, "broken", "--wait=false")
				if cli.ExitCode(err) != cli.ExitMisuse || !strings.Contains(err.Error(), flag) {
					t.Fatalf("%v", err)
				}
			})
		}
	}
}

// The --env alias was removed: it is refused as an unknown flag, with no write.
func TestRemovedEnvAliasIsRefused(t *testing.T) {
	cases := [][]string{
		{"env", "update", "shop", "production", "--env", "A=b", "--wait=false"},
		{"env", "create", "shop", "production", "--env", "A=b", "--cluster", "europe", "--image", digest, "--port", "8080", "--wait=false"},
		{"deploy", "--env", "A=b", "--image", digest, "--yes", "--wait=false"},
	}
	for _, args := range cases {
		fake := releaseFake()
		_, _, err := run(factoryWith(fake), args...)
		if err == nil || !strings.Contains(err.Error(), "unknown flag: --env") || len(writes(fake)) != 0 {
			t.Errorf("%v: err=%v calls=%v", args, err, fake.Calls)
		}
	}
}

func TestEnvironmentHelpGoesToStdout(t *testing.T) {
	for _, verb := range []string{"create", "update"} {
		t.Run(verb, func(t *testing.T) {
			out, notes, err := run(factoryWith(clienttest.New()), "env", verb, "--help")
			if err != nil || !strings.Contains(out, "Usage:") || !strings.Contains(out, "--set-env") || notes != "" {
				t.Fatalf("err=%v stdout=%q stderr=%q", err, out, notes)
			}
		})
	}
}
