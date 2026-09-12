package langtest

import (
	"testing"

	"github.com/cms103/owlexpr"
)

// evalString parses, compiles, and runs a single string-literal
// expression, failing the test on any parse/compile/run error, and
// returning the resulting Go string.
func evalString(t *testing.T, input string) string {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := mc.Run(instructions, nil)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	s, ok := res.(string)
	if !ok {
		t.Fatalf("result is %T, want string", res)
	}
	return s
}

// TestStringLiteralEscapes covers the backslash escapes readString now
// recognizes, for both quote styles.
func TestStringLiteralEscapes(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`"tab\there"`, "tab\there"},
		{`"line one\nline two"`, "line one\nline two"},
		{`"cr\rlf"`, "cr\rlf"},
		{`"backslash: \\ end"`, `backslash: \ end`},
		{`"a quote: \" inside"`, `a quote: " inside`},
		{`'a quote: \' inside'`, `a quote: ' inside`},
		// An unrecognized escape keeps both characters literally rather
		// than silently dropping the backslash or erroring.
		{`"unknown: \d escape"`, `unknown: \d escape`},
		// Multiple escapes in one literal.
		{`"a\tb\nc\\d"`, "a\tb\nc\\d"},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalString(t, tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestStringLiteralRawNewline confirms a literal, unescaped newline byte
// in the source text is preserved verbatim - this was already true before
// escape support existed (readString never rejected raw newlines), and
// must remain true now that escape processing has been added alongside
// it.
func TestStringLiteralRawNewline(t *testing.T) {
	got := evalString(t, "\"line one\nline two\"")
	want := "line one\nline two"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// Same for single-quoted strings.
	got2 := evalString(t, "'line one\nline two'")
	if got2 != want {
		t.Errorf("got %q, want %q", got2, want)
	}
}

// TestStringLiteralOtherQuoteWorkaround documents the pre-existing (and
// still valid) way to embed one kind of quote character without any
// escape at all: use the other quote as the delimiter. This keeps working
// unchanged alongside the new \" / \' escapes.
func TestStringLiteralOtherQuoteWorkaround(t *testing.T) {
	got := evalString(t, `'she said "hi"'`)
	if got != `she said "hi"` {
		t.Errorf("got %q, want %q", got, `she said "hi"`)
	}
}

// TestStringLiteralTrailingBackslash confirms a backslash as the very
// last character before end of input doesn't panic - there's no
// character left to escape, so it's kept as a literal backslash.
func TestStringLiteralTrailingBackslash(t *testing.T) {
	instructions, err := owlexpr.Compile(`"abc\`)
	if err != nil {
		// An unterminated string is expected to misbehave somehow (the
		// original lexer never handled termination errors either) - the
		// only real requirement here is that it doesn't panic.
		return
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	_, _ = mc.Run(instructions, nil)
}
