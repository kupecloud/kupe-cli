// Package app provides app and environment commands through kupe-api.
package app

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

var appService = cmdutil.Service[client.App, client.CreateAppRequest, client.PatchAppRequest]{
	Noun: "app", API: func(api client.Interface) client.ResourceAPI[client.App, client.CreateAppRequest, client.PatchAppRequest] {
		return api.Apps()
	},
	Phase: func(a *client.App) string {
		if a.Status == nil {
			return ""
		}
		return a.Status.Phase
	},
	Printer: printer.App, Translate: func(err error, name string) error { return translateErr(err, "", name) },
}

// NewCmd builds the App surface. Deploy and release operations are separate.
func NewCmd(f *cli.Factory) *cobra.Command {
	cmd := &cobra.Command{Use: "app", Aliases: []string{"apps"}, Short: "Manage apps", Long: `An app is one stateless web service, deployed from an existing image into one
or more environments. Each environment runs on a cluster you choose.
Deleting an app never deletes a cluster.`}
	list := appService.ListCmd(f, cmdutil.Help{Short: "List apps"})
	list.Args = cobra.NoArgs
	cmd.AddCommand(list, appService.GetCmd(f, cmdutil.Help{Short: "Show an app and its environments"}), newAppCreateCmd(f), newAppUpdateCmd(f), appService.DeleteCmd(f, cmdutil.Delete{Help: cmdutil.Help{Short: "Delete an app and all its environments", Long: `Every environment is deleted first, with its namespace and everything in it.
Clusters and Vault entries are kept. Turn off deletion protection on the app
and every environment before deleting. Type the app name to confirm, or use
--yes. By default, wait until the app is gone.`}, ConfirmByName: true, Warning: func(io *cli.IOStreams, _ string) {
		fmt.Fprintln(io.ErrOut, "Every environment is deleted first, with its namespace and everything in it. Clusters and Vault entries are kept.")
	}, WaitTimeout: 30 * time.Minute}), newEnvCmd(f))
	return cmd
}
func validateName(name, field string, maximum int) error {
	if len(name) > maximum || len(validation.IsDNS1123Label(name)) > 0 || stringsContainDelimiter(name) {
		return cli.MisuseError(fmt.Sprintf("%s must be 1–%d lowercase letters or digits, with single hyphens between them", field, maximum))
	}
	return nil
}
func newAppCreateCmd(f *cli.Factory) *cobra.Command {
	var noProtection bool
	var output string
	cmd := &cobra.Command{Use: "create NAME", Short: "Create an app", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateName(args[0], "app", 32); err != nil {
			return err
		}
		format, err := printer.Resolve(f, output)
		if err != nil {
			return err
		}
		api, err := f.Client()
		if err != nil {
			return err
		}
		var protection *bool
		if noProtection {
			v := false
			protection = &v
		}
		item, _, err := api.Apps().Create(cmd.Context(), client.CreateAppRequest{Name: args[0], DeletionProtection: protection})
		if err != nil {
			return translateErr(err, "", args[0])
		}
		return printer.App.One(f.IOStreams, format, item)
	}}
	cmd.Flags().BoolVar(&noProtection, "no-deletion-protection", false, "Create with deletion protection off")
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
func newAppUpdateCmd(f *cli.Factory) *cobra.Command {
	var protection bool
	var output string
	cmd := &cobra.Command{Use: "update NAME", Short: "Change app deletion protection", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateName(args[0], "app", 32); err != nil {
			return err
		}
		if !cmd.Flags().Changed("deletion-protection") {
			return cli.MisuseError("pass --deletion-protection to update the app")
		}
		format, err := printer.Resolve(f, output)
		if err != nil {
			return err
		}
		api, err := f.Client()
		if err != nil {
			return err
		}
		var first *client.App
		var conflict error
		item, err := api.Apps().UpdateRMW(cmd.Context(), args[0], func(current *client.App) *client.PatchAppRequest {
			if first != nil && (first.UID != current.UID || first.DeletionProtection != current.DeletionProtection) {
				conflict = cli.Wrap(cli.ExitConflict, "app changed while updating deletion protection", nil)
				return nil
			}
			copy := *current
			first = &copy
			return &client.PatchAppRequest{DeletionProtection: &protection}
		})
		if conflict != nil {
			return conflict
		}
		if err != nil {
			return translateErr(err, "", args[0])
		}
		return printer.App.One(f.IOStreams, format, item)
	}}
	cmd.Flags().BoolVar(&protection, "deletion-protection", false, "Turn deletion protection on or off")
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
