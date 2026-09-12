package stdlib

import (
	"math"
	"testing"

	"github.com/cms103/owlexpr"
)

// evalBits parses, compiles, and runs input with BitsBuiltins() enabled,
// failing the test on any parse/compile/run error.
func evalBits(t *testing.T, env map[string]any, input string) any {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(BitsBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	return res
}

func evalBitsExpectError(t *testing.T, env map[string]any, input string) {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(BitsBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Fatalf("expected an error for %q, got none", input)
	}
}

func TestBitsNotRegisteredByDefault(t *testing.T) {
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	instructions, err := owlexpr.Compile(`bits.bitAnd(1, 1)`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Fatalf("expected bits.bitAnd() to be undefined without BitsBuiltins()")
	}
}

func TestBitsWideValuesDoNotTruncate(t *testing.T) {
	// The whole point of a separate int64-width pack: values wider than a
	// byte must survive, unlike bytes.bitAnd's truncate-to-8-bits behavior.
	env := map[string]any{"mask": int64(0x0F0F0F0F00000000)}
	if got := evalBits(t, env, `bits.bitAnd(mask, mask)`); got != int64(0x0F0F0F0F00000000) {
		t.Errorf("bits.bitAnd(mask, mask) = %#x, want %#x", got, int64(0x0F0F0F0F00000000))
	}
	if got := evalBits(t, env, `bits.bitOr(mask, 1)`); got != int64(0x0F0F0F0F00000001) {
		t.Errorf("bits.bitOr(mask, 1) = %#x, want %#x", got, int64(0x0F0F0F0F00000001))
	}
	if got := evalBits(t, nil, `bits.shiftLeft(1, 40)`); got != int64(1)<<40 {
		t.Errorf("bits.shiftLeft(1, 40) = %v, want %v", got, int64(1)<<40)
	}
}

// TestBitsDayMaskUseCase models the bitmask-column use case this pack
// exists for: a "days of week" recurrence field packed into one int64.
func TestBitsDayMaskUseCase(t *testing.T) {
	const (
		monday  = int64(1) << 0
		tuesday = int64(1) << 1
	)
	env := map[string]any{"dayMask": monday | tuesday}
	if got := evalBits(t, env, `bits.bitAnd(dayMask, 1) != 0`); got != true {
		t.Errorf("dayMask & MONDAY != 0 = %v, want true", got)
	}
	if got := evalBits(t, env, `bits.bitTest(dayMask, 2)`); got != false {
		t.Errorf("bits.bitTest(dayMask, 2) (Wednesday) = %v, want false", got)
	}
}

func TestBitsBitwiseOps(t *testing.T) {
	if got := evalBits(t, nil, `bits.bitAnd(12, 10)`); got != int64(8) {
		t.Errorf("bits.bitAnd(12, 10) = %v, want 8", got)
	}
	if got := evalBits(t, nil, `bits.bitOr(12, 10)`); got != int64(14) {
		t.Errorf("bits.bitOr(12, 10) = %v, want 14", got)
	}
	if got := evalBits(t, nil, `bits.bitXor(12, 10)`); got != int64(6) {
		t.Errorf("bits.bitXor(12, 10) = %v, want 6", got)
	}
	if got := evalBits(t, nil, `bits.bitNot(0)`); got != int64(-1) {
		t.Errorf("bits.bitNot(0) = %v, want -1", got)
	}
	if got := evalBits(t, nil, `bits.shiftLeft(1, 4)`); got != int64(16) {
		t.Errorf("bits.shiftLeft(1, 4) = %v, want 16", got)
	}
	if got := evalBits(t, nil, `bits.shiftRight(240, 4)`); got != int64(15) {
		t.Errorf("bits.shiftRight(240, 4) = %v, want 15", got)
	}
	// shiftRight uses Go's native (arithmetic, sign-extending) >> on
	// int64 - a negative operand must stay negative, not turn into a
	// huge positive value the way a logical shift would.
	if got := evalBits(t, nil, `bits.shiftRight(-8, 1)`); got != int64(-4) {
		t.Errorf("bits.shiftRight(-8, 1) = %v, want -4 (arithmetic/sign-extending shift)", got)
	}
	evalBitsExpectError(t, nil, `bits.shiftLeft(1, -1)`)
}

func TestBitsTestSetClear(t *testing.T) {
	env := map[string]any{"v": int64(0x81)} // 1000 0001
	if got := evalBits(t, env, `bits.bitTest(v, 0)`); got != true {
		t.Errorf("bits.bitTest(0x81, 0) = %v, want true", got)
	}
	if got := evalBits(t, env, `bits.bitTest(v, 1)`); got != false {
		t.Errorf("bits.bitTest(0x81, 1) = %v, want false", got)
	}
	if got := evalBits(t, env, `bits.bitTest(v, 63)`); got != false {
		t.Errorf("bits.bitTest(0x81, 63) = %v, want false", got)
	}
	if got := evalBits(t, nil, `bits.bitSet(0, 3)`); got != int64(8) {
		t.Errorf("bits.bitSet(0, 3) = %v, want 8", got)
	}
	if got := evalBits(t, nil, `bits.bitSet(0, 62)`); got != int64(1)<<62 {
		t.Errorf("bits.bitSet(0, 62) = %v, want %v", got, int64(1)<<62)
	}
	// idx 63 is the sign bit - setting it must produce math.MinInt64, not
	// panic or silently wrap to something else.
	if got := evalBits(t, nil, `bits.bitSet(0, 63)`); got != int64(math.MinInt64) {
		t.Errorf("bits.bitSet(0, 63) = %v, want %v (sign bit)", got, int64(math.MinInt64))
	}
	if got := evalBits(t, nil, `bits.bitClear(255, 0)`); got != int64(254) {
		t.Errorf("bits.bitClear(255, 0) = %v, want 254", got)
	}
	env["allBits"] = int64(-1)
	if got := evalBits(t, env, `bits.bitClear(allBits, 63)`); got != int64(math.MaxInt64) {
		t.Errorf("bits.bitClear(-1, 63) = %v, want %v (sign bit cleared)", got, int64(math.MaxInt64))
	}
	evalBitsExpectError(t, nil, `bits.bitTest(0, 64)`) // out of range
	evalBitsExpectError(t, nil, `bits.bitTest(0, -1)`)
}

func TestBitsPopCount(t *testing.T) {
	if got := evalBits(t, nil, `bits.popCount(255)`); got != int64(8) {
		t.Errorf("bits.popCount(255) = %v, want 8", got)
	}
	if got := evalBits(t, nil, `bits.popCount(0)`); got != int64(0) {
		t.Errorf("bits.popCount(0) = %v, want 0", got)
	}
	if got := evalBits(t, nil, `bits.popCount(-1)`); got != int64(64) {
		t.Errorf("bits.popCount(-1) = %v, want 64 (all bits set)", got)
	}
}

// TestBitsIndexOutOfRangeErrors covers bitsIndexArg's own 0-63 range
// guard, shared by bitTest/bitSet/bitClear.
func TestBitsIndexOutOfRangeErrors(t *testing.T) {
	env := map[string]any{"b": int64(1)}
	evalBitsExpectError(t, env, `bits.bitTest(b, 64)`)
	evalBitsExpectError(t, env, `bits.bitSet(b, -1)`)
	evalBitsExpectError(t, env, `bits.bitClear(b, 100)`)
}

// TestBitsShiftNegativeAmountErrors covers shiftLeft/shiftRight's own
// negative-shift-amount guard.
func TestBitsShiftNegativeAmountErrors(t *testing.T) {
	env := map[string]any{"b": int64(1)}
	evalBitsExpectError(t, env, `bits.shiftLeft(b, -1)`)
	evalBitsExpectError(t, env, `bits.shiftRight(b, -1)`)
}

// TestBitsBuiltinArgumentCountErrors is a broad sweep of each function's
// own arity guard, in one table rather than one test function each.
func TestBitsBuiltinArgumentCountErrors(t *testing.T) {
	cases := []string{
		`bits.bitAnd(1)`,
		`bits.bitOr(1)`,
		`bits.bitXor(1)`,
		`bits.bitNot()`,
		`bits.shiftLeft(1)`,
		`bits.shiftRight(1)`,
		`bits.bitTest(1)`,
		`bits.bitSet(1)`,
		`bits.bitClear(1)`,
		`bits.popCount()`,
	}
	for _, c := range cases {
		evalBitsExpectError(t, nil, c)
	}
}

// TestBitsArgTypeErrors covers argInt's wrong-type branch as reached
// through the bits.* functions.
func TestBitsArgTypeErrors(t *testing.T) {
	evalBitsExpectError(t, nil, `bits.bitNot("not an int")`)
	evalBitsExpectError(t, nil, `bits.bitAnd("not an int", 1)`)
}
