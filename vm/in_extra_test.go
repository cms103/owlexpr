package vm

import "testing"

// runIn builds `needle in haystack` bytecode and runs it.
func runIn(t *testing.T, mc *Machine, env map[string]any, needleVar, haystackVar string) (any, error) {
	t.Helper()
	instructions := []Instruction{
		{Op: OpLoad, Arg: needleVar},
		{Op: OpLoad, Arg: haystackVar},
		{Op: OpIn},
	}
	return mc.Run(instructions, env)
}

// TestInOverReflectedMap covers inValue's reflect.Map branch (mapContainsKey)
// for a map type other than map[string]any, including a needle that
// simply isn't the right type to be a key (returns false, not an error).
func TestInOverReflectedMap(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{
		"m":         map[string]int64{"a": 1, "b": 2},
		"present":   "a",
		"absent":    "z",
		"wrongType": int64(5),
	}
	got, err := runIn(t, mc, env, "present", "m")
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != true {
		t.Errorf(`"a" in m = %v, want true`, got)
	}

	got, err = runIn(t, mc, env, "absent", "m")
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != false {
		t.Errorf(`"z" in m = %v, want false`, got)
	}

	got, err = runIn(t, mc, env, "wrongType", "m")
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != false {
		t.Errorf("int64(5) in map[string]int64 = %v, want false (not an error)", got)
	}
}

// TestInOverReflectedMapNilAndInconvertibleNeedle covers mapContainsKey's
// two "not found" branches that TestInOverReflectedMap's wrongType case
// doesn't reach: a nil needle (invalid reflect.Value), and a needle whose
// type is genuinely inconvertible to the map's key type (as opposed to
// merely a different-but-convertible numeric/string type).
func TestInOverReflectedMapNilAndInconvertibleNeedle(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{
		"m":      map[string]int64{"a": 1},
		"nilKey": nil,
		"struct": accessTarget{Name: "x"},
	}
	got, err := runIn(t, mc, env, "nilKey", "m")
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != false {
		t.Errorf("nil in map[string]int64 = %v, want false", got)
	}

	got, err = runIn(t, mc, env, "struct", "m")
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != false {
		t.Errorf("a struct in map[string]int64 = %v, want false (inconvertible key type)", got)
	}
}

// TestInRejectsIterSeq2PairSource covers inValue's ForEachListElement-only
// scan for a paired (iter.Seq2) haystack - an iter.Seq2 simply doesn't
// match any shape ForEachListElement or the map branches above recognize,
// so it falls through to the same generic error as any other
// unsupported type.
func TestInRejectsIterSeq2PairSource(t *testing.T) {
	mc := mustVM(t)
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
	env := map[string]any{"haystack": seq, "needle": "b"}
	if _, err := runIn(t, mc, env, "needle", "haystack"); err == nil {
		t.Fatal("expected an error for `in` against an iter.Seq2 haystack, got none")
	}
}

// TestInUnsupportedHaystackErrors covers inValue's final error for a
// right-hand operand that's neither list-like nor pair-like.
func TestInUnsupportedHaystackErrors(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"needle": int64(1), "haystack": int64(5)}
	if _, err := runIn(t, mc, env, "needle", "haystack"); err == nil {
		t.Error("expected an error for `in` against an int64 haystack")
	}
}

// TestInStringKeyedMapWithNonStringNeedle covers inValue's map[string]any
// fast path returning false (not an error) for a needle that isn't a
// string.
func TestInStringKeyedMapWithNonStringNeedle(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"m": map[string]any{"a": int64(1)}, "needle": int64(1)}
	got, err := runIn(t, mc, env, "needle", "m")
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != false {
		t.Errorf("int64(1) in map[string]any = %v, want false", got)
	}
}

// TestInListWithCombineErrorPropagates covers inValue's iterErr plumbing:
// an element that can't be compared to the needle via Combine(OpEqual)
// must abort `in` with that error, not silently report "not found".
func TestInListWithCombineErrorPropagates(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"needle": true, "haystack": []any{int64(1), int64(2)}}
	if _, err := runIn(t, mc, env, "needle", "haystack"); err == nil {
		t.Error("expected an error comparing a bool needle against int64 elements")
	}
}
