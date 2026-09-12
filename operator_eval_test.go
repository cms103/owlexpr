package owlexpr

import (
	"testing"

	"github.com/cms103/owlexpr/vm"
)

// TestLogicalOrShortCircuits covers the "||"/"or" compiled short-circuit
// path end-to-end (compileConditional's sibling in Compile's BinaryOpNode
// case), which nothing previously exercised at the eval level - only at
// parse/precedence level. A left side that's already true must never
// evaluate the right side at all: raiseIfCalled below records a call if it
// ever runs, so a passing test proves short-circuiting actually happens,
// not just that the boolean result happens to be right.
func TestLogicalOrShortCircuits(t *testing.T) {
	calledRight := false
	env := map[string]any{
		"sideEffect": vm.BuiltinFunc(func(mc *vm.Machine, args ...any) (any, error) {
			calledRight = true
			return true, nil
		}),
	}

	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"true || (short-circuits)", `true || sideEffect()`, true},
		{"true or (short-circuits)", `true or sideEffect()`, true},
		{"false || true", `false || true`, true},
		{"false || false", `false || false`, false},
		{"or evaluates right when left is false", `false or true`, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			calledRight = false
			got := evalRoot(t, env, tt.input)
			if got != tt.want {
				t.Errorf("%q = %v, want %v", tt.input, got, tt.want)
			}
		})
	}

	if calledRight {
		t.Errorf("right-hand side of a short-circuiting || was evaluated when it should not have been")
	}
}

// TestLogicalAndShortCircuits mirrors TestLogicalOrShortCircuits for
// "&&"/"and": a false left side must skip the right side entirely.
func TestLogicalAndShortCircuits(t *testing.T) {
	calledRight := false
	env := map[string]any{
		"sideEffect": vm.BuiltinFunc(func(mc *vm.Machine, args ...any) (any, error) {
			calledRight = true
			return true, nil
		}),
	}

	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"false && (short-circuits)", `false && sideEffect()`, false},
		{"false and (short-circuits)", `false and sideEffect()`, false},
		{"true && true", `true && true`, true},
		{"true && false", `true && false`, false},
		{"and evaluates right when left is true", `true and false`, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			calledRight = false
			got := evalRoot(t, env, tt.input)
			if got != tt.want {
				t.Errorf("%q = %v, want %v", tt.input, got, tt.want)
			}
		})
	}

	if calledRight {
		t.Errorf("right-hand side of a short-circuiting && was evaluated when it should not have been")
	}
}

// TestExponentOperator covers "**" (OpPow) end-to-end, including its
// right-associativity (2**3**2 == 2**(3**2) == 512, not (2**3)**2 == 64 -
// see infixBindingPowers' doc comment) and that it binds tighter than "*".
func TestExponentOperator(t *testing.T) {
	cases := []struct {
		input string
		want  any
	}{
		{"2 ** 10", int64(1024)},
		{"2 ** 3 ** 2", int64(512)},
		{"2 * 3 ** 2", int64(18)},
		{"2.0 ** 0.5", 1.4142135623730951},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalRoot(t, nil, tt.input)
			if got != tt.want {
				t.Errorf("%q = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestExponentNegativeIntExponentErrors covers intPow's negative-exponent
// guard end-to-end (unreachable via floatOperations, which uses math.Pow
// and never errors).
func TestExponentNegativeIntExponentErrors(t *testing.T) {
	evalRootExpectError(t, nil, `2 ** -1`)
}

// TestModuloOperator covers "%" (OpMod) end-to-end for both int and float
// operands, plus its divide-by-zero guard - none of which any existing
// test reached through a compiled expression.
func TestModuloOperator(t *testing.T) {
	cases := []struct {
		input string
		want  any
	}{
		{"7 % 3", int64(1)},
		{"7.5 % 2.0", 1.5},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalRoot(t, nil, tt.input)
			if got != tt.want {
				t.Errorf("%q = %v, want %v", tt.input, got, tt.want)
			}
		})
	}

	t.Run("modulus by zero errors", func(t *testing.T) {
		evalRootExpectError(t, nil, `7 % 0`)
	})
}

// TestIfWithoutElse covers compileConditional's nil-elseExpr branch
// (IfNode with no `else`), which pushes a literal nil rather than
// compiling anything - untested by any existing if/ternary test, which
// all supply an else branch.
func TestIfWithoutElse(t *testing.T) {
	if got := evalRoot(t, nil, `if false { 1 }`); got != nil {
		t.Errorf("if false with no else = %v, want nil", got)
	}
	if got := evalRoot(t, nil, `if true { 1 }`); got != int64(1) {
		t.Errorf("if true with no else = %v, want 1", got)
	}
}

// TestReduceOverList and TestReduceOverMap cover reduceFunc, which had no
// test coverage at all despite being one of the three higher-order
// builtins registered alongside map/filter.
func TestReduceOverList(t *testing.T) {
	got := evalRoot(t, nil, `reduce([1, 2, 3, 4], (acc, x) => acc + x, 0)`)
	if got != int64(10) {
		t.Errorf("reduce sum = %v, want 10", got)
	}
}

func TestReduceOverMap(t *testing.T) {
	env := map[string]any{"m": map[string]any{"a": int64(1), "b": int64(2), "c": int64(3)}}
	got := evalRoot(t, env, `reduce(m, (acc, k, v) => acc + v, 0)`)
	if got != int64(6) {
		t.Errorf("reduce over map = %v, want 6", got)
	}
}

// TestMapOverMapSource covers mapFunc's paired-source branch (a 2-arg
// lambda called with (k, v)) - the existing map() tests only ever pass a
// list source.
func TestMapOverMapSource(t *testing.T) {
	env := map[string]any{"m": map[string]any{"a": int64(1), "b": int64(2)}}
	got := evalRoot(t, env, `map(m, (k, v) => v * 10)`)
	list, ok := got.([]any)
	if !ok {
		t.Fatalf("map() over a map returned %T, want []any", got)
	}
	sum := int64(0)
	for _, v := range list {
		sum += v.(int64)
	}
	if len(list) != 2 || sum != 30 {
		t.Errorf("map(m, ...) = %v, want two elements summing to 30", list)
	}
}

// TestMapOverList covers mapFunc's plain list-source branch (a 1-arg
// lambda called with each element) with an actual value assertion -
// despite map() being registered alongside filter/reduce, no existing
// test checked that mapping a list produces the correctly transformed
// values, only that it errors correctly or returns [] on an empty list.
func TestMapOverList(t *testing.T) {
	got := evalRoot(t, nil, `map([1, 2, 3], x => x * 2)`).([]any)
	want := []any{int64(2), int64(4), int64(6)}
	if len(got) != len(want) {
		t.Fatalf("map([1,2,3], ...) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

// TestFilterOverList covers filterFunc's plain list-source branch with an
// actual filtering assertion - existing filter-over-list tests only cover
// the empty-list and error-propagation cases, never that a real predicate
// keeps the right elements.
func TestFilterOverList(t *testing.T) {
	got := evalRoot(t, nil, `filter([1, 2, 3, 4, 5], x => x > 2)`).([]any)
	want := []any{int64(3), int64(4), int64(5)}
	if len(got) != len(want) {
		t.Fatalf("filter([1..5], x > 2) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

// TestMapFilterReduceRejectIterSeq and TestMapFilterReduceRejectIterSeq2
// cover core map/filter/reduce's DESIGN_NOTES.md "iter/list split" guard:
// a raw Go iter.Seq[T]/iter.Seq2[K,V] value from the environment (e.g. a
// lazily computed sequence a host application hands in) is now a type
// error, not silently drained - they used to accept and materialize
// either shape directly (see git history for what this test covered
// before), the same way `filter`/`in` did; stdlib's iter.map/iter.filter/
// iter.reduce (see iter_builtins_test.go's
// TestIterMapFilterReduceOverSeq/Seq2) are the explicit replacements.
func TestMapFilterReduceRejectIterSeq(t *testing.T) {
	seq := func(yield func(int64) bool) {
		yield(1)
	}
	env := map[string]any{"seq": seq}

	evalRootExpectError(t, env, `map(seq, x => x * 10)`)
	evalRootExpectError(t, env, `filter(seq, x => x > 2)`)
	evalRootExpectError(t, env, `reduce(seq, (acc, x) => acc + x, 0)`)
}

func TestMapFilterReduceRejectIterSeq2(t *testing.T) {
	seq := func(yield func(string, int64) bool) {
		yield("a", 1)
	}
	env := map[string]any{"seq": seq}

	evalRootExpectError(t, env, `map(seq, (k, v) => v * 10)`)
	evalRootExpectError(t, env, `filter(seq, (k, v) => v > 1)`)
	evalRootExpectError(t, env, `reduce(seq, (acc, k, v) => acc + v, 0)`)
}

// TestSumMinMaxNativeFastPath covers sumOrdered/foldExtremeOrdered/
// foldExtreme's []float64/[]int64/[]int fast-path branches in sumFunc and
// foldExtreme, reached only when a single argument is passed and it's
// already one of those three concrete slice types - none of which any
// existing test constructed (existing sum/min/max tests all go through
// the generic Combine-based path via list literals, which produce []any).
func TestSumMinMaxNativeFastPath(t *testing.T) {
	envF := map[string]any{"xs": []float64{1.5, 2.5, 3.0}}
	envI64 := map[string]any{"xs": []int64{1, 2, 3}}
	envI := map[string]any{"xs": []int{1, 2, 3}}

	if got := evalRoot(t, envF, `sum(xs)`); got != 7.0 {
		t.Errorf("sum([]float64) = %v, want 7.0", got)
	}
	if got := evalRoot(t, envI64, `sum(xs)`); got != int64(6) {
		t.Errorf("sum([]int64) = %v, want 6", got)
	}
	if got := evalRoot(t, envI, `sum(xs)`); got != 6 {
		t.Errorf("sum([]int) = %v, want 6", got)
	}

	if got := evalRoot(t, envF, `max(xs)`); got != 3.0 {
		t.Errorf("max([]float64) = %v, want 3.0", got)
	}
	if got := evalRoot(t, envI64, `min(xs)`); got != int64(1) {
		t.Errorf("min([]int64) = %v, want 1", got)
	}
	if got := evalRoot(t, envI, `max(xs)`); got != 3 {
		t.Errorf("max([]int) = %v, want 3", got)
	}
}

// TestFoldExtremeEmptyErrors covers the "at least 1 argument" guard shared
// by min/max's generic (non-fast-path) branch.
func TestFoldExtremeEmptyErrors(t *testing.T) {
	env := map[string]any{"xs": []any{}}
	evalRootExpectError(t, env, `max(xs)`)
	evalRootExpectError(t, env, `min(xs)`)
}

// TestLenOnReflectedTypes covers lenFunc's reflection fallback (a slice,
// array, or map type other than []any/map[string]any/string) and its
// unsupported-type error.
func TestLenOnReflectedTypes(t *testing.T) {
	env := map[string]any{
		"xs": [3]int{1, 2, 3},
		"m":  map[string]int{"a": 1, "b": 2},
	}
	if got := evalRoot(t, env, `len(xs)`); got != int64(3) {
		t.Errorf("len([3]int) = %v, want 3", got)
	}
	if got := evalRoot(t, env, `len(m)`); got != int64(2) {
		t.Errorf("len(map[string]int) = %v, want 2", got)
	}
}

func TestLenUnsupportedTypeErrors(t *testing.T) {
	env := map[string]any{"n": int64(5)}
	evalRootExpectError(t, env, `len(n)`)
}
