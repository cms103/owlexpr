package langtest

import (
	"testing"

	"github.com/cms103/owlexpr"
)

// evalCoalesce parses, compiles, and runs input against env, failing the
// test on any parse/compile/run error.
func evalCoalesce(t *testing.T, env map[string]any, input string) any {
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

type role struct{ Title string }
type user struct{ Role *role }

// TestCoalesceMotivatingExample exercises exactly the scenario that
// prompted this operator: users["bob"].role.title, where "bob" might be
// missing, the role might be a nil pointer, or the field might not exist
// at all - all recoverable with a single trailing "??", no per-step
// marker needed.
func TestCoalesceMotivatingExample(t *testing.T) {
	env := map[string]any{
		"users": map[string]any{
			"alice": user{Role: &role{Title: "Admin"}},
			"bob":   user{Role: nil}, // present, but Role is a nil pointer
		},
	}

	cases := []struct {
		input string
		want  any
	}{
		// Happy path: nothing missing, fallback not used.
		{`users["alice"].Role.Title ?? "No Title"`, "Admin"},
		// "bob" exists but Role is a nil pointer - accessMember errors
		// accessing .Title on it, caught by "??".
		{`users["bob"].Role.Title ?? "No Title"`, "No Title"},
		// "charlie" doesn't exist at all - indexValue errors, caught.
		{`users["charlie"].Role.Title ?? "No Title"`, "No Title"},
		// Role itself, with no further chaining - also nil-coalesces
		// even though accessMember returned a typed-nil *role rather
		// than an error (the typed-nil-interface case isNilResult exists
		// for).
		{`users["bob"].Role ?? "no role"`, "no role"},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalCoalesce(t, env, tt.input)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCoalesceLiteralNil confirms the accessMember nil-panic bug fix: a
// literal nil value (as opposed to a typed-nil pointer) used to panic on
// member access; it must now be a catchable error like any other.
func TestCoalesceLiteralNil(t *testing.T) {
	env := map[string]any{"n": nil}
	got := evalCoalesce(t, env, `n.Title ?? "fallback"`)
	if got != "fallback" {
		t.Errorf("got %v, want fallback", got)
	}

	// And without "??", it should now be a clean error, not a panic.
	instructions, err := owlexpr.Compile(`n.Title`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatal("expected an error accessing a member on nil, got none")
	}
}

// TestCoalesceSkipsRightWhenLeftSucceeds confirms the right-hand operand
// is never evaluated when the left succeeds - not just that its value is
// discarded, but that it doesn't run at all. A right side that would
// itself error is the proof: if it ran, the test would fail with a run
// error instead of returning the left value.
func TestCoalesceSkipsRightWhenLeftSucceeds(t *testing.T) {
	env := map[string]any{"x": int64(5)}
	got := evalCoalesce(t, env, `x ?? (1 / 0)`)
	if got != int64(5) {
		t.Errorf("got %v, want 5 - right side must not have run (1/0 would error)", got)
	}
}

// TestCoalesceCatchesAnyError confirms "??" catches errors generally, not
// just the specific missing-key/nil-field cases - e.g. a divide by zero
// on the left is just as recoverable.
func TestCoalesceCatchesAnyError(t *testing.T) {
	got := evalCoalesce(t, nil, `(1 / 0) ?? -1`)
	if got != int64(-1) {
		t.Errorf("got %v, want -1", got)
	}
}

// TestCoalesceRightSideErrorPropagates confirms only the left operand is
// protected: if the left is nil/errors AND the right also errors, the
// overall expression must still error.
func TestCoalesceRightSideErrorPropagates(t *testing.T) {
	instructions, err := owlexpr.Compile(`(1 / 0) ?? (1 / 0)`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Fatal("expected an error when both sides fail, got none")
	}
}

// TestCoalesceChainingIsRightAssociative confirms "a ?? b ?? c" behaves
// as "a ?? (b ?? c)" - picking the first non-nil value in a fallback
// chain, regardless of how many links there are.
func TestCoalesceChainingIsRightAssociative(t *testing.T) {
	env := map[string]any{"c": "third"}
	got := evalCoalesce(t, env, `n ?? m ?? c ?? "fourth"`)
	if got != "third" {
		t.Errorf("got %v, want third", got)
	}
}

// TestCoalescePrecedence checks "??" composes with "||" and ternary
// without requiring parens, per the precedence placed between them (see
// parser.go's infixBindingPowers comment for "??"). This is asserted
// directly on the parsed AST shape rather than an evaluated value:
// working the intended composition backwards into a value-based test
// turns out to be structurally impossible here (whatever value sits
// immediately after "??" ends up playing the same role - the ternary's
// condition, say - under either grouping, so both groupings evaluate to
// the same result or the same error regardless of which one actually
// happened).
func TestCoalescePrecedence(t *testing.T) {
	t.Run("?? binds tighter than ternary, composes as its condition", func(t *testing.T) {
		// "a ?? b ? c : d" should parse as "(a ?? b) ? c : d" - the
		// TernaryNode is outermost, with the "??" as its Cond.
		ast, err := owlexpr.Parse(`a ?? b ? c : d`)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		tern, ok := ast.(owlexpr.TernaryNode)
		if !ok {
			t.Fatalf("got %T, want TernaryNode", ast)
		}
		if _, ok := tern.Cond.(owlexpr.BinaryOpNode); !ok {
			t.Fatalf("ternary condition is %T, want BinaryOpNode (\"??\")", tern.Cond)
		}
		if op := tern.Cond.(owlexpr.BinaryOpNode).Op; op != "??" {
			t.Fatalf("ternary condition operator is %q, want \"??\"", op)
		}
	})

	t.Run("?? binds looser than ||, absorbing it into the fallback", func(t *testing.T) {
		// "a ?? b || c" should parse as "a ?? (b || c)" - the outer node
		// is "??", whose Right is the "||" BinaryOpNode.
		ast, err := owlexpr.Parse(`a ?? b || c`)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		bin, ok := ast.(owlexpr.BinaryOpNode)
		if !ok || bin.Op != "??" {
			t.Fatalf("got %#v, want top-level BinaryOpNode{Op: \"??\"}", ast)
		}
		right, ok := bin.Right.(owlexpr.BinaryOpNode)
		if !ok || right.Op != "||" {
			t.Fatalf("right operand is %#v, want BinaryOpNode{Op: \"||\"}", bin.Right)
		}
	})
}
