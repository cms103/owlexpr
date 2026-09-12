package owlexpr

import "testing"

// TestParseErrors is a table of malformed inputs, each expected to fail
// with a parse error rather than silently parsing a prefix of the input
// or panicking. Before this, most of these error branches in parser.go -
// one per production rule - had no test reaching them at all; only the
// "happy path" of each grammar rule was exercised.
func TestParseErrors(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"dot not followed by identifier", `a.5`},
		{"dot followed by nothing", `a.`},
		{"unclosed call arguments", `f(1, 2`},
		{"error inside call argument", `f(1+)`},
		{"unclosed index brackets", `a[1`},
		{"empty index brackets", `a[]`},
		{"error inside index expression", `a[1+]`},
		{"unclosed slice brackets", `a[1:2`},
		{"error inside slice low bound", `a[1+:2]`},
		{"error inside slice high bound", `a[1:2+]`},
		{"ternary missing colon", `true ? 1`},
		{"error inside ternary then branch", `true ? 1+ : 2`},
		{"error inside ternary else branch", `true ? 1 : 2+`},
		{"unclosed list literal", `[1, 2`},
		{"error inside list element", `[1+]`},
		{"map literal with non-ident/string key", `{1: 2}`},
		{"map literal missing colon", `{a 2}`},
		{"map literal missing closing brace", `{a: 1`},
		{"error inside map value", `{a: 1+}`},
		{"empty parentheses", `()`},
		{"unclosed grouping parens", `(1 + 2`},
		{"error inside grouping parens", `(1+)`},
		{"mismatched closing paren", `(1 + 2]`},
		{"let missing identifier", `let 5 = 1; 5`},
		{"let missing equals", `let x 1; x`},
		{"let missing semicolon", `let x = 1 x`},
		{"error inside let value", `let x = 1+; x`},
		{"error inside let body", `let x = 1; 1+`},
		{"if condition error", `if 1+ { 2 } else { 3 }`},
		{"if-block missing closing brace", `if true { 1 else 2`},
		{"unexpected token at start of expression", `,`},
		{"unrecognized character trails the expression", `1 @ 2`},
		{"unrecognized character alone", `@`},
		{"lambda param list not identifiers falls back and leaves trailing arrow", `(1) => x`},
		{"trailing tokens after a valid expression", `1 2`},
		{"dangling binary operator", `1 +`},
		{"empty input", ``},
		{"error inside single-param lambda body", `x => 1+`},
		{"error inside multi-param lambda body", `(a, b) => 1+`},
		{"error inside unary operand", `-,`},
		{"unclosed paren param list falls back to grouping, which then errors", `(a, b => x`},
		{"error inside second call argument", `f(1, 2+)`},
		{"error inside if-else block", `if true { 1 } else { 2+ }`},
		{"error inside if-then block", `if true { 1+ }`},
		{"opt-dot not followed by identifier or '['", `a?.5`},
		{"opt-dot index unclosed bracket", `a?.[1`},
		{"error inside opt-dot index expression", `a?.[1+]`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(tt.input); err == nil {
				t.Errorf("Parse(%q) succeeded, want a parse error", tt.input)
			}
		})
	}
}

// TestParseNumberWithTwoDots covers readNumber's isFloat-already-true break
// (a second '.' stops the number rather than being consumed into it), which
// then surfaces as a dot-not-followed-by-identifier parse error one level
// up - both previously unreached.
func TestParseNumberWithTwoDots(t *testing.T) {
	if _, err := Parse(`1.2.3`); err == nil {
		t.Errorf("Parse(\"1.2.3\") succeeded, want a parse error")
	}
}

// TestParseValidEdgeCases confirms a few inputs that look similar to the
// error cases above but are in fact valid, so the new error-path coverage
// doesn't come at the cost of over-rejecting legitimate syntax.
func TestParseValidEdgeCases(t *testing.T) {
	cases := []struct {
		input string
		want  any
	}{
		{`(1 + 2)`, int64(3)},
		{`[]`, []any{}},
		{`{}`, map[string]any{}},
		{`a[:]`, []any{int64(1), int64(2)}},
		{`{"a": 1}`, map[string]any{"a": int64(1)}},
		{`if true 1 else 2`, int64(1)},
		{`if false 1 else 2`, int64(2)},
	}
	env := map[string]any{"a": []any{int64(1), int64(2)}}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalRoot(t, env, tt.input)
			switch want := tt.want.(type) {
			case []any:
				gotList, ok := got.([]any)
				if !ok || len(gotList) != len(want) {
					t.Errorf("%q = %v, want %v", tt.input, got, want)
				}
			case map[string]any:
				gotMap, ok := got.(map[string]any)
				if !ok || len(gotMap) != len(want) {
					t.Errorf("%q = %v, want %v", tt.input, got, want)
				}
			default:
				if got != tt.want {
					t.Errorf("%q = %v, want %v", tt.input, got, tt.want)
				}
			}
		})
	}
}
