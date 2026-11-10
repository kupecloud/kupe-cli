package database

import (
	"errors"
	"fmt"
	"slices"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func postgresFromNames(roles, extensions []string, setRoles, setExtensions bool) *client.DatabasePostgres {
	if !setRoles && !setExtensions {
		return nil
	}
	out := &client.DatabasePostgres{}
	if setRoles {
		list := make([]client.DeclaredRole, 0, len(roles))
		for _, name := range roles {
			list = append(list, client.DeclaredRole{Name: name})
		}
		out.Roles = &list
	}
	if setExtensions {
		list := make([]client.Extension, 0, len(extensions))
		for _, name := range extensions {
			list = append(list, client.Extension{Name: name})
		}
		out.Extensions = &list
	}
	return out
}

// Pooler flags, shared by create and update.
const (
	flagPooler          = "pooler"
	flagPoolMode        = "pool-mode"
	flagPoolerInstances = "pooler-instances"
)

// maxPoolerInstances is the most pooler instances the API accepts. The
// platform may offer fewer; the API checks.
const maxPoolerInstances = 2

// poolerHelp is the approved wording for the create flag (decisions.md,
// 2026-11-08).
const poolerHelp = "Connection pooler. Turn this on if your app opens a lot of connections: many pods, serverless functions, or short-lived connections. Small apps with a few steady connections don't need it. You can turn it on or off later."

// poolerLongHelp explains the pooler for create and update.
const poolerLongHelp = `--pooler runs a connection pooler in front of the database. Readwrite,
readonly and custom bindings connect through it; owner bindings connect
directly. --pool-mode sets how connections are shared: transaction (the
default) does not keep session state between transactions, so SET, SQL
PREPARE, LISTEN, session advisory locks and WITH HOLD cursors need session.
--pooler-instances is 1 (the default) or 2. The platform sets the pool sizes.`

// poolerFlags is the pooler flag values of create and update.
type poolerFlags struct {
	enabled   bool
	mode      string
	instances int32
}

func addPoolerFlags(cmd *cobra.Command, p *poolerFlags, enabledHelp string) {
	cmd.Flags().BoolVar(&p.enabled, flagPooler, false, enabledHelp)
	cmd.Flags().StringVar(&p.mode, flagPoolMode, "", "How the pooler shares connections: transaction (the default) or session; needs the pooler on")
	cmd.Flags().Int32Var(&p.instances, flagPoolerInstances, 0, "Pooler instances, 1 (the default) or 2; needs the pooler on")
}

// poolerFromFlags is the pooler settings the user passed, nil when none was:
// only a flag that was passed is sent. On create (create true) or when the
// pooler is being turned off, the mode and instances need the pooler on.
func poolerFromFlags(flags *pflag.FlagSet, p poolerFlags, create bool) (*client.DatabasePooler, error) {
	setEnabled, setMode, setInstances := flags.Changed(flagPooler), flags.Changed(flagPoolMode), flags.Changed(flagPoolerInstances)
	if !setEnabled && !setMode && !setInstances {
		return nil, nil
	}
	if (setMode || setInstances) && (create && !p.enabled || setEnabled && !p.enabled) {
		return nil, cli.MisuseError("--pool-mode and --pooler-instances need --pooler").
			WithHint("turn the pooler on with --pooler, or leave these flags out")
	}
	out := &client.DatabasePooler{}
	if setEnabled {
		on := p.enabled
		out.Enabled = &on
	}
	if setMode {
		if !slices.Contains(client.DatabasePoolModes, p.mode) {
			return nil, cli.MisuseError("invalid --pool-mode " + p.mode).WithHint("use --pool-mode transaction or --pool-mode session")
		}
		out.PoolMode = p.mode
	}
	if setInstances {
		if p.instances < 1 || p.instances > maxPoolerInstances {
			return nil, cli.MisuseError(fmt.Sprintf("invalid --pooler-instances %d", p.instances)).WithHint("use --pooler-instances 1 or --pooler-instances 2")
		}
		out.Instances = p.instances
	}
	return out, nil
}

// withPooler sets the pooler on a copy of postgres, allocating it when nil;
// a nil pooler returns postgres unchanged.
func withPooler(postgres *client.DatabasePostgres, pooler *client.DatabasePooler) *client.DatabasePostgres {
	if pooler == nil {
		return postgres
	}
	out := client.DatabasePostgres{}
	if postgres != nil {
		out = *postgres
	}
	out.Pooler = pooler
	return &out
}

// dbSettled is when a database's change is in effect: Ready, and the pooler
// serving when it is on. A pooler that is off is not waited on.
func dbSettled(db *client.Database) bool {
	return db != nil && db.Status.Ready() && (!db.PoolerOn() || db.Status.PoolerReady())
}

func validateListEdits(opts *updateOpts) error {
	for _, edit := range []struct {
		noun        string
		add, remove []string
	}{{"role", opts.addRoles, opts.removeRoles}, {"extension", opts.addExtensions, opts.removeExtensions}} {
		for _, name := range edit.add {
			if slices.Contains(edit.remove, name) {
				return cli.MisuseError(fmt.Sprintf("%s %s is both added and removed", edit.noun, name))
			}
		}
	}
	return nil
}

// editedPostgres runs after each read, including the retry after a concurrent update.
// Only the list being edited is sent; an empty list remains an explicit clear.
func editedPostgres(current *client.DatabasePostgres, opts *updateOpts) *client.DatabasePostgres {
	var roles, extensions []string
	if current != nil {
		if current.Roles != nil {
			for _, r := range *current.Roles {
				roles = append(roles, r.Name)
			}
		}
		if current.Extensions != nil {
			for _, e := range *current.Extensions {
				extensions = append(extensions, e.Name)
			}
		}
	}
	return postgresFromNames(editNames(roles, opts.addRoles, opts.removeRoles), editNames(extensions, opts.addExtensions, opts.removeExtensions), len(opts.addRoles)+len(opts.removeRoles) > 0, len(opts.addExtensions)+len(opts.removeExtensions) > 0)
}

func editNames(current, add, remove []string) []string {
	out := make([]string, 0, len(current)+len(add))
	for _, name := range current {
		if !slices.Contains(remove, name) {
			out = append(out, name)
		}
	}
	for _, name := range add {
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// postgresWaitErr includes the platform's tenant-facing explanation for an
// unapplied role or extension change, while preserving the wait's exit code.
func postgresWaitErr(err error, db *client.Database) error {
	var out *cli.Error
	if db == nil || db.Status == nil || !errors.As(err, &out) {
		return err
	}
	if logicalFailed(db) {
		for _, st := range db.Status.Databases {
			if st.Phase == client.PhaseFailed && st.Message != "" {
				return out.WithHint(st.Message + "\n" + out.Hint)
			}
		}
	}
	for _, condition := range db.Status.Conditions {
		if (condition.Type == "RolesReady" || condition.Type == client.ConditionPoolerReady || condition.Type == "ExtensionsReady" || condition.Type == "DatabasesReady") && condition.Status == "False" && condition.Message != "" {
			return out.WithHint(condition.Message + "\n" + out.Hint)
		}
	}
	return err
}
