package cli

import "testing"

func TestDefaultColorEnabled(t *testing.T) {
	tests := []struct {
		name      string
		stdoutTTY bool
		noColor   string
		term      string
		want      bool
	}{
		{"tty no signals", true, "", "xterm", true},
		{"not tty", false, "", "xterm", false},
		{"NO_COLOR set", true, "1", "xterm", false},
		{"TERM=dumb", true, "", "dumb", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tt.noColor)
			t.Setenv("TERM", tt.term)
			if got := defaultColorEnabled(tt.stdoutTTY); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultSpinnersEnabled(t *testing.T) {
	tests := []struct {
		name       string
		stderrTTY  bool
		ci         string
		noProgress string
		want       bool
	}{
		{"tty no signals", true, "", "", true},
		{"not tty", false, "", "", false},
		{"CI=true", true, "true", "", false},
		{"KUPE_NO_PROGRESS", true, "", "1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CI", tt.ci)
			t.Setenv("KUPE_NO_PROGRESS", tt.noProgress)
			if got := defaultSpinnersEnabled(tt.stderrTTY); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetPromptsEnabledHonoursTTY(t *testing.T) {
	s := &IOStreams{stdinIsTTY: false, stderrIsTTY: false}
	s.SetPromptsEnabled(true)
	if s.PromptsEnabled {
		t.Fatal("PromptsEnabled should stay false when stdin/stderr are not TTY")
	}

	s = &IOStreams{stdinIsTTY: true, stderrIsTTY: true}
	s.SetPromptsEnabled(true)
	if !s.PromptsEnabled {
		t.Fatal("PromptsEnabled should become true when stdin and stderr are TTY")
	}
	s.SetPromptsEnabled(false)
	if s.PromptsEnabled {
		t.Fatal("SetPromptsEnabled(false) must force-disable")
	}
}

func TestTestConstructor(t *testing.T) {
	io, out, errOut := Test()
	if io.ColorEnabled || io.SpinnersEnabled || io.PromptsEnabled {
		t.Fatalf("Test streams should have all features off; got %+v", io)
	}
	if out == nil || errOut == nil {
		t.Fatal("Test should return non-nil buffers")
	}
}
