package vm

import "testing"

// TestForEachSeqElementOverIterSeq covers ForEachSeqElement's iter.Seq[T]
// branch (a func(func(T) bool), Go 1.23's push-iterator shape), reached
// via asSeqYield's reflect-based shape detection.
func TestForEachSeqElementOverIterSeq(t *testing.T) {
	seq := func(yield func(int64) bool) {
		for _, v := range []int64{1, 2, 3} {
			if !yield(v) {
				return
			}
		}
	}
	var got []int64
	ok := ForEachSeqElement(seq, func(el any) bool {
		got = append(got, el.(int64))
		return true
	})
	if !ok {
		t.Fatal("ForEachSeqElement did not recognize an iter.Seq[int64]")
	}
	if len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("got %v, want [1 2 3]", got)
	}
}

// TestForEachSeqElementOverIterSeqEarlyExit covers yield returning false
// stopping iteration early through the iter.Seq bridge.
func TestForEachSeqElementOverIterSeqEarlyExit(t *testing.T) {
	seq := func(yield func(int64) bool) {
		for _, v := range []int64{1, 2, 3, 4, 5} {
			if !yield(v) {
				return
			}
		}
	}
	var got []int64
	ForEachSeqElement(seq, func(el any) bool {
		got = append(got, el.(int64))
		return len(got) < 2
	})
	if len(got) != 2 {
		t.Errorf("got %v, want exactly 2 elements (early exit)", got)
	}
}

// TestForEachSeq2PairOverIterSeq2 covers ForEachSeq2Pair's iter.Seq2[K, V]
// branch.
func TestForEachSeq2PairOverIterSeq2(t *testing.T) {
	seq := func(yield func(string, int64) bool) {
		pairs := []struct {
			k string
			v int64
		}{{"a", 1}, {"b", 2}}
		for _, p := range pairs {
			if !yield(p.k, p.v) {
				return
			}
		}
	}
	got := map[string]int64{}
	ok := ForEachSeq2Pair(seq, func(k, v any) bool {
		got[k.(string)] = v.(int64)
		return true
	})
	if !ok {
		t.Fatal("ForEachSeq2Pair did not recognize an iter.Seq2[string, int64]")
	}
	if len(got) != 2 || got["a"] != 1 || got["b"] != 2 {
		t.Errorf("got %v, want {a:1 b:2}", got)
	}
}

// TestForEachMapPairOverReflectedMap covers ForEachMapPair's reflect.Map
// branch (a map type other than map[string]any).
func TestForEachMapPairOverReflectedMap(t *testing.T) {
	m := map[string]int64{"a": 1, "b": 2}
	got := map[string]int64{}
	ok := ForEachMapPair(m, func(k, v any) bool {
		got[k.(string)] = v.(int64)
		return true
	})
	if !ok {
		t.Fatal("ForEachMapPair did not recognize a map[string]int64")
	}
	if len(got) != 2 {
		t.Errorf("got %v, want 2 entries", got)
	}
}

// TestForEachListElementNotRecognized, TestForEachSeqElementNotRecognized,
// TestForEachMapPairNotRecognized, and TestForEachSeq2PairNotRecognized
// cover the ok=false path for a value that's none of the shapes each
// function individually recognizes.
func TestForEachListElementNotRecognized(t *testing.T) {
	if ok := ForEachListElement(nil, int64(5), func(el any) bool { return true }); ok {
		t.Error("ForEachListElement should not recognize a plain int64")
	}
}

func TestForEachSeqElementNotRecognized(t *testing.T) {
	if ok := ForEachSeqElement(int64(5), func(el any) bool { return true }); ok {
		t.Error("ForEachSeqElement should not recognize a plain int64")
	}
}

func TestForEachMapPairNotRecognized(t *testing.T) {
	if ok := ForEachMapPair(int64(5), func(k, v any) bool { return true }); ok {
		t.Error("ForEachMapPair should not recognize a plain int64")
	}
}

func TestForEachSeq2PairNotRecognized(t *testing.T) {
	if ok := ForEachSeq2Pair(int64(5), func(k, v any) bool { return true }); ok {
		t.Error("ForEachSeq2Pair should not recognize a plain int64")
	}
}

// TestForEachSeqElementRejectsWrongShapedFunc covers asSeqYield's two
// shape mismatch branches: a Go function value that isn't shaped like
// iter.Seq[T] - func(func(T) bool) - must be reported as unrecognized
// (ok=false), not mistaken for one.
func TestForEachSeqElementRejectsWrongShapedFunc(t *testing.T) {
	wrongArgCount := func(a, b int) {}
	if ok := ForEachSeqElement(wrongArgCount, func(el any) bool { return true }); ok {
		t.Error("ForEachSeqElement should reject a func with the wrong argument count")
	}

	wrongYieldShape := func(yield func(int, int) bool) {}
	if ok := ForEachSeqElement(wrongYieldShape, func(el any) bool { return true }); ok {
		t.Error("ForEachSeqElement should reject a func whose parameter isn't a func(T) bool")
	}
}

// TestForEachSeq2PairRejectsWrongShapedFunc mirrors the above for
// asSeq2Yield/ForEachSeq2Pair.
func TestForEachSeq2PairRejectsWrongShapedFunc(t *testing.T) {
	wrongArgCount := func(a, b int) {}
	if ok := ForEachSeq2Pair(wrongArgCount, func(k, v any) bool { return true }); ok {
		t.Error("ForEachSeq2Pair should reject a func with the wrong argument count")
	}

	wrongYieldShape := func(yield func(int) bool) {}
	if ok := ForEachSeq2Pair(wrongYieldShape, func(k, v any) bool { return true }); ok {
		t.Error("ForEachSeq2Pair should reject a func whose parameter isn't a func(K, V) bool")
	}
}

// TestFastSliceElementIterateAllHardcodedTypes covers every one of
// fastSliceElementIterate's hardcoded element-type cases (int, int64,
// float64, string, bool) by calling ForEachListElement directly. Existing
// sum/min/max tests over []int64/[]float64 don't reach this at all - they
// go through builtin_funcs.go's own native fast path instead, bypassing
// vm.ForEachListElement entirely; only map()/filter()/reduce() over a
// typed slice argument would reach fastSliceElementIterate, and no
// existing test does that either.
func TestFastSliceElementIterateAllHardcodedTypes(t *testing.T) {
	mc := mustVM(t)

	var gotInts []int
	ok := ForEachListElement(mc, []int{1, 2}, func(el any) bool {
		gotInts = append(gotInts, el.(int))
		return true
	})
	if !ok || len(gotInts) != 2 {
		t.Errorf("ForEachListElement over []int = %v, ok=%v", gotInts, ok)
	}

	var gotInt64s []int64
	ok = ForEachListElement(mc, []int64{1, 2, 3}, func(el any) bool {
		gotInt64s = append(gotInt64s, el.(int64))
		return true
	})
	if !ok || len(gotInt64s) != 3 {
		t.Errorf("ForEachListElement over []int64 = %v, ok=%v", gotInt64s, ok)
	}

	var gotFloats []float64
	ok = ForEachListElement(mc, []float64{1.5, 2.5}, func(el any) bool {
		gotFloats = append(gotFloats, el.(float64))
		return true
	})
	if !ok || len(gotFloats) != 2 {
		t.Errorf("ForEachListElement over []float64 = %v, ok=%v", gotFloats, ok)
	}

	var gotStrings []string
	ok = ForEachListElement(mc, []string{"a", "b"}, func(el any) bool {
		gotStrings = append(gotStrings, el.(string))
		return true
	})
	if !ok || len(gotStrings) != 2 {
		t.Errorf("ForEachListElement over []string = %v, ok=%v", gotStrings, ok)
	}

	var gotBools []bool
	ok = ForEachListElement(mc, []bool{true, false, true}, func(el any) bool {
		gotBools = append(gotBools, el.(bool))
		return true
	})
	if !ok || len(gotBools) != 3 {
		t.Errorf("ForEachListElement over []bool = %v, ok=%v", gotBools, ok)
	}
}

// TestFastSliceElementIterateEarlyExit covers each hardcoded case's own
// "yield returned false, stop" branch - the loop above only ever returns
// true from yield.
func TestFastSliceElementIterateEarlyExit(t *testing.T) {
	mc := mustVM(t)
	cases := []any{
		[]int{1, 2, 3},
		[]int64{1, 2, 3},
		[]float64{1, 2, 3},
		[]string{"a", "b", "c"},
		[]bool{true, true, true},
	}
	for _, slice := range cases {
		n := 0
		ForEachListElement(mc, slice, func(el any) bool {
			n++
			return n < 2
		})
		if n != 2 {
			t.Errorf("ForEachListElement over %T stopped after %d elements, want 2 (early exit)", slice, n)
		}
	}
}

// customFastIterType is a named type with no core TypeCode recognition,
// used to exercise fastSliceElementIterate's RegisterFastSliceIterator
// fallback (the loop over mc.fastSliceIterators after the hardcoded
// switch).
type customFastIterType struct{ n int64 }

func TestFastSliceElementIterateRegisteredFallback(t *testing.T) {
	mc, err := UnconfiguredVM(RegisterFastSliceIterator(customFastIterType{}, func(slice any, yield func(any) bool) bool {
		arr, ok := slice.([]customFastIterType)
		if !ok {
			return false
		}
		for _, v := range arr {
			if !yield(v) {
				break
			}
		}
		return true
	}))
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}

	slice := []customFastIterType{{1}, {2}, {3}}
	var got []int64
	ok := ForEachListElement(mc, slice, func(el any) bool {
		got = append(got, el.(customFastIterType).n)
		return true
	})
	if !ok || len(got) != 3 {
		t.Errorf("ForEachListElement over registered custom slice type = %v, ok=%v", got, ok)
	}
}

// TestRegisterFastSliceIteratorRejectsNilIterator mirrors the nil-handler
// guard RegisterOperation already has.
func TestRegisterFastSliceIteratorRejectsNilIterator(t *testing.T) {
	if _, err := UnconfiguredVM(RegisterFastSliceIterator(customFastIterType{}, nil)); err == nil {
		t.Error("expected RegisterFastSliceIterator(nil iterator) to error")
	}
}

// TestFastSliceElementIterateUnrecognizedElementFallsBack covers
// ForEachListElement's generic reflect-per-element path for a slice whose
// element type has no fast path at all (neither hardcoded nor a
// registered FastSliceIterator).
func TestFastSliceElementIterateUnrecognizedElementFallsBack(t *testing.T) {
	mc := mustVM(t)
	type plain struct{ n int }
	slice := []plain{{1}, {2}}
	var got []int
	ok := ForEachListElement(mc, slice, func(el any) bool {
		got = append(got, el.(plain).n)
		return true
	})
	if !ok || len(got) != 2 {
		t.Errorf("ForEachListElement over unrecognized slice type = %v, ok=%v", got, ok)
	}
}

// TestForEachListElementGenericReflectEarlyExit covers
// ForEachListElement's generic (non-fast-path) reflect loop's own
// early-exit branch, for a slice element type with no fast path at all.
func TestForEachListElementGenericReflectEarlyExit(t *testing.T) {
	mc := mustVM(t)
	type plain struct{ n int }
	slice := []plain{{1}, {2}, {3}}
	n := 0
	ForEachListElement(mc, slice, func(el any) bool {
		n++
		return n < 2
	})
	if n != 2 {
		t.Errorf("ForEachListElement stopped after %d elements, want 2 (early exit)", n)
	}
}

// TestForEachMapPairReflectedMapEarlyExit covers ForEachMapPair's
// reflect.Map branch's own early-exit behavior.
func TestForEachMapPairReflectedMapEarlyExit(t *testing.T) {
	m := map[string]int64{"a": 1, "b": 2, "c": 3}
	n := 0
	ForEachMapPair(m, func(k, v any) bool {
		n++
		return n < 1
	})
	if n != 1 {
		t.Errorf("ForEachMapPair stopped after %d pairs, want 1 (early exit)", n)
	}
}

// TestIterateListOrMapSourceFallsBackToPairSource covers
// IterateListOrMapSource's own try-list-then-try-pair dispatch when the
// value is a map, not a list.
func TestIterateListOrMapSourceFallsBackToPairSource(t *testing.T) {
	mc := mustVM(t)
	m := map[string]any{"a": int64(1)}
	var elCalls, pairCalls int
	ok := IterateListOrMapSource(mc, m,
		func(el any) bool { elCalls++; return true },
		func(k, v any) bool { pairCalls++; return true },
	)
	if !ok || elCalls != 0 || pairCalls != 1 {
		t.Errorf("ok=%v elCalls=%d pairCalls=%d, want ok=true elCalls=0 pairCalls=1", ok, elCalls, pairCalls)
	}
}

// TestIterateSeqSourceFallsBackToPairSource mirrors the above for
// IterateSeqSource - the value is an iter.Seq2, not an iter.Seq, so
// dispatch has to fall through to the pair callback.
func TestIterateSeqSourceFallsBackToPairSource(t *testing.T) {
	seq := func(yield func(string, int64) bool) {
		yield("a", 1)
	}
	var elCalls, pairCalls int
	ok := IterateSeqSource(seq,
		func(el any) bool { elCalls++; return true },
		func(k, v any) bool { pairCalls++; return true },
	)
	if !ok || elCalls != 0 || pairCalls != 1 {
		t.Errorf("ok=%v elCalls=%d pairCalls=%d, want ok=true elCalls=0 pairCalls=1", ok, elCalls, pairCalls)
	}
}
