package cmdutil

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/kupecloud/kupe-cli/internal/cli"
	"github.com/kupecloud/kupe-cli/internal/client"
	"github.com/kupecloud/kupe-cli/internal/printer"
)

// Service describes one managed service (clusters, databases) for what
// every service's commands share. T is the resource, C and P its create and
// patch bodies. The list, get, wait and delete commands are built from it
// (ListCmd, GetCmd, WaitCmd, DeleteCmd), each with the service's own help
// text; create, update and anything else stay hand-written, on the same
// pieces (Getter, WaitForPhase, WaitForConverged, Translate, Printer). A
// service may extend a built command or write its own instead.
type Service[T, C, P any] struct {
	// Noun is the command noun ("cluster", "database"): progress labels,
	// hints ("kupe NOUN get NAME") and "NOUN/NAME ..." lines.
	Noun string
	// API selects the service's collection, e.g. client.Interface.Clusters.
	API func(client.Interface) client.ResourceAPI[T, C, P]
	// Phase reads a resource's lifecycle phase, "" while it has no status.
	Phase func(*T) string
	// Converge says when a spec change has taken effect (WaitForConverged).
	Converge Converge[*T]
	// Printer renders the resource.
	Printer printer.Resource[T]
	// Translate maps the service's refusals onto CLI errors; name is the
	// resource acted on, "" when none. Nil means TranslateCommon alone.
	Translate func(err error, name string) error
}

// Help is a built command's text. Each service writes its own.
type Help struct {
	Short, Long, Example string
}

// translate applies the service's translator, or the shared one.
func (s Service[T, C, P]) translate(err error, name string) error {
	if s.Translate != nil {
		return s.Translate(err, name)
	}
	return TranslateCommon(s.Noun, name, err)
}

// TranslateDelete adds the refusal every delete can get to the service's
// own: a resource that kept changing while kupe-api tried to delete it (a
// 409 with no code, after the API's own retries).
func (s Service[T, C, P]) TranslateDelete(err error, name string) error {
	if client.IsConflict(err) && client.ErrorCode(err) == "" {
		return cli.WrapAPI(cli.ExitConflict, fmt.Sprintf("%s %q changed while it was being deleted", s.Noun, name), err).
			WithHint(fmt.Sprintf("retry:  kupe %s delete %s", s.Noun, name))
	}
	return s.translate(err, name)
}

// Getter fetches name for the wait helpers, its errors translated.
func (s Service[T, C, P]) Getter(api client.Interface, name string) PhaseGetter[*T] {
	return func(ctx context.Context) (*T, string, error) {
		item, _, err := s.API(api).Get(ctx, name)
		if err != nil {
			return nil, "", s.translate(err, name)
		}
		return item, s.Phase(item), nil
	}
}

// WaitForPhase waits for name to reach target (see the package function).
func (s Service[T, C, P]) WaitForPhase(ctx context.Context, streams *cli.IOStreams, api client.Interface, name, target string, timeout time.Duration) (*T, error) {
	return WaitForPhase(ctx, streams, s.Noun, name, target, s.Getter(api, name), timeout)
}

// WaitForConverged waits for the change at generation to take effect on
// name, by the service's Converge (see the package function).
func (s Service[T, C, P]) WaitForConverged(ctx context.Context, streams *cli.IOStreams, api client.Interface, name string, generation int64, timeout time.Duration) (*T, error) {
	return WaitForConverged(ctx, streams, s.Noun, name, s.Getter(api, name), s.Converge, generation, timeout)
}

// ListCmd builds `list`: every resource of the service, -o and a
// client-side --phase filter.
func (s Service[T, C, P]) ListCmd(f *cli.Factory, help Help) *cobra.Command {
	var output, phase string
	cmd := &cobra.Command{
		Use:     "list",
		Short:   help.Short,
		Long:    help.Long,
		Example: help.Example,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Normalised so --phase is case-insensitive and takes the same
			// spellings as `wait --for`. An unknown value is a misuse, not a
			// silently empty result.
			want := ""
			if phase != "" {
				want = NormalizePhase(phase)
				if want == "" || want == PhaseDeleted {
					return cli.MisuseError(fmt.Sprintf("unknown --phase %q", phase)).
						WithHint("valid phases: pending | provisioning | running | upgrading | degraded | terminating")
				}
			}
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			items, err := s.API(api).List(cmd.Context())
			if err != nil {
				return s.translate(err, "")
			}
			if want != "" {
				kept := items[:0]
				for i := range items {
					if s.Phase(&items[i]) == want {
						kept = append(kept, items[i])
					}
				}
				items = kept
			}
			return s.Printer.List(f.IOStreams, format, items)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpList)
	cmd.Flags().StringVar(&phase, "phase", "", fmt.Sprintf("Filter to %ss in this phase (client-side)", s.Noun))
	return cmd
}

// GetCmd builds `get NAME`.
func (s Service[T, C, P]) GetCmd(f *cli.Factory, help Help) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:     "get NAME",
		Short:   help.Short,
		Long:    help.Long,
		Example: help.Example,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := printer.Resolve(f, output)
			if err != nil {
				return err
			}
			api, err := f.Client()
			if err != nil {
				return err
			}
			item, _, err := s.API(api).Get(cmd.Context(), args[0])
			if err != nil {
				return s.translate(err, args[0])
			}
			return s.Printer.One(f.IOStreams, format, item)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", printer.OutputHelpGet)
	return cmd
}

// WaitCmd builds `wait NAME --for PHASE`.
func (s Service[T, C, P]) WaitCmd(f *cli.Factory, help Help) *cobra.Command {
	var forPhase string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:     "wait NAME",
		Short:   help.Short,
		Long:    help.Long,
		Example: help.Example,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			api, err := f.Client()
			if err != nil {
				return err
			}
			return RunWait(cmd.Context(), f.IOStreams, s.Noun, args[0], forPhase, s.Getter(api, args[0]), timeout)
		},
	}
	cmd.Flags().StringVar(&forPhase, "for", "running", "Target phase to wait for: running | pending | provisioning | upgrading | degraded | terminating | deleted")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Give up after this long")
	return cmd
}

// Delete is what a service's `delete NAME` adds to the shared flow.
type Delete struct {
	Help
	// Warning, when set, writes what deleting means to stderr before the
	// prompt. --yes skips it with the prompt: automation has decided.
	Warning func(io *cli.IOStreams, name string)
	// ConfirmByName makes the prompt ask for the name to be typed, not y/N.
	ConfirmByName bool
	// Translate maps the delete's refusals; nil means TranslateDelete. A
	// service with refusals of its own handles them and passes the rest to
	// TranslateDelete.
	Translate func(err error, name string) error
	// WaitTimeout is the --wait-timeout default.
	WaitTimeout time.Duration
}

// DeleteCmd builds `delete NAME`: warning, confirmation, DELETE, then by
// default a wait until the resource is gone (a plain 404), whose success
// line is the renderer's "NOUN NAME deleted". --wait=false prints
// "NOUN/NAME delete requested" instead.
func (s Service[T, C, P]) DeleteCmd(f *cli.Factory, opts Delete) *cobra.Command {
	var yes bool
	wait := true
	waitTimeout := opts.WaitTimeout
	translate := opts.Translate
	if translate == nil {
		translate = s.TranslateDelete
	}
	cmd := &cobra.Command{
		Use:     "delete NAME",
		Short:   opts.Short,
		Long:    opts.Long,
		Example: opts.Example,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			io := f.IOStreams
			api, err := f.Client()
			if err != nil {
				return err
			}
			if !yes && opts.Warning != nil {
				opts.Warning(io, name)
			}
			confirm := cli.ConfirmDelete
			if opts.ConfirmByName {
				confirm = cli.ConfirmDeleteByName
			}
			if err := confirm(io, yes, s.Noun, name); err != nil {
				return err
			}
			if err := s.API(api).Delete(cmd.Context(), name); err != nil {
				return translate(err, name)
			}
			if !wait {
				fmt.Fprintf(io.Out, "%s/%s delete requested\n", s.Noun, name)
				return nil
			}
			if err := WaitForGone(cmd.Context(), io, s.Noun, name, s.Getter(api, name), waitTimeout); err != nil {
				return MapWaitErr(err, s.Noun, name, "delete")
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	cmd.Flags().BoolVar(&wait, "wait", true, fmt.Sprintf("Wait for the %s to disappear before returning", s.Noun))
	cmd.Flags().DurationVar(&waitTimeout, "wait-timeout", opts.WaitTimeout, "Give up after this long")
	return cmd
}
