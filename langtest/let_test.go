package langtest

import (
	"testing"

	"github.com/cms103/owlexpr"
)

// evalLet parses, compiles, and runs input against env, failing the test
// on any parse/compile/run error.
func evalLet(t *testing.T, env map[string]any, input string) any {
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

func evalLetExpectError(t *testing.T, env map[string]any, input string) {
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

// TestLetMotivatingExample is the exact scenario that prompted this
// feature: naming a repeated/expensive sub-computation for readability.
func TestLetMotivatingExample(t *testing.T) {
	env := map[string]any{"records": []any{1, 2, 3, 4, 5, 6}}
	got := evalLet(t, env, `let total = len(records); if total > 5 {"Nice job"} else {"More to do"}`)
	if got != "Nice job" {
		t.Errorf("got %v, want \"Nice job\"", got)
	}
}

func TestLetShadowsWithoutOverwritingEnv(t *testing.T) {
	env := map[string]any{"total": int64(999)}

	if got := evalLet(t, env, `let total = 1; total`); got != int64(1) {
		t.Errorf("inside let: got %v, want 1", got)
	}
	if got := evalLet(t, env, `total`); got != int64(999) {
		t.Errorf("env value after let expression finished: got %v, want 999 (unmutated)", got)
	}
	if env["total"] != int64(999) {
		t.Errorf("env map itself was mutated: got %v, want 999", env["total"])
	}
}

// TestLetChaining confirms `let a = 1; let b = a + 1; ...` falls out of
// ordinary recursive parsing (nested LetNodes), with each let seeing all
// prior bindings.
func TestLetChaining(t *testing.T) {
	got := evalLet(t, nil, `let a = 1; let b = a + 1; let c = a + b; c`)
	if got != int64(3) {
		t.Errorf("got %v, want 3", got)
	}
}

func TestLetInLambdaBody(t *testing.T) {
	env := map[string]any{"records": []any{1, 2, 3}}
	got := evalLet(t, env, `map(records, x => let doubled = x * 2; doubled + 1)`).([]any)
	want := []any{3, 5, 7}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

// TestLetDoesNotLeakToSiblingArguments is the critical isolation
// property: a let's binding must be visible only within its own body,
// not to a sibling argument in the same call - proving OpLet's nested
// mc.run() design (a fresh scopes slice, never mutating the caller's)
// actually holds, rather than leaking via some shared/aliased slice.
func TestLetDoesNotLeakToSiblingArguments(t *testing.T) {
	env := map[string]any{
		"concat": func(a, b string) string { return a + b },
	}
	// The second "x" is NOT in scope - it belongs only to the first
	// argument's own let body.
	evalLetExpectError(t, env, `concat(let x = "a"; x, x)`)
}

// TestLetValueCanReferenceOuterEnv confirms the bound value expression
// itself can read the environment (or an enclosing let), and nested lets
// compose with that outer context.
func TestLetValueCanReferenceOuterEnv(t *testing.T) {
	env := map[string]any{"total": int64(999)}
	got := evalLet(t, env, `let x = total + 1; let y = x + 1; y`)
	if got != int64(1001) {
		t.Errorf("got %v, want 1001", got)
	}
}

// TestSemicolonOnlyValidAfterLet confirms the restriction: ';' is never
// accepted anywhere except right after a let binding's value. This isn't
// a special check - it falls out of ';' never being consumed anywhere
// else in the grammar - but the restriction is worth pinning down with a
// test regardless, since it's an explicit design decision.
func TestSemicolonOnlyValidAfterLet(t *testing.T) {
	cases := []string{
		`1 + 2; 3`,
		`if true { 1; 2 } else { 3 }`,
		`(1; 2)`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			if _, err := owlexpr.Parse(input); err == nil {
				t.Errorf("expected a parse error for %q, got none", input)
			}
		})
	}
}

// TestLetParseErrors covers malformed let syntax.
func TestLetParseErrors(t *testing.T) {
	cases := []string{
		`let = 1; 1`,  // missing name
		`let x 1; x`,  // missing '='
		`let x = 1 x`, // missing ';'
		`let x = ; x`, // missing value
		`let x = 1;`,  // missing body
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			if _, err := owlexpr.Parse(input); err == nil {
				t.Errorf("expected a parse error for %q, got none", input)
			}
		})
	}
}
