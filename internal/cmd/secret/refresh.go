package secret

import (
	"context"
	"fmt"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/printer"
	"github.com/kupecloud/kupe-cli/internal/ux"
	"github.com/spf13/cobra"
)

type refreshResult struct {
	Secret      string `json:"secret" yaml:"secret"`
	RequestedAt string `json:"requestedAt" yaml:"requestedAt"`
	Delivered   bool   `json:"delivered" yaml:"delivered"`
}

func refreshDelivered(s *client.Secret, token string) bool {
	if s == nil || s.Status == nil || s.Status.Refresh == nil || s.Status.Refresh.ObservedAt != token {
		return false
	}
	if len(s.Sync) == 0 {
		return true
	}
	for _, target := range s.Sync {
		found := false
		for _, state := range s.Status.SyncStatuses {
			if state.Cluster == target.Cluster && state.Namespace == target.Namespace && state.Phase == "Synced" && state.DeliveredRefresh == token {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, state := range s.Status.SyncStatuses {
		if state.Phase != "Withdrawing" && (state.Phase != "Synced" || state.DeliveredRefresh != token) {
			return false
		}
	}
	return true
}

func waitForRefresh(ctx context.Context, streams *cli.IOStreams, api client.Interface, result *refreshResult, timeout time.Duration) (bool, error) {
	if result.RequestedAt == "" {
		return false, nil
	}
	get := secretGetter(api, result.Secret)
	first, _, err := get(ctx)
	if err != nil && !cmdutil.IsTransientWaitErr(err) {
		return true, err
	}
	if err == nil {
		if terminal := secretTerminal(first); terminal != nil {
			return true, terminal
		}
	}
	if err == nil && (first == nil || first.Status == nil || first.Status.Refresh == nil) {
		return false, nil
	}
	poll := cmdutil.PollPhase(get, func(s *client.Secret, _ string) (bool, error) {
		if err := secretTerminal(s); err != nil {
			return false, err
		}
		if s == nil || s.Status == nil || s.Status.Refresh == nil {
			return false, nil
		}
		requested := s.Status.Refresh.RequestedAt
		if requested != "" && requested != result.RequestedAt {
			fmt.Fprintf(streams.ErrOut, "A newer refresh (%s) replaced this one; waiting for it instead.\n", requested)
			result.RequestedAt = requested
		}
		return refreshDelivered(s, result.RequestedAt), nil
	})
	err = ux.WaitFor(ctx, streams, ux.WaitForOpts{Label: "secret " + result.Secret, DoneVerb: "refreshed", Poll: poll, Timeout: timeout})
	return true, err
}

func newRefreshCmd(f *cli.Factory) *cobra.Command {
	var wait bool
	var timeout time.Duration
	var output string
	cmd := &cobra.Command{Use: "refresh NAME", Short: "Refresh a secret's stored values in its target namespaces",
		Long: `Request the current values from Vault and wait until they reach the target namespaces.
Pass --wait=false to return once the refresh is requested. If another refresh
replaces this one, the wait follows it. No stored values are printed.`, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			name := args[0]
			token, err := api.RefreshSecret(cmd.Context(), name)
			if err != nil {
				if client.IsConflict(err) && client.ErrorCode(err) == "terminating" {
					return cmdutil.MapWaitErr(&cmdutil.TerminalPhaseError{Noun: "secret", Name: name, Phase: "Deleting"}, "secret", name, "wait")
				}
				return err
			}
			result := refreshResult{Secret: name, RequestedAt: token}
			if wait {
				observable, werr := waitForRefresh(cmd.Context(), f.IOStreams, api, &result, timeout)
				if werr != nil {
					return secretWaitErr(werr, name, false)
				}
				result.Delivered = observable
			}
			if format.Kind != printer.Table && format.Kind != printer.Wide {
				return printer.RenderOne(f.IOStreams.Out, format, &result, nil, func(r *refreshResult) string { return r.Secret })
			}
			if result.Delivered {
				fmt.Fprintf(f.IOStreams.Out, "secret/%s refreshed\n", name)
			} else {
				fmt.Fprintf(f.IOStreams.Out, "secret/%s refresh requested\n", name)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&wait, "wait", true, "Wait until the refresh is delivered before returning")
	cmd.Flags().DurationVar(&timeout, "wait-timeout", 5*time.Minute, "Give up waiting after this long")
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}
