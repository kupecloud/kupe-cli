// Package printer handles rendering resources to stdout in the formats the
// -o flag supports (table, wide, json, yaml, name, go-template). Commands
// parse the user's -o value once via Parse, then dispatch to the matching
// helper.
//
// jsonpath is a planned Phase 3.x add-on — for now Parse recognises the
// flag value so help text stays accurate, and PrintJSONPath returns a
// "not yet implemented" error directing users to go-template or json+jq.
package printer

import (
	"fmt"
	"strings"

	"github.com/kupecloud/kupe-cli/internal/cli"
)

// Kind identifies an output format.
type Kind int

// Kind values. The zero value is Table (the default TTY format).
const (
	Table Kind = iota
	Wide
	JSON
	YAML
	Name
	Template
	JSONPath
)

// Format is the parsed -o value.
type Format struct {
	Kind Kind
	// Template holds the template string for Kind=Template (source from
	// `go-template=...`).
	Template string
	// Path holds the jsonpath expression for Kind=JSONPath.
	Path string
}

// MustParse wraps Parse, converting any error into a cli.MisuseError
// (exit 2). Lets command bodies write:
//
//	format, err := printer.MustParse(output)
//	if err != nil { return err }
//
// instead of duplicating the same MisuseError wrap across packages.
func MustParse(flag string) (*Format, error) {
	f, err := Parse(flag)
	if err != nil {
		return nil, cli.MisuseError(err.Error())
	}
	return f, nil
}

// Resolve picks the effective output format for a command: the user's
// raw -o value if set, otherwise the preferences default from the
// factory. Returns a parsed Format ready for the render dispatchers,
// or a cli.MisuseError on an unrecognised value. Replaces a copy of
// the same six-line helper that previously lived in every cmd/<noun>
// package.
func Resolve(f *cli.Factory, raw string) (*Format, error) {
	if raw == "" {
		raw = f.DefaultOutput()
	}
	return MustParse(raw)
}

// Output flag descriptions used in --help. Keep these as the single
// source of truth so every command surfaces the same wording for the
// same set of supported formats. Pick the constant that matches the
// command's actual capability:
//
//	OutputHelpList   — list commands (table, wide, json, yaml, name, ...)
//	OutputHelpGet    — single-resource get commands (table, json, yaml, ...)
//	OutputHelpToggle — commands with text-or-json toggle (whoami, get-token-style)
//
// jsonpath= is intentionally absent here: PrintJSONPath is still a
// placeholder, so advertising it in --help would promise a format that always
// errors (KC-17). Parse still recognises jsonpath= so users who try it get a
// friendly "not yet implemented" message rather than "unsupported format".
const (
	OutputHelpList   = "Output format: table | wide | json | yaml | name | go-template=..."
	OutputHelpGet    = "Output format: table | json | yaml | go-template=..."
	OutputHelpToggle = "Output format: text (default) or json"
)

// Parse converts the raw -o flag value into a Format. Empty string → Table.
// Returns an error the command can surface to the user for unrecognised values.
func Parse(flag string) (*Format, error) {
	switch flag {
	case "", "table":
		return &Format{Kind: Table}, nil
	case "wide":
		return &Format{Kind: Wide}, nil
	case "json":
		return &Format{Kind: JSON}, nil
	case "yaml":
		return &Format{Kind: YAML}, nil
	case "name":
		return &Format{Kind: Name}, nil
	}
	if tpl, ok := strings.CutPrefix(flag, "go-template="); ok {
		return &Format{Kind: Template, Template: tpl}, nil
	}
	if path, ok := strings.CutPrefix(flag, "jsonpath="); ok {
		return &Format{Kind: JSONPath, Path: path}, nil
	}
	return nil, fmt.Errorf("unsupported output format %q (expected one of: table, wide, json, yaml, name, go-template=..., jsonpath=...)", flag)
}
