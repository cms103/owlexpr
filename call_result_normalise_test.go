package owlexpr

import (
	"errors"
	"math"
	"testing"
	"time"
)

// level is a named int32 with no registered operation - it should be
// normalised to int64 like any other unmodelled integer kind.
type level int32

// TestCallResultNormalisesUnmodelledNumericKinds covers
// Machine.normaliseResult: a Go function or method returning an integer
// or float kind owlexpr doesn't model is converted to int64/float64 at the
// call boundary, so arithmetic, comparison and conversion all just work.
func TestCallResultNormalisesUnmodelledNumericKinds(t *testing.T) {
	env := map[string]any{
		"i8":     func() int8 { return -8 },
		"i16":    func() int16 { return 16 },
		"i32":    func() int32 { return -32 },
		"u8":     func() uint8 { return 8 },
		"u32":    func() uint32 { return 32 },
		"u64":    func() uint64 { return 64 },
		"f32":    func() float32 { return 1.5 },
		"lvl":    func() level { return 3 },
		"i32err": func() (int32, error) { return 7, nil },
	}
	cases := []struct {
		input string
		want  any
	}{
		{"i8()", int64(-8)},
		{"i16()", int64(16)},
		{"i32()", int64(-32)},
		{"u8()", int64(8)},
		{"u32()", int64(32)},
		{"u64()", int64(64)},
		{"f32()", float64(1.5)},
		{"lvl()", int64(3)},
		{"i32err()", int64(7)},
		{"i32() + 2", int64(-30)},
		{"i32() < 0", true},
		{"f32() * 2", float64(3)},
		{"int(i32())", int(-32)},
		{"float64(u8())", float64(8)},
	}
	for _, c := range cases {
		if got := evalRoot(t, env, c.input); got != c.want {
			t.Errorf("%s = %v (%T), want %v (%T)", c.input, got, got, c.want, c.want)
		}
	}
}

// TestCallResultNormaliseLeavesOtherValuesAlone covers the values
// normaliseResult must NOT convert: core types, a uint64 too large for
// int64, and non-numeric kinds.
func TestCallResultNormaliseLeavesOtherValuesAlone(t *testing.T) {
	env := map[string]any{
		"plainInt": func() int { return 5 },
		"bigU64":   func() uint64 { return math.MaxUint64 },
		"dur":      func() time.Duration { return time.Second },
		"bytes":    func() []byte { return []byte("x") },
	}
	if got := evalRoot(t, env, "plainInt()"); got != int(5) {
		t.Errorf("plainInt() = %v (%T), want int(5)", got, got)
	}
	if got := evalRoot(t, env, "bigU64()"); got != uint64(math.MaxUint64) {
		t.Errorf("bigU64() = %v (%T), want uint64 max unchanged", got, got)
	}
	if _, ok := evalRoot(t, env, "bytes()").([]byte); !ok {
		t.Errorf("bytes() should stay a []byte")
	}
	// Without TimeBuiltins, time.Duration has no registered operation, so
	// it's just an unmodelled int64-kind value and is normalised too.
	if got := evalRoot(t, env, "dur()"); got != int64(time.Second) {
		t.Errorf("dur() = %v (%T), want int64(%d)", got, got, int64(time.Second))
	}
}

// TestCallResultMultipleValuesBecomeList covers callReflectFunc's
// multi-value convention: two or more data values (after dropping a
// trailing error) come back as a list, each element normalised, while
// the idiomatic (value, error) shape still gives a plain value.
func TestCallResultMultipleValuesBecomeList(t *testing.T) {
	env := map[string]any{
		"pair":       func() (int32, string) { return 7, "x" },
		"pairErr":    func() (int, float32, error) { return 1, 2.5, nil },
		"valueErr":   func() (int, error) { return 3, nil },
		"triple":     func() (int, int, int) { return 1, 2, 3 },
		"commaOk":    func() (string, bool) { return "v", false },
		"failingTwo": func() (int, int, error) { return 0, 0, errors.New("boom") },
	}
	cases := []struct {
		input string
		want  any
	}{
		{"pair()[0]", int64(7)},
		{"pair()[1]", "x"},
		{"len(pair())", int64(2)},
		{"pairErr()[1]", float64(2.5)},
		{"valueErr()", int(3)},
		{"triple()[2]", int(3)},
		{"sum(triple())", int(6)},
		{"commaOk()[1]", false},
	}
	for _, c := range cases {
		if got := evalRoot(t, env, c.input); got != c.want {
			t.Errorf("%s = %v (%T), want %v (%T)", c.input, got, got, c.want, c.want)
		}
	}
	if got, ok := evalRoot(t, env, "pair()").([]any); !ok || len(got) != 2 {
		t.Errorf("pair() = %v (%T), want a 2-element []any", got, got)
	}
	evalRootExpectError(t, env, "failingTwo()")
}
