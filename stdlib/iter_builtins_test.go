package stdlib

import (
	"iter"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/cms103/owlexpr"
)

// evalIter parses, compiles, and runs input with IterBuiltins() enabled,
// failing the test on any parse/compile/run error.
func evalIter(t *testing.T, env map[string]any, input string) any {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(IterBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	return res
}

func evalIterExpectError(t *testing.T, env map[string]any, input string) {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(IterBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatalf("expected an error for %q, got none", input)
	}
}

func TestIterBuiltinsNotRegisteredByDefault(t *testing.T) {
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	instructions, err := owlexpr.Compile(`iter.of([1]) `)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Fatalf("expected iter.of() to be undefined without IterBuiltins()")
	}
}

func TestIterOf(t *testing.T) {
	env := map[string]any{"xs": []any{int64(1), int64(2), int64(3)}}
	got := evalIter(t, env, `iter.toList(iter.of(xs))`).([]any)
	want := []any{int64(1), int64(2), int64(3)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("iter.toList(iter.of(xs)) = %v, want %v", got, want)
	}
}

func TestIterOfRejectsNonList(t *testing.T) {
	evalIterExpectError(t, nil, `iter.of(5)`)
}

// TestIterKeysValuesEntries covers the three map-shaped producers -
// migrated from list_builtins_test.go's TestKeysValuesOverOtherMapShapes,
// which used to exercise this same iter.Seq2 shape directly against
// list.keys/list.values before those became list/map-only.
func TestIterKeysValuesEntries(t *testing.T) {
	env := map[string]any{"m": map[string]any{"a": int64(1), "b": int64(2)}}

	keys := evalIter(t, env, `iter.toList(iter.keys(m))`).([]any)
	if len(keys) != 2 {
		t.Errorf("iter.keys(m) = %v, want 2 entries", keys)
	}

	values := evalIter(t, env, `iter.toList(iter.values(m))`).([]any)
	if len(values) != 2 {
		t.Errorf("iter.values(m) = %v, want 2 entries", values)
	}

	entries := evalIter(t, env, `iter.toList(iter.entries(m))`).([]any)
	if len(entries) != 2 {
		t.Fatalf("iter.entries(m) = %v, want 2 entries", entries)
	}
	for _, e := range entries {
		pair, ok := e.([]any)
		if !ok || len(pair) != 2 {
			t.Errorf("iter.entries(m) element = %v, want a [k, v] pair", e)
		}
	}
}

func TestIterKeysValuesEntriesRejectNonMap(t *testing.T) {
	evalIterExpectError(t, nil, `iter.keys(5)`)
	evalIterExpectError(t, nil, `iter.values(5)`)
	evalIterExpectError(t, nil, `iter.entries(5)`)
}

// TestIterMapFilterReduceOverSeq and TestIterMapFilterReduceOverSeq2
// cover iter.map/iter.filter/iter.reduce against a raw Go
// iter.Seq[T]/iter.Seq2[K,V] value from the environment
func TestIterMapFilterReduceOverSeq(t *testing.T) {
	env := map[string]any{"seq": slices.Values([]int64{1, 2, 3, 4})}

	mapped := evalIter(t, env, `iter.toList(iter.map(seq, x => x * 10))`).([]any)
	want := []any{int64(10), int64(20), int64(30), int64(40)}
	if !reflect.DeepEqual(mapped, want) {
		t.Errorf("iter.map(seq, ...) = %v, want %v", mapped, want)
	}

	filtered := evalIter(t, env, `iter.toList(iter.filter(seq, x => x > 2))`).([]any)
	want = []any{int64(3), int64(4)}
	if !reflect.DeepEqual(filtered, want) {
		t.Errorf("iter.filter(seq, x > 2) = %v, want %v", filtered, want)
	}

	reduced := evalIter(t, env, `iter.reduce(seq, (acc, x) => acc + x, 0)`)
	if reduced != int64(10) {
		t.Errorf("iter.reduce(seq, +, 0) = %v, want 10", reduced)
	}
}

func TestIterMapFilterReduceOverSeq2(t *testing.T) {
	env := map[string]any{"seq": maps.All(map[string]int64{"a": 1, "b": 2, "c": 3})}

	mapped := evalIter(t, env, `iter.toList(iter.map(seq, (k, v) => v * 10))`).([]any)
	sum := int64(0)
	for _, v := range mapped {
		sum += v.(int64)
	}
	if len(mapped) != 3 || sum != 60 {
		t.Errorf("iter.map(seq, (k,v) => v*10) = %v, want three elements summing to 60", mapped)
	}

	filtered := evalIter(t, env, `iter.toMap(iter.filter(seq, (k, v) => v > 1))`)
	m, ok := filtered.(map[string]any)
	if !ok || len(m) != 2 || m["b"] != int64(2) || m["c"] != int64(3) {
		t.Errorf("iter.toMap(iter.filter(seq, (k,v) => v > 1)) = %v, want map{b:2 c:3}", filtered)
	}

	reduced := evalIter(t, env, `iter.reduce(seq, (acc, k, v) => acc + v, 0)`)
	if reduced != int64(6) {
		t.Errorf("iter.reduce(seq, (acc,k,v) => acc+v, 0) = %v, want 6", reduced)
	}
}

// TestIterPipelineIsLazy proves iter.map/iter.filter don't touch source
// until something actually pulls (iter.toList here) - a call counter
// incremented from inside the mapped lambda stays at zero right after
// iter.map/iter.filter return, and only advances once iter.toList drives
// the chain.
func TestIterPipelineIsLazy(t *testing.T) {
	calls := 0
	env := map[string]any{
		"xs": []any{int64(1), int64(2), int64(3)},
		"track": func(x int64) int64 {
			calls++
			return x
		},
	}
	instructions, err := owlexpr.Compile(`iter.filter(iter.map(iter.of(xs), x => track(x)), x => x > 1)`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	mc, err := owlexpr.NewVM(IterBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	pipeline, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if calls != 0 {
		t.Fatalf("track() called %d times before the pipeline was ever driven, want 0", calls)
	}

	seq, ok := pipeline.(iter.Seq[any])
	if !ok {
		t.Fatalf("iter.filter(iter.map(...)) returned %T, want an iter.Seq[any]", pipeline)
	}
	var out []any
	seq(func(el any) bool {
		out = append(out, el)
		return true
	})
	if calls != 3 {
		t.Errorf("track() called %d times after driving the pipeline, want 3", calls)
	}
	if !reflect.DeepEqual(out, []any{int64(2), int64(3)}) {
		t.Errorf("driven pipeline = %v, want [2 3]", out)
	}
}

// TestIterMapErrorSurfacesAtTerminal proves a lambda error deep inside a
// lazy iter.map/iter.filter chain surfaces as an ordinary returned error
// from the terminal op (iter.toList here), not a panic escaping to the
// caller - the panic+recover channel described in recoverIterErr's doc
// comment.
func TestIterMapErrorSurfacesAtTerminal(t *testing.T) {
	env := map[string]any{"xs": []any{int64(1), int64(2), int64(0), int64(3)}}
	evalIterExpectError(t, env, `iter.toList(iter.map(iter.of(xs), x => 10 / x))`)
}

// TestIterMapConstructionSucceedsEvenIfFnWouldFail proves iter.map itself
// never touches source - it returns a valid, undriven iter.Seq value even
// when fn would fail on the very first element, since nothing has pulled
// from it yet.
func TestIterMapConstructionSucceedsEvenIfFnWouldFail(t *testing.T) {
	env := map[string]any{"xs": []any{int64(0)}}
	got := evalIter(t, env, `iter.map(iter.of(xs), x => 10 / x)`)
	if _, ok := got.(iter.Seq[any]); !ok {
		t.Fatalf("iter.map(iter.of(xs), badFn) = %T, want an iter.Seq[any]", got)
	}
}

func TestIterTakeSkip(t *testing.T) {
	env := map[string]any{"xs": []any{int64(1), int64(2), int64(3), int64(4), int64(5)}}

	taken := evalIter(t, env, `iter.toList(iter.take(iter.of(xs), 2))`).([]any)
	if !reflect.DeepEqual(taken, []any{int64(1), int64(2)}) {
		t.Errorf("iter.take(iter.of(xs), 2) = %v, want [1 2]", taken)
	}

	skipped := evalIter(t, env, `iter.toList(iter.skip(iter.of(xs), 3))`).([]any)
	if !reflect.DeepEqual(skipped, []any{int64(4), int64(5)}) {
		t.Errorf("iter.skip(iter.of(xs), 3) = %v, want [4 5]", skipped)
	}

	taken = evalIter(t, env, `iter.toList(iter.take(iter.of(xs), 0))`).([]any)
	if len(taken) != 0 {
		t.Errorf("iter.take(iter.of(xs), 0) = %v, want []", taken)
	}
}

// TestIterTakeSkipOverSeq2 proves take/skip accept a paired source too,
// flattening to [k, v] elements like iter.map/iter.filter do. Uses a
// hand-built iter.Seq2 (index/value pairs off a slice) rather than a real
// Go map, since take/skip's result is only meaningful against an ordered
// source - unlike a map's randomized range order, this one iterates in a
// well-defined order, proving ordering is a property of the specific
// source, not something iter.Seq2 forecloses (see iterTakeFunc's doc
// comment).
func TestIterTakeSkipOverSeq2(t *testing.T) {
	orderedPairs := func(yield func(int, string) bool) {
		for i, s := range []string{"a", "b", "c", "d", "e"} {
			if !yield(i, s) {
				return
			}
		}
	}
	env := map[string]any{"seq": orderedPairs}

	taken := evalIter(t, env, `iter.toList(iter.take(seq, 2))`).([]any)
	want := []any{[]any{0, "a"}, []any{1, "b"}}
	if !reflect.DeepEqual(taken, want) {
		t.Errorf("iter.take(seq, 2) = %v, want %v", taken, want)
	}

	skipped := evalIter(t, env, `iter.toList(iter.skip(seq, 3))`).([]any)
	want = []any{[]any{3, "d"}, []any{4, "e"}}
	if !reflect.DeepEqual(skipped, want) {
		t.Errorf("iter.skip(seq, 3) = %v, want %v", skipped, want)
	}
}

// TestIterTakeSkipRejectPlainList covers iter.take/iter.skip's iterator-
// only guard - unlike a producer (iter.of), these combinators don't
// accept a plain list directly (see IterBuiltins' own doc comment).
func TestIterTakeSkipRejectPlainList(t *testing.T) {
	env := map[string]any{"xs": []any{int64(1), int64(2)}}
	evalIterExpectError(t, env, `iter.toList(iter.take(xs, 1))`)
	evalIterExpectError(t, env, `iter.toList(iter.skip(xs, 1))`)
}

// TestIterTakeStopsPullingSource proves iter.take(source, n) only pulls n
// elements from source, not the whole thing - the memory/latency-saving
// property this whole pack exists for.
func TestIterTakeStopsPullingSource(t *testing.T) {
	pulls := 0
	seq := func(yield func(int64) bool) {
		for i := int64(1); i <= 1000; i++ {
			pulls++
			if !yield(i) {
				return
			}
		}
	}
	env := map[string]any{"seq": seq}
	got := evalIter(t, env, `iter.toList(iter.take(seq, 3))`).([]any)
	if !reflect.DeepEqual(got, []any{int64(1), int64(2), int64(3)}) {
		t.Errorf("iter.take(seq, 3) = %v, want [1 2 3]", got)
	}
	if pulls != 3 {
		t.Errorf("source pulled %d times, want exactly 3", pulls)
	}
}

func TestIterCountFirstFindAnyAllNone(t *testing.T) {
	env := map[string]any{"seq": slices.Values([]int64{1, 2, 3, 4})}

	if got := evalIter(t, env, `iter.count(seq)`); got != int64(4) {
		t.Errorf("iter.count(seq) = %v, want 4", got)
	}
	if got := evalIter(t, env, `iter.first(seq)`); got != int64(1) {
		t.Errorf("iter.first(seq) = %v, want 1", got)
	}
	if got := evalIter(t, env, `iter.find(seq, x => x > 2)`); got != int64(3) {
		t.Errorf("iter.find(seq, x > 2) = %v, want 3", got)
	}
	if got := evalIter(t, env, `iter.any(seq, x => x > 3)`); got != true {
		t.Errorf("iter.any(seq, x > 3) = %v, want true", got)
	}
	if got := evalIter(t, env, `iter.all(seq, x => x > 0)`); got != true {
		t.Errorf("iter.all(seq, x > 0) = %v, want true", got)
	}
	if got := evalIter(t, env, `iter.none(seq, x => x > 10)`); got != true {
		t.Errorf("iter.none(seq, x > 10) = %v, want true", got)
	}
}

// TestIterFindAnyAllNoneOverSeq2 proves find/any/all/none accept a paired
// source too, same as iter.map/iter.filter/iter.reduce - fn is called as
// fn(k, v), and iter.find returns a flattened [k, v] pair on a match (see
// iterFindFunc's doc comment).
func TestIterFindAnyAllNoneOverSeq2(t *testing.T) {
	env := map[string]any{"seq": maps.All(map[string]int64{"a": 1, "b": 2, "c": 3})}

	found := evalIter(t, env, `iter.find(seq, (k, v) => v == 2)`).([]any)
	if !reflect.DeepEqual(found, []any{"b", int64(2)}) {
		t.Errorf(`iter.find(seq, (k,v) => v == 2) = %v, want [b 2]`, found)
	}
	if got := evalIter(t, env, `iter.find(seq, (k, v) => v > 10)`); got != nil {
		t.Errorf("iter.find(seq, (k,v) => v > 10) = %v, want nil", got)
	}

	if got := evalIter(t, env, `iter.any(seq, (k, v) => v > 2)`); got != true {
		t.Errorf("iter.any(seq, (k,v) => v > 2) = %v, want true", got)
	}
	if got := evalIter(t, env, `iter.any(seq, (k, v) => v > 10)`); got != false {
		t.Errorf("iter.any(seq, (k,v) => v > 10) = %v, want false", got)
	}

	if got := evalIter(t, env, `iter.all(seq, (k, v) => v > 0)`); got != true {
		t.Errorf("iter.all(seq, (k,v) => v > 0) = %v, want true", got)
	}
	if got := evalIter(t, env, `iter.all(seq, (k, v) => v > 1)`); got != false {
		t.Errorf("iter.all(seq, (k,v) => v > 1) = %v, want false", got)
	}

	if got := evalIter(t, env, `iter.none(seq, (k, v) => v > 10)`); got != true {
		t.Errorf("iter.none(seq, (k,v) => v > 10) = %v, want true", got)
	}
	if got := evalIter(t, env, `iter.none(seq, (k, v) => v > 2)`); got != false {
		t.Errorf("iter.none(seq, (k,v) => v > 2) = %v, want false", got)
	}
}

// TestIterCountOverSeq2 proves iter.count accepts a paired source too -
// unlike find/any/all/none it doesn't call fn per element at all, so there
// was never a shape-arity reason for it to have stayed Seq-only.
func TestIterCountOverSeq2(t *testing.T) {
	env := map[string]any{"seq": maps.All(map[string]int64{"a": 1, "b": 2, "c": 3})}
	if got := evalIter(t, env, `iter.count(seq)`); got != int64(3) {
		t.Errorf("iter.count(seq) = %v, want 3", got)
	}
}

// TestIterFirstOverSeq2 proves iter.first accepts a paired source too,
// flattening to [k, v] the same way iter.find does - a single-entry map
// keeps the result deterministic despite a real map's randomized range
// order.
func TestIterFirstOverSeq2(t *testing.T) {
	env := map[string]any{"seq": maps.All(map[string]int64{"a": 1})}
	got := evalIter(t, env, `iter.first(seq)`).([]any)
	want := []any{"a", int64(1)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("iter.first(seq) = %v, want %v", got, want)
	}
}

func TestIterFirstOverEmpty(t *testing.T) {
	env := map[string]any{"empty": []any{}}
	got := evalIter(t, env, `iter.first(iter.of(empty))`)
	if got != nil {
		t.Errorf("iter.first(iter.of(empty)) = %v, want nil", got)
	}
}

// TestIterContainsShortCircuits and TestIterContainsOverPairSource migrate
// langtest/in_test.go's former TestInShortCircuits and
// vm/in_extra_test.go's former TestInOverIterSeq2PairSource - `in` used
// to accept and short-circuit-scan either shape directly; iter.contains is now
// the explicit way to do that
func TestIterContainsShortCircuits(t *testing.T) {
	pulls := 0
	seq := func(yield func(int64) bool) {
		for i := int64(1); i <= 1000; i++ {
			pulls++
			if !yield(i) {
				return
			}
		}
	}
	env := map[string]any{"seq": seq}

	if got := evalIter(t, env, `iter.contains(5, seq)`); got != true {
		t.Fatalf("iter.contains(5, seq) = %v, want true", got)
	}
	if pulls != 5 {
		t.Errorf("expected exactly 5 elements to be pulled before stopping at the match, got %d", pulls)
	}
}

func TestIterContainsOverPairSource(t *testing.T) {
	env := map[string]any{"pairs": maps.All(map[string]int64{"a": 1, "b": 2})}

	if got := evalIter(t, env, `iter.contains("b", pairs)`); got != true {
		t.Errorf(`iter.contains("b", pairs) = %v, want true`, got)
	}
	if got := evalIter(t, env, `iter.contains("z", pairs)`); got != false {
		t.Errorf(`iter.contains("z", pairs) = %v, want false`, got)
	}
}

// TestIterToMapNonComparableKeyErrors migrates root's former
// TestFilterMapNonComparableKeyErrors - core filter() no longer accepts
// an iter.Seq2 at all (see builtin_funcs_test.go's
// TestFilterRejectsIterSeq2), so iter.toMap is the only place left that
// can actually reach a non-comparable key: a real Go map can never hold
// one to begin with (Go itself forbids constructing one), only an
// iter.Seq2 - which isn't backed by a real map - has no such restriction
// on what it yields.
func TestIterToMapNonComparableKeyErrors(t *testing.T) {
	env := map[string]any{"seq": func(yield func([]int, string) bool) {
		yield([]int{1, 2}, "x")
	}}
	evalIterExpectError(t, env, `iter.toMap(seq)`)
}

func TestIterToMapNonStringKeys(t *testing.T) {
	env := map[string]any{"seq": func(yield func(int64, string) bool) {
		yield(1, "a")
		yield(2, "b")
	}}
	got, ok := evalIter(t, env, `iter.toMap(seq)`).(map[any]any)
	if !ok {
		t.Fatalf("iter.toMap(seq) returned %T, want map[any]any", got)
	}
	if got[int64(1)] != "a" || got[int64(2)] != "b" {
		t.Errorf("iter.toMap(seq) = %v", got)
	}
}

func TestIterArgumentCountErrors(t *testing.T) {
	cases := []string{
		`iter.of([1], [2])`,
		`iter.keys()`,
		`iter.values()`,
		`iter.entries()`,
		`iter.map([1])`,
		`iter.filter([1])`,
		`iter.take([1])`,
		`iter.skip([1])`,
		`iter.toList()`,
		`iter.toMap()`,
		`iter.reduce([1])`,
		`iter.count()`,
		`iter.first()`,
		`iter.find([1])`,
		`iter.any([1])`,
		`iter.all([1])`,
		`iter.none([1])`,
		`iter.contains(1)`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			evalIterExpectError(t, nil, input)
		})
	}
}
