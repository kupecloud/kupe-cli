package cli

import (
	"bufio"
	"fmt"
	"strings"
)

// ConfirmDelete prompts for a deletion confirmation on a TTY. The user
// must type the exact identifier (cluster name, secret name, email, api
// key id) to proceed.
//
// skip=true bypasses the prompt entirely (for --yes flags). Non-TTY
// callers without --yes get a MisuseError so CI never hangs on stdin.
//
// noun is the human-friendly description that appears in the prompt,
// e.g., "cluster", "managed secret", "member of the tenant". identifier
// is the value the user must echo back.
//
// Keeps every delete command's confirmation logic identical — one place
// to change the wording, hint text, or trim rules.
func ConfirmDelete(io *IOStreams, skip bool, noun, identifier string) error {
	if skip {
		return nil
	}
	if !io.PromptsEnabled {
		return MisuseError("refusing to delete without --yes in non-interactive mode")
	}
	fmt.Fprintf(io.ErrOut, "This will delete %s %q. Type the identifier to confirm: ", noun, identifier)
	line, err := bufio.NewReader(io.In).ReadString('\n')
	if err != nil {
		return Wrap(ExitGeneral, "reading confirmation", err)
	}
	if strings.TrimRight(line, "\r\n") != identifier {
		return MisuseError("confirmation did not match; aborting")
	}
	return nil
}
