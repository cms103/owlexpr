package langtest

import (
	"maps"
	"slices"
	"testing"

	"github.com/cms103/owlexpr"
)

// evalIn parses, compiles, and runs input against env, failing the test on
// any parse/compile/run error.
func evalIn(t *testing.T, env map[string]any, input string) any {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	return res
}

// evalInExpectError is evalIn's error-expecting counterpart.
func evalInExpectError(t *testing.T, env map[string]any, input string) {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatalf("expected an error for %q, got none", input)
	}
}

func TestInList(t *testing.T) {
	env := map[string]any{
		"nums":  []any{1, 2, 3},
		"words": []any{"a", "b", "c"},
	}

	tests := []struct {
		input string
		want  bool
	}{
		{`2 in nums`, true},
		{`5 in nums`, false},
		{`"b" in words`, true},
		{`"z" in words`, false},
		{`2 in [1, 2, 3]`, true},
		{`4 in [1, 2, 3]`, false},
		// Numeric coercion: an int64 literal against a []float64-typed
		// needle set should match via Combine(OpEqual), the same as `==`
		// already does for mixed numeric types.
		{`2 in [2.0, 3.0]`, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := evalIn(t, env, tt.input)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInReflectSlice(t *testing.T) {
	env := map[string]any{
		"ints": []int64{10, 20, 30},
	}
	if got := evalIn(t, env, `20 in ints`); got != true {
		t.Errorf("got %v, want true", got)
	}
	if got := evalIn(t, env, `25 in ints`); got != false {
		t.Errorf("got %v, want false", got)
	}
}

func TestInMap(t *testing.T) {
	env := map[string]any{
		"labels": map[string]any{"a": "One", "b": "Two"},
	}

	tests := []struct {
		input string
		want  bool
	}{
		{`"a" in labels`, true},
		{`"z" in labels`, false},
		// A value, not a key, must not match - `in` on a map checks keys.
		{`"One" in labels`, false},
		// A non-string needle can't be a key of a map[string]any at all -
		// this should cleanly report false, not error.
		{`5 in labels`, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := evalIn(t, env, tt.input)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInReflectMap(t *testing.T) {
	env := map[string]any{
		"scores": map[string]int{"alice": 1, "bob": 2},
		"byNum":  map[int]string{1: "one", 2: "two"},
	}
	if got := evalIn(t, env, `"alice" in scores`); got != true {
		t.Errorf("got %v, want true", got)
	}
	if got := evalIn(t, env, `"carol" in scores`); got != false {
		t.Errorf("got %v, want false", got)
	}
	// int64 needle (owlexpr's own integer literal type) against a
	// map[int]string key - exercises mapContainsKey's type conversion.
	if got := evalIn(t, env, `1 in byNum`); got != true {
		t.Errorf("got %v, want true", got)
	}
	if got := evalIn(t, env, `9 in byNum`); got != false {
		t.Errorf("got %v, want false", got)
	}
}

// TestInRejectsIterSeq and TestInRejectsIterSeq2 cover `in`'s own
// "iter/list split" guard: a bare push iterator (real
// stdlib ones here - slices.Values/maps.All - not just a hand-rolled
// closure) on the right-hand side is a compile-clean, runtime type error
// now, not silently drained. `in` used to accept and short-circuit-scan
// either shape directly; stdlib's iter.contains is the explicit replacement -
// see TestIterContainsShortCircuits/TestIterContainsOverPairSource in
// stdlib/iter_builtins_test.go for the positive coverage this test used
// to provide.
func TestInRejectsIterSeq(t *testing.T) {
	env := map[string]any{
		"seq": slices.Values([]int{1, 2, 3}),
	}
	evalInExpectError(t, env, `2 in seq`)
}

func TestInRejectsIterSeq2(t *testing.T) {
	env := map[string]any{
		"pairs": maps.All(map[string]any{"x": 1, "y": 2}),
	}
	evalInExpectError(t, env, `"x" in pairs`)
}

func TestInUnsupportedType(t *testing.T) {
	env := map[string]any{"n": 42}
	instructions, err := owlexpr.Compile(`5 in n`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	_, err = mc.Run(instructions, env)
	if err == nil {
		t.Fatal("expected an error for `in` against an unsupported right-hand type, got none")
	}
}

// TestNotInViaNegation documents that `!(x in y)` still works as a plain
// negation of `in`, alongside the dedicated `not in` spelling below.
func TestNotInViaNegation(t *testing.T) {
	env := map[string]any{"nums": []any{1, 2, 3}}
	if got := evalIn(t, env, `!(5 in nums)`); got != true {
		t.Errorf("got %v, want true", got)
	}
	if got := evalIn(t, env, `!(2 in nums)`); got != false {
		t.Errorf("got %v, want false", got)
	}
}

// TestNotIn exercises the dedicated infix "not in" spelling, which
// desugars to !(x in y) at parse time (see ParseExpression's TokOp case
// in parser.go) - so it should behave identically to the negation form
// above, just read more naturally.
func TestNotIn(t *testing.T) {
	env := map[string]any{"nums": []any{1, 2, 3}}
	if got := evalIn(t, env, `5 not in nums`); got != true {
		t.Errorf("got %v, want true", got)
	}
	if got := evalIn(t, env, `2 not in nums`); got != false {
		t.Errorf("got %v, want false", got)
	}
}

// TestNotInMap confirms "not in" also works against a map's keys, same
// as plain "in" does.
func TestNotInMap(t *testing.T) {
	env := map[string]any{"m": map[string]any{"a": 1}}
	if got := evalIn(t, env, `"b" not in m`); got != true {
		t.Errorf("got %v, want true", got)
	}
	if got := evalIn(t, env, `"a" not in m`); got != false {
		t.Errorf("got %v, want false", got)
	}
}

// TestNotInPrecedence confirms "not in" shares "in"'s precedence rather
// than binding as loosely as a standalone "not" would - "a not in b ==
// c" should parse as "(a not in b) == c", matching how plain "a in b ==
// c" already parses as "(a in b) == c".
func TestNotInPrecedence(t *testing.T) {
	env := map[string]any{"nums": []any{1, 2, 3}}
	if got := evalIn(t, env, `5 not in nums == true`); got != true {
		t.Errorf("got %v, want true", got)
	}
}

// TestNotInRequiresIn confirms a bare infix "not" without a following
// "in" is a clear parse error rather than silently misparsing.
func TestNotInRequiresIn(t *testing.T) {
	if _, err := owlexpr.Compile(`5 not nums`); err == nil {
		t.Fatal("expected a parse error for 'not' without a following 'in', got none")
	}
}
