package app

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

func newDeployCmd(f *cli.Factory) *cobra.Command {
	o := &envOptions{}
	var yes bool
	cmd := &cobra.Command{Use: "deploy APP [ENV] --image IMAGE", Short: "Deploy an image to an app environment", Long: `Resolve an image tag to its immutable digest, review the change, then deploy.
Keep the current configuration unless --set-env, --port or --replicas changes it.
When the app has one environment, ENV may be omitted. Non-interactive use needs
--yes. A stale review is shown again and requires a new command, even with --yes.
By default, wait for this release's outcome; recovery still counts as failure.`, Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, args []string) error {
		if !cmd.Flags().Changed("image") || o.image == "" {
			return cli.MisuseError("pass --image to deploy")
		}
		if err := validateImageInput(o.image); err != nil {
			return err
		}
		if o.waitTimeout <= 0 {
			return cli.MisuseError("--wait-timeout must be positive")
		}
		for _, entry := range o.env {
			if _, _, err := splitAssignment(entry, "--set-env"); err != nil {
				return err
			}
		}
		format, err := printer.Resolve(f, o.output)
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
		source := o.image
		if !digestImage.MatchString(o.image) {
			resolved, err := api.ResolveAppImage(cmd.Context(), o.image)
			if err != nil {
				return imageError(err)
			}
			if resolved == nil || !digestImage.MatchString(resolved.Reference) {
				return cli.New(cli.ExitUnavailable, "image resolver returned an invalid digest reference")
			}
			o.image = resolved.Reference
		}
		var first *client.AppEnvironment
		var expected *client.AppWeb
		var mutationErr error
		changedRuntime := false
		environments := api.AppEnvironments(app)
		var reviewedETag string
		mutate := func(current *client.AppEnvironment) *client.PatchAppEnvironmentRequest {
			if first != nil {
				reviewRelease(f.IOStreams, "Review current configuration", app, env, "", current, current.Web, nil)
				mutationErr = staleReviewError()
				return nil
			}
			if strings.TrimSpace(reviewedETag) == "" || current.UID == "" || current.Generation <= 0 || current.Web == nil {
				mutationErr = cli.New(cli.ExitUnavailable, "environment has no review precondition; deploy was not sent")
				return nil
			}
			req, buildErr := o.build(cmd, current, nil, env)
			if buildErr != nil {
				mutationErr = buildErr
				return nil
			}
			var changed []string
			if current.Web.Image != req.Web.Image {
				changed = append(changed, "web.image")
			}
			if current.Web.Port != req.Web.Port {
				changed = append(changed, "web.port")
			}
			if !reflect.DeepEqual(current.Web.Replicas, req.Web.Replicas) {
				changed = append(changed, "web.replicas")
			}
			if !reflect.DeepEqual(current.Web.Env, req.Web.Env) {
				changed = append(changed, "web.env")
			}
			reviewRelease(f.IOStreams, "Review deploy", app, env, source, current, req.Web, changed)
			first, expected = current, req.Web
			if confirmErr := cli.ConfirmAction(f.IOStreams, yes, "deploy", "Deploy this configuration?"); confirmErr != nil {
				mutationErr = confirmErr
				return nil
			}
			if contextErr := cmd.Context().Err(); contextErr != nil {
				mutationErr = contextErr
				return nil
			}
			if reflect.DeepEqual(current.Web, req.Web) {
				return nil
			}
			changedRuntime = true
			return &client.PatchAppEnvironmentRequest{Web: req.Web}
		}
		// Keep the shared RMW policy, but require the exact reviewed read's
		// opaque precondition before allowing its complete web snapshot to PATCH.
		item, err := client.UpdateResourceRMW(cmd.Context(),
			func(ctx context.Context) (*client.AppEnvironment, string, error) {
				current, etag, getErr := environments.Get(ctx, env)
				reviewedETag = etag
				return current, etag, getErr
			},
			func(ctx context.Context, etag string, patch client.PatchAppEnvironmentRequest) (*client.AppEnvironment, string, error) {
				return environments.Update(ctx, env, etag, patch)
			},
			mutate, client.ErrRMWContention)
		if mutationErr != nil {
			return mutationErr
		}
		if err != nil {
			return releaseRequestError(err, app, env, true)
		}
		if !changedRuntime {
			fmt.Fprintln(f.IOStreams.ErrOut, "Configuration unchanged; no release requested.")
			return printer.AppEnvironment.One(f.IOStreams, format, item)
		}
		if item == nil || item.UID != first.UID || item.Generation <= first.Generation {
			return cli.New(cli.ExitUnavailable, "deploy was accepted without a verifiable environment generation; inspect its releases before retrying")
		}
		if o.wait {
			item, err = waitForRelease(cmd.Context(), f.IOStreams, api, app, env, releaseTarget{uid: item.UID, generation: item.Generation, revision: deploymentRevision(item), previous: requestedRelease(first), web: expected, secrets: item.Secrets}, o.waitTimeout)
			if err != nil {
				return err
			}
		}
		return printer.AppEnvironment.One(f.IOStreams, format, item)
	}}
	cmd.Flags().StringVar(&o.image, "image", "", "Image tag or digest reference (required)")
	cmd.Flags().StringArrayVar(&o.env, "set-env", nil, "Set a literal environment variable: NAME=VALUE")
	cmd.Flags().Int32Var(&o.port, "port", 0, "Web port (1-65535)")
	cmd.Flags().Int32Var(&o.replicas, "replicas", 1, "Number of replicas (1-10)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm the reviewed deploy")
	cmd.Flags().BoolVar(&o.wait, "wait", true, "Wait for the requested release")
	cmd.Flags().DurationVar(&o.waitTimeout, "wait-timeout", 15*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&o.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
