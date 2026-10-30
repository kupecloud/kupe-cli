package secret

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/cmd/cmdutil"
	"github.com/kupecloud/kupe-cli/internal/ux"
	"github.com/spf13/cobra"
)

func secretGetter(api client.Interface, name string) cmdutil.PhaseGetter[*client.Secret] {
	return func(ctx context.Context) (*client.Secret, string, error) {
		s, _, err := api.GetSecret(ctx, name)
		phase := ""
		if s != nil && s.Status != nil {
			phase = s.Status.Phase
		}
		return s, phase, err
	}
}

func secretDelivered(s *client.Secret) bool {
	if s == nil || s.Status == nil || s.Status.ObservedGeneration != s.Generation || s.Status.Phase != "Active" {
		return false
	}
	for _, state := range s.Status.SyncStatuses {
		if state.Phase == "Withdrawing" {
			return false
		}
	}
	for _, target := range s.Sync {
		found := false
		for _, state := range s.Status.SyncStatuses {
			if state.Cluster == target.Cluster && state.Namespace == target.Namespace && state.Phase == "Synced" {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func secretTerminal(s *client.Secret) error {
	if s == nil || s.Status == nil {
		return nil
	}
	if s.Status.Phase == "Deleting" {
		return &cmdutil.TerminalPhaseError{Noun: "secret", Name: s.Name, Phase: "Deleting"}
	}
	for _, target := range s.Sync {
		for _, state := range s.Status.SyncStatuses {
			if state.Cluster == target.Cluster && state.Namespace == target.Namespace && state.Phase == "Failed" && state.Reason == "TargetConflict" {
				return &cmdutil.TerminalPhaseError{Noun: fmt.Sprintf("secret %q target", s.Name), Name: target.Cluster + "/" + target.Namespace, Phase: "Failed (TargetConflict)"}
			}
		}
	}
	return nil
}

func waitForDelivery(ctx context.Context, streams *cli.IOStreams, api client.Interface, name string, timeout time.Duration) (*client.Secret, error) {
	var latest *client.Secret
	poll := cmdutil.PollPhase(secretGetter(api, name), func(s *client.Secret, _ string) (bool, error) {
		latest = s
		if err := secretTerminal(s); err != nil {
			return false, err
		}
		return secretDelivered(s), nil
	})
	err := ux.WaitFor(ctx, streams, ux.WaitForOpts{Label: "secret " + name, DoneVerb: "delivered", Poll: poll, Timeout: timeout})
	return latest, err
}

func secretWaitErr(err error, name string, deleting bool) error {
	check := "check status:  kupe secret get " + name
	if errors.Is(err, ux.ErrWaitTimeout) {
		message, hint := fmt.Sprintf("secret %q is not delivered yet", name), "the change was accepted and is still being delivered; the platform keeps trying"
		if deleting {
			message, hint = fmt.Sprintf("secret %q is not deleted yet", name), "the delete was accepted and the platform keeps removing copies"
		}
		return cli.TimeoutError(message).WithHint(hint + "\n" + check)
	}
	var terminal *cmdutil.TerminalPhaseError
	if errors.As(err, &terminal) && terminal.Phase == "Failed (TargetConflict)" {
		return cli.Wrap(cli.ExitGeneral, terminal.Error(), err).WithHint("A secret with this name already exists in the namespace and was not created by Kupe. Choose another name with --sync CLUSTER:NAMESPACE:SECRET_NAME." + "\n" + check)
	}
	verb := "wait"
	if deleting {
		verb = "delete"
	}
	return cmdutil.MapWaitErr(err, "secret", name, verb)
}

func newWaitCmd(f *cli.Factory) *cobra.Command {
	var forState string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use: "wait NAME", Short: "Wait for a secret to be delivered or deleted", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := strings.ToLower(forState)
			if target != "delivered" && target != "deleted" {
				return cli.MisuseError("--for must be delivered or deleted")
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			name := args[0]
			if target == "deleted" {
				err = cmdutil.WaitForGone(cmd.Context(), f.IOStreams, "secret", name, secretGetter(api, name), timeout)
			} else {
				_, err = waitForDelivery(cmd.Context(), f.IOStreams, api, name, timeout)
			}
			if err != nil {
				return secretWaitErr(err, name, target == "deleted")
			}
			fmt.Fprintf(f.IOStreams.Out, "secret/%s %s\n", name, target)
			return nil
		},
	}
	cmd.Flags().StringVar(&forState, "for", "delivered", "Wait for delivered or deleted")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Give up waiting after this long")
	return cmd
}
