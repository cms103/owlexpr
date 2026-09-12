package owlexpr

import "testing"

// TestComments covers "//" line comments and "/* */" block comments in
// every position skipWhitespace needs to handle: leading, trailing,
// between tokens, spanning a newline, and back-to-back.
func TestComments(t *testing.T) {
	cases := []struct {
		input string
		want  any
	}{
		{"1 + 2 // trailing line comment", int64(3)},
		{"// leading line comment\n1 + 2", int64(3)},
		{"1 /* inline block */ + 2", int64(3)},
		{"/* leading block */ 1 + 2 /* trailing block */", int64(3)},
		{"1 +\n// comment on its own line\n2", int64(3)},
		{"1 /* multi\nline\nblock */ + 2", int64(3)},
		{"1 /*a*/ + /*b*/ 2", int64(3)},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalRoot(t, nil, tt.input)
			if got != tt.want {
				t.Errorf("%q = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestUnterminatedBlockCommentConsumesToEOF confirms an unterminated "/*"
// doesn't hang the lexer - it consumes to end of input rather than
// looping forever looking for a "*/" that never arrives. Everything after
// "/*" (including what would otherwise be a dangling "+" operator) is
// swallowed as comment text, so this parses as the complete, valid
// expression "1" rather than erroring - which is itself the proof the
// lexer reached EOF cleanly instead of hanging.
func TestUnterminatedBlockCommentConsumesToEOF(t *testing.T) {
	got := evalRoot(t, nil, "1 /* unterminated +")
	if got != int64(1) {
		t.Errorf("got %v, want 1", got)
	}
}

// TestDivisionNotMistakenForComment confirms ordinary division still
// works and isn't swallowed by the comment-detection check ("/" alone,
// not followed by another "/" or "*", is still the division operator).
func TestDivisionNotMistakenForComment(t *testing.T) {
	got := evalRoot(t, nil, "6 / 2")
	if got != int64(3) {
		t.Errorf("6 / 2 = %v, want 3", got)
	}
}

// TestNilLiteral covers the "nil" literal itself and "==" / "!=" against
// it - both a plain nil value and the typed-nil-pointer gotcha
// (env["n"] is a *int(nil), boxed into any by the map, matching the
// accessMember/typeFunc/OpCoalesce case this same check already handles
// elsewhere).
func TestNilLiteral(t *testing.T) {
	var typedNilPtr *int
	env := map[string]any{
		"x": int64(5),
		"n": nil,
		"p": typedNilPtr,
	}
	cases := []struct {
		input string
		want  bool
	}{
		{`nil == nil`, true},
		{`nil != nil`, false},
		{`n == nil`, true},
		{`n != nil`, false},
		{`x == nil`, false},
		{`x != nil`, true},
		{`nil == x`, false},
		// The typed-nil-pointer gotcha: p is a non-nil `any` boxing a nil
		// *int, but must still compare equal to the literal nil.
		{`p == nil`, true},
		{`p != nil`, false},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalRoot(t, env, tt.input)
			if got != tt.want {
				t.Errorf("%s = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestNilAsIdentifierIsReserved confirms "nil" can no longer be used as a
// bare identifier/variable name now that it's a keyword - matching how
// "true"/"false" already behave.
func TestNilAsIdentifierIsReserved(t *testing.T) {
	got := evalRoot(t, nil, `nil`)
	if got != nil {
		t.Errorf("nil = %v, want nil", got)
	}
}
