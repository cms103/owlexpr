package stdlib

import (
	"reflect"
	"testing"

	"github.com/cms103/owlexpr"
)

// evalList parses, compiles, and runs input with ListBuiltins() enabled,
// failing the test on any parse/compile/run error.
func evalList(t *testing.T, env map[string]any, input string) any {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(ListBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	return res
}

func evalListExpectError(t *testing.T, env map[string]any, input string) {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(ListBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatalf("expected an error for %q, got none", input)
	}
}

func TestListBuiltinsNotRegisteredByDefault(t *testing.T) {
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	instructions, err := owlexpr.Compile(`list.any([1], x => true)`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Fatalf("expected list.any() to be undefined without ListBuiltins()")
	}
}

func TestAllAnyNoneOne(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{`list.all([2, 4, 6], x => x % 2 == 0)`, true},
		{`list.all([2, 4, 5], x => x % 2 == 0)`, false},
		{`list.all([], x => false)`, true}, // vacuously true
		{`list.any([1, 2, 3], x => x > 2)`, true},
		{`list.any([1, 2, 3], x => x > 5)`, false},
		{`list.any([], x => true)`, false},
		{`list.none([1, 2, 3], x => x > 5)`, true},
		{`list.none([1, 2, 3], x => x > 2)`, false},
		{`list.one([1, 2, 3], x => x == 2)`, true},
		{`list.one([1, 2, 2], x => x == 2)`, false},
		{`list.one([1, 3], x => x == 2)`, false},
	}
	for _, c := range cases {
		if got := evalList(t, nil, c.input); got != c.want {
			t.Errorf("%s = %v, want %v", c.input, got, c.want)
		}
	}
}

// TestCount is list.count, distinct from string.count's substring-count -
// see ListBuiltins' doc comment for the collision namespacing resolved.
func TestCount(t *testing.T) {
	if got := evalList(t, nil, `list.count([1, 2, 3, 4], x => x % 2 == 0)`); got != int64(2) {
		t.Errorf("list.count(evens) = %v, want 2", got)
	}
	if got := evalList(t, nil, `list.count([1, 3, 5], x => x % 2 == 0)`); got != int64(0) {
		t.Errorf("list.count(none matching) = %v, want 0", got)
	}
	if got := evalList(t, nil, `list.count([], x => true)`); got != int64(0) {
		t.Errorf("list.count([]) = %v, want 0", got)
	}

	// count never short-circuits - every element must be visited, unlike
	// any()'s stop-at-first-match behavior (see TestAllAnyNoneShortCircuit).
	calls := 0
	track := func(x int64) bool {
		calls++
		return x%2 == 0
	}
	env := map[string]any{"nums": []any{int64(1), int64(2), int64(3), int64(4)}, "track": track}
	if got := evalList(t, env, `list.count(nums, x => track(x))`); got != int64(2) {
		t.Fatalf("list.count() = %v, want 2", got)
	}
	if calls != 4 {
		t.Fatalf("list.count() called predicate %d times, want 4 (no short-circuit)", calls)
	}

	// Both string.count and list.count coexist once StringBuiltins() is
	// also loaded - the exact collision namespacing was meant to resolve.
	instructions, err := owlexpr.Compile(`list.count([1, 2, 2], x => x == 2) + string.count("aaa", "a")`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	mc, err := owlexpr.NewVM(ListBuiltins(), StringBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if got, err := mc.Run(instructions, nil); err != nil {
		t.Fatalf("run: %v", err)
	} else if got != int64(5) {
		t.Errorf("list.count + string.count = %v, want 5", got)
	}
}

func TestAllAnyNoneShortCircuit(t *testing.T) {
	// track counts how many elements the predicate actually gets called
	// on; if list.any() visited every element instead of stopping at the
	// first match, calls would end up at len(nums) instead of 1.
	calls := 0
	track := func(x int64) bool {
		calls++
		return x == 1
	}
	env := map[string]any{"nums": []any{int64(1), int64(0), int64(0)}, "track": track}

	if got := evalList(t, env, `list.any(nums, x => track(x))`); got != true {
		t.Fatalf("list.any() = %v, want true", got)
	}
	if calls != 1 {
		t.Fatalf("list.any() called predicate %d times, want 1 (should short-circuit on first match)", calls)
	}
}

func TestFindFamily(t *testing.T) {
	env := map[string]any{"nums": []any{int64(1), int64(2), int64(3), int64(4)}}

	if got := evalList(t, env, `list.find(nums, x => x > 2)`); got != int64(3) {
		t.Errorf("list.find() = %v, want 3", got)
	}
	if got := evalList(t, env, `list.find(nums, x => x > 10)`); got != nil {
		t.Errorf("list.find() = %v, want nil", got)
	}
	if got := evalList(t, env, `list.findIndex(nums, x => x > 2)`); got != int64(2) {
		t.Errorf("list.findIndex() = %v, want 2", got)
	}
	if got := evalList(t, env, `list.findIndex(nums, x => x > 10)`); got != int64(-1) {
		t.Errorf("list.findIndex() = %v, want -1", got)
	}
	if got := evalList(t, env, `list.findLast(nums, x => x < 3)`); got != int64(2) {
		t.Errorf("list.findLast() = %v, want 2", got)
	}
	if got := evalList(t, env, `list.findLastIndex(nums, x => x < 3)`); got != int64(1) {
		t.Errorf("list.findLastIndex() = %v, want 1", got)
	}
	if got := evalList(t, env, `list.findLast(nums, x => x > 10)`); got != nil {
		t.Errorf("list.findLast() = %v, want nil", got)
	}
	if got := evalList(t, env, `list.findLastIndex(nums, x => x > 10)`); got != int64(-1) {
		t.Errorf("list.findLastIndex() = %v, want -1", got)
	}
}

func TestGroupBy(t *testing.T) {
	got := evalList(t, nil, `list.groupBy([1, 2, 3, 4], x => x % 2 == 0 ? "even" : "odd")`)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("list.groupBy() returned %T, want map[string]any", got)
	}
	if !reflect.DeepEqual(m["even"], []any{int64(2), int64(4)}) {
		t.Errorf("list.groupBy()[even] = %v", m["even"])
	}
	if !reflect.DeepEqual(m["odd"], []any{int64(1), int64(3)}) {
		t.Errorf("list.groupBy()[odd] = %v", m["odd"])
	}
}

// TestGroupByNumericKey covers grouping by a non-string key (e.g. an int
// field like a user's age) - the result should come back as map[any]any,
// and be directly indexable/searchable with that key, matching sortBy's
// existing support for non-string key functions.
func TestGroupByNumericKey(t *testing.T) {
	got := evalList(t, nil, `list.groupBy([1, 2, 3, 4, 5, 6], x => x % 3)`)
	m, ok := got.(map[any]any)
	if !ok {
		t.Fatalf("list.groupBy() returned %T, want map[any]any", got)
	}
	if !reflect.DeepEqual(m[int64(0)], []any{int64(3), int64(6)}) {
		t.Errorf("list.groupBy()[0] = %v", m[int64(0)])
	}
	if !reflect.DeepEqual(m[int64(1)], []any{int64(1), int64(4)}) {
		t.Errorf("list.groupBy()[1] = %v", m[int64(1)])
	}

	// Indexing straight into the result with a numeric key, and `in`
	// against it, both need to work with no other changes - see
	// indexValue/inValue's existing reflect.Map fallback.
	env := map[string]any{"groups": m}
	if got := evalList(t, env, `groups[1]`); !reflect.DeepEqual(got, []any{int64(1), int64(4)}) {
		t.Errorf("groups[1] = %v", got)
	}
	if got := evalList(t, env, `1 in groups`); got != true {
		t.Errorf("1 in groups = %v, want true", got)
	}
	if got := evalList(t, env, `9 in groups`); got != false {
		t.Errorf("9 in groups = %v, want false", got)
	}
}

// TestGroupByNonComparableKeyErrors proves a key function returning
// something that can't be a Go map key (a list) produces a clean error
// instead of panicking the whole program.
func TestGroupByNonComparableKeyErrors(t *testing.T) {
	evalListExpectError(t, nil, `list.groupBy([1, 2], x => [x])`)
}

func TestSortAndSortBy(t *testing.T) {
	got := evalList(t, nil, `list.sort([3, 1, 2])`)
	if !reflect.DeepEqual(got, []any{int64(1), int64(2), int64(3)}) {
		t.Errorf("list.sort() = %v", got)
	}

	got = evalList(t, nil, `list.sortBy([3, 1, 2], x => -x)`)
	if !reflect.DeepEqual(got, []any{int64(3), int64(2), int64(1)}) {
		t.Errorf("list.sortBy() = %v", got)
	}
}

func TestReverseListFlattenConcat(t *testing.T) {
	if got := evalList(t, nil, `list.reverse([1, 2, 3])`); !reflect.DeepEqual(got, []any{int64(3), int64(2), int64(1)}) {
		t.Errorf("list.reverse() = %v", got)
	}
	if got := evalList(t, nil, `list.flatten([1, [2, 3], [4, [5, 6]]])`); !reflect.DeepEqual(got, []any{int64(1), int64(2), int64(3), int64(4), int64(5), int64(6)}) {
		t.Errorf("list.flatten() = %v", got)
	}
	if got := evalList(t, nil, `list.concat([1, 2], [3], [])`); !reflect.DeepEqual(got, []any{int64(1), int64(2), int64(3)}) {
		t.Errorf("list.concat() = %v", got)
	}
}

func TestZip(t *testing.T) {
	got := evalList(t, nil, `list.zip([1, 2, 3], ["a", "b", "c"])`)
	want := []any{
		[]any{int64(1), "a"},
		[]any{int64(2), "b"},
		[]any{int64(3), "c"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("list.zip() = %v, want %v", got, want)
	}
}

// TestZipThreeLists covers more than two lists at once.
func TestZipThreeLists(t *testing.T) {
	got := evalList(t, nil, `list.zip([1, 2], [3, 4], [5, 6])`)
	want := []any{
		[]any{int64(1), int64(3), int64(5)},
		[]any{int64(2), int64(4), int64(6)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("list.zip() = %v, want %v", got, want)
	}
}

// TestZipStopsAtShortest confirms zip follows Python's convention of
// truncating to the shortest input list rather than padding with nil.
func TestZipStopsAtShortest(t *testing.T) {
	got := evalList(t, nil, `list.zip([1, 2, 3], ["a", "b"])`)
	want := []any{
		[]any{int64(1), "a"},
		[]any{int64(2), "b"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("list.zip() = %v, want %v", got, want)
	}
}

func TestZipRequiresAtLeastTwoLists(t *testing.T) {
	evalListExpectError(t, nil, `list.zip([1, 2])`)
}

func TestZipRejectsNonList(t *testing.T) {
	evalListExpectError(t, nil, `list.zip([1, 2], "not a list")`)
}

func TestUniq(t *testing.T) {
	got := evalList(t, nil, `list.uniq([1, 2, 2, 3, 1, 3, 3])`)
	if !reflect.DeepEqual(got, []any{int64(1), int64(2), int64(3)}) {
		t.Errorf("list.uniq() = %v, want first-occurrence order [1, 2, 3]", got)
	}
}

// TestUniqExactEqualityNotCoercive confirms uniq's equality is exact Go
// `==`, not Combine(OpEqual)'s cross-type coercion - int64(2) and
// float64(2.0) are distinct elements here, unlike the `in` operator.
func TestUniqExactEqualityNotCoercive(t *testing.T) {
	got := evalList(t, nil, `list.uniq([2, 2.0])`)
	if !reflect.DeepEqual(got, []any{int64(2), 2.0}) {
		t.Errorf("list.uniq() = %v, want both int64(2) and float64(2.0) kept", got)
	}
}

func TestUniqRejectsNonList(t *testing.T) {
	evalListExpectError(t, nil, `list.uniq("not a list")`)
}

func TestUniqNonComparableElementErrors(t *testing.T) {
	env := map[string]any{"lst": []any{[]any{1, 2}}}
	evalListExpectError(t, env, `list.uniq(lst)`)
}

func TestKeysAndValues(t *testing.T) {
	env := map[string]any{"m": map[string]any{"a": int64(1), "b": int64(2)}}
	keys := evalList(t, env, `list.keys(m)`).([]any)
	values := evalList(t, env, `list.values(m)`).([]any)
	if len(keys) != 2 || len(values) != 2 {
		t.Fatalf("list.keys()=%v list.values()=%v, want 2 entries each", keys, values)
	}
}

func TestMeanAndMedian(t *testing.T) {
	if got := evalList(t, nil, `list.mean([1, 2, 3])`); got != int64(2) {
		t.Errorf("list.mean() = %v, want 2", got)
	}
	if got := evalList(t, nil, `list.mean([1, 2, 4])`); got != int64(2) {
		t.Errorf("list.mean() = %v, want 2 (integer truncation)", got)
	}
	if got := evalList(t, nil, `list.median([1, 3, 2])`); got != int64(2) {
		t.Errorf("list.median() = %v, want 2", got)
	}
	if got := evalList(t, nil, `list.median([1, 2, 3, 4])`); got != int64(2) {
		t.Errorf("list.median() = %v, want 2 (avg of 2,3 truncated)", got)
	}
	evalListExpectError(t, nil, `list.mean([])`)
	evalListExpectError(t, nil, `list.median([])`)
}

func TestConcatRejectsNonList(t *testing.T) {
	evalListExpectError(t, nil, `list.concat([1], 2)`)
}

// TestCallPredicateErrors covers callPredicate's two failure modes -
// the predicate call itself erroring, and the predicate returning a
// non-bool value - via list.all as a representative caller (every
// all/any/none/one/find/findIndex function shares this same helper).
func TestCallPredicateErrors(t *testing.T) {
	evalListExpectError(t, nil, `list.all([1, 2], x => x / 0)`)
	evalListExpectError(t, nil, `list.all([1, 2], x => x)`) // returns int64, not bool
}

// TestScanElementsRejectsNonListSource covers scanElements' own
// "first argument must be a list or iterator" error, shared by all/any/
// none/one/count/find/findIndex.
func TestScanElementsRejectsNonListSource(t *testing.T) {
	evalListExpectError(t, nil, `list.all(5, x => true)`)
	evalListExpectError(t, nil, `list.find(5, x => true)`)
}

// TestFindLastPredicateErrors covers findLast's own callPredicate-error
// propagation.
func TestFindLastPredicateErrors(t *testing.T) {
	evalListExpectError(t, nil, `list.findLast([1, 2], x => x / 0)`)
}

// TestSortRejectsNonList and TestSortByRejectsNonList cover sortFunc/
// sortByFunc's own type guard.
func TestSortRejectsNonList(t *testing.T) {
	evalListExpectError(t, nil, `list.sort(5)`)
}

func TestSortByRejectsNonList(t *testing.T) {
	evalListExpectError(t, nil, `list.sortBy(5, x => x)`)
}

// TestSortByKeyFunctionErrorPropagates covers sortByFunc's own key-
// function-call error branch (as opposed to a comparison error).
func TestSortByKeyFunctionErrorPropagates(t *testing.T) {
	evalListExpectError(t, nil, `list.sortBy([1, 2], x => x / 0)`)
}

// TestSortComparisonErrorPropagates covers sortStable's own Combine-error
// branch, shared by sort/sortBy/median: sorting elements with no
// registered comparison between them (bools) must error instead of
// producing a nonsensical order.
func TestSortComparisonErrorPropagates(t *testing.T) {
	evalListExpectError(t, nil, `list.sort([true, false])`)
	evalListExpectError(t, nil, `list.sortBy([1, 2], x => x == 1)`)
	evalListExpectError(t, nil, `list.median([true, false])`)
}

// TestKeysValuesRejectNonMap covers keysFunc/valuesFunc's own type guard.
func TestKeysValuesRejectNonMap(t *testing.T) {
	evalListExpectError(t, nil, `list.keys(5)`)
	evalListExpectError(t, nil, `list.values(5)`)
}

// TestListBuiltinArgumentCountErrors is a broad sweep of every remaining
// function's own arity guard, in one table.
func TestListBuiltinArgumentCountErrors(t *testing.T) {
	cases := []string{
		`list.all([1])`,
		`list.any([1])`,
		`list.none([1])`,
		`list.one([1])`,
		`list.count([1])`,
		`list.find([1])`,
		`list.findIndex([1])`,
		`list.findLast([1])`,
		`list.findLastIndex([1])`,
		`list.groupBy([1])`,
		`list.sort([1], [2])`,
		`list.sortBy([1])`,
		`list.reverse([1], [2])`,
		`list.flatten([1], [2])`,
		`list.keys([1], [2])`,
		`list.values([1], [2])`,
		`list.mean([1], [2])`,
		`list.median([1], [2])`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			evalListExpectError(t, nil, input)
		})
	}
}

// TestFlattenRejectsNonList and TestReverseListRejectsNonList cover the
// remaining first-argument type guards.
func TestFlattenRejectsNonList(t *testing.T) {
	evalListExpectError(t, nil, `list.flatten(5)`)
}

func TestReverseListRejectsNonList(t *testing.T) {
	evalListExpectError(t, nil, `list.reverse(5)`)
}

// TestListFunctionsRejectIterSeq covers scanElements'/groupByFunc's
// DESIGN_NOTES.md "iter/list split" guard: a bare iter.Seq source is a
// type error now, not silently drained, for every function that used to
// accept one - see stdlib's iter_builtins_test.go for the positive
// coverage (iter.all/iter.any/iter.find/iter.count/etc.) this test used
// to provide directly against list.*.
func TestListFunctionsRejectIterSeq(t *testing.T) {
	seq := func(yield func(int64) bool) {
		yield(1)
	}
	env := map[string]any{"seq": seq}

	cases := []string{
		`list.all(seq, x => x > 0)`,
		`list.any(seq, x => x > 0)`,
		`list.none(seq, x => x > 0)`,
		`list.one(seq, x => x > 0)`,
		`list.count(seq, x => x > 0)`,
		`list.find(seq, x => x > 0)`,
		`list.findIndex(seq, x => x > 0)`,
		`list.findLast(seq, x => x > 0)`,
		`list.findLastIndex(seq, x => x > 0)`,
		`list.groupBy(seq, x => x > 0)`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			evalListExpectError(t, env, input)
		})
	}
}

// TestListElementsReflectionFallback covers owlexpr.ListElements' own
// reflection-based fallback (any slice type other than []any, e.g. an
// env-supplied []int64 or []string) for every function that goes through
// it: sort/sortBy/reverse/flatten/concat/uniq/mean/median. Every existing
// test for these passes a list literal, which always compiles to []any -
// so the reflection branch itself had no real-value assertion anywhere.
func TestListElementsReflectionFallback(t *testing.T) {
	ints := map[string]any{"xs": []int64{3, 1, 2}}
	strs := map[string]any{"xs": []string{"b", "a"}}

	if got := evalList(t, ints, `list.sort(xs)`); !reflect.DeepEqual(got, []any{int64(1), int64(2), int64(3)}) {
		t.Errorf("list.sort([]int64) = %v", got)
	}
	if got := evalList(t, ints, `list.sortBy(xs, x => -x)`); !reflect.DeepEqual(got, []any{int64(3), int64(2), int64(1)}) {
		t.Errorf("list.sortBy([]int64) = %v", got)
	}
	if got := evalList(t, ints, `list.reverse(xs)`); !reflect.DeepEqual(got, []any{int64(2), int64(1), int64(3)}) {
		t.Errorf("list.reverse([]int64) = %v", got)
	}
	if got := evalList(t, ints, `list.concat(xs, [4])`); !reflect.DeepEqual(got, []any{int64(3), int64(1), int64(2), int64(4)}) {
		t.Errorf("list.concat([]int64, ...) = %v", got)
	}
	if got := evalList(t, ints, `list.uniq(xs)`); !reflect.DeepEqual(got, []any{int64(3), int64(1), int64(2)}) {
		t.Errorf("list.uniq([]int64) = %v", got)
	}
	if got := evalList(t, ints, `list.mean(xs)`); got != int64(2) {
		t.Errorf("list.mean([]int64) = %v, want 2", got)
	}
	if got := evalList(t, ints, `list.median(xs)`); got != int64(2) {
		t.Errorf("list.median([]int64) = %v, want 2", got)
	}
	nestedInts := map[string]any{"xs": []any{int64(1), []int64{2, 3}}}
	if got := evalList(t, nestedInts, `list.flatten(xs)`); !reflect.DeepEqual(got, []any{int64(1), int64(2), int64(3)}) {
		t.Errorf("list.flatten() over nested []int64 = %v", got)
	}
	if got := evalList(t, strs, `list.sort(xs)`); !reflect.DeepEqual(got, []any{"a", "b"}) {
		t.Errorf("list.sort([]string) = %v", got)
	}
}

// TestKeysValuesOverOtherMapShapes covers keysFunc/valuesFunc's other
// ForEachMapPair-supported shape - a reflect.Map type other than
// map[string]any - which the existing TestKeysAndValues only ever
// exercised via a map[string]any.
func TestKeysValuesOverOtherMapShapes(t *testing.T) {
	env := map[string]any{"m": map[int]string{1: "a", 2: "b"}}
	keys := evalList(t, env, `list.keys(m)`).([]any)
	values := evalList(t, env, `list.values(m)`).([]any)
	if len(keys) != 2 || len(values) != 2 {
		t.Fatalf("list.keys()=%v list.values()=%v, want 2 entries each (map[int]string)", keys, values)
	}
}

// TestKeysValuesRejectIterSeq2 covers keysFunc/valuesFunc's
// "iter/list split" guard - a bare iter.Seq2 used to be
// accepted the same as a real map (both matched by the old, now-removed
// ForEachPair); see stdlib's
// TestIterKeysValuesEntries for the positive coverage (iter.keys/
// iter.values/iter.entries) this test used to provide directly against
// list.*.
func TestKeysValuesRejectIterSeq2(t *testing.T) {
	env := map[string]any{"seq": func(yield func(string, int64) bool) {
		yield("a", 1)
	}}
	evalListExpectError(t, env, `list.keys(seq)`)
	evalListExpectError(t, env, `list.values(seq)`)
}

// TestGroupByBoolKey covers groupBy's non-string, non-numeric key flavour
// (a bool key function) - TestGroupBy/TestGroupByNumericKey only ever
// covered a string key and an int64 key.
func TestGroupByBoolKey(t *testing.T) {
	got, ok := evalList(t, nil, `list.groupBy([1, 2, 3, 4], x => x > 2)`).(map[any]any)
	if !ok {
		t.Fatalf("list.groupBy() returned %T, want map[any]any", got)
	}
	if !reflect.DeepEqual(got[false], []any{int64(1), int64(2)}) {
		t.Errorf("list.groupBy()[false] = %v", got[false])
	}
	if !reflect.DeepEqual(got[true], []any{int64(3), int64(4)}) {
		t.Errorf("list.groupBy()[true] = %v", got[true])
	}
}
