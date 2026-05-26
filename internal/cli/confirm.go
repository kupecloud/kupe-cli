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
	if skip {
		return nil
	}
	if !io.PromptsEnabled {
		return MisuseError("refusing to delete without --yes in non-interactive mode")
	}
	fmt.Fprintf(io.ErrOut, "This will delete %s %q. Type yes to confirm: ", noun, identifier)
	line, err := bufio.NewReader(io.In).ReadString('\n')
	if err != nil {
		return Wrap(ExitGeneral, "reading confirmation", err)
	}
	if strings.ToLower(strings.TrimSpace(line)) != "yes" {
		return MisuseError("confirmation did not match; aborting")
	}
	return nil
}
