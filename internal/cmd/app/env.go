package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

func envService(parent string) cmdutil.Service[client.AppEnvironment, client.CreateAppEnvironmentRequest, client.PatchAppEnvironmentRequest] {
	return cmdutil.Service[client.AppEnvironment, client.CreateAppEnvironmentRequest, client.PatchAppEnvironmentRequest]{Noun: "environment", API: func(api client.Interface) client.ResourceAPI[client.AppEnvironment, client.CreateAppEnvironmentRequest, client.PatchAppEnvironmentRequest] {
		return api.AppEnvironments(parent)
	}, Phase: func(e *client.AppEnvironment) string {
		if e.Status == nil {
			return ""
		}
		return e.Status.Phase
	}, Converge: cmdutil.Converge[*client.AppEnvironment]{ObservedGeneration: func(e *client.AppEnvironment) int64 {
		if e == nil || e.Status == nil {
			return 0
		}
		return e.Status.ObservedGeneration
	}}, Printer: printer.AppEnvironment, Translate: func(err error, name string) error { return translateErr(err, parent, name) }}
}
func newEnvCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "env", Aliases: []string{"environment"}, Short: "Manage app environments"}
	for _, verb := range []string{"list", "get", "delete"} {
		child := &cobra.Command{Use: verb + " APP ENV", Short: map[string]string{"list": "List an app's environments", "get": "Show an environment", "delete": "Delete an environment and its namespace"}[verb]}
		count := 2
		if verb == "list" {
			count = 1
			child.Use = "list APP"
		}
		child.Args = cobra.ExactArgs(count)
		// Build once for flag registration and again with the parsed parent.
		build := func(parent string) *cobra.Command {
			service := envService(parent)
			switch verb {
			case "list":
				return service.ListCmd(f, cmdutil.Help{Short: child.Short})
			case "get":
				return service.GetCmd(f, cmdutil.Help{Short: child.Short})
			default:
				return service.DeleteCmd(f, cmdutil.Delete{Help: cmdutil.Help{Short: child.Short, Long: `Delete the environment, its namespace and everything in it. The cluster
and Vault entries are kept. Turn off deletion protection first, then type
the environment name to confirm, or use --yes. By default, wait until it is gone.`}, ConfirmByName: true, WaitTimeout: 30 * time.Minute, Warning: func(io *cli.IOStreams, _ string) {
					fmt.Fprintln(io.ErrOut, "The environment namespace and everything in it will be deleted. The cluster and Vault entries are kept.")
				}})
			}
		}
		template := build("")
		child.Flags().AddFlagSet(template.Flags())
		child.Long = template.Long
		child.RunE = func(cmd *cobra.Command, args []string) error {
			if err := validateName(args[0], "app", 32); err != nil {
				return err
			}
			if len(args) > 1 {
				if err := validateName(args[1], "environment", 20); err != nil {
					return err
				}
			}
			actual := build(args[0])
			var forwarded []string
			cmd.Flags().Visit(func(flag *pflag.Flag) {
				if actual.Flags().Lookup(flag.Name) != nil {
					forwarded = append(forwarded, "--"+flag.Name+"="+flag.Value.String())
				}
			})
			if err := actual.ParseFlags(forwarded); err != nil {
				return cli.Wrap(cli.ExitMisuse, "", err)
			}
			return environmentHint(actual.RunE(cmd, args[1:]), args[0])
		}
		cmd.AddCommand(child)
	}
	cmd.AddCommand(newEnvWriteCmd(f, false), newEnvWriteCmd(f, true))
	return cmd
}

type envOptions struct {
	file, cluster, image, cpu, memory, cpuLimit, memoryLimit, readiness, output string
	port, replicas                                                              int32
	env, secretEnv, secrets, domains, unsetEnv, removeSecrets, removeDomains    []string
	noProtection, protection, wait                                              bool
	waitTimeout                                                                 time.Duration
}

func newEnvWriteCmd(f *cli.Factory, update bool) *cobra.Command {
	o := &envOptions{}
	verb := "create"
	if update {
		verb = "update"
	}
	cmd := &cobra.Command{Use: verb + " APP ENV", Short: map[bool]string{false: "Create an app environment", true: "Change an app environment"}[update], Long: `Use flags for a simple configuration, or -f FILE for a YAML or JSON environment
specification. Use -f - to read stdin. Flags override file values. Images must
use an immutable digest. --cpu and --memory set requests; --cpu-limit and
--memory-limit set limits. Unspecified values use platform defaults on create
and keep the current configuration on update.

Updates retain list entries unless you replace them in a file or remove them
with --unset-env, --remove-secret or --remove-domain. The cluster and environment
name cannot change. By default, wait until the new generation is observed and
the environment settles. --wait=false returns as soon as the change is accepted.`, Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		parent, name := args[0], args[1]
		if err := validateName(parent, "app", 32); err != nil {
			return err
		}
		if err := validateName(name, "environment", 20); err != nil {
			return err
		}
		if update && cmd.Flags().Changed("cluster") {
			return cli.MisuseError("cluster cannot be changed on update")
		}
		if cmd.Flags().Changed("image") && (len(o.image) > 512 || !digestImage.MatchString(o.image)) {
			return cli.MisuseError("image must be a digest reference").WithHint(imageHint)
		}
		file, err := readEnvironmentFile(o.file, f.IOStreams.In, update)
		if err != nil {
			return err
		}
		if web, ok := file["web"].(map[string]any); ok && !cmd.Flags().Changed("image") {
			if image, ok := web["image"].(string); ok && (len(image) > 512 || !digestImage.MatchString(image)) {
				return cli.MisuseError("image must be a digest reference").WithHint(imageHint)
			}
		}
		if update && !hasEnvironmentChange(cmd) {
			return cli.MisuseError("pass a file or configuration flags to update the environment")
		}
		format, err := printer.Resolve(f, o.output)
		if err != nil {
			return err
		}
		if !update {
			req, err := o.build(cmd, nil, file, name)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			created, _, err := api.AppEnvironments(parent).Create(cmd.Context(), req)
			if err != nil {
				return translateErr(err, parent, name)
			}
			return renderEnvironmentWrite(cmd, f, api, parent, name, verb, o, format, created, "")
		}
		api, err := f.Client()
		if err != nil {
			return err
		}
		var first *client.AppEnvironment
		var mutationErr error
		updated, err := api.AppEnvironments(parent).UpdateRMW(cmd.Context(), name, func(current *client.AppEnvironment) *client.PatchAppEnvironmentRequest {
			if first != nil && changedTargets(first, current, o, file, cmd) {
				mutationErr = cli.ConflictError("environment changed in a field being updated; review it before retrying")
				return nil
			}
			if first == nil {
				data, marshalErr := json.Marshal(current)
				if marshalErr != nil {
					mutationErr = fmt.Errorf("save reviewed environment: %w", marshalErr)
					return nil
				}
				first = &client.AppEnvironment{}
				if decodeErr := json.Unmarshal(data, first); decodeErr != nil {
					mutationErr = fmt.Errorf("read reviewed environment: %w", decodeErr)
					return nil
				}
			}
			req, buildErr := o.build(cmd, current, file, name)
			if buildErr != nil {
				mutationErr = buildErr
				return nil
			}
			patch := client.PatchAppEnvironmentRequest{}
			if !reflect.DeepEqual(req.Web, current.Web) {
				patch.Web = req.Web
			}
			if !reflect.DeepEqual(req.Secrets, current.Secrets) {
				patch.Secrets = &req.Secrets
			}
			if !reflect.DeepEqual(req.Domains, current.Domains) {
				patch.Domains = &req.Domains
			}
			if req.DeletionProtection != nil && *req.DeletionProtection != current.DeletionProtection {
				patch.DeletionProtection = req.DeletionProtection
			}
			if patch.Web == nil && patch.Secrets == nil && patch.Domains == nil && patch.DeletionProtection == nil {
				return nil
			}
			return &patch
		})
		if mutationErr != nil {
			return mutationErr
		}
		if err != nil {
			return translateErr(err, parent, name)
		}
		previous := ""
		if first != nil && first.Status != nil && first.Status.RequestedRelease != nil {
			previous = first.Status.RequestedRelease.Name
		}
		return renderEnvironmentWrite(cmd, f, api, parent, name, verb, o, format, updated, previous)
	}}
	cmd.Flags().StringVarP(&o.file, "file", "f", "", "Read a YAML or JSON environment specification (- for stdin)")
	cmd.Flags().StringVar(&o.cluster, "cluster", "", "Cluster for this environment (create only)")
	cmd.Flags().StringVar(&o.image, "image", "", "Image digest reference")
	cmd.Flags().Int32Var(&o.port, "port", 0, "Web port (1-65535)")
	cmd.Flags().Int32Var(&o.replicas, "replicas", 1, "Number of replicas (1-10)")
	cmd.Flags().StringVar(&o.cpu, "cpu", "", "CPU request per replica")
	cmd.Flags().StringVar(&o.memory, "memory", "", "Memory request per replica")
	cmd.Flags().StringVar(&o.cpuLimit, "cpu-limit", "", "CPU limit per replica")
	cmd.Flags().StringVar(&o.memoryLimit, "memory-limit", "", "Memory limit per replica")
	cmd.Flags().StringArrayVar(&o.env, "env", nil, "Set a literal environment variable: NAME=VALUE")
	cmd.Flags().StringArrayVar(&o.secretEnv, "secret-env", nil, "Set a secret reference: VAR=SECRET:KEY")
	cmd.Flags().StringArrayVar(&o.secrets, "secret", nil, "Declare a Vault reference: NAME=VAULT_PATH")
	cmd.Flags().StringArrayVar(&o.domains, "domain", nil, "Add an exact hostname")
	cmd.Flags().StringVar(&o.readiness, "readiness", "", "Readiness check: tcp or http:/path")
	cmd.Flags().BoolVar(&o.noProtection, "no-deletion-protection", false, "Turn deletion protection off")
	if update {
		cmd.Flags().BoolVar(&o.protection, "deletion-protection", false, "Turn deletion protection on or off")
		cmd.Flags().StringArrayVar(&o.unsetEnv, "unset-env", nil, "Remove an environment variable by name")
		cmd.Flags().StringArrayVar(&o.removeSecrets, "remove-secret", nil, "Remove a Vault reference by name")
		cmd.Flags().StringArrayVar(&o.removeDomains, "remove-domain", nil, "Remove a hostname")
	}
	cmd.Flags().BoolVar(&o.wait, "wait", true, "Wait until the environment settles")
	cmd.Flags().DurationVar(&o.waitTimeout, "wait-timeout", 30*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&o.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
func hasEnvironmentChange(cmd *cobra.Command) bool {
	changed := false
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		switch flag.Name {
		case "output", "wait", "wait-timeout":
		default:
			changed = true
		}
	})
	return changed
}
func renderEnvironmentWrite(cmd *cobra.Command, f *cli.Factory, api client.Interface, parent, name, verb string, o *envOptions, format *printer.Format, item *client.AppEnvironment, previous string) error {
	notice := func(e *client.AppEnvironment) {
		if verb != "update" || e == nil || e.Status == nil || e.Status.RequestedRelease == nil {
			return
		}
		ref := e.Status.RequestedRelease.Name
		if ref != "" && ref != previous {
			prefix := parent + "-" + name + "-"
			if strings.HasPrefix(ref, prefix) {
				fmt.Fprintf(f.IOStreams.ErrOut, "deploying release %s\n", strings.TrimPrefix(ref, prefix))
			}
			previous = ref
		}
	}
	notice(item)
	if o.wait {
		service := envService(parent)
		get := service.Getter(api, name)
		observing := func(ctx context.Context) (*client.AppEnvironment, string, error) {
			current, phase, err := get(ctx)
			if err == nil {
				notice(current)
			}
			return current, phase, err
		}
		final, err := cmdutil.WaitForConverged(cmd.Context(), f.IOStreams, "environment", name, observing, service.Converge, item.Generation, o.waitTimeout)
		if err != nil {
			return environmentHint(service.MapWaitErr(err, name, verb), parent)
		}
		if final != nil {
			item = final
		}
		notice(item)
	}
	return printer.AppEnvironment.One(f.IOStreams, format, item)
}

// Shared wait helpers use the resource noun; this nested command adds its app
// argument to the existing hints without changing polling or error classes.
func environmentHint(err error, parent string) error {
	var ce *cli.Error
	if !errors.As(err, &ce) || ce.Hint == "" {
		return err
	}
	hint := ce.Hint
	for _, verb := range []string{"get", "delete", "update"} {
		hint = strings.ReplaceAll(hint, "kupe environment "+verb+" ", "kupe app env "+verb+" "+parent+" ")
	}
	return ce.WithHint(hint)
}
