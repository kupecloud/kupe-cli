package cli

import (
	"bufio"
	"fmt"
	"strings"
)

// ConfirmDelete prompts for a deletion confirmation on a TTY. The user
// must type "yes" to proceed.
//
// skip=true bypasses the prompt entirely (for --yes flags). Non-TTY
// callers without --yes get a MisuseError so CI never hangs on stdin.
//
// noun is the human-friendly description that appears in the prompt,
// e.g., "cluster", "managed secret", "member of the tenant". identifier
// is the value of the resource being deleted; surfaced in the prompt
// so the user sees which thing they're confirming, but no longer
// required as the typed answer.
//
// Keeps every delete command's confirmation logic identical — one place
// to change the wording, accepted answers, or trim rules.
func ConfirmDelete(io *IOStreams, skip bool, noun, identifier string) error {
	return confirm(io, skip, "delete",
		fmt.Sprintf("This will delete %s %q. Type yes to confirm: ", noun, identifier), "yes")
}

// ConfirmDeleteByName is ConfirmDelete for deletes that cannot be undone
// and destroy data: the user must type the resource name itself, so a
// reflexive "yes" can't remove the wrong thing.
func ConfirmDeleteByName(io *IOStreams, skip bool, noun, name string) error {
	return confirm(io, skip, "delete",
		fmt.Sprintf("This will delete %s %q. Type the name to confirm: ", noun, name), name)
}

// ConfirmAction prompts before a disruptive action that is not a delete
// (e.g. "rotate credentials"). The user must type "yes"; action names the
// operation in the non-interactive refusal.
func ConfirmAction(io *IOStreams, skip bool, action, prompt string) error {
	return confirm(io, skip, action, prompt+" Type yes to confirm: ", "yes")
}

// confirm writes prompt to stderr and requires want as the answer
// (trimmed, case-insensitive).
func confirm(io *IOStreams, skip bool, action, prompt, want string) error {
	if skip {
		return nil
	}
	if !io.PromptsEnabled {
		return MisuseError(fmt.Sprintf("refusing to %s without --yes in non-interactive mode", action))
	}
	fmt.Fprint(io.ErrOut, prompt)
	line, err := bufio.NewReader(io.In).ReadString('\n')
	if err != nil {
		return Wrap(ExitGeneral, "reading confirmation", err)
	}
	if !strings.EqualFold(strings.TrimSpace(line), want) {
		return MisuseError("confirmation did not match; aborting")
	}
	return nil
}
