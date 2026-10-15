package database

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

type createOpts struct {
	engine               string
	version              string
	cpu                  string
	memory               string
	storage              string
	highAvailability     bool
	noDeletionProtection bool
	wait                 bool
	waitTimeout          time.Duration
	output               string
}

func newCreateCmd(f *cli.Factory) *cobra.Command {
	opts := &createOpts{}

	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a database",
		Long: `Create a PostgreSQL database and (by default) wait for it to reach
the Running phase. Pass --wait=false to return as soon as the request is
accepted, then use "kupe database wait NAME".

--cpu, --memory and --storage are required and are per instance: a highly
available database (--ha) runs two instances, each reserving and billing for
these resources. Storage can grow later but never shrink.

Deletion protection is on by default; turn it off with
"kupe database update NAME --deletion-protection=false" before deleting, or
create the database with --no-deletion-protection.`,
		Args: cobra.ExactArgs(1),
		Example: `  # Smallest database, wait for Running
  kupe database create orders-db --cpu 250m --memory 512Mi --storage 5Gi

  # Highly available, specific major version, fire-and-forget
  kupe db create orders-db --version 17 --cpu 1 --memory 2Gi --storage 20Gi --ha --wait=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateCreateOpts(opts); err != nil {
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

			req := client.CreateDatabaseRequest{
				Name:             name,
				Engine:           opts.engine,
				Version:          opts.version,
				Resources:        &client.DatabaseResources{CPU: opts.cpu, Memory: opts.memory, Storage: opts.storage},
				HighAvailability: opts.highAvailability,
			}
			if opts.noDeletionProtection {
				off := false
				req.DeletionProtection = &off
			}
			if opts.highAvailability {
				// Stderr so -o json/yaml output stays clean.
				fmt.Fprintf(f.IOStreams.ErrOut,
					"Creating %q with high availability: two instances, each reserving and billing for the requested CPU, memory and storage.\n\n", name)
			}

			created, _, err := api.Databases().Create(cmd.Context(), req)
			if err != nil {
				return translateErr(err)
			}
			if !opts.wait {
				return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, created)
			}

			final, werr := cmdutil.WaitForPhase(cmd.Context(), f.IOStreams, noun, name, client.PhaseRunning, phaseGetter(api, name), opts.waitTimeout)
			if werr != nil {
				return cmdutil.MapWaitErr(werr, noun, name, "create")
			}
			if final == nil {
				final = created
			}
			return renderOne(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, final)
		},
	}

	cmd.Flags().StringVar(&opts.engine, "engine", "postgres", "Database engine (only postgres is supported)")
	cmd.Flags().StringVar(&opts.version, "version", "", "Engine major version (e.g. 18). Defaults to the platform default if unset.")
	cmd.Flags().StringVar(&opts.cpu, "cpu", "", "CPU per instance, without a unit (e.g. 500m, 2) (required)")
	cmd.Flags().StringVar(&opts.memory, "memory", "", "Memory per instance, with a unit (e.g. 1Gi, 512Mi) (required)")
	cmd.Flags().StringVar(&opts.storage, "storage", "", "Storage per instance, with a unit (e.g. 10Gi); can grow later, never shrink (required)")
	cmd.Flags().BoolVar(&opts.highAvailability, "ha", false, "Run two instances with a synchronous standby; resources are reserved and billed per instance")
	cmd.Flags().BoolVar(&opts.noDeletionProtection, "no-deletion-protection", false, "Create without deletion protection (it is on by default)")
	cmd.Flags().BoolVar(&opts.wait, "wait", true, "Wait for the database to reach Running before returning")
	cmd.Flags().DurationVar(&opts.waitTimeout, "wait-timeout", 30*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

// validateCreateOpts catches missing or malformed resources before the
// request; the server remains authoritative for floors and ceilings.
func validateCreateOpts(opts *createOpts) error {
	for _, req := range []struct{ flag, value, example string }{
		{"--cpu", opts.cpu, "--cpu 500m"},
		{"--memory", opts.memory, "--memory 1Gi"},
		{"--storage", opts.storage, "--storage 10Gi"},
	} {
		if req.value == "" {
			return cli.MisuseError(req.flag + " is required").WithHint("example: " + req.example)
		}
	}
	return validateResources(opts.cpu, opts.memory, opts.storage)
}

// maxQuantityLength is the longest CPU, memory or storage value the
// database API accepts.
const maxQuantityLength = 20

// validateResources checks the format of each quantity that is set.
func validateResources(cpu, memory, storage string) error {
	for _, q := range []struct{ flag, value string }{{"--cpu", cpu}, {"--memory", memory}, {"--storage", storage}} {
		if len(q.value) > maxQuantityLength {
			return cli.MisuseError(fmt.Sprintf("%s is longer than %d characters: %s", q.flag, maxQuantityLength, q.value))
		}
	}
	if cpu != "" && !cmdutil.IsCPUQuantity(cpu) {
		return cli.MisuseError("--cpu has an invalid format: " + cpu).
			WithHint("use cores or millicores without a unit (e.g. 2, 0.5, 500m)")
	}
	if memory != "" && !cmdutil.IsUnitQuantity(memory) {
		return cli.MisuseError("--memory must include a unit suffix; got: " + memory).
			WithHint("example: --memory 1Gi or --memory 512Mi")
	}
	if storage != "" && !cmdutil.IsUnitQuantity(storage) {
		return cli.MisuseError("--storage must include a unit suffix; got: " + storage).
			WithHint("example: --storage 10Gi")
	}
	return nil
}
