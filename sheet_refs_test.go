package owlexpr

import (
	"reflect"
	"testing"
)

// TestSheetReferences covers the exported SheetReferences: sorted,
// de-duplicated names, the default and a custom namespace, and the same
// shadowing rules CompileSheet relies on.
func TestSheetReferences(t *testing.T) {
	cases := []struct {
		expr      string
		namespace string
		want      []string
	}{
		{`sheet.b + sheet.a * sheet.b`, "", []string{"a", "b"}},
		{`sheet.b + sheet.a`, "sheet", []string{"a", "b"}},
		{`cells.total / cells?.count`, "cells", []string{"count", "total"}},
		{`sheet.a + cells.b`, "cells", []string{"b"}},
		{`1 + 2`, "", []string{}},
		{`let sheet = {a: 1}; sheet.a + 1`, "", []string{}},
		{`map(xs, sheet => sheet.a)`, "", []string{}},
		{`let x = sheet.a; x + sheet.b`, "", []string{"a", "b"}},
		{`len(sheet)`, "", []string{}},
		{`if sheet.flag { sheet.x } else { [sheet.y][0] }`, "", []string{"flag", "x", "y"}},
		{`sheet["b"] + sheet?.["a"] + sheet.b`, "", []string{"a", "b"}},
		{`cells["x"] + sheet["y"]`, "cells", []string{"x"}},
	}
	for _, c := range cases {
		got, err := SheetReferences(c.expr, c.namespace)
		if err != nil {
			t.Errorf("SheetReferences(%q, %q): %v", c.expr, c.namespace, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("SheetReferences(%q, %q) = %#v, want %#v", c.expr, c.namespace, got, c.want)
		}
	}
}

func TestSheetReferencesErrors(t *testing.T) {
	cases := []struct{ expr, namespace string }{
		{`sheet.a +`, ""},    // parse error
		{`x.a`, "not valid"}, // not a single identifier
		{`x.a`, "if"},        // reserved word
		{`x.a`, "1abc"},
		{`sheet[k]`, ""}, // not an identifier
	}
	for _, c := range cases {
		if _, err := SheetReferences(c.expr, c.namespace); err == nil {
			t.Errorf("SheetReferences(%q, %q): expected an error", c.expr, c.namespace)
		}
	}
}

// TestSheetReferencesUnknownNodeErrors checks the internal scanner reports
// an unknown AST node as an error instead of panicking.
func TestSheetReferencesUnknownNodeErrors(t *testing.T) {
	if _, err := sheetReferences(struct{}{}, "sheet"); err == nil {
		t.Error("sheetReferences(struct{}{}): expected an error for an unknown node type")
	}
	if _, err := sheetReferences(BinaryOpNode{Left: NumberNode{Value: int64(1)}, Right: "oops"}, "sheet"); err == nil {
		t.Error("sheetReferences with a nested unknown node: expected an error")
	}
}
