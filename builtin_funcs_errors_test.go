package owlexpr

import "testing"

// TestLenArgCountErrors covers lenFunc's "expects 1 argument" guard.
func TestLenArgCountErrors(t *testing.T) {
	evalRootExpectError(t, nil, `len()`)
}

// TestLenOnLiterals rounds out lenFunc's own-literal-type fast paths
// (string/[]any/map[string]any), which existing tests reached only via
// reflection (a [3]int array, a map[string]int).
func TestLenOnLiterals(t *testing.T) {
	if got := evalRoot(t, nil, `len("hello")`); got != int64(5) {
		t.Errorf("len(string) = %v, want 5", got)
	}
	if got := evalRoot(t, nil, `len([1, 2, 3])`); got != int64(3) {
		t.Errorf("len(list literal) = %v, want 3", got)
	}
	if got := evalRoot(t, nil, `len({a: 1, b: 2})`); got != int64(2) {
		t.Errorf("len(map literal) = %v, want 2", got)
	}
}

// TestSumEmptyListIsZero covers sumFunc's zero-values guard.
func TestSumEmptyListIsZero(t *testing.T) {
	if got := evalRoot(t, nil, `sum([])`); got != int64(0) {
		t.Errorf("sum([]) = %v, want 0", got)
	}
}

// TestSumSpreadArgs covers sum() called with multiple positional
// arguments (sum(a, b, c)) rather than a single list argument -
// builtinValues' "args as-is" branch.
func TestSumSpreadArgs(t *testing.T) {
	if got := evalRoot(t, nil, `sum(1, 2, 3)`); got != int64(6) {
		t.Errorf("sum(1, 2, 3) = %v, want 6", got)
	}
}

// TestSumCombineErrorPropagates covers sumFunc's Combine-error wrapping -
// summing two values with no registered operation between them.
func TestSumCombineErrorPropagates(t *testing.T) {
	evalRootExpectError(t, nil, `sum([true, false])`)
}

// TestMapArgCountErrors, TestFilterArgCountErrors, and
// TestReduceArgCountErrors cover each higher-order builtin's own arg-count
// guard.
func TestMapArgCountErrors(t *testing.T) {
	evalRootExpectError(t, nil, `map([1, 2])`)
}

func TestFilterArgCountErrors(t *testing.T) {
	evalRootExpectError(t, nil, `filter([1, 2])`)
}

func TestReduceArgCountErrors(t *testing.T) {
	evalRootExpectError(t, nil, `reduce([1, 2], (acc, x) => acc)`)
}

// TestMapFilterReduceRejectUnsupportedSource covers the "first argument
// must be a list, map, or iterator" error each of the three higher-order
// builtins produces for a source type that's none of those.
func TestMapFilterReduceRejectUnsupportedSource(t *testing.T) {
	env := map[string]any{"n": int64(5)}
	evalRootExpectError(t, env, `map(n, x => x)`)
	evalRootExpectError(t, env, `filter(n, x => true)`)
	evalRootExpectError(t, env, `reduce(n, (acc, x) => acc, 0)`)
}

// TestMapFilterReduceOnEmptyList covers the empty-list output branches:
// map/filter must return an empty []any (not nil), and reduce must return
// the initial accumulator untouched.
func TestMapFilterReduceOnEmptyList(t *testing.T) {
	if got := evalRoot(t, nil, `map([], x => x)`); len(got.([]any)) != 0 {
		t.Errorf("map([], ...) = %v, want empty list", got)
	}
	if got := evalRoot(t, nil, `filter([], x => true)`); len(got.([]any)) != 0 {
		t.Errorf("filter([], ...) = %v, want empty list", got)
	}
	if got := evalRoot(t, nil, `reduce([], (acc, x) => acc, 42)`); got != int64(42) {
		t.Errorf("reduce([], ..., 42) = %v, want 42", got)
	}
}

// TestMapCallErrorPropagates covers mapFunc's callErr wrapping for both a
// list source and a paired (map) source - an error raised while
// evaluating the lambda body must abort map() with that error, not swallow
// it or partially return.
func TestMapCallErrorPropagates(t *testing.T) {
	evalRootExpectError(t, nil, `map([1, 2], x => x / 0)`)
	env := map[string]any{"m": map[string]any{"a": int64(1)}}
	evalRootExpectError(t, env, `map(m, (k, v) => v / 0)`)
}

// TestFilterPredicateErrors covers filterFunc's two failure modes for both
// source shapes: the predicate itself erroring, and the predicate
// returning a non-bool value.
func TestFilterPredicateErrors(t *testing.T) {
	t.Run("list source: predicate call errors", func(t *testing.T) {
		evalRootExpectError(t, nil, `filter([1, 2], x => x / 0)`)
	})
	t.Run("list source: predicate returns non-bool", func(t *testing.T) {
		evalRootExpectError(t, nil, `filter([1, 2], x => x)`)
	})
	env := map[string]any{"m": map[string]any{"a": int64(1)}}
	t.Run("map source: predicate call errors", func(t *testing.T) {
		evalRootExpectError(t, env, `filter(m, (k, v) => v / 0)`)
	})
	t.Run("map source: predicate returns non-bool", func(t *testing.T) {
		evalRootExpectError(t, env, `filter(m, (k, v) => v)`)
	})
}

// TestReduceCallErrorPropagates mirrors TestMapCallErrorPropagates for
// reduce()'s two source shapes.
func TestReduceCallErrorPropagates(t *testing.T) {
	evalRootExpectError(t, nil, `reduce([1, 2], (acc, x) => x / 0, 0)`)
	env := map[string]any{"m": map[string]any{"a": int64(1)}}
	evalRootExpectError(t, env, `reduce(m, (acc, k, v) => v / 0, 0)`)
}

// TestFoldExtremeGenericPath covers max/min's generic (non-fast-path)
// branch, reached via spread arguments or a []any list, as opposed to the
// []float64/[]int64/[]int native fast path TestSumMinMaxNativeFastPath
// already covers.
func TestFoldExtremeGenericPath(t *testing.T) {
	if got := evalRoot(t, nil, `max(1, 5, 3)`); got != int64(5) {
		t.Errorf("max(1, 5, 3) = %v, want 5", got)
	}
	if got := evalRoot(t, nil, `min(1, 5, 3)`); got != int64(1) {
		t.Errorf("min(1, 5, 3) = %v, want 1", got)
	}
	if got := evalRoot(t, nil, `max([1, 5, 3])`); got != int64(5) {
		t.Errorf("max([1, 5, 3]) = %v, want 5", got)
	}
}

// TestFoldExtremeOrderedEmptyErrors covers foldExtremeOrdered's own
// "expects at least 1 argument" guard on the native-fast-path side (an
// empty []float64/[]int64/[]int), distinct from the generic path's
// equivalent guard TestFoldExtremeEmptyErrors already covers.
func TestFoldExtremeOrderedEmptyErrors(t *testing.T) {
	env := map[string]any{"xs": []float64{}}
	evalRootExpectError(t, env, `max(xs)`)
}

// TestFoldExtremeCombineErrorPropagates covers foldExtreme's Combine-error
// wrapping in the generic path.
func TestFoldExtremeCombineErrorPropagates(t *testing.T) {
	evalRootExpectError(t, nil, `max(true, false)`)
}
