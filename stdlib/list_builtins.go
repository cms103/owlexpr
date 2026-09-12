package stdlib

import (
	"fmt"
	"sort"

	"github.com/cms103/owlexpr"
	"github.com/cms103/owlexpr/vm"
)

// ListBuiltins registers a set of list-manipulation builtins.
//
// any/all/none/one/find/findIndex/findLast/findLastIndex/count are native
// Go loops over vm.ForEachListElement, not lambda emulations built on
// reduce/filter, for the same reason the `in` operator is native rather
// than a reduce-based emulation: reduce's contract always
// visits every element, so a reduce-based any/find could never
// short-circuit. ForEachListElement already returns early the moment its
// yield callback returns false
//
// None of the functions in this pack accept a paired (map) source, except
// `keys`/`values`, which by definition only make sense against one. This pack is
// also list/map-only in the other direction: no function here accepts a
// bare iter.Seq/iter.Seq2 either (a []any or other slice/array via
// reflection is still fine).
func ListBuiltins() vm.VMOption {
	return func(mc *vm.Machine) error {
		for name, fn := range namespacedListFuncs {
			mc.RegisterNamespacedBuiltin("list", name, fn)
		}
		return nil
	}
}

// namespacedListFuncs is the full ListBuiltins roster keyed by its
// `list.<name>` member name
var namespacedListFuncs = map[string]vm.BuiltinFunc{
	"all":           allFunc,
	"any":           anyFunc,
	"none":          noneFunc,
	"one":           oneFunc,
	"count":         countListFunc,
	"find":          findFunc,
	"findIndex":     findIndexFunc,
	"findLast":      findLastFunc,
	"findLastIndex": findLastIndexFunc,
	"groupBy":       groupByFunc,
	"sort":          sortFunc,
	"sortBy":        sortByFunc,
	"reverse":       reverseListFunc,
	"flatten":       flattenFunc,
	"concat":        concatFunc,
	"uniq":          uniqFunc,
	"keys":          keysFunc,
	"values":        valuesFunc,
	"mean":          meanFunc,
	"median":        medianFunc,
}

// callPredicate calls call with args and requires the result to be a bool
// - the shared contract every predicate-taking function in this file
// relies on. call is a *vm.ReusableCall constructed once by the caller
// before its scan starts (see e.g. allFunc), so every element/pair visited
// during that one scan reuses the same underlying call-frame buffer
// instead of paying closure.call's normal per-call allocation cost on
// every single one - see vm.ReusableCall's own doc comment. Variadic so a
// single-element call site (call.Call1(el)) and a paired one
// (call.Call2(k, v), for iter_builtins.go's Seq2-aware callers) share the
// same helper.
func callPredicate(call *vm.ReusableCall, fnName string, args ...any) (bool, error) {
	var res any
	var err error
	switch len(args) {
	case 1:
		res, err = call.Call1(args[0])
	case 2:
		res, err = call.Call2(args[0], args[1])
	default:
		return false, fmt.Errorf("%s(): internal error: unsupported predicate arity %d", fnName, len(args))
	}
	if err != nil {
		return false, fmt.Errorf("%s(): %w", fnName, err)
	}
	keep, ok := res.(bool)
	if !ok {
		return false, fmt.Errorf("%s(): predicate must return a bool, got %T", fnName, res)
	}
	return keep, nil
}

// scanElements drives visit over list via vm.ForEachListElement,
// translating visit's (stop, err) return into ForEachListElement's
// yield-returns-false early-exit convention - the shared short-circuiting
// core behind any/all/none/one/find/findIndex in this file. Callers have
// already validated arity/extracted fn before calling this; list here is
// always args[0].
func scanElements(mc *vm.Machine, fnName string, list any, visit func(el any) (stop bool, err error)) error {
	var visitErr error
	ok := vm.ForEachListElement(mc, list, func(el any) bool {
		stop, err := visit(el)
		if err != nil {
			visitErr = err
			return false
		}
		return !stop
	})
	if !ok {
		return fmt.Errorf("%s(): first argument must be a list, got %T", fnName, list)
	}
	return visitErr
}

func allFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("all() expects 2 arguments (list, fn), got %d", len(args))
	}
	fn := args[1]
	call := vm.NewReusableCall(mc, fn)
	result := true
	err := scanElements(mc, "all", args[0], func(el any) (bool, error) {
		match, err := callPredicate(call, "all", el)
		if err != nil {
			return false, err
		}
		if !match {
			result = false
		}
		return !match, nil // stop as soon as one element fails
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func anyFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("any() expects 2 arguments (list, fn), got %d", len(args))
	}
	fn := args[1]
	call := vm.NewReusableCall(mc, fn)
	found := false
	err := scanElements(mc, "any", args[0], func(el any) (bool, error) {
		match, err := callPredicate(call, "any", el)
		if err != nil {
			return false, err
		}
		if match {
			found = true
		}
		return match, nil // stop as soon as one element matches
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

func noneFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("none() expects 2 arguments (list, fn), got %d", len(args))
	}
	fn := args[1]
	call := vm.NewReusableCall(mc, fn)
	result := true
	err := scanElements(mc, "none", args[0], func(el any) (bool, error) {
		match, err := callPredicate(call, "none", el)
		if err != nil {
			return false, err
		}
		if match {
			result = false
		}
		return match, nil // stop as soon as one element matches
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// oneFunc reports whether exactly one element matches. It can't
// short-circuit as aggressively as any/all/none - confirming "exactly
// one" when 0 or 1 matches have been seen still requires visiting every
// remaining element - but it does bail the moment a *second* match is
// found, since that alone already disproves "exactly one" regardless of
// what's left.
func oneFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("one() expects 2 arguments (list, fn), got %d", len(args))
	}
	fn := args[1]
	call := vm.NewReusableCall(mc, fn)
	count := 0
	err := scanElements(mc, "one", args[0], func(el any) (bool, error) {
		match, err := callPredicate(call, "one", el)
		if err != nil {
			return false, err
		}
		if match {
			count++
		}
		return count > 1, nil
	})
	if err != nil {
		return nil, err
	}
	return count == 1, nil
}

// countListFunc returns how many elements match the predicate - unlike
// all/any/none/one above, it can never short-circuit (every remaining
// element could still match), so it always visits the whole list. This is
// `list.count`, distinct from `string.count`'s substring-count -
// namespacing is what lets both exist under the same name (see
// ListBuiltins' doc comment).
func countListFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("count() expects 2 arguments (list, fn), got %d", len(args))
	}
	fn := args[1]
	call := vm.NewReusableCall(mc, fn)
	var result int64
	err := scanElements(mc, "count", args[0], func(el any) (bool, error) {
		match, err := callPredicate(call, "count", el)
		if err != nil {
			return false, err
		}
		if match {
			result++
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// findFunc returns the first matching element, or nil if none match.
func findFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("find() expects 2 arguments (list, fn), got %d", len(args))
	}
	fn := args[1]
	call := vm.NewReusableCall(mc, fn)
	var result any
	err := scanElements(mc, "find", args[0], func(el any) (bool, error) {
		match, err := callPredicate(call, "find", el)
		if err != nil {
			return false, err
		}
		if match {
			result = el
		}
		return match, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// findIndexFunc returns the index of the first matching element, or -1
// if none match.
func findIndexFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("findIndex() expects 2 arguments (list, fn), got %d", len(args))
	}
	fn := args[1]
	call := vm.NewReusableCall(mc, fn)
	result := int64(-1)
	i := int64(0)
	err := scanElements(mc, "findIndex", args[0], func(el any) (bool, error) {
		match, err := callPredicate(call, "findIndex", el)
		if err != nil {
			return false, err
		}
		if match {
			result = i
		}
		i++
		return match, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// findLast walks args[0] backwards for the last element matching args[1],
// sharing its core between findLastFunc and findLastIndexFunc. Uses
// vm.ForEachListElementReversed rather than owlexpr.ListElements so it
// short-circuits the moment it finds a match, the same way findFunc's
// forward scanElements does - ListElements would box every element of
// the source up front, which for a large list still not matching until
// near the end used to cost the same as materializing the whole thing
// (see BenchmarkListFindLast in bench_list_find_test.go).
func findLast(mc *vm.Machine, fnName string, args []any) (el any, idx int64, found bool, err error) {
	if len(args) != 2 {
		return nil, 0, false, fmt.Errorf("%s() expects 2 arguments (list, fn), got %d", fnName, len(args))
	}
	fn := args[1]
	call := vm.NewReusableCall(mc, fn)

	var resEl any
	var resIdx int64
	var resFound bool
	var visitErr error
	ok := vm.ForEachListElementReversed(mc, args[0], func(i int64, elem any) bool {
		match, matchErr := callPredicate(call, fnName, elem)
		if matchErr != nil {
			visitErr = matchErr
			return false
		}
		if match {
			resEl, resIdx, resFound = elem, i, true
			return false
		}
		return true
	})
	if !ok {
		return nil, 0, false, fmt.Errorf("%s(): first argument must be a list, got %T", fnName, args[0])
	}
	if visitErr != nil {
		return nil, 0, false, visitErr
	}
	return resEl, resIdx, resFound, nil
}

func findLastFunc(mc *vm.Machine, args ...any) (any, error) {
	el, _, _, err := findLast(mc, "findLast", args)
	if err != nil {
		return nil, err
	}
	return el, nil
}

func findLastIndexFunc(mc *vm.Machine, args ...any) (any, error) {
	_, idx, found, err := findLast(mc, "findLastIndex", args)
	if err != nil {
		return nil, err
	}
	if !found {
		return int64(-1), nil
	}
	return idx, nil
}

// groupByFunc buckets elements of a list by a key derived from each one,
// e.g. groupBy(users, u => u.Age) -> {30: [...], 42: [...]}. Unlike
// owlexpr's own map literals (string keys only, by parser design - see
// parseMapLiteral), the key function's result can be anything Go allows
// as a map key - int64, float64, bool, a decimal.Decimal, not just a
// string - so grouping by a numeric field works the same way sortBy(u =>
// u.Age) already does. A key that isn't actually comparable (the key
// function returns a slice, map, or another list) is rejected with an
// error rather than left to panic Go's own map insertion - see
// owlexpr.IsComparableKey.
//
// The accumulator has to be map[any]any while grouping, since the key
// type isn't known in advance and isn't guaranteed to be a string at
// all. But when every key that actually showed up happens to be a
// string - grouping by a string field, the common case - the result is
// copied into a map[string]any before returning, matching filterFunc's
// own "map[string]any when every surviving key is a string" convention:
// same reasoning, decided the same way, after the fact, rather than
// guessed from (say) only the first element's key, which would be wrong
// the moment a later element's key turns out to be a different type.
//
// Note this groups by Go's own equality (exact type + value), not
// owlexpr's coercing `==` - a key function that sometimes returns int64
// and sometimes float64 for what a user would consider "the same" value
// groups them separately, the same way two differently-typed Go map keys
// always would.
func groupByFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("groupBy() expects 2 arguments (list, fn), got %d", len(args))
	}
	fn := args[1]
	call := vm.NewReusableCall(mc, fn)
	out := make(map[any]any)
	var callErr error
	ok := vm.ForEachListElement(mc, args[0], func(el any) bool {
		key, err := call.Call1(el)
		if err != nil {
			callErr = fmt.Errorf("groupBy(): %w", err)
			return false
		}
		if !owlexpr.IsComparableKey(key) {
			callErr = fmt.Errorf("groupBy(): key %v of type %T cannot be used as a map key", key, key)
			return false
		}
		group, _ := out[key].([]any)
		out[key] = append(group, el)
		return true
	})
	if !ok {
		return nil, fmt.Errorf("groupBy(): first argument must be a list, got %T", args[0])
	}
	if callErr != nil {
		return nil, callErr
	}

	allStringKeys := true
	for k := range out {
		if _, isStr := k.(string); !isStr {
			allStringKeys = false
			break
		}
	}
	if !allStringKeys {
		return out, nil
	}
	strOut := make(map[string]any, len(out))
	for k, v := range out {
		strOut[k.(string)] = v
	}
	return strOut, nil
}

// sortStable sorts elems ascending in place using mc.Combine(OpLess) as
// the comparator, so sort/median compose with any registered numeric
// type (e.g. decimal.Decimal) exactly the way sum/min/max already do via
// foldExtreme, rather than only handling Go's native numeric kinds.
func sortStable(mc *vm.Machine, fnName string, elems []any) error {
	var sortErr error
	sort.SliceStable(elems, func(i, j int) bool {
		if sortErr != nil {
			return false
		}
		res, err := mc.Combine(elems[i], elems[j], vm.OpLess)
		if err != nil {
			sortErr = fmt.Errorf("%s(): %w", fnName, err)
			return false
		}
		less, ok := res.(bool)
		if !ok {
			sortErr = fmt.Errorf("%s(): comparison did not return a bool", fnName)
			return false
		}
		return less
	})
	return sortErr
}

// sortFunc returns a new ascending-sorted copy of list - it never mutates
// the argument, matching filter/map's own "always return a fresh value"
// convention.
func sortFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("sort() expects 1 argument, got %d", len(args))
	}
	elems, ok := owlexpr.ListElements(args[0])
	if !ok {
		return nil, fmt.Errorf("sort(): argument must be a list, got %T", args[0])
	}
	out := append([]any(nil), elems...)
	if err := sortStable(mc, "sort", out); err != nil {
		return nil, err
	}
	return out, nil
}

// sortByFunc sorts by a key derived from each element, e.g. sortBy(users,
// u => u.Age). The key function is called exactly once per element
// (decorate-sort-undecorate) rather than repeatedly during comparisons,
// both for performance and so a key function with side effects (or one
// that's simply expensive) doesn't get called O(n log n) times.
func sortByFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("sortBy() expects 2 arguments (list, fn), got %d", len(args))
	}
	elems, ok := owlexpr.ListElements(args[0])
	if !ok {
		return nil, fmt.Errorf("sortBy(): first argument must be a list, got %T", args[0])
	}
	fn := args[1]
	call := vm.NewReusableCall(mc, fn)

	type keyedElem struct {
		el  any
		key any
	}
	pairs := make([]keyedElem, len(elems))
	for i, el := range elems {
		key, err := call.Call1(el)
		if err != nil {
			return nil, fmt.Errorf("sortBy(): %w", err)
		}
		pairs[i] = keyedElem{el: el, key: key}
	}

	var sortErr error
	sort.SliceStable(pairs, func(i, j int) bool {
		if sortErr != nil {
			return false
		}
		res, err := mc.Combine(pairs[i].key, pairs[j].key, vm.OpLess)
		if err != nil {
			sortErr = fmt.Errorf("sortBy(): %w", err)
			return false
		}
		less, ok := res.(bool)
		if !ok {
			sortErr = fmt.Errorf("sortBy(): comparison did not return a bool")
			return false
		}
		return less
	})
	if sortErr != nil {
		return nil, sortErr
	}

	out := make([]any, len(pairs))
	for i, p := range pairs {
		out[i] = p.el
	}
	return out, nil
}

// reverseListFunc returns a reversed copy of a list. Registered as
// "list.reverse".
func reverseListFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("list.reverse() expects 1 argument, got %d", len(args))
	}
	elems, ok := owlexpr.ListElements(args[0])
	if !ok {
		return nil, fmt.Errorf("list.reverse(): argument must be a list, got %T", args[0])
	}
	out := make([]any, len(elems))
	for i, el := range elems {
		out[len(elems)-1-i] = el
	}
	return out, nil
}

// appendFlattened recurses into any list-like element, so flatten handles
// arbitrarily nested lists rather than just one level.
func appendFlattened(out *[]any, elems []any) {
	for _, el := range elems {
		if nested, ok := owlexpr.ListElements(el); ok {
			appendFlattened(out, nested)
			continue
		}
		*out = append(*out, el)
	}
}

func flattenFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("flatten() expects 1 argument, got %d", len(args))
	}
	elems, ok := owlexpr.ListElements(args[0])
	if !ok {
		return nil, fmt.Errorf("flatten(): argument must be a list, got %T", args[0])
	}
	out := []any{}
	appendFlattened(&out, elems)
	return out, nil
}

// concatFunc concatenates any number of list-like arguments into one
// []any, in argument order. Every argument must be list-like - unlike
// expr's concat (which also accepts bare scalars interspersed with
// lists), a caller here wraps a single value in a list literal instead,
// e.g. concat(a, [x], b).
func concatFunc(mc *vm.Machine, args ...any) (any, error) {
	out := []any{}
	for i, arg := range args {
		elems, ok := owlexpr.ListElements(arg)
		if !ok {
			return nil, fmt.Errorf("concat(): argument %d must be a list, got %T", i+1, arg)
		}
		out = append(out, elems...)
	}
	return out, nil
}

// uniqFunc removes duplicate elements, keeping the first occurrence of
// each and preserving order. Equality is exact Go `==` via a map[any]bool
// - the same convention filterFunc's paired-source branch and
// owlexpr.IsComparableKey already use for a computed map key - not
// Combine(OpEqual)'s coercive cross-type equality (2 == 2.0) the `in`
// operator uses, so int64(2) and float64(2.0) are treated as distinct
// here. An element that isn't comparable (a list, map, or func) errors
// via the same check rather than panicking on insertion into seen.
func uniqFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("uniq() expects 1 argument, got %d", len(args))
	}
	elems, ok := owlexpr.ListElements(args[0])
	if !ok {
		return nil, fmt.Errorf("uniq(): argument must be a list, got %T", args[0])
	}
	seen := make(map[any]bool, len(elems))
	out := make([]any, 0, len(elems))
	for _, el := range elems {
		if !owlexpr.IsComparableKey(el) {
			return nil, fmt.Errorf("uniq(): element %v of type %T cannot be compared", el, el)
		}
		if seen[el] {
			continue
		}
		seen[el] = true
		out = append(out, el)
	}
	return out, nil
}

// keysFunc and valuesFunc iterate a map-shaped source via ForEachMapPair -
// the same primitive filter/map/reduce use for their paired-source case -
// and collect one side of each pair. Like ranging a Go map directly,
// iteration order is not guaranteed and not sorted here; two calls
// against the same map may return keys/values in different orders.
//
// map(m, (k, v) => k) already gets a user the same result, so these two
// are pure convenience, but with higher performance than via lambda.
func keysFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("keys() expects 1 argument, got %d", len(args))
	}
	out := []any{}
	if ok := vm.ForEachMapPair(args[0], func(k, _ any) bool {
		out = append(out, k)
		return true
	}); !ok {
		return nil, fmt.Errorf("keys(): argument must be a map, got %T", args[0])
	}
	return out, nil
}

func valuesFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("values() expects 1 argument, got %d", len(args))
	}
	out := []any{}
	if ok := vm.ForEachMapPair(args[0], func(_, v any) bool {
		out = append(out, v)
		return true
	}); !ok {
		return nil, fmt.Errorf("values(): argument must be a map, got %T", args[0])
	}
	return out, nil
}

// sumElements folds list via mc.Combine(OpAdd), the same general-case
// path root's sumFunc falls back to for non-[]float64/[]int64/[]int
// inputs - see builtin_funcs.go. Not exported from there, so duplicated
// here rather than reaching into root for an unexported helper; mean's
// list is already small (never the []float64/[]int64/[]int fast-path
// case matters for), so the extra Combine call per element is not worth
// avoiding.
func sumElements(mc *vm.Machine, elems []any) (any, error) {
	acc := elems[0]
	for _, v := range elems[1:] {
		var err error
		acc, err = mc.Combine(acc, v, vm.OpAdd)
		if err != nil {
			return nil, err
		}
	}
	return acc, nil
}

// meanFunc divides the sum by the count via mc.Combine(OpDiv), so it
// follows exactly the same arithmetic rules as owlexpr's own `/`
// operator - including integer truncation: mean([1, 2, 4]) is 2, not
// 2.3333, the same way 7 / 3 is 2 elsewhere in this language. Convert
// elements to float first (e.g. via a cast, if one is registered) for a
// fractional result.
func meanFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("mean() expects 1 argument, got %d", len(args))
	}
	elems, ok := owlexpr.ListElements(args[0])
	if !ok {
		return nil, fmt.Errorf("mean(): argument must be a list, got %T", args[0])
	}
	if len(elems) == 0 {
		return nil, fmt.Errorf("mean() expects a non-empty list")
	}
	total, err := sumElements(mc, elems)
	if err != nil {
		return nil, fmt.Errorf("mean(): %w", err)
	}
	result, err := mc.Combine(total, int64(len(elems)), vm.OpDiv)
	if err != nil {
		return nil, fmt.Errorf("mean(): %w", err)
	}
	return result, nil
}

// medianFunc sorts a copy of list (via the same mc.Combine(OpLess)
// comparator sort/sortBy use) and returns the middle element, or - for an
// even-length list - the average of the two middle elements, subject to
// the same integer-division truncation meanFunc documents.
func medianFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("median() expects 1 argument, got %d", len(args))
	}
	elems, ok := owlexpr.ListElements(args[0])
	if !ok {
		return nil, fmt.Errorf("median(): argument must be a list, got %T", args[0])
	}
	if len(elems) == 0 {
		return nil, fmt.Errorf("median() expects a non-empty list")
	}
	sorted := append([]any(nil), elems...)
	if err := sortStable(mc, "median", sorted); err != nil {
		return nil, err
	}
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid], nil
	}
	total, err := mc.Combine(sorted[mid-1], sorted[mid], vm.OpAdd)
	if err != nil {
		return nil, fmt.Errorf("median(): %w", err)
	}
	result, err := mc.Combine(total, int64(2), vm.OpDiv)
	if err != nil {
		return nil, fmt.Errorf("median(): %w", err)
	}
	return result, nil
}
