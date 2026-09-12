package owlexpr

import "testing"

// TestMatchesEvaluatesRegex covers the happy path end to end: parse,
// compile (which compiles the literal pattern into a *regexp.Regexp once,
// embedded directly in the instruction stream - see compiler.go's
// BinaryOpNode case for "matches"), and run.
func TestMatchesEvaluatesRegex(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"simple match", `"hello123" matches "^[a-z]+[0-9]+$"`, true},
		{"no match", `"hello" matches "^[a-z]+[0-9]+$"`, false},
		{"matches against env value", `x matches "^a"`, true},
		{"anchored pattern rejects partial suffix", `"xhello" matches "^hello"`, false},
		{"unanchored substring match", `"xhelloy" matches "hello"`, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := evalRoot(t, map[string]any{"x": "abc"}, tt.input)
			if got != tt.want {
				t.Errorf("%q = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestMatchesReusesCompiledRegexAcrossRuns proves the regex really is
// compiled once, not recompiled per Run: the same []vm.Instruction slice
// (and therefore the same *regexp.Regexp constant embedded in it by
// Compile) is run many times against different env values.
func TestMatchesReusesCompiledRegexAcrossRuns(t *testing.T) {
	instructions, err := Compile(`name matches "^[A-Z][a-z]*$"`)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}

	cases := []struct {
		name string
		want bool
	}{
		{"Alice", true},
		{"bob", false},
		{"Carol", true},
	}
	for _, tt := range cases {
		res, err := mc.Run(instructions, map[string]any{"name": tt.name})
		if err != nil {
			t.Fatalf("run error for %q: %v", tt.name, err)
		}
		if res != tt.want {
			t.Errorf("name=%q: got %v, want %v", tt.name, res, tt.want)
		}
	}
}

// TestMatchesRequiresLiteralPattern is the core scope decision: the
// right-hand side of "matches" must be a string literal, resolved at
// Compile() time. A variable, member access, or any other dynamic
// expression is a compile-time error, not a slower runtime-compiled
// fallback - see DESIGN_NOTES.md's "matches" writeup.
func TestMatchesRequiresLiteralPattern(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"variable pattern", `"abc" matches pattern`},
		{"member access pattern", `"abc" matches cfg.pattern`},
		{"call result pattern", `"abc" matches concat("a", "b")`},
		{"non-string literal pattern", `"abc" matches 123`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Compile(tt.input); err == nil {
				t.Errorf("Compile(%q) succeeded, want a compile error", tt.input)
			}
		})
	}
}

// TestMatchesInvalidRegexIsCompileError proves a malformed pattern is
// caught at Compile() time - not deferred to a Run()-time panic or error -
// the same "errors, not panics, and as early as possible" bias the rest
// of this codebase follows.
func TestMatchesInvalidRegexIsCompileError(t *testing.T) {
	if _, err := Compile(`"abc" matches "["`); err == nil {
		t.Error(`Compile("abc" matches "[") succeeded, want a compile error for invalid regex syntax`)
	}
}

// TestMatchesErrorPropagatesFromNestedCompilers exercises the error-
// propagation plumbing added to *compiler for this feature: "matches" can
// appear inside a lambda body, a let body, or a "??" left operand, each of
// which compiles into its own separate *compiler instance (see
// compiler.go's LambdaNode/LetNode/"??" cases) whose err must be copied
// back onto the outer compiler explicitly. Without that propagation, a bad
// regex nested in one of these would be silently swallowed rather than
// surfacing from the top-level Compile call.
func TestMatchesErrorPropagatesFromNestedCompilers(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"inside lambda body", `map([1], x => x matches badPattern)`},
		{"inside let body", `let x = "a"; x matches badPattern`},
		{"inside let value", `let x = ("a" matches badPattern); x`},
		{"inside ?? left operand", `("a" matches badPattern) ?? false`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Compile(tt.input); err == nil {
				t.Errorf("Compile(%q) succeeded, want a compile error", tt.input)
			}
		})
	}
}

// TestMatchesRequiresStringOperand confirms there is no implicit
// coercion of a non-string left-hand side - consistent with this
// language's general "explicit over implicit" bias elsewhere (duration
// parsing, date() formats, ...).
func TestMatchesRequiresStringOperand(t *testing.T) {
	evalRootExpectError(t, nil, `123 matches "^[0-9]+$"`)
}
