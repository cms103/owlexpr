package langtest

import (
	"testing"

	"github.com/cms103/owlexpr"
)

// evalSlice parses, compiles, and runs input against env, failing the
// test on any parse/compile/run error.
func evalSlice(t *testing.T, env map[string]any, input string) any {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	return res
}

func evalSliceExpectError(t *testing.T, env map[string]any, input string) {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatalf("expected an error for %q, got none", input)
	}
}

func TestSliceList(t *testing.T) {
	env := map[string]any{"nums": []any{int64(0), int64(1), int64(2), int64(3), int64(4)}}

	cases := []struct {
		input string
		want  []any
	}{
		{`nums[1:]`, []any{int64(1), int64(2), int64(3), int64(4)}},
		{`nums[:3]`, []any{int64(0), int64(1), int64(2)}},
		{`nums[:]`, []any{int64(0), int64(1), int64(2), int64(3), int64(4)}},
		{`nums[1:3]`, []any{int64(1), int64(2)}},
		{`nums[0:0]`, []any{}},
		{`nums[5:5]`, []any{}},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalSlice(t, env, tt.input).([]any)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("index %d: got %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestIndexStillWorks confirms plain indexing (no colon) is unaffected by
// the parser now also recognizing slice syntax in the same bracket
// position.
func TestIndexStillWorks(t *testing.T) {
	env := map[string]any{"nums": []any{int64(10), int64(20), int64(30)}}
	if got := evalSlice(t, env, `nums[1]`); got != int64(20) {
		t.Errorf("got %v, want 20", got)
	}
}

func TestSliceReflectSlice(t *testing.T) {
	env := map[string]any{"ints": []int64{10, 20, 30, 40}}
	got := evalSlice(t, env, `ints[1:3]`).([]int64)
	want := []int64{20, 30}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestSliceFixedArray confirms slicing a fixed-size array works via the
// copy-based fallback (reflect.Value.Slice panics on a non-addressable
// array, which is exactly what an array boxed into `any` is).
func TestSliceFixedArray(t *testing.T) {
	env := map[string]any{"arr": [5]int{1, 2, 3, 4, 5}}
	got := evalSlice(t, env, `arr[1:4]`).([]int)
	want := []int{2, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

// TestSliceString and TestIndexString exercise rune-based (not Go's
// byte-based) semantics, using a string with multi-byte UTF-8 characters
// where byte-based slicing would produce something different (or invalid
// UTF-8 entirely).
func TestSliceString(t *testing.T) {
	env := map[string]any{"s": "héllo wörld"}

	cases := []struct {
		input string
		want  string
	}{
		{`s[:5]`, "héllo"},
		{`s[6:]`, "wörld"},
		{`s[:]`, "héllo wörld"},
		{`s[1:2]`, "é"}, // a single 2-byte-in-UTF-8 rune, whole and intact
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalSlice(t, env, tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIndexString(t *testing.T) {
	env := map[string]any{"s": "héllo"}
	cases := []struct {
		input string
		want  string
	}{
		{`s[0]`, "h"},
		{`s[1]`, "é"},
		{`s[2]`, "l"},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalSlice(t, env, tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLenMatchesRuneIndexing confirms len() and indexing/slicing agree on
// what "length" means for a string with multi-byte characters - the
// whole point of switching lenFunc to RuneCountInString. Before that
// change, len() would report the byte count (12 for "héllo wörld", since
// é and ö are each 2 bytes), which would NOT be a valid rune-slice upper
// bound.
func TestLenMatchesRuneIndexing(t *testing.T) {
	env := map[string]any{"s": "héllo wörld"}
	length := evalSlice(t, env, `len(s)`)
	if length != int64(11) {
		t.Fatalf("len(s) = %v, want 11 (rune count)", length)
	}
	got := evalSlice(t, env, `s[0:len(s)]`)
	if got != "héllo wörld" {
		t.Errorf("s[0:len(s)] = %q, want the original string unchanged", got)
	}
}

// TestSliceNegativeIndexErrors confirms negative indices are rejected
// with a clean error, matching Go's own refusal to support them (Go
// panics; this errors instead, but doesn't silently wrap around
// Python-style).
func TestSliceNegativeIndexErrors(t *testing.T) {
	env := map[string]any{"nums": []any{int64(1), int64(2), int64(3)}}
	evalSliceExpectError(t, env, `nums[-1:]`)
	evalSliceExpectError(t, env, `nums[:-1]`)
}

// TestSliceOutOfRangeErrors confirms out-of-range and inverted bounds are
// clean errors, not panics.
func TestSliceOutOfRangeErrors(t *testing.T) {
	env := map[string]any{"nums": []any{int64(1), int64(2), int64(3)}}
	evalSliceExpectError(t, env, `nums[0:10]`)
	evalSliceExpectError(t, env, `nums[3:1]`)
}

// TestSliceUnsupportedTypeErrors confirms slicing a type with no
// well-defined slice semantics (a map) is a clean error.
func TestSliceUnsupportedTypeErrors(t *testing.T) {
	env := map[string]any{"m": map[string]any{"a": 1}}
	evalSliceExpectError(t, env, `m[0:1]`)
}
