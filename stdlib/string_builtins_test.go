package stdlib

import (
	"testing"

	"github.com/cms103/owlexpr"
)

// evalStr parses, compiles, and runs input with StringBuiltins() enabled,
// failing the test on any parse/compile/run error.
func evalStr(t *testing.T, env map[string]any, input string) any {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(StringBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	return res
}

func evalStrExpectError(t *testing.T, env map[string]any, input string) {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(StringBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatalf("expected an error for %q, got none", input)
	}
}

func TestStringBuiltinsNotRegisteredByDefault(t *testing.T) {
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	instructions, err := owlexpr.Compile(`string.upper("hi")`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Fatalf("expected string.upper() to be undefined without StringBuiltins()")
	}
}

func TestTrimFamily(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`string.trim("  hi  ")`, "hi"},
		{`string.trim("xxhixx", "x")`, "hi"},
		{`string.trimLeft("  hi  ")`, "hi  "},
		{`string.trimLeft("xxhixx", "x")`, "hixx"},
		{`string.trimRight("  hi  ")`, "  hi"},
		{`string.trimRight("xxhixx", "x")`, "xxhi"},
		{`string.trimPrefix("hello world", "hello ")`, "world"},
		{`string.trimPrefix("hello world", "bye ")`, "hello world"},
		{`string.trimSuffix("hello.go", ".go")`, "hello"},
		{`string.trimSuffix("hello.go", ".rb")`, "hello.go"},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalStr(t, nil, tt.input)
			if got != tt.want {
				t.Errorf("%s = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestUpperLower(t *testing.T) {
	if got := evalStr(t, nil, `string.upper("café")`); got != "CAFÉ" {
		t.Errorf("string.upper(café) = %v, want CAFÉ", got)
	}
	if got := evalStr(t, nil, `string.lower("CAFÉ")`); got != "café" {
		t.Errorf("string.lower(CAFÉ) = %v, want café", got)
	}
}

func TestSplitFamily(t *testing.T) {
	got := evalStr(t, nil, `string.split("a,b,c", ",")`).([]any)
	want := []any{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("string.split() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("string.split()[%d] = %v, want %v", i, got[i], want[i])
		}
	}

	gotN := evalStr(t, nil, `string.split("a,b,c", ",", 2)`).([]any)
	wantN := []any{"a", "b,c"}
	if len(gotN) != len(wantN) || gotN[0] != wantN[0] || gotN[1] != wantN[1] {
		t.Errorf("string.split(n=2) = %v, want %v", gotN, wantN)
	}

	gotAfter := evalStr(t, nil, `string.splitAfter("a,b,c", ",")`).([]any)
	wantAfter := []any{"a,", "b,", "c"}
	if len(gotAfter) != len(wantAfter) {
		t.Fatalf("string.splitAfter() = %v, want %v", gotAfter, wantAfter)
	}
	for i := range wantAfter {
		if gotAfter[i] != wantAfter[i] {
			t.Errorf("string.splitAfter()[%d] = %v, want %v", i, gotAfter[i], wantAfter[i])
		}
	}
}

func TestReplaceRepeat(t *testing.T) {
	if got := evalStr(t, nil, `string.replace("aaa", "a", "b")`); got != "bbb" {
		t.Errorf("string.replace() = %v, want bbb", got)
	}
	if got := evalStr(t, nil, `string.repeat("ab", 3)`); got != "ababab" {
		t.Errorf("string.repeat() = %v, want ababab", got)
	}
	evalStrExpectError(t, nil, `string.repeat("ab", -1)`)
}

// TestIndexOfRunePositions is the key Unicode-consistency test: a
// multi-byte character before the match must not shift the reported
// index, matching len()/slicing's existing rune-based semantics.
func TestIndexOfRunePositions(t *testing.T) {
	if got := evalStr(t, nil, `string.indexOf("café bar", "bar")`); got != int64(5) {
		t.Errorf(`string.indexOf("café bar", "bar") = %v, want 5`, got)
	}
	if got := evalStr(t, nil, `string.indexOf("abc", "z")`); got != int64(-1) {
		t.Errorf(`string.indexOf("abc", "z") = %v, want -1`, got)
	}
	if got := evalStr(t, nil, `string.lastIndexOf("café café", "café")`); got != int64(5) {
		t.Errorf(`string.lastIndexOf("café café", "café") = %v, want 5`, got)
	}
}

func TestHasPrefixSuffixContainsCount(t *testing.T) {
	if got := evalStr(t, nil, `string.hasPrefix("hello", "he")`); got != true {
		t.Errorf("string.hasPrefix() = %v, want true", got)
	}
	if got := evalStr(t, nil, `string.hasSuffix("hello", "lo")`); got != true {
		t.Errorf("string.hasSuffix() = %v, want true", got)
	}
	if got := evalStr(t, nil, `string.contains("hello", "ell")`); got != true {
		t.Errorf("string.contains() = %v, want true", got)
	}
	if got := evalStr(t, nil, `string.count("banana", "a")`); got != int64(3) {
		t.Errorf("string.count() = %v, want 3", got)
	}
}

func TestJoin(t *testing.T) {
	if got := evalStr(t, nil, `string.join(["a", "b", "c"], "-")`); got != "a-b-c" {
		t.Errorf("string.join() = %v, want a-b-c", got)
	}
	if got := evalStr(t, nil, `string.join(["a", "b", "c"])`); got != "abc" {
		t.Errorf("string.join() no sep = %v, want abc", got)
	}
	evalStrExpectError(t, nil, `string.join([1, 2, 3], "-")`)
}

// TestReverseUnicode is the other key Unicode-consistency test: reversing
// must operate on runes, not bytes, or a multi-byte character would be
// corrupted into invalid UTF-8.
func TestReverseUnicode(t *testing.T) {
	if got := evalStr(t, nil, `string.reverse("héllo")`); got != "olléh" {
		t.Errorf(`string.reverse("héllo") = %v, want olléh`, got)
	}
}

func TestPadStartEnd(t *testing.T) {
	if got := evalStr(t, nil, `string.padStart("7", 3, "0")`); got != "007" {
		t.Errorf(`string.padStart("7", 3, "0") = %v, want 007`, got)
	}
	if got := evalStr(t, nil, `string.padEnd("7", 3, "0")`); got != "700" {
		t.Errorf(`string.padEnd("7", 3, "0") = %v, want 700`, got)
	}
	if got := evalStr(t, nil, `string.padStart("hi", 5)`); got != "   hi" {
		t.Errorf(`string.padStart("hi", 5) = %q, want "   hi"`, got)
	}
	if got := evalStr(t, nil, `string.padStart("hello world", 3)`); got != "hello world" {
		t.Errorf("string.padStart() with target shorter than input should be a no-op, got %v", got)
	}
	// Pad target length is rune-based: "é" is one rune, so padding to 3
	// should add exactly 2 pad characters, not fewer due to byte length.
	if got := evalStr(t, nil, `string.padStart("é", 3, "x")`); got != "xxé" {
		t.Errorf(`string.padStart("é", 3, "x") = %q, want "xxé"`, got)
	}
}

func TestFormat(t *testing.T) {
	if got := evalStr(t, nil, `string.format("%s is %d", "Bob", 42)`); got != "Bob is 42" {
		t.Errorf("string.format() = %v, want 'Bob is 42'", got)
	}
}

// TestStringBuiltinArgumentErrors covers each function's own arity guard
// (wrong number of arguments) - previously untested for every function in
// this file except via happy-path calls.
func TestStringBuiltinArgumentErrors(t *testing.T) {
	cases := []string{
		`string.trim()`,
		`string.trim("a", "b", "c")`,
		`string.trimLeft()`,
		`string.trimRight()`,
		`string.trimPrefix("a")`,
		`string.trimSuffix("a")`,
		`string.upper()`,
		`string.upper("a", "b")`,
		`string.lower()`,
		`string.split("a")`,
		`string.split("a", "b", 1, 2)`,
		`string.splitAfter("a")`,
		`string.replace("a", "b")`,
		`string.repeat("a")`,
		`string.indexOf("a")`,
		`string.lastIndexOf("a")`,
		`string.hasPrefix("a")`,
		`string.hasSuffix("a")`,
		`string.contains("a")`,
		`string.count("a")`,
		`string.join()`,
		`string.join("a", "b", "c")`,
		`string.reverse()`,
		`string.padStart("a")`,
		`string.padEnd("a")`,
		`string.format()`,
		`string.toBase64()`,
		`string.fromBase64()`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			evalStrExpectError(t, nil, input)
		})
	}
}

// TestStringBuiltinArgumentTypeErrors covers argString/argInt's own
// wrong-type branch (as opposed to the wrong-arg-count branch above),
// which every one of these functions delegates to.
func TestStringBuiltinArgumentTypeErrors(t *testing.T) {
	cases := []string{
		`string.trim(42)`,
		`string.trim("a", 42)`,
		`string.split(42, "b")`,
		`string.split("a", 42)`,
		`string.split("a", "b", "not an int")`,
		`string.repeat("a", "not an int")`,
		`string.padStart("a", "not an int")`,
		`string.join("not a list")`,
		`string.join([1, 2, 3])`,
		`string.format(42)`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			evalStrExpectError(t, nil, input)
		})
	}
}

func TestStringBase64AndBack(t *testing.T) {
	if got := evalStr(t, nil, `string.toBase64("Hello")`); got != "SGVsbG8=" {
		t.Errorf(`string.toBase64("Hello") = %v, want "SGVsbG8="`, got)
	}
	if got := evalStr(t, nil, `string.fromBase64("SGVsbG8=")`); got != "Hello" {
		t.Errorf(`string.fromBase64("SGVsbG8=") = %v, want "Hello"`, got)
	}
	if got := evalStr(t, nil, `string.fromBase64(string.toBase64("round trip"))`); got != "round trip" {
		t.Errorf(`round trip = %v, want "round trip"`, got)
	}
	evalStrExpectError(t, nil, `string.fromBase64("not valid base64!!")`)
	// Valid base64 that decodes to a non-UTF-8 byte sequence.
	evalStrExpectError(t, nil, `string.fromBase64("/w==")`)
}

// TestArgIntAcceptsPlainInt covers argInt's `case int:` branch - every
// other test passes a number literal, which the parser always compiles as
// int64, so a plain Go `int` value (as an environment might supply) was
// never exercised.
func TestArgIntAcceptsPlainInt(t *testing.T) {
	env := map[string]any{"n": int(2)}
	got := evalStr(t, env, `string.repeat("ab", n)`)
	if got != "abab" {
		t.Errorf(`string.repeat("ab", int(2)) = %v, want "abab"`, got)
	}
}

// TestRepeatNegativeCountErrors covers repeatFunc's own domain check
// (n < 0), distinct from argInt's wrong-TYPE check above.
func TestRepeatNegativeCountErrors(t *testing.T) {
	evalStrExpectError(t, nil, `string.repeat("a", -1)`)
}

// TestPadWithEmptyPadStringErrors covers padRunes' own guard against an
// empty pad string, shared by both padStart and padEnd.
func TestPadWithEmptyPadStringErrors(t *testing.T) {
	evalStrExpectError(t, nil, `string.padStart("a", 5, "")`)
	evalStrExpectError(t, nil, `string.padEnd("a", 5, "")`)
}

// TestPadNoOpWhenAlreadyLongEnough covers padRunes' "already at or past
// the target length" branch, returning the original string untouched
// rather than an empty pad.
func TestPadNoOpWhenAlreadyLongEnough(t *testing.T) {
	if got := evalStr(t, nil, `string.padStart("hello", 3)`); got != "hello" {
		t.Errorf(`string.padStart("hello", 3) = %v, want "hello" unchanged`, got)
	}
	if got := evalStr(t, nil, `string.padEnd("hello", 5)`); got != "hello" {
		t.Errorf(`string.padEnd("hello", 5) = %v, want "hello" unchanged`, got)
	}
}

// TestSplitWithLimit and TestSplitAfterWithLimit cover the 3-argument
// (limit-bearing) branch of split/splitAfter, which TestSplitFamily
// doesn't reach.
func TestSplitWithLimit(t *testing.T) {
	got := evalStr(t, nil, `string.split("a,b,c", ",", 2)`)
	list, ok := got.([]any)
	if !ok || len(list) != 2 || list[1] != "b,c" {
		t.Errorf(`string.split("a,b,c", ",", 2) = %v, want ["a" "b,c"]`, got)
	}
}

func TestSplitAfterWithLimit(t *testing.T) {
	got := evalStr(t, nil, `string.splitAfter("a,b,c", ",", 2)`)
	list, ok := got.([]any)
	if !ok || len(list) != 2 || list[0] != "a," {
		t.Errorf(`string.splitAfter("a,b,c", ",", 2) = %v, want ["a," "b,c"]`, got)
	}
}

// TestJoinWithNoSeparator covers joinFunc's 1-argument (default empty
// separator) form.
func TestJoinWithNoSeparator(t *testing.T) {
	if got := evalStr(t, nil, `string.join(["a", "b", "c"])`); got != "abc" {
		t.Errorf(`string.join(["a","b","c"]) = %v, want "abc"`, got)
	}
}
