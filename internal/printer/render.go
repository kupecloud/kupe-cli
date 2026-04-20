package printer

import (
	"fmt"
	"io"
)

// RenderList writes items in the requested output Format. columns is the
// table spec used for Kind=Table/Wide; nameFn extracts the pipe-friendly
// identifier for Kind=Name. All other kinds marshal items as-is.
//
// Every command's list handler is "parse format, fetch slice, dispatch by
// kind" — this helper keeps that dispatch in one place so new Kinds only
// need to be added here.
func RenderList[T any](out io.Writer, format *Format, items []T, columns Columns, nameFn func(T) string) error {
	switch format.Kind {
	case Table, Wide:
		return PrintTable(out, items, columns, format.Kind == Wide)
	case JSON:
		return PrintJSON(out, items)
	case YAML:
		return PrintYAML(out, items)
	case Name:
		return PrintNames(out, items, func(v any) string {
			if t, ok := v.(T); ok {
				return nameFn(t)
			}
			return ""
		})
	case Template:
		return PrintTemplate(out, items, format.Template)
	case JSONPath:
		return PrintJSONPath(out, items, format.Path)
	}
	return fmt.Errorf("unhandled output kind %v", format.Kind)
}

// RenderOne writes a single item. detailColumns is used for the Kind=Table
// key:value view (via PrintDetails); other kinds marshal item directly.
// nameFn supports -o name for single-item commands (e.g., "kupe cluster
// get prod -o name" prints "prod").
func RenderOne[T any](out io.Writer, format *Format, item *T, detailColumns Columns, nameFn func(*T) string) error {
	switch format.Kind {
	case Table, Wide:
		return PrintDetails(out, item, detailColumns)
	case JSON:
		return PrintJSON(out, item)
	case YAML:
		return PrintYAML(out, item)
	case Name:
		return PrintNames(out, item, func(v any) string {
			if p, ok := v.(*T); ok {
				return nameFn(p)
			}
			return ""
		})
	case Template:
		return PrintTemplate(out, item, format.Template)
	case JSONPath:
		return PrintJSONPath(out, item, format.Path)
	}
	return fmt.Errorf("unhandled output kind %v", format.Kind)
}
