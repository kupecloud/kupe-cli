package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
	"github.com/kupecloud/kupe-cli/internal/ux"
)

type releaseTarget struct {
	uid                  string
	generation, revision int64
	name, previous       string
	web                  *client.AppWeb
	secrets              []client.AppSecret
}

func newWaitCmd(f *cli.Factory) *cobra.Command {
	var timeout time.Duration
	var forPhase, output string
	cmd := &cobra.Command{Use: "wait APP [ENV]", Short: "Wait for the requested app release", Long: `Watch the release currently requested by the environment. Waiting never changes it.
When the app has one environment, ENV may be omitted. A failed release exits 1,
including after successful recovery. A superseded release exits 1; a timeout
exits 8 and prints the command to resume.`, Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, args []string) error {
		if !strings.EqualFold(forPhase, "Succeeded") {
			return cli.MisuseError("--for must be Succeeded")
		}
		if timeout <= 0 {
			return cli.MisuseError("--timeout must be positive")
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
		item, err := waitForRelease(cmd.Context(), f.IOStreams, api, app, env, releaseTarget{}, timeout)
		if err != nil {
			return err
		}
		return printer.AppEnvironment.One(f.IOStreams, format, item)
	}}
	cmd.Flags().StringVar(&forPhase, "for", "Succeeded", "Release outcome to wait for (Succeeded)")
	cmd.Flags().DurationVar(&timeout, "timeout", 15*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

// WaitForConverged remains the shared poll/cancellation/transient policy. Its
// lifecycle phases are mapped from this pinned release's outcome, never the
// environment's active release or recovery result.
func waitForRelease(ctx context.Context, io *cli.IOStreams, api client.Interface, app, env string, target releaseTarget, timeout time.Duration) (*client.AppEnvironment, error) {
	var outcome error
	get := func(ctx context.Context) (*client.AppEnvironment, string, error) {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		current, _, err := api.AppEnvironments(app).Get(ctx, env)
		if err != nil {
			return nil, "", err
		}
		if target.uid == "" {
			target.uid = current.UID
		}
		if current.UID == "" || current.UID != target.uid {
			outcome = cli.New(cli.ExitGeneral, "environment was replaced while waiting; inspect its releases and start a new wait")
			return current, client.PhaseDegraded, nil
		}
		if current.Status != nil && current.Status.Phase == client.PhaseTerminating {
			return current, client.PhaseTerminating, nil
		}
		if target.generation == 0 && target.name == "" {
			target.name = requestedRelease(current)
		}
		if current.Status == nil || current.Status.ObservedGeneration < target.generation {
			return current, client.PhasePending, nil
		}
		items, err := api.ListAppReleases(ctx, app, env)
		if err != nil {
			return current, "", err
		}
		// The releases endpoint scopes to the environment it looks up. A delete
		// and recreate between our first GET and that lookup must not complete
		// an old wait using the new incarnation's identically named release.
		after, _, err := api.AppEnvironments(app).Get(ctx, env)
		if err != nil {
			return current, "", err
		}
		if after.UID != target.uid {
			outcome = cli.New(cli.ExitGeneral, "environment was replaced while waiting; inspect its releases and start a new wait")
			return after, client.PhaseDegraded, nil
		}
		var requested, pinned *client.AppRelease
		for i := range items {
			if items[i].Name == requestedRelease(after) {
				requested = &items[i]
			}
			if items[i].Name == target.name && target.name != "" {
				pinned = &items[i]
			}
		}
		if target.name == "" && after.Generation > target.generation && target.generation > 0 {
			message := "the environment changed before its release could be identified; check its current state before deploying again"
			if requested != nil && requested.Name != target.previous {
				message += "; current requested release is " + requested.Name
			}
			outcome = cli.New(cli.ExitUnavailable, message)
			return current, client.PhaseDegraded, nil
		}
		if target.name == "" && requested != nil && requested.Name != target.previous && matchesRelease(requested, target) {
			target.name, pinned = requested.Name, requested
		}
		if target.name == "" {
			return current, client.PhasePending, nil
		}
		if pinned != nil && !matchesRelease(pinned, target) {
			outcome = cli.New(cli.ExitGeneral, "release no longer matches the requested deployment; inspect the environment before waiting again")
			return current, client.PhaseDegraded, nil
		}
		phase := ""
		if pinned != nil {
			phase = pinned.Status.Phase
		}
		if phase == "" && current.Status.Deployment != nil && releaseName(current.Status.Deployment.Release) == target.name {
			phase = current.Status.Deployment.Phase
		}
		switch phase {
		case "Succeeded":
			return current, client.PhaseRunning, nil
		case "Failed":
			message := "release " + target.name + " failed"
			recovery := ""
			if pinned != nil && pinned.Status.Recovery != nil {
				recovery = pinned.Status.Recovery.Phase
			}
			if recovery == "" && requestedRelease(current) == target.name && current.Status.Recovery != nil {
				recovery = current.Status.Recovery.Phase
			}
			if recovery == "Succeeded" {
				message += "; the previous release is serving again"
			} else if recovery != "" {
				message += "; recovery: " + recovery
			}
			outcome = cli.Wrap(cli.ExitGeneral, "", &releaseFailedError{message: message, terminal: &cmdutil.TerminalPhaseError{Noun: "release", Name: target.name, Phase: "Failed"}})
			return current, client.PhaseDegraded, nil
		case "Superseded":
			message := "release " + target.name + " was superseded"
			if pinned != nil && requested != nil && requested.Number > pinned.Number {
				message += "; current requested release is " + requested.Name
			} else {
				message += "; the newer release name is not available in retained history"
			}
			outcome = cli.New(cli.ExitGeneral, message)
			return current, client.PhaseDegraded, nil
		}
		return current, client.PhasePending, nil
	}
	converge := cmdutil.Converge[*client.AppEnvironment]{ObservedGeneration: func(e *client.AppEnvironment) int64 {
		if e == nil || e.Status == nil {
			return 0
		}
		return e.Status.ObservedGeneration
	}}
	item, err := cmdutil.WaitForConverged(ctx, io, "release", app+"/"+env, get, converge, 0, timeout)
	resume := "kupe app wait " + app + " " + env
	if outcome != nil {
		return item, outcome
	}
	if errors.Is(err, ux.ErrWaitTimeout) {
		name := target.name
		if name == "" {
			name = "not yet observed"
		}
		return item, cli.TimeoutError(fmt.Sprintf("timed out waiting for release %s", name)).WithHint("resume wait: " + resume)
	}
	if errors.Is(err, context.Canceled) {
		return item, cli.New(cli.ExitInterrupted, "stopped waiting; the deployment may still be in progress").WithHint("resume wait: " + resume)
	}
	var terminal *cmdutil.TerminalPhaseError
	if errors.As(err, &terminal) {
		return item, cli.New(cli.ExitGeneral, "environment is being deleted; release wait stopped")
	}
	if err != nil {
		return item, releaseRequestError(err, app, env, false)
	}
	return item, nil
}

func matchesRelease(release *client.AppRelease, target releaseTarget) bool {
	if target.revision > 0 && release.DeploymentRevision != target.revision {
		return false
	}
	if target.web == nil {
		return true
	}
	if !reflect.DeepEqual(effectiveReleaseWeb(release.Runtime.AppWeb), effectiveReleaseWeb(*target.web)) {
		return false
	}
	paths := map[string]string{}
	for _, secret := range target.secrets {
		paths[secret.Name] = secret.Path
	}
	keys := map[string][]string{}
	for _, v := range target.web.Env {
		if v.ValueFrom != nil {
			ref := v.ValueFrom.SecretKeyRef
			if !slices.Contains(keys[ref.Secret], ref.Key) {
				keys[ref.Secret] = append(keys[ref.Secret], ref.Key)
			}
		}
	}
	if len(keys) != len(release.Runtime.Secrets) {
		return false
	}
	for _, secret := range release.Runtime.Secrets {
		want, exists := keys[secret.Name]
		if !exists || paths[secret.Name] != secret.Path {
			return false
		}
		want, got := slices.Clone(want), slices.Clone(secret.Keys)
		slices.Sort(want)
		slices.Sort(got)
		if !slices.Equal(want, got) {
			return false
		}
	}
	return true
}

// Compare the defaulted workload, including semantic quantities. The operator
// canonicalises quantities and default readiness before recording the runtime.
func effectiveReleaseWeb(web client.AppWeb) client.AppWeb {
	if len(web.Command) == 0 {
		web.Command = nil
	}
	if len(web.Args) == 0 {
		web.Args = nil
	}
	if len(web.Env) == 0 {
		web.Env = nil
	}
	if web.Replicas == nil {
		n := int32(1)
		web.Replicas = &n
	}
	checks := client.AppHealthChecks{}
	if web.HealthChecks != nil {
		checks = *web.HealthChecks
	}
	if checks.Readiness == nil {
		checks.Readiness = &client.AppProbe{TCP: &struct{}{}}
	}
	web.HealthChecks = &checks
	if web.Resources != nil {
		resources := *web.Resources
		for _, quantity := range []*string{&resources.Requests.CPU, &resources.Requests.Memory, &resources.Limits.CPU, &resources.Limits.Memory} {
			if parsed, err := resource.ParseQuantity(*quantity); err == nil {
				*quantity = parsed.String()
			}
		}
		web.Resources = &resources
	}
	return web
}

// releaseFailedError prints only its sentence, so a failed release is not
// reported twice, and keeps the TerminalPhaseError for errors.As.
type releaseFailedError struct {
	message  string
	terminal *cmdutil.TerminalPhaseError
}

func (e *releaseFailedError) Error() string { return e.message }
func (e *releaseFailedError) Unwrap() error { return e.terminal }
