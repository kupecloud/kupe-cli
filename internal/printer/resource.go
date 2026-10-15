package printer

import "github.com/kupecloud/kupe-cli/internal/cli"

// Resource is how one kind of resource renders: its list table, its
// key:value detail view and its -o name. Commands render through it, so a
// service declares its columns once (Cluster, Database, DatabaseBinding).
type Resource[T any] struct {
	// Columns are the list table's columns; Wide ones show with -o wide.
	Columns func(colorEnabled bool) Columns
	// DetailColumns are the key:value fields of a single resource.
	DetailColumns func(colorEnabled bool) Columns
	// Name is the identifier -o name prints.
	Name func(*T) string
}

// One writes a single resource to io.Out in format.
func (r Resource[T]) One(io *cli.IOStreams, format *Format, item *T) error {
	return RenderOne(io.Out, format, item, r.DetailColumns(io.ColorEnabled), r.Name)
}

// List writes resources to io.Out in format.
func (r Resource[T]) List(io *cli.IOStreams, format *Format, items []T) error {
	return RenderList(io.Out, format, items, r.Columns(io.ColorEnabled), func(item T) string { return r.Name(&item) })
}

// rowOf asserts a table row into a *T, never nil. Rows are values or
// pointers, so both []T and []*T render; anything else, a nil pointer
// included, is the zero value.
func rowOf[T any](v any) *T {
	switch x := v.(type) {
	case *T:
		if x != nil {
			return x
		}
	case T:
		return &x
	}
	return new(T)
}

// valueOf returns *p, or the zero value when p is nil: a status or
// resources block the API left out.
func valueOf[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// versionDisplay renders a version for list and detail views from the
// version requested, the one running and, mid-upgrade, the one being
// upgraded from:
//
//   - only one of requested and running known: that one ("1.32");
//   - both known and equal: the value;
//   - otherwise: "1.33 (running 1.32)", "18 (running 18.0)",
//     "18 (running 17.6, upgrading from 17.6)".
func versionDisplay(requested, running, previous string) string {
	switch {
	case requested == "":
		return running
	case running == "" || running == requested:
		return requested
	}
	out := requested + " (running " + running
	if previous != "" {
		out += ", upgrading from " + previous
	}
	return out + ")"
}
