package langtest

import (
	"testing"

	"github.com/cms103/owlexpr"
)

// evalNot parses, compiles, and runs input against env, failing the test
// on any parse/compile/run error. Shared with in_test.go's evalIn helper
// in spirit, but kept local since these are the only two callers so far.
func evalNot(t *testing.T, env map[string]any, input string) any {
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

// TestNotOperator checks both spellings - "!" and "not" - produce
// identical results, since "not" is meant to be an exact synonym: same
// precedence, same OpNot bytecode, not Python's looser-than-comparisons
// "not".
func TestNotOperator(t *testing.T) {
	env := map[string]any{
		"t": true,
		"f": false,
		"n": []any{1, 2, 3},
	}

	cases := []struct {
		input string
		want  bool
	}{
		{`!true`, false},
		{`not true`, false},
		{`!false`, true},
		{`not false`, true},
		{`!t`, false},
		{`not t`, false},
		{`!f`, true},
		{`not f`, true},

		// Double negation, both spellings and mixed.
		{`!!true`, true},
		{`not not true`, true},
		{`!not true`, true},
		{`not !true`, true},

		// Precedence: "not"/"!" bind as tightly as each other (both
		// PREC_PREFIX), so "not true == false" parses as "(not true) ==
		// false", i.e. "false == false" -> true - matching "!true ==
		// false" exactly, not Python's "not (true == false)".
		{`not true == false`, true},
		{`!true == false`, true},

		// Negating membership requires parens, since "not"/"!" bind
		// tighter than "in" (PREC_PREFIX > PREC_LESSGREATER) - documented
		// here as the correct spelling (see also TestNotInViaNegation in
		// in_test.go).
		{`!(5 in n)`, true},
		{`not (5 in n)`, true},
		{`!(2 in n)`, false},
		{`not (2 in n)`, false},
	}

	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalNot(t, env, tt.input)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNotOperatorNonBoolOperand confirms "not" errors on a non-bool
// operand exactly like "!" already does - both compile to the same OpNot
// instruction.
func TestNotOperatorNonBoolOperand(t *testing.T) {
	for _, input := range []string{`!5`, `not 5`} {
		t.Run(input, func(t *testing.T) {
			instructions, err := owlexpr.Compile(input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			mc, err := owlexpr.NewVM()
			if err != nil {
				t.Fatalf("NewVM: %v", err)
			}
			if _, err := mc.Run(instructions, nil); err == nil {
				t.Fatalf("expected an error for %q, got none", input)
			}
		})
	}
}

// TestNotDoesNotShadowIdentifiers confirms "not" as a keyword only matches
// the exact word - an identifier like "notify" that merely starts with
// "not" must still lex as one ordinary identifier, not "not" + "ify".
func TestNotDoesNotShadowIdentifiers(t *testing.T) {
	env := map[string]any{"notify": true}
	got := evalNot(t, env, `notify`)
	if got != true {
		t.Errorf("got %v, want true", got)
	}
}
