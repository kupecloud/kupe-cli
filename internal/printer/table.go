package printer

import (
	"fmt"
	"io"
	"reflect"
	"text/tabwriter"
)

// Column is one column in a per-resource table spec. Get receives each row's
// value (the element type of the slice passed to PrintTable) and returns the
// already-stringified cell. For coloured output the Get function applies any
// styling via lipgloss before returning — the table printer doesn't know
// about colour.
type Column struct {
	Name string
	Wide bool // only rendered when PrintTable is called with wide=true
	Get  func(any) string
}

// Columns is a list of columns. Filter returns the subset visible at the
// requested width.
type Columns []Column

// Filter returns the columns visible in the requested view.
func (cs Columns) Filter(wide bool) Columns {
	out := make(Columns, 0, len(cs))
	for _, c := range cs {
		if c.Wide && !wide {
			continue
		}
		out = append(out, c)
	}
	return out
}

// PrintTable renders items — which must be a slice — as a column-aligned
// table with the given columns. Header row is always rendered; zero items
// produces just the header (kubectl convention).
func PrintTable(w io.Writer, items any, cols Columns, wide bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer tw.Flush() //nolint:errcheck // stdout writes can't meaningfully fail

	active := cols.Filter(wide)
	for i, c := range active {
		if i > 0 {
			fmt.Fprint(tw, "\t")
		}
		fmt.Fprint(tw, c.Name)
	}
	fmt.Fprintln(tw)

	v := reflect.ValueOf(items)
	if v.Kind() != reflect.Slice {
		return fmt.Errorf("PrintTable: expected a slice, got %s", v.Kind())
	}
	for i := 0; i < v.Len(); i++ {
		row := v.Index(i).Interface()
		for j, c := range active {
			if j > 0 {
				fmt.Fprint(tw, "\t")
			}
			fmt.Fprint(tw, c.Get(row))
		}
		fmt.Fprintln(tw)
	}
	return nil
}

// PrintDetails renders a single item as a list of "Field: value" lines.
// Used by `get` commands for the default compact view.
func PrintDetails(w io.Writer, item any, cols Columns) error {
	// Column width for alignment: longest name + ":"
	maxName := 0
	for _, c := range cols {
		if c.Wide {
			continue
		}
		if len(c.Name) > maxName {
			maxName = len(c.Name)
		}
	}
	for _, c := range cols {
		if c.Wide {
			continue
		}
		pad := maxName - len(c.Name)
		fmt.Fprintf(w, "%s:%s  %s\n", c.Name, spaces(pad), c.Get(item))
	}
	return nil
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
