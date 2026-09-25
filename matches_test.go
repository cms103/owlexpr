package owlexpr

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/cms103/owlexpr/vm"
)

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

// TestMatchesRejectsNonRegexLiteralPattern: a right-hand literal that can
// never be a regex (anything but a string or regex literal) is a compile
// error, since it's certain to fail at run time.
func TestMatchesRejectsNonRegexLiteralPattern(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"int literal pattern", `"abc" matches 123`},
		{"bool literal pattern", `"abc" matches true`},
		{"nil literal pattern", `"abc" matches nil`},
		{"list literal pattern", `"abc" matches ["a"]`},
		{"map literal pattern", `"abc" matches {a: 1}`},
		{"lambda literal pattern", `"abc" matches (x => x)`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Compile(tt.input); err == nil {
				t.Errorf("Compile(%q) succeeded, want a compile error", tt.input)
			}
		})
	}
}

// TestMatchesDynamicPatternMustBeRegexValue is the core scope decision: a
// non-literal right-hand side compiles, but must evaluate to a regex value
// (compiled before Run - a let-bound re`...` literal or an env
// vm.NewRegex). A string arriving there at run time is an error, never
// compiled on the fly - see DESIGN_NOTES.md's "matches" writeup.
func TestMatchesDynamicPatternMustBeRegexValue(t *testing.T) {
	env := map[string]any{
		"pattern":  "^a",
		"cfg":      map[string]any{"pattern": "^a"},
		"envRegex": vm.NewRegex(regexp.MustCompile("^a")),
	}
	cases := []struct {
		name  string
		input string
	}{
		{"string variable pattern", `"abc" matches pattern`},
		{"string member access pattern", `"abc" matches cfg.pattern`},
		// Not `("a" + "b")`: foldConstants folds that to a string literal
		// before compilation, so it's compiled as a literal pattern.
		{"string call result pattern", `"abc" matches str(pattern)`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			evalRootExpectError(t, env, tt.input)
		})
	}

	valid := []struct {
		name  string
		input string
		want  bool
	}{
		{"let-bound regex", "let r = re`^a`; \"abc\" matches r", true},
		{"let-bound regex no match", "let r = re`^b`; \"abc\" matches r", false},
		{"env regex", `"abc" matches envRegex`, true},
		{"inline regex literal", "\"abc\" matches re`c$`", true},
		{"regex in lambda", "filter([\"ab\", \"cd\", \"ax\"], x => x matches re`^a`)", true},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			got := evalRoot(t, env, tt.input)
			if tt.name == "regex in lambda" {
				want := []any{"ab", "ax"}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%q = %v, want %v", tt.input, got, want)
				}
				return
			}
			if got != tt.want {
				t.Errorf("%q = %v, want %v", tt.input, got, tt.want)
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
		{"inside lambda body", `map([1], x => x matches "[")`},
		{"inside let body", `let x = "a"; x matches "["`},
		{"inside let value", `let x = ("a" matches "["); x`},
		{"inside ?? left operand", `("a" matches "[") ?? false`},
		{"regex literal inside lambda body", "map([1], x => re`[`)"},
		{"regex literal inside let value", "let r = re`[`; r"},
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
