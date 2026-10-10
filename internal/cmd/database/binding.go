package database

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

// maxNameLength is the longest resource name the API accepts.
const maxNameLength = 63

func newBindingCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "binding",
		Short: "Bind a database into a cluster namespace",
		Long: `Manage database bindings. A binding grants one role of a database to one
namespace of one cluster: the namespace receives a Service named after the
database and a credentials Secret, and gains a network path to the database.
Other namespaces see neither.

Roles: owner can change the schema; app can read and write data. All fields
of a binding are fixed; to change one, delete the binding and add another.`,
	}

	cmd.AddCommand(newBindingAddCmd(f))
	cmd.AddCommand(newBindingListCmd(f))
	cmd.AddCommand(newBindingGetCmd(f))
	cmd.AddCommand(newBindingDeleteCmd(f))
	return cmd
}

func renderBinding(out io.Writer, colorEnabled bool, format *printer.Format, b *client.DatabaseBinding) error {
	return printer.RenderOne(out, format, b, printer.DatabaseBindingDetailColumns(colorEnabled), func(b *client.DatabaseBinding) string { return b.Name })
}

func renderBindings(out io.Writer, colorEnabled bool, format *printer.Format, bs []client.DatabaseBinding) error {
	return printer.RenderList(out, format, bs, printer.DatabaseBindingColumns(colorEnabled), func(b client.DatabaseBinding) string { return b.Name })
}

type bindingAddOpts struct {
	cluster    string
	namespace  string
	role       string
	secretName string
	name       string
	output     string
}

func newBindingAddCmd(f *cli.Factory) *cobra.Command {
	opts := &bindingAddOpts{}

	cmd := &cobra.Command{
		Use:   "add DATABASE --cluster CLUSTER --namespace NAMESPACE --role owner|app",
		Short: "Grant a database role to a cluster namespace",
		Long: `Bind a database into one namespace of one cluster. Applications in that
namespace connect to the host named after the database and read their
credentials from the Secret (default name: DATABASE-ROLE).

The cluster does not have to exist yet; the binding reports it until it does.
The binding name defaults to DATABASE-CLUSTER-NAMESPACE-ROLE; pass --name if
that is longer than 63 characters.`,
		Args: cobra.ExactArgs(1),
		Example: `  # Migrations run with the owner role, the API with the app role
  kupe database binding add orders-db --cluster prod --namespace api --role owner --secret-name orders-db-migrate
  kupe db binding add orders-db --cluster prod --namespace api --role app`,
		RunE: func(cmd *cobra.Command, args []string) error {
			database := args[0]
			if opts.cluster == "" || opts.namespace == "" {
				return cli.MisuseError("--cluster and --namespace are required")
			}
			if err := validateRole(opts.role); err != nil {
				return err
			}
			name := opts.name
			if name == "" {
				name = fmt.Sprintf("%s-%s-%s-%s", database, opts.cluster, opts.namespace, opts.role)
				if len(name) > maxNameLength {
					return cli.MisuseError(fmt.Sprintf("default binding name %q is longer than %d characters", name, maxNameLength)).
						WithHint("pass a shorter one with --name")
				}
			}
			format, err := printer.Resolve(f, opts.output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			b, err := api.CreateDatabaseBinding(cmd.Context(), database, client.CreateDatabaseBindingRequest{
				Name:       name,
				Cluster:    opts.cluster,
				Namespace:  opts.namespace,
				Role:       opts.role,
				SecretName: opts.secretName,
			})
			if err != nil {
				return translateErr(err)
			}
			return renderBinding(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, b)
		},
	}

	cmd.Flags().StringVar(&opts.cluster, "cluster", "", "Cluster to bind into (required)")
	cmd.Flags().StringVar(&opts.namespace, "namespace", "", "Namespace inside the cluster that receives the Service and Secret (required)")
	cmd.Flags().StringVar(&opts.role, "role", "", "Role granted: owner (schema changes) or app (read and write data) (required)")
	cmd.Flags().StringVar(&opts.secretName, "secret-name", "", "Name of the credentials Secret in the namespace (default DATABASE-ROLE)")
	cmd.Flags().StringVar(&opts.name, "name", "", "Binding name (default DATABASE-CLUSTER-NAMESPACE-ROLE)")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

func newBindingListCmd(f *cli.Factory) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "list DATABASE",
		Short: "List the bindings of a database",
		Args:  cobra.ExactArgs(1),
		Example: `  kupe database binding list orders-db
  kupe db binding list orders-db -o wide`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			list, err := api.ListDatabaseBindings(cmd.Context(), args[0])
			if err != nil {
				return translateErr(err)
			}
			return renderBindings(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, list)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpList)
	return cmd
}

func newBindingGetCmd(f *cli.Factory) *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:     "get DATABASE BINDING",
		Short:   "Show a binding, including the host and Secret applications use",
		Args:    cobra.ExactArgs(2),
		Example: `  kupe database binding get orders-db orders-db-prod-api-app`,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			b, err := api.GetDatabaseBinding(cmd.Context(), args[0], args[1])
			if err != nil {
				return translateErr(err)
			}
			return renderBinding(f.IOStreams.Out, f.IOStreams.ColorEnabled, format, b)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

func newBindingDeleteCmd(f *cli.Factory) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete DATABASE BINDING",
		Short: "Remove a binding",
		Long: `Remove a binding: the Service and credentials Secret leave the namespace
and its network path to the database closes. The role's password does not
change; rotate it with "kupe database rotate-credentials" if the namespace
should also lose knowledge of it.

The request is asynchronous: the Service and Secret are removed in the
background. Prompts on a TTY; pass --yes to skip (required in
non-interactive environments).`,
		Args:    cobra.ExactArgs(2),
		Example: `  kupe database binding delete orders-db orders-db-prod-api-app --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			database, name := args[0], args[1]
			io := f.IOStreams
			if err := cli.ConfirmDelete(io, yes, "database binding", name); err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			if err := api.DeleteDatabaseBinding(cmd.Context(), database, name); err != nil {
				return translateErr(err)
			}
			fmt.Fprintf(io.Out, "binding/%s delete requested\n", name)
			fmt.Fprintf(io.ErrOut, "The role's password is unchanged; to revoke it too: kupe database rotate-credentials %s --role ROLE\n", database)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	return cmd
}
