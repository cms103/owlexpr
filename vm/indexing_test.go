package vm

import "testing"

// runIndex builds `target[idx]` bytecode and runs it. targetVar names an
// env variable (always resolved via OpLoad); idx is always pushed as a
// literal, regardless of its Go type - unlike operandInstruction's
// var-or-literal convention, an index/key value here is never itself a
// variable reference.
func runIndex(t *testing.T, mc *Machine, env map[string]any, targetVar string, idx any) (any, error) {
	t.Helper()
	instructions := []Instruction{
		{Op: OpLoad, Arg: targetVar},
		{Op: OpPush, Arg: idx},
		{Op: OpIndex},
	}
	return mc.Run(instructions, env)
}

// runSlice builds `target[low:high]` bytecode and runs it. low/high may be
// nil, matching an omitted bound.
func runSlice(t *testing.T, mc *Machine, env map[string]any, targetVar string, low, high any) (any, error) {
	t.Helper()
	instructions := []Instruction{
		{Op: OpLoad, Arg: targetVar},
		{Op: OpPush, Arg: low},
		{Op: OpPush, Arg: high},
		{Op: OpSlice},
	}
	return mc.Run(instructions, env)
}

func mustVM(t *testing.T) *Machine {
	t.Helper()
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	return mc
}

// TestIndexValueReflectedSlice covers indexValue's reflect.Slice fallback
// (a concrete typed slice other than []any, e.g. []int64 from the
// environment), including its out-of-range error.
func TestIndexValueReflectedSlice(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"xs": []int64{10, 20, 30}}
	got, err := runIndex(t, mc, env, "xs", int64(1))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != int64(20) {
		t.Errorf("xs[1] = %v, want 20", got)
	}

	if _, err := runIndex(t, mc, env, "xs", int64(5)); err == nil {
		t.Error("expected an out-of-range error for xs[5]")
	}
}

// TestIndexValueReflectedArray covers indexValue's reflect.Array case.
func TestIndexValueReflectedArray(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"xs": [3]string{"a", "b", "c"}}
	got, err := runIndex(t, mc, env, "xs", int64(2))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != "c" {
		t.Errorf(`xs[2] = %v, want "c"`, got)
	}
}

// TestIndexValueReflectedString covers indexValue's reflect.String case
// (a named string type, as opposed to a plain `string` which has its own
// direct type switch elsewhere in the codebase - here only via reflection
// since indexValue itself has no bare `case string:`).
type namedString string

func TestIndexValueReflectedString(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"s": namedString("hello")}
	got, err := runIndex(t, mc, env, "s", int64(1))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != "e" {
		t.Errorf(`s[1] = %v, want "e"`, got)
	}
	if _, err := runIndex(t, mc, env, "s", int64(99)); err == nil {
		t.Error("expected an out-of-range error for s[99]")
	}
}

// TestIndexValueReflectedMap covers indexValue's reflect.Map fallback (a
// map type other than map[string]any, e.g. map[string]int), including its
// key-conversion and not-found error paths.
func TestIndexValueReflectedMap(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"m": map[string]int{"a": 1, "b": 2}}
	got, err := runIndex(t, mc, env, "m", "b")
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != 2 {
		t.Errorf(`m["b"] = %v, want 2`, got)
	}
	if _, err := runIndex(t, mc, env, "m", "missing"); err == nil {
		t.Error("expected a not-found error for m[\"missing\"]")
	}
	if _, err := runIndex(t, mc, env, "m", nil); err == nil {
		t.Error("expected an error indexing a map with a nil key")
	}
	if _, err := runIndex(t, mc, env, "m", int64(5)); err == nil {
		t.Error("expected an error indexing a map[string]int with a non-string key")
	}
}

// TestIndexValueUnsupportedType covers indexValue's final default error
// for a target type that can't be indexed at all.
func TestIndexValueUnsupportedType(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"n": int64(5)}
	if _, err := runIndex(t, mc, env, "n", int64(0)); err == nil {
		t.Error("expected an error indexing into an int64")
	}
}

// TestIndexValueBadIndexType covers indexValue's []any and map[string]any
// key/index-type-mismatch errors (a float index for a list, a non-string
// key for a map[string]any).
func TestIndexValueBadIndexType(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{
		"xs": []any{int64(1), int64(2)},
		"m":  map[string]any{"a": int64(1)},
	}
	if _, err := runIndex(t, mc, env, "xs", 1.5); err == nil {
		t.Error("expected an error indexing a list with a float64")
	}
	if _, err := runIndex(t, mc, env, "m", int64(1)); err == nil {
		t.Error("expected an error indexing a map[string]any with a non-string key")
	}
	if _, err := runIndex(t, mc, env, "xs", int64(99)); err == nil {
		t.Error("expected an out-of-range error for xs[99]")
	}
}

// TestSliceValueReflectedSlice, TestSliceValueReflectedArray, and
// TestSliceValueReflectedString cover sliceValue's reflect-based branches
// - existing slice tests (langtest/slice_test.go) only ever sliced []any
// and plain strings, whose fast paths are separate from these.
func TestSliceValueReflectedSlice(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"xs": []int64{1, 2, 3, 4, 5}}
	got, err := runSlice(t, mc, env, "xs", int64(1), int64(3))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	want := []int64{2, 3}
	gotSlice, ok := got.([]int64)
	if !ok || len(gotSlice) != len(want) || gotSlice[0] != want[0] || gotSlice[1] != want[1] {
		t.Errorf("xs[1:3] = %v, want %v", got, want)
	}
}

func TestSliceValueReflectedArray(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"xs": [4]int64{1, 2, 3, 4}}
	got, err := runSlice(t, mc, env, "xs", int64(1), nil)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	gotSlice, ok := got.([]int64)
	if !ok || len(gotSlice) != 3 {
		t.Errorf("xs[1:] = %v, want a 3-element slice", got)
	}
}

func TestSliceValueReflectedString(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"s": namedString("hello")}
	got, err := runSlice(t, mc, env, "s", nil, int64(3))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != "hel" {
		t.Errorf(`s[:3] = %v, want "hel"`, got)
	}
}

// TestSliceValueUnsupportedType covers sliceValue's final default error.
func TestSliceValueUnsupportedType(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"n": int64(5)}
	if _, err := runSlice(t, mc, env, "n", nil, nil); err == nil {
		t.Error("expected an error slicing an int64")
	}
}

// TestResolveSliceBoundsErrors covers resolveSliceBounds' out-of-range and
// inverted-bound guard, plus toIndexInt's own bad-index-type error as
// surfaced through a slice bound.
func TestResolveSliceBoundsErrors(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"xs": []any{int64(1), int64(2), int64(3)}}
	if _, err := runSlice(t, mc, env, "xs", int64(2), int64(1)); err == nil {
		t.Error("expected an error for an inverted slice bound (low > high)")
	}
	if _, err := runSlice(t, mc, env, "xs", int64(-1), nil); err == nil {
		t.Error("expected an error for a negative slice bound")
	}
	if _, err := runSlice(t, mc, env, "xs", int64(0), int64(99)); err == nil {
		t.Error("expected an error for a high bound beyond the sequence length")
	}
	if _, err := runSlice(t, mc, env, "xs", 1.5, nil); err == nil {
		t.Error("expected an error for a non-integer low bound")
	}
}

// TestAccessMemberOnNil covers accessMember's explicit nil-target guard.
func TestAccessMemberOnNil(t *testing.T) {
	mc := mustVM(t)
	instructions := []Instruction{
		{Op: OpLoad, Arg: "x"},
		{Op: OpAccess, Arg: "field"},
	}
	if _, err := mc.Run(instructions, map[string]any{"x": nil}); err == nil {
		t.Error("expected an error accessing a member on nil")
	}
}

// accessTarget is a plain struct used to exercise accessMember's struct
// field and pointer-dereference branches.
type accessTarget struct {
	Name string
}

func (a accessTarget) Greet() string { return "hello " + a.Name }

func TestAccessMemberOnStructAndPointer(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{
		"s":  accessTarget{Name: "Ada"},
		"ps": &accessTarget{Name: "Grace"},
	}

	instructions := []Instruction{
		{Op: OpLoad, Arg: "s"},
		{Op: OpAccess, Arg: "Name"},
	}
	got, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != "Ada" {
		t.Errorf("s.Name = %v, want Ada", got)
	}

	instructions = []Instruction{
		{Op: OpLoad, Arg: "ps"},
		{Op: OpAccess, Arg: "Name"},
	}
	got, err = mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != "Grace" {
		t.Errorf("ps.Name = %v, want Grace", got)
	}

	instructions = []Instruction{
		{Op: OpLoad, Arg: "s"},
		{Op: OpAccess, Arg: "Greet"},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 0}},
	}
	got, err = mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != "hello Ada" {
		t.Errorf("s.Greet() = %v, want %q", got, "hello Ada")
	}
}

// TestAccessMemberNotFound covers accessMember's final "member not found"
// error for a struct field/method that doesn't exist.
func TestAccessMemberNotFound(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"s": accessTarget{Name: "Ada"}}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "s"},
		{Op: OpAccess, Arg: "NoSuchField"},
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Error("expected an error accessing a nonexistent member")
	}
}

// TestAccessMemberOnTypedMap covers accessMember's map[string]V dot-access
// fallback and its own missing-key error.
func TestAccessMemberOnTypedMap(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"m": map[string]int{"count": 5}}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "m"},
		{Op: OpAccess, Arg: "count"},
	}
	got, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != 5 {
		t.Errorf("m.count = %v, want 5", got)
	}

	instructions = []Instruction{
		{Op: OpLoad, Arg: "m"},
		{Op: OpAccess, Arg: "missing"},
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Error("expected an error accessing a missing map key via dot access")
	}
}

// TestAccessMemberCacheDistinctInstances covers Machine.memberCache's most
// important correctness property: it caches a *resolution* (a field/method
// index), never a *value* - so warming the cache against one accessTarget
// instance must not leak that instance's own field value into a later
// access against a different instance of the same type.
func TestAccessMemberCacheDistinctInstances(t *testing.T) {
	mc := mustVM(t)
	instructions := []Instruction{
		{Op: OpLoad, Arg: "s"},
		{Op: OpAccess, Arg: "Name"},
	}

	first, err := mc.Run(instructions, map[string]any{"s": accessTarget{Name: "Ada"}})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if first != "Ada" {
		t.Fatalf("s.Name = %v, want Ada", first)
	}

	// Same instructions, same Machine (so the cache entry populated above
	// is still live), a different accessTarget value entirely - the
	// resolved field index is the same, but the value behind it must not
	// be.
	second, err := mc.Run(instructions, map[string]any{"s": accessTarget{Name: "Grace"}})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if second != "Grace" {
		t.Fatalf("s.Name = %v, want Grace (cache must not have pinned Ada's value)", second)
	}
}

// accessTargetWithPtrField has a struct-pointer field, for exercising
// Machine.memberCache's cache-hit path against a nil pointer - a resolution
// warmed from an earlier, non-nil instance must still produce a clean
// error rather than a panic when a later instance's pointer is nil.
type accessTargetWithPtrField struct {
	Inner *accessTarget
}

func TestAccessMemberCacheThenNilPointerField(t *testing.T) {
	mc := mustVM(t)
	instructions := []Instruction{
		{Op: OpLoad, Arg: "s"},
		{Op: OpAccess, Arg: "Inner"},
		{Op: OpAccess, Arg: "Name"},
	}

	// Warm the cache: a non-nil Inner resolves cleanly, populating a
	// cached field resolution for both accessTargetWithPtrField.Inner and
	// (*accessTarget).Name.
	warm := map[string]any{"s": accessTargetWithPtrField{Inner: &accessTarget{Name: "Ada"}}}
	if got, err := mc.Run(instructions, warm); err != nil || got != "Ada" {
		t.Fatalf("warm-up run: got %v, err %v, want Ada", got, err)
	}

	// Same instructions, same Machine, but this instance's Inner is nil -
	// the cached resolution for accessTargetWithPtrField.Inner still
	// applies (it's the same struct type), but the field it points at now
	// doesn't exist for this value.
	cold := map[string]any{"s": accessTargetWithPtrField{Inner: nil}}
	if _, err := mc.Run(instructions, cold); err == nil {
		t.Error("expected an error accessing a member through a nil pointer field, even with a warm cache")
	}
}

// TestAccessMemberCacheRespectsDisableStructMethods confirms the
// memberCache never lets a method become reachable on a Machine built
// with DisableStructMethods, and that repeating the same access (now
// against the struct-field path only) still resolves correctly on a
// warmed cache.
func TestAccessMemberCacheRespectsDisableStructMethods(t *testing.T) {
	mc, err := UnconfiguredVM(DisableStructMethods())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	env := map[string]any{"s": accessTarget{Name: "Ada"}}

	methodInstructions := []Instruction{
		{Op: OpLoad, Arg: "s"},
		{Op: OpAccess, Arg: "Greet"},
	}
	fieldInstructions := []Instruction{
		{Op: OpLoad, Arg: "s"},
		{Op: OpAccess, Arg: "Name"},
	}

	// Run each twice: the first run is a cache miss, the second a cache
	// hit (if anything got cached at all) - DisableStructMethods must hold
	// on both.
	for i := 0; i < 2; i++ {
		if _, err := mc.Run(methodInstructions, env); err == nil {
			t.Errorf("run %d: expected DisableStructMethods to make s.Greet fail as an unknown member", i)
		}
		got, err := mc.Run(fieldInstructions, env)
		if err != nil {
			t.Fatalf("run %d: field access error: %v", i, err)
		}
		if got != "Ada" {
			t.Errorf("run %d: s.Name = %v, want Ada", i, got)
		}
	}
}
