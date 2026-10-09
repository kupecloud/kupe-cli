package cli

import (
	"strings"
	"testing"
)

func TestConfirm(t *testing.T) {
	tests := []struct {
		name     string
		confirm  func(*IOStreams) error
		prompts  bool
		answer   string
		wantExit int
		wantText string
	}{
		{"skip bypasses the prompt", func(io *IOStreams) error { return ConfirmDeleteByName(io, true, "database", "orders-db") }, false, "", ExitOK, ""},
		{"non-interactive delete is refused", func(io *IOStreams) error { return ConfirmDelete(io, false, "cluster", "prod") }, false, "", ExitMisuse, "refusing to delete without --yes"},
		{"non-interactive action names the action", func(io *IOStreams) error { return ConfirmAction(io, false, "rotate credentials", "Rotate?") }, false, "", ExitMisuse, "refusing to rotate credentials without --yes"},
		{"delete accepts yes", func(io *IOStreams) error { return ConfirmDelete(io, false, "cluster", "prod") }, true, "YES\n", ExitOK, ""},
		{"delete by name accepts the name", func(io *IOStreams) error { return ConfirmDeleteByName(io, false, "database", "orders-db") }, true, " orders-db \n", ExitOK, ""},
		{"delete by name rejects yes", func(io *IOStreams) error { return ConfirmDeleteByName(io, false, "database", "orders-db") }, true, "yes\n", ExitMisuse, "did not match"},
		{"action accepts yes", func(io *IOStreams) error { return ConfirmAction(io, false, "rotate credentials", "Rotate?") }, true, "yes\n", ExitOK, ""},
		{"action rejects anything else", func(io *IOStreams) error { return ConfirmAction(io, false, "rotate credentials", "Rotate?") }, true, "no\n", ExitMisuse, "did not match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			io, _, _ := Test()
			io.PromptsEnabled = tt.prompts
			io.In = strings.NewReader(tt.answer)
			err := tt.confirm(io)
			if code := ExitCode(err); code != tt.wantExit {
				t.Fatalf("exit = %d; want %d (%v)", code, tt.wantExit, err)
			}
			if tt.wantText != "" && !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("err = %v; want containing %q", err, tt.wantText)
			}
		})
	}
}

func TestConfirmDeleteByName_PromptAsksForTheName(t *testing.T) {
	io, _, errOut := Test()
	io.PromptsEnabled = true
	io.In = strings.NewReader("orders-db\n")
	if err := ConfirmDeleteByName(io, false, "database", "orders-db"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), `delete database "orders-db". Type the name to confirm`) {
		t.Fatalf("prompt = %q", errOut.String())
	}
}
