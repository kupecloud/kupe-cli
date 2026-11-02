package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
)

func selectEnvironment(ctx context.Context, api client.Interface, args []string) (string, string, error) {
	app := args[0]
	if err := validateName(app, "app", 32); err != nil {
		return "", "", err
	}
	if len(args) > 1 {
		if err := validateName(args[1], "environment", 20); err != nil {
			return "", "", err
		}
		return app, args[1], nil
	}
	items, err := api.AppEnvironments(app).List(ctx)
	if err != nil {
		return "", "", releaseRequestError(err, app, "", false)
	}
	if len(items) == 1 {
		return app, items[0].Environment, nil
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Environment)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "", "", cli.MisuseError("app has no environments; create one with kupe app env create")
	}
	return "", "", cli.MisuseError("choose an environment: " + strings.Join(names, ", "))
}

func releaseName(ref *client.AppReleaseReference) string {
	if ref == nil {
		return ""
	}
	return ref.Name
}
func requestedRelease(env *client.AppEnvironment) string {
	if env == nil || env.Status == nil {
		return ""
	}
	return releaseName(env.Status.RequestedRelease)
}
func deploymentRevision(env *client.AppEnvironment) int64 {
	if env.DeploymentRevision == 0 {
		return 1
	}
	return env.DeploymentRevision
}

// Reviews describe configuration without echoing literal variable values.
func reviewRelease(io *cli.IOStreams, action, app, env, source string, current *client.AppEnvironment, web *client.AppWeb, changed []string) {
	fmt.Fprintf(io.ErrOut, "%s %s/%s (generation %d, deployment revision %d)\n", action, app, env, current.Generation, deploymentRevision(current))
	if web == nil {
		return
	}
	if source != "" && source != web.Image {
		fmt.Fprintf(io.ErrOut, "  image: %s → %s\n", source, web.Image)
	} else {
		fmt.Fprintf(io.ErrOut, "  image: %s\n", web.Image)
	}
	replicas := int32(1)
	if web.Replicas != nil {
		replicas = *web.Replicas
	}
	fmt.Fprintf(io.ErrOut, "  port: %d; replicas: %d\n", web.Port, replicas)
	if web.Resources != nil {
		fmt.Fprintf(io.ErrOut, "  requests: %s CPU, %s memory; limits: %s CPU, %s memory\n", web.Resources.Requests.CPU, web.Resources.Requests.Memory, web.Resources.Limits.CPU, web.Resources.Limits.Memory)
	}
	var names []string
	for _, v := range web.Env {
		name := v.Name
		if v.ValueFrom != nil {
			name += " (secret reference)"
		}
		names = append(names, name)
	}
	if len(names) > 0 {
		fmt.Fprintln(io.ErrOut, "  variables: "+strings.Join(names, ", "))
	}
	if len(changed) > 0 {
		fmt.Fprintln(io.ErrOut, "  changed fields: "+strings.Join(changed, ", "))
	}
}

func staleReviewError() error {
	return cli.ConflictError("environment changed since the review; review the current configuration above and run the command again")
}

// A transport error can contain submitted values or credential-source output.
// Keep its identity for classification, but expose only a safe message.
type safeReleaseCause struct{ cause error }

func (e *safeReleaseCause) Error() string {
	var api *client.APIError
	if errors.As(e.cause, &api) && api.RequestID != "" {
		return "request failed (request-id: " + api.RequestID + ")"
	}
	return "request failed"
}
func (e *safeReleaseCause) Unwrap() error { return e.cause }

func releaseRequestError(err error, app, env string, write bool) error {
	if err == nil {
		return nil
	}
	var api *client.APIError
	if errors.As(err, &api) {
		code := cli.ExitCode(err)
		message := http.StatusText(api.StatusCode)
		switch client.ErrorCode(err) {
		case "apps_unavailable":
			return translateErr(err, app, env)
		case client.CodeTerminating:
			message = "environment is being deleted"
		case client.CodeTenantTerminating:
			message = "tenant is being deleted"
		case client.CodeTenantSuspended:
			message = "tenant is suspended"
		case client.CodePaymentSetupRequired:
			message = "set up a payment method before deploying"
		case "registries_unavailable":
			code, message = cli.ExitUnavailable, "registry is not available on this platform"
		}
		if api.StatusCode >= 500 {
			code, message = cli.ExitUnavailable, "kupe-api is temporarily unavailable"
			if write {
				message = "request outcome is unknown; inspect the environment and releases before retrying"
			}
		}
		if api.StatusCode == http.StatusBadRequest && api.Field == "deploymentRevision" {
			message = "the environment cannot be retried again; change its configuration to deploy"
		}
		return cli.Wrap(code, message, &safeReleaseCause{cause: err})
	}
	code, message := cli.ExitUnavailable, "could not reach kupe-api"
	var setup *client.RequestNotSentError
	switch {
	case errors.As(err, &setup):
		message = "request was not sent"
		if setup.TokenResolution {
			code, message = cli.ExitAuth, "could not resolve credentials; request was not sent"
		}
	case write:
		message = "request outcome is unknown; inspect the environment and releases before retrying"
	}
	if errors.Is(err, context.Canceled) {
		code = cli.ExitInterrupted
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code = cli.ExitTimeout
	}
	result := cli.Wrap(code, message, &safeReleaseCause{cause: err})
	if app != "" && env != "" {
		return result.WithHint("kupe app env get " + app + " " + env + "\nkupe app releases " + app + " " + env)
	}
	return result
}
