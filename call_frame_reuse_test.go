package owlexpr

import "testing"

// TestMapEscapingClosuresStayDistinct guards the correctness assumption
// vm.ReusableCall's scope-reuse fast path depends on: a lambda body that
// itself creates and returns a nested closure (here, map(Ints, x => (y
// => x + y))) must be excluded from the reuse fast path (LambdaProto.
// NoEscape must be false for it), because each returned inner
// closure captures the outer lambda's param scope *by reference* - if
// that scope were reused/mutated across iterations, every returned
// closure would wrongly observe the *last* iteration's x instead of the
// one live when it was created.
func TestMapEscapingClosuresStayDistinct(t *testing.T) {
	env := map[string]any{"Ints": []int64{1, 2, 3}}
	instructions, err := Compile(`map(Ints, x => (y => x + y))`)
	if err != nil {
		t.Fatal(err)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatal(err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatal(err)
	}
	closures := res.([]any)
	if len(closures) != 3 {
		t.Fatalf("got %d closures, want 3", len(closures))
	}
	want := []int64{1 + 10, 2 + 10, 3 + 10}
	for i, c := range closures {
		got, err := mc.Call(c, []any{int64(10)})
		if err != nil {
			t.Fatal(err)
		}
		if got != want[i] {
			t.Fatalf("closure %d: got %v, want %v (stale-capture bug if this equals %v)", i, got, want[i], want[len(want)-1])
		}
	}
}
