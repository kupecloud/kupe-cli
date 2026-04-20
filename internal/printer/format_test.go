package printer

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		kind Kind
		tmpl string
		path string
		ok   bool
	}{
		{"", Table, "", "", true},
		{"table", Table, "", "", true},
		{"wide", Wide, "", "", true},
		{"json", JSON, "", "", true},
		{"yaml", YAML, "", "", true},
		{"name", Name, "", "", true},
		{"go-template=hello", Template, "hello", "", true},
		{"jsonpath=.items[*].name", JSONPath, "", ".items[*].name", true},
		{"unknown", 0, "", "", false},
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
			if got.Path != tt.path {
				t.Errorf("path = %q; want %q", got.Path, tt.path)
			}
		})
	}
}
