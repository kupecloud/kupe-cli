// Package registry wires the "kupe registry" command tree for the tenant's
// container registry: turning it on and off, connecting clusters, access
// tokens, browsing and deleting repositories, images and tags, and signing
// Docker in. A tenant has at most one registry, always named "registry", so
// no command takes a registry name.
package registry

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

// noun is the command noun used in progress labels, hints and
// "registry/registry ..." output lines.
const noun = "registry"

// deleteWarning is the one sentence every surface (kupe-api, console,
// Terraform, CLI) shows before a registry is deleted.
const deleteWarning = "Deleting a registry deletes every image in it. They cannot be restored."

// NewCmd returns the parent registry command with every subcommand wired in.
func NewCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage your container registry",
		Long: `Turn on your tenant's container registry, connect clusters to it, create
access tokens, and browse or delete its repositories, images and tags.

A tenant has one registry. Its images live at registry.kupe.cloud/TENANT/...
and stay private to the tenant. "kupe registry login" signs Docker in for 12
hours. "kupe registry connect CLUSTER" lets every pod in a cluster pull from
the registry, and can give chosen namespaces a Secret that pushes to it.
"kupe registry token create" makes access tokens for CI and other automation.`,
	}

	cmd.AddCommand(newCreateCmd(f))
	cmd.AddCommand(newGetCmd(f))
	cmd.AddCommand(newListCmd(f))
	cmd.AddCommand(newUpdateCmd(f))
	cmd.AddCommand(newDeleteCmd(f))
	cmd.AddCommand(newWaitCmd(f))
	cmd.AddCommand(newConnectCmd(f))
	cmd.AddCommand(newDisconnectCmd(f))
	cmd.AddCommand(newConnectionCmd(f))
	cmd.AddCommand(newRepoCmd(f))
	cmd.AddCommand(newImageCmd(f))
	cmd.AddCommand(newTagCmd(f))
	cmd.AddCommand(newTokenCmd(f))
	cmd.AddCommand(newLoginCmd(f))
	cmd.AddCommand(newLogoutCmd(f))
	return cmd
}

// service is the registry's shared command surface: list, get and delete
// are built from it, and create and wait wait and translate through it. Its
// one resource is always client.RegistryName.
var service = cmdutil.Service[client.Registry, client.CreateRegistryRequest, client.PatchRegistryRequest]{
	Noun: noun,
	API:  client.Interface.Registries,
	Phase: func(r *client.Registry) string {
		if r.Status == nil {
			return ""
		}
		return r.Status.Phase
	},
	Converge: cmdutil.Converge[*client.Registry]{
		ObservedGeneration: func(r *client.Registry) int64 {
			if r == nil || r.Status == nil {
				return 0
			}
			return r.Status.ObservedGeneration
		},
	},
	Printer:   printer.Registry,
	Translate: func(err error, _ string) error { return translateRegistryErr(err) },
	Singleton: client.RegistryName,
}

func newGetCmd(f *cli.Factory) *cobra.Command {
	return service.GetCmd(f, cmdutil.Help{
		Short: "Show the registry",
		Long: `Show the registry: its phase, the endpoint to push to and pull from, the
storage it uses and its limit, how many repositories it holds, and whether
deletion protection is on.`,
		Example: `  kupe registry get
  kupe registry get -o json`,
	})
}

func newListCmd(f *cli.Factory) *cobra.Command {
	cmd := service.ListCmd(f, cmdutil.Help{
		Short: "List the registry (a tenant has at most one)",
		Long: `List the tenant's registry as a table of at most one row, for scripts that
treat every service alike. "kupe registry get" shows more.`,
		Example: `  kupe registry list
  kupe registry list -o wide`,
	})
	cmd.Args = cobra.NoArgs
	return cmd
}

func newDeleteCmd(f *cli.Factory) *cobra.Command {
	return service.DeleteCmd(f, cmdutil.Delete{
		Help: cmdutil.Help{
			Short: "Delete the registry and every image in it",
			Long: deleteWarning + `

Deletion protection is on by default, and a protected registry is never
deleted: turn it off first with
"kupe registry update --deletion-protection=false".

On a TTY you must type the registry's name, "registry"; pass --yes to skip
the prompt (required in non-interactive environments). By default the command
waits until the registry is gone.`,
			Example: `  kupe registry update --deletion-protection=false
  kupe registry delete`,
		},
		Warning: func(io *cli.IOStreams, _ string) {
			fmt.Fprintf(io.ErrOut, "\n%s\n\n", deleteWarning)
		},
		ConfirmByName: true,
		Translate:     translateDeleteErr,
		WaitTimeout:   15 * time.Minute,
	})
}

func newWaitCmd(f *cli.Factory) *cobra.Command {
	var forPhase string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "wait",
		Short: "Wait for the registry to be ready or deleted",
		Long: `Wait until the registry is ready (phase Running) or deleted. On success it
prints "registry/registry running" or "registry/registry deleted".`,
		Args: cobra.NoArgs,
		Example: `  kupe registry wait --for ready
  kupe registry wait --for deleted --timeout 20m`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if forPhase == "ready" {
				forPhase = "running"
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			return service.RunWait(cmd.Context(), f.IOStreams, api, client.RegistryName, forPhase, timeout)
		},
	}
	cmd.Flags().StringVar(&forPhase, "for", "ready", "What to wait for: ready (phase Running) | deleted | pending | provisioning | degraded | terminating")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Give up after this long")
	return cmd
}

type createOpts struct {
	storageLimit         string
	noDeletionProtection bool
	wait                 bool
	waitTimeout          time.Duration
	output               string
}

func newCreateCmd(f *cli.Factory) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Turn on the container registry",
		Long: `Create the tenant's registry and, by default, wait until it is ready to push
to and pull from. Pass --wait=false to return as soon as the request is
accepted, then use "kupe registry wait".

--storage-limit caps what the registry may store, below your plan's cap, in
binary units (Mi, Gi or Ti). Without it the plan's cap applies.

Deletion protection is on by default; turn it off with
"kupe registry update --deletion-protection=false" before deleting, or create
the registry with --no-deletion-protection.`,
		Args: cobra.NoArgs,
		Example: `  kupe registry create
  kupe registry create --storage-limit 5Gi
  kupe registry create --no-deletion-protection --wait=false -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateStorageLimit(opts.storageLimit); err != nil {
				return err
			}
			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			req := client.CreateRegistryRequest{Name: client.RegistryName, StorageLimit: opts.storageLimit}
			if opts.noDeletionProtection {
				off := false
				req.DeletionProtection = &off
			}
			created, _, err := api.Registries().Create(cmd.Context(), req)
			if err != nil {
				return translateErr(err, "")
			}
			final := created
			if opts.wait {
				ready, werr := service.WaitForPhase(cmd.Context(), f.IOStreams, api, client.RegistryName, client.PhaseRunning, opts.waitTimeout)
				if werr != nil {
					return service.MapWaitErr(werr, client.RegistryName, "create")
				}
				if ready != nil {
					final = ready
				}
			}
			if err := printer.Registry.One(f.IOStreams, format, final); err != nil {
				return err
			}
			printNextSteps(f.IOStreams, final)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.storageLimit, "storage-limit", "", "Most the registry may store, in Mi, Gi or Ti (e.g. 5Gi); default the plan's cap")
	cmd.Flags().BoolVar(&opts.noDeletionProtection, "no-deletion-protection", false, "Create without deletion protection (it is on by default)")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait for the registry to be ready before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 15*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

// printNextSteps tells a person, on stderr, what to do with a new registry.
func printNextSteps(io *cli.IOStreams, r *client.Registry) {
	fmt.Fprintln(io.ErrOut)
	if r.Status != nil && r.Status.Endpoint != "" {
		fmt.Fprintf(io.ErrOut, "Push and pull images at %s/REPOSITORY:TAG.\n", r.Status.Endpoint)
	}
	fmt.Fprintln(io.ErrOut, "Next steps:")
	fmt.Fprintln(io.ErrOut, "  kupe registry login               sign Docker in for 12 hours")
	fmt.Fprintln(io.ErrOut, "  kupe registry connect CLUSTER     let the pods of a cluster pull from the registry")
}

// storageLimitPattern is the registry's storage limit format: a number with
// a binary unit, Mi, Gi or Ti.
var storageLimitPattern = regexp.MustCompile(`^\d+(\.\d+)?(Mi|Gi|Ti)$`)

// maxStorageLimitLength is the longest storage limit the API accepts.
const maxStorageLimitLength = 20

// validateStorageLimit checks --storage-limit before a request; the API
// stays authoritative for the plan's cap.
func validateStorageLimit(limit string) error {
	if limit == "" {
		return nil
	}
	const hint = "use a number with a binary unit, Mi, Gi or Ti, e.g. --storage-limit 5Gi"
	if len(limit) > maxStorageLimitLength || !storageLimitPattern.MatchString(limit) {
		return cli.MisuseError("invalid --storage-limit " + limit).WithHint(hint)
	}
	if n, err := strconv.ParseFloat(limit[:len(limit)-2], 64); err != nil || n <= 0 {
		return cli.MisuseError("--storage-limit must be more than zero").WithHint(hint)
	}
	return nil
}

type updateOpts struct {
	storageLimit       string
	clearStorageLimit  bool
	deletionProtection bool
	output             string
}

func newUpdateCmd(f *cli.Factory) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Change the registry's storage limit or deletion protection",
		Long: `Change the registry's storage limit or deletion protection. Only the flags
you pass are changed.

--storage-limit sets a cap below your plan's; --clear-storage-limit removes
it so the plan's cap applies.

The command reads the registry and writes the change with its version; if
another client changed it in between, it reads it again and retries once.
The change is in effect when the command returns.`,
		Args: cobra.NoArgs,
		Example: `  kupe registry update --storage-limit 10Gi
  kupe registry update --clear-storage-limit
  kupe registry update --deletion-protection=false`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			patch := client.PatchRegistryRequest{}
			switch {
			case opts.storageLimit != "" && opts.clearStorageLimit:
				return cli.MisuseError("pass --storage-limit or --clear-storage-limit, not both")
			case opts.storageLimit != "":
				if err := validateStorageLimit(opts.storageLimit); err != nil {
					return err
				}
				patch.StorageLimit = &opts.storageLimit
			case opts.clearStorageLimit:
				none := ""
				patch.StorageLimit = &none
			}
			if cmd.Flags().Changed("deletion-protection") {
				patch.DeletionProtection = &opts.deletionProtection
			}
			if patch == (client.PatchRegistryRequest{}) {
				return cli.MisuseError("nothing to update: pass at least one of --storage-limit, --clear-storage-limit, --deletion-protection")
			}
			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			updated, err := api.Registries().UpdateRMW(cmd.Context(), client.RegistryName, func(*client.Registry) *client.PatchRegistryRequest {
				return &patch
			})
			if err != nil {
				return translateRegistryErr(err)
			}
			return printer.Registry.One(f.IOStreams, format, updated)
		},
	}
	cmd.Flags().StringVar(&opts.storageLimit, "storage-limit", "", "New storage limit, in Mi, Gi or Ti (e.g. 10Gi)")
	cmd.Flags().BoolVar(&opts.clearStorageLimit, "clear-storage-limit", false, "Remove the storage limit, so the plan's cap applies")
	cmd.Flags().BoolVar(&opts.deletionProtection, "deletion-protection", false, "Turn deletion protection on (--deletion-protection) or off (--deletion-protection=false)")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
