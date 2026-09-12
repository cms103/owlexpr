package owlexpr

import (
	"strings"
	"testing"

	"github.com/cms103/owlexpr/vm"
)

func runSheetT(t *testing.T, env map[string]any, cells []CellDef) map[string]any {
	t.Helper()
	sheet, err := CompileSheet(cells)
	if err != nil {
		t.Fatalf("CompileSheet: %v", err)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := RunSheet(mc, sheet, env)
	if err != nil {
		t.Fatalf("RunSheet: %v", err)
	}
	return res
}

func TestSheetLinearChain(t *testing.T) {
	res := runSheetT(t, map[string]any{"base": int64(10)}, []CellDef{
		{Name: "a", Expression: "base + 1"},
		{Name: "b", Expression: "sheet.a + 1"},
		{Name: "c", Expression: "sheet.b + 1"},
	})
	if res["a"] != int64(11) || res["b"] != int64(12) || res["c"] != int64(13) {
		t.Fatalf("got %+v", res)
	}
}

func TestSheetDiamondDependency(t *testing.T) {
	// d depends on both b and c, which both depend on a - classic diamond.
	res := runSheetT(t, map[string]any{"base": int64(2)}, []CellDef{
		{Name: "d", Expression: "sheet.b + sheet.c"},
		{Name: "a", Expression: "base * 10"},
		{Name: "b", Expression: "sheet.a + 1"},
		{Name: "c", Expression: "sheet.a + 2"},
	})
	if res["a"] != int64(20) || res["b"] != int64(21) || res["c"] != int64(22) || res["d"] != int64(43) {
		t.Fatalf("got %+v", res)
	}
}

func TestSheetIndependentCellsBothResolve(t *testing.T) {
	// x and y have no relationship to each other - order between them must
	// not matter for correctness, only that both resolve.
	res := runSheetT(t, map[string]any{"base": int64(5)}, []CellDef{
		{Name: "x", Expression: "base + 1"},
		{Name: "y", Expression: "base + 2"},
	})
	if res["x"] != int64(6) || res["y"] != int64(7) {
		t.Fatalf("got %+v", res)
	}
}

func TestSheetSelfReferenceIsCycle(t *testing.T) {
	_, err := CompileSheet([]CellDef{
		{Name: "a", Expression: "sheet.a + 1"},
	})
	if err == nil {
		t.Fatal("expected a cycle error, got none")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected a cycle error, got: %v", err)
	}
}

func TestSheetMutualCycleIsDetected(t *testing.T) {
	_, err := CompileSheet([]CellDef{
		{Name: "a", Expression: "sheet.b + 1"},
		{Name: "b", Expression: "sheet.c + 1"},
		{Name: "c", Expression: "sheet.a + 1"},
	})
	if err == nil {
		t.Fatal("expected a cycle error, got none")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected a cycle error, got: %v", err)
	}
}

func TestSheetDuplicateNameIsError(t *testing.T) {
	_, err := CompileSheet([]CellDef{
		{Name: "a", Expression: "1"},
		{Name: "a", Expression: "2"},
	})
	if err == nil {
		t.Fatal("expected a duplicate-name error, got none")
	}
}

func TestSheetUnknownCellReferenceIsError(t *testing.T) {
	_, err := CompileSheet([]CellDef{
		{Name: "a", Expression: "sheet.doesNotExist + 1"},
	})
	if err == nil {
		t.Fatal("expected an unknown-cell-reference error, got none")
	}
}

// TestSheetBareNameNeverReachesCell is the core payoff of the sheet./bare
// namespace split: a cell and an env variable can share a name with no
// ambiguity at all - bare `x` always means env, `sheet.x` always means the
// cell, and both are reachable from the same expression at once.
func TestSheetBareNameNeverReachesCell(t *testing.T) {
	res := runSheetT(t, map[string]any{"x": int64(1)}, []CellDef{
		{Name: "x", Expression: "100"},
		{Name: "y", Expression: "x + sheet.x"},
	})
	if res["x"] != int64(100) {
		t.Fatalf("got %+v", res)
	}
	if res["y"] != int64(101) {
		t.Fatalf("expected y = env x (1) + cell x (100) = 101, got %+v", res)
	}
}

func TestSheetEnvCollisionWithNamespaceIsError(t *testing.T) {
	sheet, err := CompileSheet([]CellDef{
		{Name: "a", Expression: "1"},
	})
	if err != nil {
		t.Fatalf("CompileSheet: %v", err)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	_, err = RunSheet(mc, sheet, map[string]any{"sheet": "oops"})
	if err == nil {
		t.Fatal("expected an error when env already defines \"sheet\", got none")
	}
}

func TestSheetLocalSheetBindingIsNotConfusedWithNamespace(t *testing.T) {
	// The cell is itself named "x", and its own body shadows the reserved
	// "sheet" name with a local `let` binding of the same shape
	// (map with an "x" key) - this must resolve against the local, not
	// create a self-dependency, and not route through the real namespace.
	sheet, err := CompileSheet([]CellDef{
		{Name: "x", Expression: `let sheet = {"x": 999}; sheet.x`},
	})
	if err != nil {
		t.Fatalf("CompileSheet: %v", err)
	}
	if len(sheet.cells[0].deps) != 0 {
		t.Fatalf("expected no cross-cell deps from a shadowed local `sheet`, got %v", sheet.cells[0].deps)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := RunSheet(mc, sheet, map[string]any{})
	if err != nil {
		t.Fatalf("RunSheet: %v", err)
	}
	if res["x"] != int64(999) {
		t.Fatalf("got %+v", res)
	}
}

func TestSheetLambdaParamNamedSheetIsNotConfused(t *testing.T) {
	sheet, err := CompileSheet([]CellDef{
		{Name: "result", Expression: `map([{"v": 1}, {"v": 2}], sheet => sheet.v)`},
	})
	if err != nil {
		t.Fatalf("CompileSheet: %v", err)
	}
	if len(sheet.cells[0].deps) != 0 {
		t.Fatalf("expected no cross-cell deps from a shadowing lambda param, got %v", sheet.cells[0].deps)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := RunSheet(mc, sheet, map[string]any{})
	if err != nil {
		t.Fatalf("RunSheet: %v", err)
	}
	got, ok := res["result"].([]any)
	if !ok || len(got) != 2 || got[0] != int64(1) || got[1] != int64(2) {
		t.Fatalf("got %+v", res["result"])
	}
}

func TestSheetFailFastStopsDownstreamCells(t *testing.T) {
	ranC := false
	env := map[string]any{
		"markRanC": vm.BuiltinFunc(func(mc *vm.Machine, args ...any) (any, error) {
			ranC = true
			return true, nil
		}),
	}
	sheet, err := CompileSheet([]CellDef{
		{Name: "a", Expression: `1 / 0`},
		{Name: "c", Expression: `markRanC()`},
	})
	if err != nil {
		t.Fatalf("CompileSheet: %v", err)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	_, err = RunSheet(mc, sheet, env)
	if err == nil {
		t.Fatal("expected an error from RunSheet, got none")
	}
	if !strings.Contains(err.Error(), `"a"`) {
		t.Fatalf("expected error to name the failing cell, got: %v", err)
	}
	if ranC {
		t.Fatal("cell c ran even though it does not depend on the failing cell a - fail-fast should have stopped the whole run")
	}
}

func TestSheetCellCanReturnClosure(t *testing.T) {
	// Confirms the fresh-per-cell-scope design doesn't just tolerate a
	// returned closure but leaves it fully correct: applying the closure
	// after RunSheet returns must still see the exact dependency value it
	// closed over, unaffected by any other cell's result.
	res := runSheetT(t, map[string]any{}, []CellDef{
		{Name: "rate", Expression: "2"},
		{Name: "scaler", Expression: "x => x * sheet.rate"},
		{Name: "unrelated", Expression: "999"},
	})
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	out, err := mc.Call(res["scaler"], []any{int64(21)})
	if err != nil {
		t.Fatalf("calling returned closure: %v", err)
	}
	if out != int64(42) {
		t.Fatalf("got %v, want 42", out)
	}
}

func TestSheetResultMapWrapsLambdaCellAsCallableFunc(t *testing.T) {
	// The map RunSheet hands back to its caller should hold an ordinary
	// callable Go func for a lambda-valued cell (see WrapCallable, vm/vm.go)
	// - not vm's unexported *closure type, which nothing outside vm can even
	// name, let alone call directly without going through mc.Call.
	res := runSheetT(t, map[string]any{}, []CellDef{
		{Name: "rate", Expression: "2"},
		{Name: "scaler", Expression: "x => x * sheet.rate"},
	})
	fn, ok := res["scaler"].(func(args ...any) (any, error))
	if !ok {
		t.Fatalf("res[\"scaler\"] is %T, want func(args ...any) (any, error)", res["scaler"])
	}
	out, err := fn(int64(21))
	if err != nil {
		t.Fatalf("calling wrapped cell closure: %v", err)
	}
	if out != int64(42) {
		t.Fatalf("got %v, want 42", out)
	}
}

// TestSheetOptMemberAccessRecordsDependency is a regression test: `?.`
// anywhere inside a cell used to panic sheetReferences, because
// OptMemberAccessNode had no case in its AST walk (sheet_refs.go) and fell
// through to the "unhandled node type" default. b is declared before a, so
// this also confirms sheet.a?.field records the same dependency edge
// sheet.a.field would - sheet is never nil (RunSheet injects it fresh per
// cell), so the ?. can only ever matter for the field, never for reaching
// the right cell first.
func TestSheetOptMemberAccessRecordsDependency(t *testing.T) {
	sheet, err := CompileSheet([]CellDef{
		{Name: "b", Expression: `sheet.a?.field ?? "default"`},
		{Name: "a", Expression: `{"field": "value"}`},
	})
	if err != nil {
		t.Fatalf("CompileSheet: %v", err)
	}
	if sheet.cells[0].name != "a" || sheet.cells[1].name != "b" {
		t.Fatalf("expected topo order [a, b], got [%s, %s]", sheet.cells[0].name, sheet.cells[1].name)
	}
	res := runSheetT(t, map[string]any{}, []CellDef{
		{Name: "a", Expression: `{"field": "value"}`},
		{Name: "b", Expression: `sheet.a?.field ?? "default"`},
	})
	if res["b"] != "value" {
		t.Fatalf("got %+v", res)
	}
}

func TestSheetOptMemberAccessShortCircuitsOnNilTarget(t *testing.T) {
	res := runSheetT(t, map[string]any{}, []CellDef{
		{Name: "a", Expression: "nil"},
		{Name: "b", Expression: `sheet.a?.field ?? "default"`},
	})
	if res["b"] != "default" {
		t.Fatalf("got %+v", res)
	}
}

// TestSheetOptIndexDoesNotPanic covers OptIndexNode (target?.[index]), the
// other AST node sheetReferences was missing a case for.
func TestSheetOptIndexDoesNotPanic(t *testing.T) {
	res := runSheetT(t, map[string]any{}, []CellDef{
		{Name: "a", Expression: "[1, 2, 3]"},
		{Name: "b", Expression: "sheet.a?.[0]"},
	})
	if res["b"] != int64(1) {
		t.Fatalf("got %+v", res)
	}
}

// TestSheetNilLiteralDoesNotPanic covers NilNode, also missing a case in
// sheetReferences - any cell containing a bare `nil` literal panicked too.
func TestSheetNilLiteralDoesNotPanic(t *testing.T) {
	res := runSheetT(t, map[string]any{}, []CellDef{
		{Name: "a", Expression: "nil"},
		{Name: "b", Expression: `sheet.a ?? "default"`},
	})
	if res["b"] != "default" {
		t.Fatalf("got %+v", res)
	}
}

func TestSheetEmptyIsError(t *testing.T) {
	if _, err := CompileSheet(nil); err == nil {
		t.Fatal("expected an error for an empty sheet, got none")
	}
}
