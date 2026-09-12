package stdlib

import (
	"fmt"
	"iter"

	"github.com/cms103/owlexpr"
	"github.com/cms103/owlexpr/vm"
)

// IterBuiltins registers a set of push-iterator (`iter.Seq[T]`/
// `iter.Seq2[K, V]`) functions under the `iter` namespace. This is the
// exclusive home for iterator consumption/production: core
// map/filter/reduce, the `in` operator, and every function in `list.*`
// deliberately reject a bare iterator (see their own doc comments)
// precisely so that consuming one - a one-way, env-visible side effect,
// since a Go push iterator can't be rewound - is never something that
// happens by accident. Typing `iter.` is the only way to do it.
//
// Three kinds of function live here, and the boundary between them is a
// hard one: everything except the producers is iterator-*only* (built on
// vm.ForEachSeqElement/ForEachSeq2Pair/IterateSeqSource, which recognize
// nothing but a push iterator - not vm.ForEachListElement/ForEachMapPair/
// IterateListOrMapSource, list.*'s own list/map-only equivalents) - a
// plain list or map argument is rejected the same way any other wrong
// type is, with no special-case check needed, mirroring exactly how
// `list.*` now rejects a bare iterator (see ForEachSeqElement's own doc
// comment for the full reasoning: a list has no drain hazard, but the
// point of typing `iter.` is that it *certifies* an iterator is
// involved, not merely tolerates one).
//
//   - Producers (`of`, `keys`, `values`, `entries`) are the one place
//     list/map input belongs: they turn a concrete list or map into an
//     `iter.Seq`/`iter.Seq2`.
//   - Lazy combinators (`map`, `filter`, `take`, `skip`) turn an iterator
//     source into a *new*, still-undriven iterator - see iterMapFunc's
//     doc comment for how laziness and error propagation actually work.
//   - Terminal ops (`toList`, `toMap`, `reduce`, `count`, `first`, `find`,
//     `any`, `all`, `none`, `contains`) drive an iterator source to
//     completion (or to a short-circuit) and return an ordinary owlexpr
//     value.
func IterBuiltins() vm.VMOption {
	return func(mc *vm.Machine) error {
		for name, fn := range namespacedIterFuncs {
			mc.RegisterNamespacedBuiltin("iter", name, fn)
		}
		return nil
	}
}

var namespacedIterFuncs = map[string]vm.BuiltinFunc{
	// Of, keys, values and entries all duplicate the content of the list / map passed and then provide an iterator
	"of":      iterOfFunc,
	"keys":    iterKeysFunc,
	"values":  iterValuesFunc,
	"entries": iterEntriesFunc,

	// map and filter work on both Seq and Seq2 iterators
	"map":    iterMapFunc,
	"filter": iterFilterFunc,
	"reduce": iterReduceFunc,

	// Drains a Seq iterator, returning a list of all elements that were drained.
	"toList": iterToListFunc,

	// Drains a Seq2 iterator, returning a map of all elements that were drained.
	"toMap": iterToMapFunc,

	// Terminating and partially-draining ops - all work on both Seq and Seq2 iterators
	"take":     iterTakeFunc,
	"skip":     iterSkipFunc,
	"count":    iterCountFunc,
	"first":    iterFirstFunc,
	"find":     iterFindFunc,
	"any":      iterAnyFunc,
	"all":      iterAllFunc,
	"none":     iterNoneFunc,
	"contains": iterContainsFunc,
}

// recoverIterErr is the shared recover for every terminal op below. A
// lazy combinator's pull closure (iterMapFunc/iterFilterFunc/etc.) has no
// error-return slot to report a failed fn/predicate call through - a bare
// `func(func(T) bool)` genuinely has no room for one, by construction -
// so it panics instead, mirroring vm.makeReflectFuncAdapter's identical
// "no other channel" tradeof.
// Only an `error`-typed panic is recovered here anything else (a genuine bug, a real Go
// runtime panic from somewhere in env) is re-panicked rather than
// silently swallowed as a clean returned error.
func recoverIterErr(errp *error) {
	if r := recover(); r != nil {
		if e, ok := r.(error); ok {
			*errp = e
			return
		}
		panic(r)
	}
}

// iterOfFunc turns a concrete list into an iter.Seq[any] - the source is
// already fully in memory either way, so this is eager (validated and
// snapshotted immediately), not lazy; the laziness this pack is for
// starts at the combinators below.
func iterOfFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("iter.of() expects 1 argument, got %d", len(args))
	}
	elems, ok := owlexpr.ListElements(args[0])
	if !ok {
		return nil, fmt.Errorf("iter.of(): argument must be a list, got %T", args[0])
	}
	var seq iter.Seq[any] = func(yield func(any) bool) {
		for _, el := range elems {
			if !yield(el) {
				return
			}
		}
	}
	return seq, nil
}

// mapPairs snapshots a concrete map source (via vm.ForEachMapPair - a
// real map only, deliberately not also recognizing an iter.Seq2: these
// three producers' whole job is converting a map into an iterator, so
// accepting an iter.Seq2 here too would make them a redundant,
// silently-working identity for input that's already an iterator) into a
// slice - the
// shared eager step behind iterKeysFunc/iterValuesFunc/iterEntriesFunc,
// all three of which build their iter.Seq/iter.Seq2 from a snapshot
// rather than re-driving the source on every pull, since a real Go map's
// own range order isn't stable across multiple passes anyway.
type kvPair struct{ k, v any }

func mapPairs(fnName string, v any) ([]kvPair, error) {
	var pairs []kvPair
	if ok := vm.ForEachMapPair(v, func(k, val any) bool {
		pairs = append(pairs, kvPair{k, val})
		return true
	}); !ok {
		return nil, fmt.Errorf("%s(): argument must be a map, got %T", fnName, v)
	}
	return pairs, nil
}

func iterKeysFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("iter.keys() expects 1 argument, got %d", len(args))
	}
	pairs, err := mapPairs("iter.keys", args[0])
	if err != nil {
		return nil, err
	}
	var seq iter.Seq[any] = func(yield func(any) bool) {
		for _, p := range pairs {
			if !yield(p.k) {
				return
			}
		}
	}
	return seq, nil
}

func iterValuesFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("iter.values() expects 1 argument, got %d", len(args))
	}
	pairs, err := mapPairs("iter.values", args[0])
	if err != nil {
		return nil, err
	}
	var seq iter.Seq[any] = func(yield func(any) bool) {
		for _, p := range pairs {
			if !yield(p.v) {
				return
			}
		}
	}
	return seq, nil
}

func iterEntriesFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("iter.entries() expects 1 argument, got %d", len(args))
	}
	pairs, err := mapPairs("iter.entries", args[0])
	if err != nil {
		return nil, err
	}
	var seq iter.Seq2[any, any] = func(yield func(any, any) bool) {
		for _, p := range pairs {
			if !yield(p.k, p.v) {
				return
			}
		}
	}
	return seq, nil
}

// iterMapFunc is iter's lazy combinator: unlike core mapFunc, it doesn't
// drive source at all - it returns a new, undriven iter.Seq[any] closure
// that will drive source itself, later, whenever something finally pulls
// from it (another combinator, or a terminal op). The closure literal is
// assigned to a var explicitly typed iter.Seq[any] (not `seq := func(...)
// {...}`) before being returned: a bare func literal's type is the
// unnamed `func(func(any) bool)`, and boxing *that* into the `any` return
// slot stores that unnamed type as the interface's dynamic type, not
// iter.Seq[any] - so an embedder calling Run() and doing
// `result.(iter.Seq[any])` would fail even though the shape matches
// exactly.
//
// Internal consumers (vm.ForEachSeqElement's asSeqYield, in
// particular) don't care either way, since they detect the shape via
// reflection rather than asserting the named type - but a clean,
// idiomatic type assertion at the embedder boundary does, so every
// producer/combinator in this file explicitly types its returned closure
// as iter.Seq[any] (or iter.Seq2[any, any] for the paired producers).
//
// Because of that, chaining needs no special-case code either:
// iter.filter(iter.map(x, f), pred) just works, since iter.filter's own
// implementation sees iter.map's return value as nothing more than
// another iter.Seq-shaped source.
//
// Error propagation is the one genuinely new piece - see
// recoverIterErr's doc comment for the full reasoning. Concretely here:
// if the wrapped fn call fails while source is finally being driven, the
// pull closure panics with that error instead of quietly stopping. This
// also means iter.map(seq, badFn) can "succeed" (return a valid
// iter.Seq) even though fn would fail on the very first element - the
// same as a Python generator function not running its body until the
// first next() - the failure only surfaces once something actually
// pulls. A source that vm.IterateSeqSource doesn't recognize at all
// (including a plain list/map - see IterBuiltins' own doc comment for why
// that's rejected here too, not just non-iterator types) behaves the same
// way: the type error surfaces lazily too, via the same panic channel,
// rather than at iter.map's own call site.
func iterMapFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("iter.map() expects 2 arguments (source, fn), got %d", len(args))
	}
	source, fn := args[0], args[1]

	// call is set up once per iter.map() call (i.e. once per `seq`
	// closure, shared across however many times it's eventually driven -
	// always sequentially, never concurrently, since push-iterators are
	// synchronous), not once per element - see core mapFunc's identical
	// comment in builtin_funcs.go, which this mirrors.
	call := vm.NewReusableCall(mc, fn)

	var seq iter.Seq[any] = func(yield func(any) bool) {
		ok := vm.IterateSeqSource(source,
			func(el any) bool {
				res, err := call.Call1(el)
				if err != nil {
					panic(fmt.Errorf("iter.map(): %w", err))
				}
				return yield(res)
			},
			func(k, v any) bool {
				res, err := call.Call2(k, v)
				if err != nil {
					panic(fmt.Errorf("iter.map(): %w", err))
				}
				return yield(res)
			},
		)
		if !ok {
			panic(fmt.Errorf("iter.map(): source must be an iterator, got %T", source))
		}
	}

	return seq, nil
}

// iterFilterFunc is iter.map's filtering counterpart - same laziness and
// error-propagation shape (see iterMapFunc's doc comment), but the
// predicate decides whether to yield rather than transforming the value.
// Unlike core filterFunc, a paired source always flattens to (k, v) pairs
// - []any{k, v} - rather than preserving map shape: the combinators in
// this pack always return a plain iter.Seq[any] regardless of what shape
// their source was, precisely so a caller never has to branch on which
// shape a lazy value turned out to be before it's driven - the source's
// shape isn't even known until pull time (see iterMapFunc's doc comment),
// so there'd be no way to decide "return an iter.Seq or iter.Seq2" up
// front anyway. iter.toMap is still the way to get a map back out - see
// its own doc comment for how it reconstructs one from exactly this
// flattened shape.
func iterFilterFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("iter.filter() expects 2 arguments (source, fn), got %d", len(args))
	}
	source, fn := args[0], args[1]

	// See iterMapFunc's identical comment just above.
	call := vm.NewReusableCall(mc, fn)

	var seq iter.Seq[any] = func(yield func(any) bool) {
		ok := vm.IterateSeqSource(source,
			func(el any) bool {
				res, err := call.Call1(el)
				if err != nil {
					panic(fmt.Errorf("iter.filter(): %w", err))
				}
				keep, isBool := res.(bool)
				if !isBool {
					panic(fmt.Errorf("iter.filter(): predicate must return a bool, got %T", res))
				}
				if !keep {
					return true
				}
				return yield(el)
			},
			func(k, v any) bool {
				res, err := call.Call2(k, v)
				if err != nil {
					panic(fmt.Errorf("iter.filter(): %w", err))
				}
				keep, isBool := res.(bool)
				if !isBool {
					panic(fmt.Errorf("iter.filter(): predicate must return a bool, got %T", res))
				}
				if !keep {
					return true
				}
				return yield([]any{k, v})
			},
		)
		if !ok {
			panic(fmt.Errorf("iter.filter(): source must be an iterator, got %T", source))
		}
	}
	return seq, nil
}

// intArg converts args[i] to a non-negative int, the shared guard behind
// iter.take/iter.skip's own count argument.
func intArg(mc *vm.Machine, fnName, argName string, v any) (int, error) {
	n, err := mc.CoercValue(v, int64(0))
	if err != nil {
		return 0, fmt.Errorf("%s(): %s must be a number, got %T", fnName, argName, v)
	}
	i := int(n.(int64))
	if i < 0 {
		return 0, fmt.Errorf("%s(): %s must not be negative, got %d", fnName, argName, i)
	}
	return i, nil
}

// iterTakeFunc and iterSkipFunc accept a paired (iter.Seq2) source too,
// same as iter.map/iter.filter - flattening to [k, v] elements the same
// way (see iterFilterFunc's doc comment). Whether "the first n"/"all but
// the first n" means anything useful depends on whether the specific
// source is actually ordered, but that's a property of the source, not of
// Seq2 as a shape: a real Go map's range order isn't guaranteed, but
// nothing stops a hand-written iter.Seq2 (a sorted slice of pairs, a
// database cursor, ...) from being perfectly ordered, and take/skip have
// no way to tell the difference - nor would rejecting Seq2 outright help,
// since an unordered *Seq* source (e.g. driven off a real map via
// iter.values) has the exact same issue and was never restricted for it.
//
// Like the combinators above, both are lazy and panic through a source-type
// mismatch (see iterMapFunc's doc comment) rather than checking source
// eagerly, since there is otherwise nothing here that could fail early
//   - n is validated up front since it doesn't depend on driving source at all.
func iterTakeFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("iter.take() expects 2 arguments (source, n), got %d", len(args))
	}
	source := args[0]
	n, err := intArg(mc, "iter.take", "n", args[1])
	if err != nil {
		return nil, err
	}

	var seq iter.Seq[any] = func(yield func(any) bool) {
		if n == 0 {
			return
		}
		taken := 0
		emit := func(el any) bool {
			if !yield(el) {
				return false
			}
			taken++
			return taken < n
		}
		ok := vm.IterateSeqSource(source,
			emit,
			func(k, v any) bool { return emit([]any{k, v}) },
		)
		if !ok {
			panic(fmt.Errorf("iter.take(): source must be an iterator, got %T", source))
		}
	}
	return seq, nil
}

func iterSkipFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("iter.skip() expects 2 arguments (source, n), got %d", len(args))
	}
	source := args[0]
	n, err := intArg(mc, "iter.skip", "n", args[1])
	if err != nil {
		return nil, err
	}

	var seq iter.Seq[any] = func(yield func(any) bool) {
		skipped := 0
		emit := func(el any) bool {
			if skipped < n {
				skipped++
				return true
			}
			return yield(el)
		}
		ok := vm.IterateSeqSource(source,
			emit,
			func(k, v any) bool { return emit([]any{k, v}) },
		)
		if !ok {
			panic(fmt.Errorf("iter.skip(): source must be an iterator, got %T", source))
		}
	}
	return seq, nil
}

// iterToListFunc is the primary terminal op - drives an iterator source
// to completion and materializes it as a []any, the same shape core
// mapFunc's list branch returns. A paired (iter.Seq2) source flattens to
// []any{k, v} pairs, same as iter.filter's paired branch above.
func iterToListFunc(mc *vm.Machine, args ...any) (result any, err error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("iter.toList() expects 1 argument, got %d", len(args))
	}
	defer recoverIterErr(&err)

	var out []any
	ok := vm.IterateSeqSource(args[0],
		func(el any) bool {
			out = append(out, el)
			return true
		},
		func(k, v any) bool {
			out = append(out, []any{k, v})
			return true
		},
	)
	if !ok {
		return nil, fmt.Errorf("iter.toList(): argument must be an iterator, got %T", args[0])
	}
	if out == nil {
		out = []any{}
	}
	return out, nil
}

// iterToMapFunc drives an iterator source to completion into a real Go
// map - map[string]any when every surviving key happens to be a string,
// map[any]any otherwise, the iter.Seq2 half of what core groupByFunc/
// filterFunc already do for a plain map. Two source shapes are accepted,
// both genuinely iterator-only - a real map is rejected here just like
// everywhere else in this pack (see IterBuiltins' own doc comment: if the
// source is already a concrete map, there's nothing for a terminal op to
// do):
//
//   - An iter.Seq2, via vm.ForEachSeq2Pair directly.
//   - A plain iter.Seq of [k, v] pairs - []any of length 2 - the
//     flattened shape iter.filter (and, for that matter, iter.map)
//     produce when driven over a paired source (see iterFilterFunc's own
//     doc comment for why they flatten rather than staying paired), via
//     vm.ForEachSeqElement. Without this, iter.toMap(iter.filter(pairedSrc,
//     pred)) - by far the most natural way to "filter a map, get a map
//     back" - could never work, since iter.filter's flattened output
//     doesn't look paired to ForEachSeq2Pair at all.
//
// This is also the only place in this pack that can reach a non-comparable
// key: a real Go map can never hold one to begin with, and only a source
// that isn't backed by an actual map (an iter.Seq2, or a hand-built [k, v]
// pair) can yield one - see filterFunc's own doc comment in
// builtin_funcs.go for the parallel argument covering its own paired
// branch.
func iterToMapFunc(mc *vm.Machine, args ...any) (result any, err error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("iter.toMap() expects 1 argument, got %d", len(args))
	}
	defer recoverIterErr(&err)

	var pairs []kvPair
	var keyErr error
	allStringKeys := true
	collect := func(k, v any) bool {
		if !owlexpr.IsComparableKey(k) {
			keyErr = fmt.Errorf("iter.toMap(): key %v of type %T cannot be used as a map key", k, k)
			return false
		}
		if _, isStr := k.(string); !isStr {
			allStringKeys = false
		}
		pairs = append(pairs, kvPair{k, v})
		return true
	}

	if ok := vm.ForEachSeq2Pair(args[0], collect); !ok {
		var elemErr error
		elemsOK := vm.ForEachSeqElement(args[0], func(el any) bool {
			pair, isPair := el.([]any)
			if !isPair || len(pair) != 2 {
				elemErr = fmt.Errorf("iter.toMap(): element %v is not a [key, value] pair", el)
				return false
			}
			return collect(pair[0], pair[1])
		})
		if !elemsOK {
			return nil, fmt.Errorf("iter.toMap(): argument must be an iterator (of pairs, or [key, value] elements), got %T", args[0])
		}
		if elemErr != nil {
			return nil, elemErr
		}
	}
	if keyErr != nil {
		return nil, keyErr
	}

	if allStringKeys {
		m := make(map[string]any, len(pairs))
		for _, p := range pairs {
			m[p.k.(string)] = p.v
		}
		return m, nil
	}
	m := make(map[any]any, len(pairs))
	for _, p := range pairs {
		m[p.k] = p.v
	}
	return m, nil
}

// iterReduceFunc mirrors core reduceFunc exactly, just iterator-inclusive
// - see reduceFunc's own doc comment in builtin_funcs.go for the (acc, x)
// vs. (acc, k, v) arity split by source shape.
func iterReduceFunc(mc *vm.Machine, args ...any) (result any, err error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("iter.reduce() expects 3 arguments (source, fn, initial), got %d", len(args))
	}
	defer recoverIterErr(&err)

	fn := args[1]
	acc := args[2]

	// See core reduceFunc's identical comment in builtin_funcs.go: acc
	// changes every iteration, so it's passed to Call2/Call3 fresh each
	// time, not just set up once.
	call := vm.NewReusableCall(mc, fn)

	var callErr error
	ok := vm.IterateSeqSource(args[0],
		func(el any) bool {
			res, e := call.Call2(acc, el)
			if e != nil {
				callErr = e
				return false
			}
			acc = res
			return true
		},
		func(k, v any) bool {
			res, e := call.Call3(acc, k, v)
			if e != nil {
				callErr = e
				return false
			}
			acc = res
			return true
		},
	)
	if !ok {
		return nil, fmt.Errorf("iter.reduce(): first argument must be an iterator, got %T", args[0])
	}
	if callErr != nil {
		return nil, callErr
	}
	return acc, nil
}

// iterCountFunc drains source fully and reports how many elements it
// produced - the explicit, drain-on-purpose counterpart to len(), which
// deliberately refuses to touch an iterator at all (see lenFunc's own
// doc comment in builtin_funcs.go) since a length isn't knowable without
// fully consuming one. Counting doesn't care what shape each yielded
// element is, so unlike iter.first (which returns an element and has
// nothing sensible to return for a pair without iter.map/iter.filter's own
// flattening convention), it accepts a paired (iter.Seq2) source too.
func iterCountFunc(mc *vm.Machine, args ...any) (result any, err error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("iter.count() expects 1 argument, got %d", len(args))
	}
	defer recoverIterErr(&err)

	var n int64
	ok := vm.IterateSeqSource(args[0],
		func(any) bool { n++; return true },
		func(_, _ any) bool { n++; return true },
	)
	if !ok {
		return nil, fmt.Errorf("iter.count(): argument must be an iterator, got %T", args[0])
	}
	return n, nil
}

// iterFirstFunc pulls at most one element from source and stops - nil if
// source is empty. Accepts a paired (iter.Seq2) source too, returning a
// flattened [k, v] pair, the same convention iter.find/iter.filter use
// for theirs.
func iterFirstFunc(mc *vm.Machine, args ...any) (result any, err error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("iter.first() expects 1 argument, got %d", len(args))
	}
	defer recoverIterErr(&err)

	var first any
	ok := vm.IterateSeqSource(args[0],
		func(el any) bool {
			first = el
			return false
		},
		func(k, v any) bool {
			first = []any{k, v}
			return false
		},
	)
	if !ok {
		return nil, fmt.Errorf("iter.first(): argument must be an iterator, got %T", args[0])
	}
	return first, nil
}

// iterFindFunc, iterAnyFunc, iterAllFunc, and iterNoneFunc are the
// iterator-inclusive counterparts to list.find/any/all/none, sharing
// list_builtins.go's callPredicate helper (same package, variadic so it
// fits both call shapes below). Unlike list.*'s versions - deliberately
// list-only, see ListBuiltins' own doc comment - these four accept a
// paired (iter.Seq2) source too, same as iter.map/iter.filter/iter.reduce:
// fn is called as fn(k, v) rather than fn(el) over a paired source, and
// iter.find returns a flattened [k, v] pair on a match, the same
// convention iter.filter uses for its own paired branch (see its doc
// comment). All four still short-circuit for the same reason list.*'s own
// scanElements does.
func iterFindFunc(mc *vm.Machine, args ...any) (result any, err error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("iter.find() expects 2 arguments (source, fn), got %d", len(args))
	}
	defer recoverIterErr(&err)

	fn := args[1]
	call := vm.NewReusableCall(mc, fn)
	var found any
	var predErr error
	ok := vm.IterateSeqSource(args[0],
		func(el any) bool {
			match, callErr := callPredicate(call, "iter.find", el)
			if callErr != nil {
				predErr = callErr
				return false
			}
			if match {
				found = el
			}
			return !match
		},
		func(k, v any) bool {
			match, callErr := callPredicate(call, "iter.find", k, v)
			if callErr != nil {
				predErr = callErr
				return false
			}
			if match {
				found = []any{k, v}
			}
			return !match
		},
	)
	if !ok {
		return nil, fmt.Errorf("iter.find(): first argument must be an iterator, got %T", args[0])
	}
	if predErr != nil {
		return nil, predErr
	}
	return found, nil
}

func iterAnyFunc(mc *vm.Machine, args ...any) (result any, err error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("iter.any() expects 2 arguments (source, fn), got %d", len(args))
	}
	defer recoverIterErr(&err)

	fn := args[1]
	call := vm.NewReusableCall(mc, fn)
	found := false
	var predErr error
	ok := vm.IterateSeqSource(args[0],
		func(el any) bool {
			match, callErr := callPredicate(call, "iter.any", el)
			if callErr != nil {
				predErr = callErr
				return false
			}
			if match {
				found = true
			}
			return !match
		},
		func(k, v any) bool {
			match, callErr := callPredicate(call, "iter.any", k, v)
			if callErr != nil {
				predErr = callErr
				return false
			}
			if match {
				found = true
			}
			return !match
		},
	)
	if !ok {
		return nil, fmt.Errorf("iter.any(): first argument must be an iterator, got %T", args[0])
	}
	if predErr != nil {
		return nil, predErr
	}
	return found, nil
}

func iterAllFunc(mc *vm.Machine, args ...any) (result any, err error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("iter.all() expects 2 arguments (source, fn), got %d", len(args))
	}
	defer recoverIterErr(&err)

	fn := args[1]
	call := vm.NewReusableCall(mc, fn)
	allMatch := true
	var predErr error
	ok := vm.IterateSeqSource(args[0],
		func(el any) bool {
			match, callErr := callPredicate(call, "iter.all", el)
			if callErr != nil {
				predErr = callErr
				return false
			}
			if !match {
				allMatch = false
			}
			return match
		},
		func(k, v any) bool {
			match, callErr := callPredicate(call, "iter.all", k, v)
			if callErr != nil {
				predErr = callErr
				return false
			}
			if !match {
				allMatch = false
			}
			return match
		},
	)
	if !ok {
		return nil, fmt.Errorf("iter.all(): first argument must be an iterator, got %T", args[0])
	}
	if predErr != nil {
		return nil, predErr
	}
	return allMatch, nil
}

func iterNoneFunc(mc *vm.Machine, args ...any) (result any, err error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("iter.none() expects 2 arguments (source, fn), got %d", len(args))
	}
	defer recoverIterErr(&err)

	fn := args[1]
	call := vm.NewReusableCall(mc, fn)
	noneMatch := true
	var predErr error
	ok := vm.IterateSeqSource(args[0],
		func(el any) bool {
			match, callErr := callPredicate(call, "iter.none", el)
			if callErr != nil {
				predErr = callErr
				return false
			}
			if match {
				noneMatch = false
			}
			return !match
		},
		func(k, v any) bool {
			match, callErr := callPredicate(call, "iter.none", k, v)
			if callErr != nil {
				predErr = callErr
				return false
			}
			if match {
				noneMatch = false
			}
			return !match
		},
	)
	if !ok {
		return nil, fmt.Errorf("iter.none(): first argument must be an iterator, got %T", args[0])
	}
	if predErr != nil {
		return nil, predErr
	}
	return noneMatch, nil
}

// iterContainsFunc is `in`'s explicit iterator-accepting complement (see
// vm.inValue's own doc comment in vm/in.go) - needle-first argument order
// to match `needle in haystack`. Matches inValue's own convention exactly:
// for a list-like source, scans values; for a paired source, scans KEYS,
// not values - both short-circuiting at the first match. Named `contains`,
// not `in` - `in` is a reserved keyword (the `in` operator itself), so
// `iter.in` can't even parse as member access at all.
func iterContainsFunc(mc *vm.Machine, args ...any) (result any, err error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("iter.contains() expects 2 arguments (needle, source), got %d", len(args))
	}
	defer recoverIterErr(&err)

	needle := args[0]
	found := false
	var cmpErr error
	compareEqual := func(other any) bool {
		res, e := mc.Combine(needle, other, vm.OpEqual)
		if e != nil {
			cmpErr = e
			return false
		}
		eq, _ := res.(bool)
		if eq {
			found = true
		}
		return !eq
	}
	ok := vm.IterateSeqSource(args[1],
		func(el any) bool { return compareEqual(el) },
		func(k, _ any) bool { return compareEqual(k) },
	)
	if !ok {
		return nil, fmt.Errorf("iter.contains(): second argument must be an iterator, got %T", args[1])
	}
	if cmpErr != nil {
		return nil, cmpErr
	}
	return found, nil
}
