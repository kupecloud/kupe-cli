// Package secret wires the "kupe secret" subcommand tree for managed
// tenant secrets. Values are stored in OpenBao by the operator; the CLI
// only handles metadata — name, Vault path pointer, and per-cluster/
// -namespace sync targets.
package secret

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

// NewCmd returns the parent secret command with every v1 subcommand wired in.
func NewCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage tenant-scoped managed secrets",
		Long: `Create, inspect, update, and delete ManagedSecret resources.

Managed secrets store their actual values in OpenBao — the CLI manages the
pointer (Vault path) and the list of clusters/namespaces where the operator
mirrors the secret into the vcluster as a Kubernetes Secret.`,
	}

	cmd.AddCommand(newListCmd(f))
	cmd.AddCommand(newGetCmd(f))
	cmd.AddCommand(newCreateCmd(f))
	cmd.AddCommand(newUpdateCmd(f))
	cmd.AddCommand(newDeleteCmd(f))
	return cmd
}

// renderOne writes a single secret in the requested output format.
func renderOne(out io.Writer, colorEnabled bool, format *printer.Format, s *client.Secret) error {
	switch format.Kind {
	case printer.Table, printer.Wide:
		return printer.PrintDetails(out, s, printer.SecretDetailColumns(colorEnabled))
	case printer.JSON:
		return printer.PrintJSON(out, s)
	case printer.YAML:
		return printer.PrintYAML(out, s)
	case printer.Name:
		return printer.PrintNames(out, s, func(v any) string {
			if s, ok := v.(*client.Secret); ok && s != nil {
				return s.Name
			}
			return ""
		})
	case printer.Template:
		return printer.PrintTemplate(out, s, format.Template)
	case printer.JSONPath:
		return printer.PrintJSONPath(out, s, format.Path)
	}
	return fmt.Errorf("unhandled output kind %v", format.Kind)
}

// renderList writes a slice of secrets.
func renderList(out io.Writer, colorEnabled bool, format *printer.Format, ss []client.Secret) error {
	switch format.Kind {
	case printer.Table, printer.Wide:
		return printer.PrintTable(out, ss, printer.SecretColumns(colorEnabled), format.Kind == printer.Wide)
	case printer.JSON:
		return printer.PrintJSON(out, ss)
	case printer.YAML:
		return printer.PrintYAML(out, ss)
	case printer.Name:
		return printer.PrintNames(out, ss, func(v any) string {
			if s, ok := v.(client.Secret); ok {
				return s.Name
			}
			return ""
		})
	case printer.Template:
		return printer.PrintTemplate(out, ss, format.Template)
	case printer.JSONPath:
		return printer.PrintJSONPath(out, ss, format.Path)
	}
	return fmt.Errorf("unhandled output kind %v", format.Kind)
}

// parseSyncTargets turns a slice of --sync flag values into SyncTargets.
// Accepted forms (colon-separated):
//
//	cluster:namespace                 — secretName defaults to the secret's name
//	cluster:namespace:secretName      — explicit target secret name
func parseSyncTargets(raw []string) ([]client.SyncTarget, error) {
	out := make([]client.SyncTarget, 0, len(raw))
	for _, r := range raw {
		parts := strings.Split(r, ":")
		switch len(parts) {
		case 2:
			if parts[0] == "" || parts[1] == "" {
				return nil, fmt.Errorf("invalid --sync %q: both cluster and namespace are required", r)
			}
			out = append(out, client.SyncTarget{Cluster: parts[0], Namespace: parts[1]})
		case 3:
			if parts[0] == "" || parts[1] == "" || parts[2] == "" {
				return nil, fmt.Errorf("invalid --sync %q: every colon-separated field must be non-empty", r)
			}
			out = append(out, client.SyncTarget{Cluster: parts[0], Namespace: parts[1], SecretName: parts[2]})
		default:
			return nil, fmt.Errorf("invalid --sync %q: expected cluster:namespace or cluster:namespace:secretName", r)
		}
	}
	return out, nil
}
