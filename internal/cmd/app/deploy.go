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

func deployUpdateCommand(f *cli.Factory, o *envOptions, yes *bool, presentation *deployPresentation) *cobra.Command {
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
			if len(args) == 1 && (deployMissing(err) || strings.Contains(err.Error(), "app has no environments")) {
				return &deployLookupError{err}
			}
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
		var expected *client.AppRuntime
		var mutationErr error
		changedRuntime := false
		environments := api.AppEnvironments(app)
		var reviewedETag string
		mutate := func(current *client.AppEnvironment) *client.PatchAppEnvironmentRequest {
			if first != nil {
				reviewRelease(f.IOStreams, "Review current configuration", app, env, "", current, current.Runtime, nil)
				mutationErr = staleReviewError()
				return nil
			}
			if strings.TrimSpace(reviewedETag) == "" || current.UID == "" || current.Generation <= 0 || current.Runtime == nil {
				mutationErr = cli.New(cli.ExitUnavailable, "environment has no review precondition; deploy was not sent")
				return nil
			}
			req, buildErr := o.build(cmd, current, nil, env)
			if buildErr != nil {
				mutationErr = buildErr
				return nil
			}
			var changed []string
			if current.Runtime.Image != req.Runtime.Image {
				changed = append(changed, "runtime.image")
			}
			if current.Runtime.Port != req.Runtime.Port {
				changed = append(changed, "runtime.port")
			}
			if !reflect.DeepEqual(current.Runtime.Replicas, req.Runtime.Replicas) {
				changed = append(changed, "runtime.replicas")
			}
			if !reflect.DeepEqual(current.Runtime.Env, req.Runtime.Env) {
				changed = append(changed, "runtime.env")
			}
			if presentation.review != nil {
				presentation.review(current, req.Runtime, changed)
			} else {
				reviewRelease(f.IOStreams, "Review deploy", app, env, source, current, req.Runtime, changed)
			}
			first, expected = current, req.Runtime
			if presentation.dryRun {
				return nil
			}
			if confirmErr := cli.ConfirmAction(f.IOStreams, *yes, "deploy", "Deploy this configuration?"); confirmErr != nil {
				mutationErr = confirmErr
				return nil
			}
			if contextErr := cmd.Context().Err(); contextErr != nil {
				mutationErr = contextErr
				return nil
			}
			if reflect.DeepEqual(current.Runtime, req.Runtime) {
				return nil
			}
			fmt.Fprintf(f.IOStreams.ErrOut, "deploying %s\n", req.Runtime.Image)
			changedRuntime = true
			return &client.PatchAppEnvironmentRequest{Runtime: req.Runtime}
		}
		// Keep the shared RMW policy, but require the exact reviewed read's
		// opaque precondition before allowing its complete runtime snapshot to PATCH.
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
			if first == nil && deployMissing(err) {
				return &deployLookupError{err}
			}
			return releaseRequestError(err, app, env, true)
		}
		if presentation.dryRun {
			return nil
		}
		if !changedRuntime {
			fmt.Fprintln(f.IOStreams.ErrOut, "Configuration unchanged; no release requested.")
			if o.wait {
				target := releaseTarget{uid: item.UID, name: requestedRelease(item), revision: deploymentRevision(item), runtime: item.Runtime, secrets: item.Secrets, notice: deployReleaseNotice(f.IOStreams)}
				if target.name == "" {
					target.generation = item.Generation
				}
				item, err = waitForRelease(cmd.Context(), f.IOStreams, api, app, env, target, o.waitTimeout)
				if err != nil {
					return err
				}
			}
			return presentation.render(f, format, app, env, item)
		}
		if item == nil || item.UID != first.UID || item.Generation <= first.Generation {
			return cli.New(cli.ExitUnavailable, "deploy was accepted without a verifiable environment generation; inspect its releases before retrying")
		}
		if o.wait {
			item, err = waitForRelease(cmd.Context(), f.IOStreams, api, app, env, releaseTarget{uid: item.UID, generation: item.Generation, revision: deploymentRevision(item), previous: requestedRelease(first), runtime: expected, secrets: item.Secrets, notice: deployReleaseNotice(f.IOStreams)}, o.waitTimeout)
			if err != nil {
				return err
			}
		}
		return presentation.render(f, format, app, env, item)
	}}
	cmd.Flags().StringVar(&o.image, "image", "", "Image tag or digest reference (required)")
	registerLiteralVariables(cmd, o)
	cmd.Flags().Int32Var(&o.port, "port", 0, "Port the app listens on (1-65535)")
	registerPodFlags(cmd, o)
	cmd.Flags().BoolVarP(yes, "yes", "y", false, "Confirm the reviewed deploy")
	cmd.Flags().BoolVar(&o.wait, "wait", true, "Wait for the requested release")
	cmd.Flags().DurationVar(&o.waitTimeout, "wait-timeout", 15*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&o.output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
