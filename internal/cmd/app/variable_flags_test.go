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
		"app deploy":     {"set-env": setEnvHelp, "secret-env": secretEnvHelp},
		"app env create": {"set-env": setEnvHelp, "secret-env": secretEnvHelp},
		"app env update": {"set-env": setEnvHelp, "secret-env": secretEnvHelp, "unset-env": unsetEnvHelp},
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
		cmd.Flags().VisitAll(func(flag *pflag.Flag) {
			if !strings.Contains(flag.Name, "env") {
				return
			}
			if flag.Name == "env" {
				if flags == nil || cmd.Name() == "deploy" || !flag.Hidden || !strings.Contains(flag.Deprecated, "use --set-env") {
					t.Errorf("unexpected alias on %s", cmd.CommandPath())
				}
			} else if _, ok := flags[flag.Name]; !ok {
				t.Errorf("unexpected variable flag %s --%s", cmd.CommandPath(), flag.Name)
			}
		})
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func TestVariableAliasMatchesCanonicalAndReportsTypedFlag(t *testing.T) {
	for _, verb := range []string{"create", "update"} {
		for _, flag := range []string{"--env", "--set-env"} {
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
				if flag == "--env" && !strings.Contains(notes, "use --set-env") {
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
		fake := releaseFake()
		_, _, err := run(factoryWith(fake), "env", verb, "shop", "production", "--env", "A=b", "--set-env", "A=c", "--wait=false")
		if cli.ExitCode(err) != cli.ExitMisuse || len(writes(fake)) != 0 {
			t.Fatalf("err=%v calls=%v", err, fake.Calls)
		}
	}
}
