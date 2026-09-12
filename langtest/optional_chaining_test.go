package langtest

import (
	"testing"

	"github.com/cms103/owlexpr"
)

// TestOptionalChainingMotivatingExample mirrors coalesce_test.go's own
// users["bob"].role.title scenario, but guarding each hop individually
// with "?." instead of wrapping the whole expression in a trailing "??" -
// the two compose to the same happy-path/missing-path outcomes here since
// every hop after the possibly-absent one is also guarded.
func TestOptionalChainingMotivatingExample(t *testing.T) {
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
		{`users["alice"]?.Role?.Title`, "Admin"},
		// "bob" exists but Role is a nil pointer - "?." on the nil Role
		// short-circuits to nil instead of erroring on .Title.
		{`users["bob"]?.Role?.Title`, nil},
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

// TestOptionalChainingCascades confirms a chain of several "?." hops
// cascades correctly with no explicit coordination between them: once an
// earlier hop short-circuits to nil, every later "?." in the same chain
// also sees nil as its own target and short-circuits again, rather than
// erroring. "b" must be a present key holding nil, not a missing one -
// accessMember errors on a missing map key (see its own doc comment),
// so a missing key isn't a "nil target" case "?." can rescue at all.
func TestOptionalChainingCascades(t *testing.T) {
	env := map[string]any{"a": map[string]any{"b": nil}}
	got := evalCoalesce(t, env, `a?.b?.c?.d`)
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestOptionalChainingOnlyGuardsItsOwnHop confirms "?." is deliberately
// narrower than JS's "poisons the rest of the chain" semantics: a plain
// "." that follows a "?." still errors normally if the "?." hop's result
// was nil - only combining with the existing "??" recovers that case.
func TestOptionalChainingOnlyGuardsItsOwnHop(t *testing.T) {
	env := map[string]any{"a": map[string]any{}} // a.b is absent
	instructions, err := owlexpr.Compile(`a?.b.c`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatal("expected an error accessing .c on the nil result of a?.b, got none")
	}

	// But combined with "??", the whole thing recovers, same as any other
	// error "??" catches.
	got := evalCoalesce(t, env, `a?.b.c ?? "fallback"`)
	if got != "fallback" {
		t.Errorf("got %v, want fallback", got)
	}
}

// TestOptionalIndexing covers "target?.[index]": a nil target
// short-circuits to nil, same as "?.member", while a non-nil target
// indexes normally (including a genuine out-of-range error still
// surfacing, since "?." only guards the target's own nilness).
func TestOptionalIndexing(t *testing.T) {
	// "missing" is a present env key holding nil, not an undefined
	// variable - OpLoad itself errors on a genuinely undefined name,
	// before "?." ever gets a target value to check.
	env := map[string]any{"missing": nil, "present": []any{int64(10), int64(20)}}

	got := evalCoalesce(t, env, `missing?.[0]`)
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}

	got = evalCoalesce(t, env, `present?.[1]`)
	if got != int64(20) {
		t.Errorf("got %v, want 20", got)
	}

	instructions, err := owlexpr.Compile(`present?.[5]`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatal("expected an out-of-range error, got none")
	}
}
