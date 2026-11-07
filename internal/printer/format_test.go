package printer

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		kind Kind
		tmpl string
		ok   bool
	}{
		{"", Table, "", true},
		{"table", Table, "", true},
		{"wide", Wide, "", true},
		{"json", JSON, "", true},
		{"yaml", YAML, "", true},
		{"name", Name, "", true},
		{"go-template=hello", Template, "hello", true},
		{"unknown", 0, "", false},
		{"jsonpath", 0, "", false},
		{"jsonpath=.items[*].name", 0, "", false},
		{"jsonpath-file=/tmp/x", 0, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := Parse(tt.in)
			if tt.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.ok {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if got.Kind != tt.kind {
				t.Errorf("kind = %v; want %v", got.Kind, tt.kind)
			}
			if got.Template != tt.tmpl {
				t.Errorf("template = %q; want %q", got.Template, tt.tmpl)
			}
		})
	}
}

func TestParseRefusesJSONPathWithGuidance(t *testing.T) {
	for _, in := range []string{"jsonpath", "jsonpath={.web.image}", "jsonpath-file=x"} {
		_, err := Parse(in)
		if err == nil || !strings.Contains(err.Error(), "-o jsonpath is not supported") || !strings.Contains(err.Error(), "go-template") {
			t.Errorf("%q: %v", in, err)
		}
	}
}
