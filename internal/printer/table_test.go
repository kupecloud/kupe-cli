package printer

import (
	"bytes"
	"strings"
	"testing"
)

type row struct {
	Name string
	Kind string
}

func rowCols() Columns {
	return Columns{
		{Name: "NAME", Get: func(v any) string { return v.(row).Name }},
		{Name: "KIND", Get: func(v any) string { return v.(row).Kind }},
		{Name: "ID", Wide: true, Get: func(_ any) string { return "wide-only" }},
	}
}

func TestPrintTableNarrowVsWide(t *testing.T) {
	var buf bytes.Buffer
	items := []row{{"a", "x"}, {"b", "y"}}
	if err := PrintTable(&buf, items, rowCols(), false); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if strings.Contains(got, "ID") {
		t.Fatalf("narrow view leaked wide column:\n%s", got)
	}
	if !strings.Contains(got, "NAME") || !strings.Contains(got, "a") {
		t.Fatalf("missing header or row:\n%s", got)
	}

	buf.Reset()
	if err := PrintTable(&buf, items, rowCols(), true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "ID") {
		t.Fatalf("wide view missing wide column:\n%s", buf.String())
	}
}

func TestPrintTableEmptyRendersJustHeader(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintTable(&buf, []row{}, rowCols(), false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "NAME") {
		t.Fatalf("expected header-only output, got:\n%s", buf.String())
	}
}

// TestPrintTableAlignsWithANSIColoredCells reproduces the cluster-list
// alignment regression where ANSI escape codes from lipgloss inflated the
// PHASE column width and pushed CPU/MEM/AGE under the wrong header.
// Verifies that with the StripEscape path, the visible column header and
// the visible cell start in the same column.
func TestPrintTableAlignsWithANSIColoredCells(t *testing.T) {
	cols := Columns{
		{Name: "NAME", Get: func(v any) string { return v.(row).Name }},
		{Name: "PHASE", Get: func(_ any) string {
			// 7-char visible content wrapped in green + reset escapes.
			return "\x1b[32mRunning\x1b[0m"
		}},
		{Name: "CPU", Get: func(_ any) string { return "2" }},
	}
	var buf bytes.Buffer
	if err := PrintTable(&buf, []row{{Name: "demo"}}, cols, false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), out)
	}
	header, data := lines[0], lines[1]

	cpuHeader := strings.Index(header, "CPU")
	if cpuHeader < 0 {
		t.Fatalf("CPU header missing from %q", header)
	}
	// Visible CPU value is "2" — strip ANSI from the data line and look up
	// where it lands. With ANSI bracketed by 0xff, StripEscape removes the
	// markers but the original \x1b[…m bytes remain. We strip them here for
	// the position assertion only.
	visible := stripANSIForTest(data)
	cpuValue := strings.LastIndex(visible, "2")
	if cpuValue != cpuHeader {
		t.Fatalf("CPU column misaligned: header at %d, value at %d\nheader: %q\ndata:   %q", cpuHeader, cpuValue, header, visible)
	}
}

func stripANSIForTest(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] == ';' || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			if j < len(s) {
				j++ // skip terminating letter
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func TestPrintDetails(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintDetails(&buf, row{"x", "y"}, rowCols()); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "NAME:") || !strings.Contains(got, "x") {
		t.Fatalf("details missing field:\n%s", got)
	}
	if strings.Contains(got, "ID:") {
		t.Fatalf("details view leaked wide column:\n%s", got)
	}
}
