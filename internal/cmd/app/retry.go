package app

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

func newRetryCmd(f *cli.Factory) *cobra.Command {
	var yes, wait bool
	var timeout time.Duration
	var output string
	cmd := &cobra.Command{Use: "retry APP [ENV]", Short: "Deploy the current configuration again", Long: `Review the current configuration and request a new Retry release.
When the app has one environment, ENV may be omitted. Non-interactive use needs
--yes. A stale review is shown again and requires a new command, even with --yes.
By default, wait for the requested release; successful recovery still exits 1.
A retry fetches fresh secret values, so it also applies values saved since the
last release to running pods.`, Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, args []string) error {
		if timeout <= 0 {
			return cli.MisuseError("--wait-timeout must be positive")
		}
		format, err := printer.Resolve(f, output)
		if err != nil {
			return err
		}
		api, err := f.Client()
		if err != nil {
			return err
		}
		app, env, err := selectEnvironment(cmd.Context(), api, args)
		if err != nil {
			return err
		}
		current, etag, err := api.AppEnvironments(app).Get(cmd.Context(), env)
		if err != nil {
			return releaseRequestError(err, app, env, false)
		}
		if etag == "" || current.UID == "" || current.Generation <= 0 {
			return cli.New(cli.ExitUnavailable, "environment has no review precondition; retry was not sent")
		}
		reviewRelease(f.IOStreams, "Review retry", app, env, "", current, current.Web, nil)
		if err := cli.ConfirmAction(f.IOStreams, yes, "retry", "Deploy this configuration again?"); err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		accepted, _, err := api.RetryAppEnvironment(cmd.Context(), app, env, etag)
		if client.IsPreconditionFailed(err) {
			fresh, _, getErr := api.AppEnvironments(app).Get(cmd.Context(), env)
			if getErr != nil {
				return releaseRequestError(getErr, app, env, false)
			}
			reviewRelease(f.IOStreams, "Review retry", app, env, "", fresh, fresh.Web, nil)
			return staleReviewError()
		}
		if err != nil {
			return releaseRequestError(err, app, env, true)
		}
		if accepted == nil || accepted.App != app || accepted.Environment != env || accepted.Generation <= current.Generation || accepted.DeploymentRevision != deploymentRevision(current)+1 {
			return cli.New(cli.ExitUnavailable, "retry was accepted without a verifiable deployment revision; inspect its releases before retrying")
		}
		if !wait {
			return printer.AppRetry.One(f.IOStreams, format, accepted)
		}
		item, err := waitForRelease(cmd.Context(), f.IOStreams, api, app, env, releaseTarget{uid: current.UID, generation: accepted.Generation, revision: accepted.DeploymentRevision, previous: requestedRelease(current), web: current.Web, secrets: current.Secrets}, timeout)
		if err != nil {
			return err
		}
		return printer.AppEnvironment.One(f.IOStreams, format, item)
	}}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm the reviewed retry")
	cmd.Flags().BoolVar(&wait, "wait", true, "Wait for the requested release")
	cmd.Flags().DurationVar(&timeout, "wait-timeout", 15*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
