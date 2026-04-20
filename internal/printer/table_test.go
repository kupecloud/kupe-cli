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
