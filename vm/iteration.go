package vm

import (
	"reflect"
)

// ForEachListElement iterates a concrete list-like source only - []any or
// any other slice/array (via reflection) - calling yield once per
// element. yield returning false stops iteration early. ok reports
// whether v was recognized as this kind of source at all; when ok is
// false, yield was never called. Push iterator (iter.Seq[T]) is not supported.
func ForEachListElement(mc *Machine, v any, yield func(el any) bool) (ok bool) {
	if list, isList := v.([]any); isList {
		for _, el := range list {
			if !yield(el) {
				break
			}
		}
		return true
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		// This is the faster route - if we can figure out the type of slice
		// E.g. []int64, []float64 we can iterate directly instead of using reflection
		if rv.Kind() == reflect.Slice && fastSliceElementIterate(mc, rv, yield) {
			return true
		}

		// If we don't know this kind of slice, we can still iterate, but we have to do it the
		// slower way
		n := rv.Len()
		for i := 0; i < n; i++ {
			if !yield(rv.Index(i).Interface()) {
				break
			}
		}
		return true
	}
	return false
}

// ForEachListElementReversed is ForEachListElement's mirror image: it
// visits elements from the end backward, so stdlib's findLast/
// findLastIndex can short-circuit on the *last* matching element without
// paying to box the whole list first. find/findIndex already get that
// short-circuit going forward, for free, from ForEachListElement; findLast
// had no backward equivalent and was materializing every element up front
// via ListElements before its walk ever started, even when the match was
// a handful of elements from the end. yield receives each element's
// original index alongside its value - a backward walk already knows its
// position for free, and findLastIndex needs it, so there's no separate
// length lookup required just to recover it. yield returning false stops
// iteration early. ok reports whether v was recognized as this kind of
// source at all; when ok is false, yield was never called. Push iterator
// (iter.Seq[T]) is not supported, matching ForEachListElement.
func ForEachListElementReversed(mc *Machine, v any, yield func(idx int64, el any) bool) (ok bool) {
	if list, isList := v.([]any); isList {
		for i := len(list) - 1; i >= 0; i-- {
			if !yield(int64(i), list[i]) {
				break
			}
		}
		return true
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && fastSliceElementIterateReversed(mc, rv, yield) {
			return true
		}

		for i := rv.Len() - 1; i >= 0; i-- {
			if !yield(int64(i), rv.Index(i).Interface()) {
				break
			}
		}
		return true
	}
	return false
}

// ForEachSeqElement iterates a push-iterator source only - iter.Seq[T] (a
// func(func(T) bool), Go 1.23's push-iterator shape) - not a concrete
// list. ok reports whether it is of the right type.
func ForEachSeqElement(v any, yield func(el any) bool) (ok bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Func {
		return false
	}
	seqEach, isSeq := asSeqYield(rv)
	if !isSeq {
		return false
	}
	seqEach(yield)
	return true
}

// ForEachMapPair iterates a concrete paired source only - a Go map of any
// key/value types - calling yield once per (key, value). yield returning
// false stops iteration early. ok reports whether v was recognized as
// this kind of source at all. Like ForEachListElement above, an
// iter.Seq2[K, V] is simply unrecognized here, not specially rejected -
// see ForEachListElement's doc comment for the reasoning.
func ForEachMapPair(v any, yield func(k, val any) bool) (ok bool) {
	if m, isMap := v.(map[string]any); isMap {
		for k, val := range m {
			if !yield(k, val) {
				break
			}
		}
		return true
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Map {
		iter := rv.MapRange()
		for iter.Next() {
			if !yield(iter.Key().Interface(), iter.Value().Interface()) {
				break
			}
		}
		return true
	}
	return false
}

// ForEachSeq2Pair iterates an iter.Seq2[K, V] source only (a func(func(K,
// V) bool)) - not a real Go map. ok reports whether v had that shape.
// ForEachMapPair's mirror image, for the same reasons ForEachSeqElement
// is ForEachListElement's - see both doc comments.
func ForEachSeq2Pair(v any, yield func(k, val any) bool) (ok bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Func {
		return false
	}
	seqEach, isSeq := asSeq2Yield(rv)
	if !isSeq {
		return false
	}
	seqEach(yield)
	return true
}

// IterateListOrMapSource tries v as a list-like source first
// (ForEachListElement), then as a paired source (ForEachMapPair), running
// whichever one matches - no iterator recognition either way (see
// ForEachListElement's doc comment). This is what lets core
// map/filter/reduce accept either a list or a map and adapt their
// callback arity accordingly (one arg for elements, two for pairs)
// without the caller having to know in advance which kind of source it
// has.
func IterateListOrMapSource(mc *Machine, v any, onElement func(el any) bool, onPair func(k, val any) bool) (ok bool) {
	if ForEachListElement(mc, v, onElement) {
		return true
	}
	return ForEachMapPair(v, onPair)
}

// IterateSeqSource is IterateListOrMapSource's iterator-only counterpart -
// ForEachSeqElement then ForEachSeq2Pair, no list/map recognition either
// way. stdlib's iter.* pack (map/filter/reduce/toList/contains) is built
// on this - a plain list/map argument simply doesn't match either shape
// and falls through to the same generic type-mismatch error as any other
// wrong type, the same "let it fail naturally" pattern
// IterateListOrMapSource's own callers use for the opposite case.
func IterateSeqSource(v any, onElement func(el any) bool, onPair func(k, val any) bool) (ok bool) {
	if ForEachSeqElement(v, onElement) {
		return true
	}
	return ForEachSeq2Pair(v, onPair)
}

// asSeqYield detects whether seq has the shape of an iter.Seq[T] -
// func(func(T) bool) - for some T, without knowing T at compile time (Go
// generics erase T at the reflect.Type level once boxed in `any`, so this
// is necessarily a shape check, not a type-parameter check). On a match it
// returns an adapter that drives seq with an `any`-based yield, bridging
// the real typed yield callback via reflect.MakeFunc.
func asSeqYield(seq reflect.Value) (each func(yield func(any) bool), ok bool) {
	t := seq.Type()
	if t.NumIn() != 1 || t.NumOut() != 0 {
		return nil, false
	}
	yieldType := t.In(0)
	if yieldType.Kind() != reflect.Func || yieldType.NumIn() != 1 || yieldType.NumOut() != 1 || yieldType.Out(0).Kind() != reflect.Bool {
		return nil, false
	}
	return func(yield func(any) bool) {
		adapter := reflect.MakeFunc(yieldType, func(in []reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(yield(in[0].Interface()))}
		})
		seq.Call([]reflect.Value{adapter})
	}, true
}

// asSeq2Yield is asSeqYield for iter.Seq2[K, V] - func(func(K, V) bool).
func asSeq2Yield(seq reflect.Value) (each func(yield func(k, v any) bool), ok bool) {
	t := seq.Type()
	if t.NumIn() != 1 || t.NumOut() != 0 {
		return nil, false
	}
	yieldType := t.In(0)
	if yieldType.Kind() != reflect.Func || yieldType.NumIn() != 2 || yieldType.NumOut() != 1 || yieldType.Out(0).Kind() != reflect.Bool {
		return nil, false
	}
	return func(yield func(k, v any) bool) {
		adapter := reflect.MakeFunc(yieldType, func(in []reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(yield(in[0].Interface(), in[1].Interface()))}
		})
		seq.Call([]reflect.Value{adapter})
	}, true
}

// fastSliceElementIterate is a TypeCode-driven optimization for
// ForEachListElement's reflect-based slice path: rather than boxing every
// element individually via reflect.Value.Index(i).Interface() (real
// per-element reflection cost), it determines the slice's element
// TypeCode ONCE via mc.typeCode and, if that TypeCode has a known concrete Go
// slice type, does a single type assertion converting the whole slice,
// then iterates it natively with zero further reflection.
//
// This runs once per ForEachListElement call, not once per element, so even
// when it doesn't apply (ok=false: an element type the typer doesn't
// recognize) the cost - one reflect.Zero, one typer call, one failed type
// assertion - is trivially amortized over any list of more than a couple
// of elements. A TypeCode match whose type assertion still fails (possible
// if a custom TypeCoder maps some other named type, e.g. `type Cents
// int64`, onto Int64TypeCode) is handled by simply reporting ok=false, not
// by panicking - the caller falls back to the generic per-element loop.
func fastSliceElementIterate(mc *Machine, rv reflect.Value, yield func(any) bool) (ok bool) {
	elemType := rv.Type().Elem()
	code := mc.typeCode(reflect.Zero(elemType).Interface())

	switch code {
	case IntTypeCode:
		arr, matched := rv.Interface().([]int)
		if !matched {
			return false
		}
		for _, v := range arr {
			if !yield(v) {
				break
			}
		}
		return true

	case Int64TypeCode:
		arr, matched := rv.Interface().([]int64)
		if !matched {
			return false
		}
		for _, v := range arr {
			if !yield(v) {
				break
			}
		}
		return true

	case FloatTypeCode:
		arr, matched := rv.Interface().([]float64)
		if !matched {
			return false
		}
		for _, v := range arr {
			if !yield(v) {
				break
			}
		}
		return true

	case StringTypeCode:
		arr, matched := rv.Interface().([]string)
		if !matched {
			return false
		}
		for _, v := range arr {
			if !yield(v) {
				break
			}
		}
		return true

	case BoolTypeCode:
		arr, matched := rv.Interface().([]bool)
		if !matched {
			return false
		}
		for _, v := range arr {
			if !yield(v) {
				break
			}
		}
		return true
	}

	// code isn't one of the hardcoded cases above - fall back to iterators
	// registered via RegisterFastSliceIterator (e.g. stdlib.DecimalBuiltins'
	// []decimal.Decimal support). This list is expected to stay tiny (a
	// handful of extension types at most), so a linear scan here mirrors
	// opHandlers' own scan in operationsRouter rather than paying for a map.
	for _, entry := range mc.fastSliceIterators {
		if entry.baseType == code {
			return entry.iter(rv.Interface(), yield)
		}
	}
	return false
}

// fastSliceElementIterateReversed is fastSliceElementIterate's mirror
// image for ForEachListElementReversed - same TypeCode-driven, single-
// type-assertion trick, walked back to front instead of front to back.
// Extension slice iterators registered via RegisterFastSliceIterator
// (e.g. stdlib.DecimalBuiltins' []decimal.Decimal support) have no
// reversed counterpart, so a TypeCode that only matches one of those
// falls through to ForEachListElementReversed's generic
// reflect.Value.Index loop instead of a hardcoded case here - still
// correct, just without the extra unboxing this covers for the five core
// types.
func fastSliceElementIterateReversed(mc *Machine, rv reflect.Value, yield func(idx int64, el any) bool) (ok bool) {
	elemType := rv.Type().Elem()
	code := mc.typeCode(reflect.Zero(elemType).Interface())

	switch code {
	case IntTypeCode:
		arr, matched := rv.Interface().([]int)
		if !matched {
			return false
		}
		for i := len(arr) - 1; i >= 0; i-- {
			if !yield(int64(i), arr[i]) {
				break
			}
		}
		return true

	case Int64TypeCode:
		arr, matched := rv.Interface().([]int64)
		if !matched {
			return false
		}
		for i := len(arr) - 1; i >= 0; i-- {
			if !yield(int64(i), arr[i]) {
				break
			}
		}
		return true

	case FloatTypeCode:
		arr, matched := rv.Interface().([]float64)
		if !matched {
			return false
		}
		for i := len(arr) - 1; i >= 0; i-- {
			if !yield(int64(i), arr[i]) {
				break
			}
		}
		return true

	case StringTypeCode:
		arr, matched := rv.Interface().([]string)
		if !matched {
			return false
		}
		for i := len(arr) - 1; i >= 0; i-- {
			if !yield(int64(i), arr[i]) {
				break
			}
		}
		return true

	case BoolTypeCode:
		arr, matched := rv.Interface().([]bool)
		if !matched {
			return false
		}
		for i := len(arr) - 1; i >= 0; i-- {
			if !yield(int64(i), arr[i]) {
				break
			}
		}
		return true
	}

	return false
}
