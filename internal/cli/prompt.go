package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// PromptLine uses the cancellable input reader without reading ahead into the
// next answer or the shared confirmation prompt.
func PromptLine(ctx context.Context, streams *IOStreams, label, fallback string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !streams.PromptsEnabled || !streams.IsStdinTTY() || !streams.IsStdoutTTY() {
		return "", MisuseError("input is required: " + label)
	}
	if fallback != "" {
		fmt.Fprintf(streams.ErrOut, "%s [%s]: ", label, fallback)
	} else {
		fmt.Fprintf(streams.ErrOut, "%s: ", label)
	}
	var answer strings.Builder
	for answer.Len() < 4096 {
		next, err := ReadInput(ctx, streams.In, 1)
		if err != nil {
			return "", Wrap(ExitGeneral, "reading input", err)
		}
		if len(next) == 0 {
			return "", Wrap(ExitGeneral, "reading input", io.EOF)
		}
		if next[0] == '\n' {
			value := strings.TrimSpace(answer.String())
			if value == "" {
				value = fallback
			}
			if value == "" {
				return "", MisuseError(label + " is required")
			}
			return value, nil
		}
		answer.WriteByte(next[0])
	}
	return "", MisuseError("answer must be at most 4096 bytes")
}
