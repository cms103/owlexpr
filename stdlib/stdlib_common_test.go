package stdlib

import (
	"reflect"
	"testing"
	"time"

	"github.com/cms103/owlexpr"
	"github.com/cms103/owlexpr/vm"

	"github.com/shopspring/decimal"
)

// evalAll parses, compiles, and runs input against a VM with All()
// enabled, failing the test on any parse/compile/run error.
func evalAll(t *testing.T, mc *vm.Machine, env map[string]any, input string) any {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	return res
}

// TestAllRegistersEveryPack is All()'s previously untested smoke test:
// registering it should make a representative call from every one of the
// packs it bundles (time, string, list, iter, decimal, bytes, ...)
// succeed against a single VM, with no error from All() itself.
func TestAllRegistersEveryPack(t *testing.T) {
	mc, err := owlexpr.NewVM(All())
	if err != nil {
		t.Fatalf("NewVM(All()): %v", err)
	}

	env := map[string]any{"amount": decimal.NewFromFloat(10.5)}

	if got := evalAll(t, mc, env, `string.upper("hi")`); got != "HI" {
		t.Errorf(`string.upper("hi") via All() = %v, want "HI"`, got)
	}
	if got := evalAll(t, mc, env, `list.reverse([1, 2, 3])`); !reflect.DeepEqual(got, []any{int64(3), int64(2), int64(1)}) {
		t.Errorf("list.reverse([1,2,3]) via All() = %v, want [3 2 1]", got)
	}
	if got := evalAll(t, mc, env, `iter.toList(iter.take(iter.of([1, 2, 3]), 2))`); !reflect.DeepEqual(got, []any{int64(1), int64(2)}) {
		t.Errorf("iter.toList(iter.take(iter.of([1,2,3]), 2)) via All() = %v, want [1 2]", got)
	}
	if got := evalAll(t, mc, env, `time.hours(2)`); got != 2*time.Hour {
		t.Errorf("time.hours(2) via All() = %v, want 2h", got)
	}
	if got := evalAll(t, mc, env, `bytes.fromHex("FF") == bytes.fromHex("FF")`); got != true {
		t.Errorf(`bytes.fromHex("FF") == bytes.fromHex("FF") via All() = %v, want true`, got)
	}
	got := evalAll(t, mc, env, `amount + 1`)
	if d, ok := got.(decimal.Decimal); !ok || d.String() != "11.5" {
		t.Errorf("amount + 1 via All() = %v, want decimal 11.5", got)
	}
}
