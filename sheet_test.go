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

func TestSheetCustomNamespaceResolvesCrossCellRefs(t *testing.T) {
	sheet, err := CompileSheet([]CellDef{
		{Name: "a", Expression: "base + 1"},
		{Name: "b", Expression: "cell.a + 1"},
	}, Namespace("cell"))
	if err != nil {
		t.Fatalf("CompileSheet: %v", err)
	}
	if len(sheet.cells[1].deps) != 1 || sheet.cells[1].deps[0].name != "a" {
		t.Fatalf("expected b to depend on a via the custom namespace, got %+v", sheet.cells[1].deps)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := RunSheet(mc, sheet, map[string]any{"base": int64(10)})
	if err != nil {
		t.Fatalf("RunSheet: %v", err)
	}
	if res["a"] != int64(11) || res["b"] != int64(12) {
		t.Fatalf("got %+v", res)
	}
}

// TestSheetCustomNamespaceLeavesDefaultNameFree is the actual payoff of
// making the namespace configurable: with a custom namespace in play, a
// cell (or an env var) can be named "sheet" with no collision at all, since
// "sheet" is no longer the reserved name - the same freedom the default
// "sheet" namespace already gives every other name.
func TestSheetCustomNamespaceLeavesDefaultNameFree(t *testing.T) {
	sheet, err := CompileSheet([]CellDef{
		{Name: "sheet", Expression: "41"},
		{Name: "b", Expression: "cell.sheet + 1"},
	}, Namespace("cell"))
	if err != nil {
		t.Fatalf("CompileSheet: %v", err)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := RunSheet(mc, sheet, map[string]any{})
	if err != nil {
		t.Fatalf("RunSheet: %v", err)
	}
	if res["sheet"] != int64(41) || res["b"] != int64(42) {
		t.Fatalf("got %+v", res)
	}
}

// TestSheetDefaultNamespaceStillReservedWithoutOption confirms the
// zero-option path is unchanged: bare "sheet" in env still collides exactly
// as it did before Namespace existed.
func TestSheetDefaultNamespaceStillReservedWithoutOption(t *testing.T) {
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
	if _, err := RunSheet(mc, sheet, map[string]any{"sheet": "oops"}); err == nil {
		t.Fatal("expected an error when env already defines \"sheet\", got none")
	}
}

func TestSheetCustomNamespaceEnvCollisionIsError(t *testing.T) {
	sheet, err := CompileSheet([]CellDef{
		{Name: "a", Expression: "1"},
	}, Namespace("cell"))
	if err != nil {
		t.Fatalf("CompileSheet: %v", err)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := RunSheet(mc, sheet, map[string]any{"cell": "oops"}); err == nil {
		t.Fatal("expected an error when env already defines the custom namespace \"cell\", got none")
	}
	// And the old default name is no longer special at all under a custom
	// namespace - env can freely define "sheet" now.
	if _, err := RunSheet(mc, sheet, map[string]any{"sheet": "fine now"}); err != nil {
		t.Fatalf("RunSheet: %v", err)
	}
}

// TestSheetNamespaceRejectsLanguageKeywords covers every keyword the lexer
// recognizes (parser.go's readIdent) - each one lexes as something other
// than a plain identifier, so a namespace named after one could never
// actually be spelled as `<namespace>.<name>` in a cell expression. This is
// a regression test: an earlier version of this check only rejected
// "and"/"or"/"in"/"not"/"matches" (and only via a case-insensitive
// comparison against upper-cased keywords, which doesn't match anything
// real since the language's keywords are lower-case and case-sensitive),
// missing "if"/"else"/"let"/"true"/"false"/"nil" entirely.
func TestSheetNamespaceRejectsLanguageKeywords(t *testing.T) {
	for _, kw := range []string{"if", "else", "let", "true", "false", "nil", "and", "or", "in", "not", "matches"} {
		if _, err := CompileSheet([]CellDef{{Name: "a", Expression: "1"}}, Namespace(kw)); err == nil {
			t.Errorf("Namespace(%q): expected an error, got none", kw)
		}
	}
}

// TestSheetNamespaceCaseSensitivity documents that keyword rejection is
// case-sensitive, matching the language itself - the lexer only recognizes
// lower-case keywords (parser.go's readIdent switches on exact string
// matches), so e.g. "And" or "AND" lex as ordinary identifiers and are
// legitimate namespace names, while "and" does not.
func TestSheetNamespaceCaseSensitivity(t *testing.T) {
	if _, err := CompileSheet([]CellDef{{Name: "a", Expression: "1"}}, Namespace("And")); err != nil {
		t.Fatalf("Namespace(\"And\"): expected no error, got %v", err)
	}
	if _, err := CompileSheet([]CellDef{{Name: "a", Expression: "1"}}, Namespace("and")); err == nil {
		t.Fatal("Namespace(\"and\"): expected an error, got none")
	}
}

// TestSheetNamespaceRejectsNonIdentifiers covers namespace names that would
// never lex as a single identifier token at all - each would make
// `<namespace>.<name>` either a parse error or silently not what the caller
// intended, so CompileSheet should reject them up front instead.
func TestSheetNamespaceRejectsNonIdentifiers(t *testing.T) {
	for _, name := range []string{"", "my-sheet", "my sheet", "1sheet", "sheet.x", "shee$t"} {
		if _, err := CompileSheet([]CellDef{{Name: "a", Expression: "1"}}, Namespace(name)); err == nil {
			t.Errorf("Namespace(%q): expected an error, got none", name)
		}
	}
}

func TestSheetNamespaceAcceptsOrdinaryIdentifiers(t *testing.T) {
	for _, name := range []string{"cell", "row", "_sheet", "sheet2", "Sheet"} {
		if _, err := CompileSheet([]CellDef{{Name: "a", Expression: "1"}}, Namespace(name)); err != nil {
			t.Errorf("Namespace(%q): expected no error, got %v", name, err)
		}
	}
}

// TestSheetNamespaceCollidesWithUnrelatedBuiltinCall documents a known
// limitation (see Namespace's doc comment, sheet.go): CompileSheet has no
// Machine to check a chosen namespace against, so nothing stops a caller
// from picking a name that collides with a registered builtin namespace
// like "string". sheetReferences has no way to tell "string.trim" used as
// the stdlib call apart from "string.trim" meaning "the cell named trim" -
// it statically treats every `<namespace>.<name>` as the latter, so a cell
// merely calling string.trim(...) is treated as depending on a cell named
// "trim" that was never meant to exist here, and CompileSheet rejects it
// with a confusing "references a cell that does not exist" error that has
// nothing to do with the actual mistake (choosing "string" as the
// namespace).
func TestSheetNamespaceCollidesWithUnrelatedBuiltinCall(t *testing.T) {
	_, err := CompileSheet([]CellDef{
		{Name: "a", Expression: `string.trim("  hi  ")`},
	}, Namespace("string"))
	if err == nil {
		t.Fatal("expected an error from the \"string\" namespace colliding with the string.trim() builtin call, got none")
	}
	if !strings.Contains(err.Error(), "trim") {
		t.Fatalf("got: %v", err)
	}
}

// TestSheetNamespaceShadowsBuiltinNamespaceAtRuntime is the sharper form of
// the same limitation: when a cell named "trim" does legitimately exist,
// CompileSheet has no reason to object, but at runtime OpLoad resolves
// "string" against the sheet's own injected depsMap before ever consulting
// the Machine's builtins (see OpLoad, vm/vm.go) - so string.trim(...) calls
// the *cell's* value, not the stdlib function, and fails because that value
// isn't callable.
func TestSheetNamespaceShadowsBuiltinNamespaceAtRuntime(t *testing.T) {
	sheet, err := CompileSheet([]CellDef{
		{Name: "trim", Expression: "1"},
		{Name: "a", Expression: `string.trim("  hi  ")`},
	}, Namespace("string"))
	if err != nil {
		t.Fatalf("CompileSheet: %v", err)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := RunSheet(mc, sheet, map[string]any{}); err == nil {
		t.Fatal("expected the builtin \"string\" namespace to be shadowed by the \"trim\" cell and fail, got no error")
	}
}
