package owlexpr

import (
	"testing"

	"github.com/cms103/owlexpr/vm"
)

// TestFilterCanBeOverriddenPerVM demonstrates precisely why filter/map/
// reduce can't be inlined to bytecode at compile time based on syntactic
// shape (`filter(list, <literal lambda>)`): Compile() takes no *Machine
// and produces plain []vm.Instruction with no VM identity baked in - the
// exact same compiled program can be run against a VM that overrides
// "filter" and one that doesn't, and must see different behavior on each.
// If the compiler rewrote this call site into inline loop bytecode
// whenever it saw a literal lambda argument, this override would be
// silently skipped - a real semantic hazard, not just a missed
// optimization.
func TestFilterCanBeOverriddenPerVM(t *testing.T) {
	instructions, err := Compile(`filter(Ints, x => x > 0)`)
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]any{"Ints": []int64{1, -2, 3}}

	stock, err := NewVM()
	if err != nil {
		t.Fatal(err)
	}
	res, err := stock.Run(instructions, env)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(res.([]any)); got != 2 {
		t.Fatalf("stock VM: got %d survivors, want 2", got)
	}

	overridden, err := NewVM(func(mc *vm.Machine) error {
		mc.RegisterBuiltin("filter", func(mc *vm.Machine, args ...any) (any, error) {
			return "overridden!", nil
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	res2, err := overridden.Run(instructions, env)
	if err != nil {
		t.Fatal(err)
	}
	if res2 != "overridden!" {
		t.Fatalf("overridden VM: got %v, want the override's result - same compiled bytecode, different VM, must differ", res2)
	}
}
