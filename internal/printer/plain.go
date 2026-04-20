package printer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"text/template"

	"gopkg.in/yaml.v3"
)

// PrintJSON encodes v with 2-space indent. Stable field order is caller
// responsibility — we use encoding/json which sorts map keys and emits
// struct fields in declaration order.
func PrintJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// PrintYAML marshals v with yaml.v3's defaults (2-space indent, no document
// markers).
func PrintYAML(w io.Writer, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshalling yaml: %w", err)
	}
	_, err = w.Write(data)
	return err
}

// PrintNames writes one name per line, pipe-friendly for `xargs`. items may
// be a single value (one name printed) or a slice (one per element). name
// extracts the name string from each element.
func PrintNames(w io.Writer, items any, name func(any) string) error {
	v := reflect.ValueOf(items)
	if v.Kind() == reflect.Slice {
		for i := 0; i < v.Len(); i++ {
			fmt.Fprintln(w, name(v.Index(i).Interface()))
		}
		return nil
	}
	fmt.Fprintln(w, name(items))
	return nil
}

// PrintTemplate renders v using text/template. Template syntax follows
// Go's text/template stdlib — documented in commands.md.
func PrintTemplate(w io.Writer, v any, tmpl string) error {
	t, err := template.New("output").Parse(tmpl)
	if err != nil {
		return fmt.Errorf("parsing template: %w", err)
	}
	if err := t.Execute(w, v); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}
	fmt.Fprintln(w)
	return nil
}

// PrintTemplateFile reads a template from path and renders v.
func PrintTemplateFile(w io.Writer, v any, path string) error {
	data, err := os.ReadFile(path) //#nosec G304 -- path is a user-provided template file
	if err != nil {
		return fmt.Errorf("reading template file: %w", err)
	}
	return PrintTemplate(w, v, string(data))
}

// PrintJSONPath is a placeholder for Phase 3.x. Returning a structured error
// keeps the -o jsonpath=... flag present on the help text while we decide
// whether to lift k8s.io/client-go/util/jsonpath or ship a lighter impl.
func PrintJSONPath(_ io.Writer, _ any, _ string) error {
	return errors.New("jsonpath output is not yet implemented — use -o go-template=... or -o json piped to jq for now")
}
