package printer

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
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
//
// Column widths are computed using *visible* width — ANSI escape
// sequences (e.g. lipgloss colour codes around the PHASE cell) are
// excluded from width measurement so the colours don't push later
// columns out of alignment. text/tabwriter's escape support is unfit
// for this because it counts escaped content toward width and only
// excludes the markers themselves.
func PrintTable(w io.Writer, items any, cols Columns, wide bool) error {
	active := cols.Filter(wide)
	if len(active) == 0 {
		return nil
	}

	v := reflect.ValueOf(items)
	if v.Kind() != reflect.Slice {
		return fmt.Errorf("PrintTable: expected a slice, got %s", v.Kind())
	}

	rows := make([][]string, 0, v.Len()+1)
	header := make([]string, len(active))
	for i, c := range active {
		header[i] = c.Name
	}
	rows = append(rows, header)
	for i := 0; i < v.Len(); i++ {
		row := v.Index(i).Interface()
		cells := make([]string, len(active))
		for j, c := range active {
			cells[j] = c.Get(row)
		}
		rows = append(rows, cells)
	}

	widths := make([]int, len(active))
	for _, r := range rows {
		for j, cell := range r {
			if vw := visibleWidth(cell); vw > widths[j] {
				widths[j] = vw
			}
		}
	}

	const padding = 2
	for _, r := range rows {
		for j, cell := range r {
			if _, err := io.WriteString(w, cell); err != nil {
				return err
			}
			if j == len(r)-1 {
				continue
			}
			gap := widths[j] - visibleWidth(cell) + padding
			if _, err := io.WriteString(w, strings.Repeat(" ", gap)); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	return nil
}

// visibleWidth returns the rune count of s with SGR-style ANSI escape
// sequences excluded — the count a terminal renders, not the byte (or
// raw-rune) count. Used by PrintTable for column-width measurement.
func visibleWidth(s string) int {
	if !strings.ContainsRune(s, '\x1b') {
		return utf8.RuneCountInString(s)
	}
	w := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !isASCIILetter(s[j]) {
				j++
			}
			if j < len(s) {
				j++ // include terminating letter
			}
			i = j
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		w++
		i += size
	}
	return w
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
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
