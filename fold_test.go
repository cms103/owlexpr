package owlexpr

import (
	"testing"

	"github.com/cms103/owlexpr/vm"
)

// countInstructionsOfOp counts how many instructions in a compiled
// program have the given Op - used below to confirm a folded expression
// really did collapse to a single OpPush, not just happen to produce the
// right answer some other way.
func countInstructionsOfOp(instructions []vm.Instruction, op vm.OpCode) int {
	n := 0
	for _, inst := range instructions {
		if inst.Op == op {
			n++
		}
	}
	return n
}

// TestFoldConstantsArithmetic checks the common case foldConstants
// exists for: 60 * 60 * 24 compiles down to a single OpPush(86400), with
// zero OpMul instructions at all.
func TestFoldConstantsArithmetic(t *testing.T) {
	instructions, err := Compile(`60 * 60 * 24`)
	if err != nil {
		t.Fatal(err)
	}
	if got := countInstructionsOfOp(instructions, vm.OpMul); got != 0 {
		t.Fatalf("got %d OpMul instructions, want 0 (should be folded away)", got)
	}
	if len(instructions) != 1 || instructions[0].Op != vm.OpPush || instructions[0].Arg != int64(86400) {
		t.Fatalf("got %v, want a single OpPush(86400)", instructions)
	}

	mc, err := NewVM()
	if err != nil {
		t.Fatal(err)
	}
	got, err := mc.Run(instructions, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != int64(86400) {
		t.Fatalf("got %v, want 86400", got)
	}
}

// TestFoldConstantsNested checks a constant sub-expression buried inside
// a call argument still folds, even though the containing CallNode
// obviously can't be folded itself.
func TestFoldConstantsNested(t *testing.T) {
	instructions, err := Compile(`len(Items) > 2 + 3`)
	if err != nil {
		t.Fatal(err)
	}
	if got := countInstructionsOfOp(instructions, vm.OpAdd); got != 0 {
		t.Fatalf("got %d OpAdd instructions, want 0 (2 + 3 should be folded)", got)
	}

	mc, err := NewVM()
	if err != nil {
		t.Fatal(err)
	}
	got, err := mc.Run(instructions, map[string]any{"Items": []any{1, 2, 3, 4, 5, 6}})
	if err != nil {
		t.Fatal(err)
	}
	if got != true {
		t.Fatalf("got %v, want true", got)
	}
}

// TestFoldConstantsDivideByZeroLeftForRuntime confirms a constant
// sub-expression that would fail to fold (divide by zero) is left as an
// ordinary BinaryOpNode, producing the normal runtime error rather than
// a compile-time one - see foldConstants' own doc comment for why that
// matters (the error should point at run time, matching every other
// runtime error in this language, not surprise the caller by moving to
// compile time just because the operands happened to be literals).
func TestFoldConstantsDivideByZeroLeftForRuntime(t *testing.T) {
	instructions, err := Compile(`1 / 0`)
	if err != nil {
		t.Fatal(err)
	}
	if got := countInstructionsOfOp(instructions, vm.OpDiv); got != 1 {
		t.Fatalf("got %d OpDiv instructions, want 1 (should not have folded)", got)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatal(err)
	}
	_, err = mc.Run(instructions, nil)
	if err == nil {
		t.Fatal("expected a divide-by-zero error at run time")
	}
}

// TestFoldConstantsMixedNumericTypesNotFolded is the regression test for
// the bug this pass's own development caught: `3 + 4.2` (int64 + float64)
// must NOT be folded, because whether that combination is even allowed
// depends on a runtime Machine setting (DisableAutoTypeCoercion) this
// pass has no way to know about. Folding it anyway would silently
// produce a valid answer even against a Machine configured to reject the
// coercion - see TestDisableAutoTypeCoercionEndToEnd (type_conversion_test.go),
// which is what actually caught this during development.
func TestFoldConstantsMixedNumericTypesNotFolded(t *testing.T) {
	instructions, err := Compile(`3 + 4.2`)
	if err != nil {
		t.Fatal(err)
	}
	if got := countInstructionsOfOp(instructions, vm.OpAdd); got != 1 {
		t.Fatalf("got %d OpAdd instructions, want 1 (mixed int64/float64 must not fold)", got)
	}

	coerced, err := NewVM()
	if err != nil {
		t.Fatal(err)
	}
	got, err := coerced.Run(instructions, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != 7.2 {
		t.Fatalf("got %v, want 7.2", got)
	}

	noCoercion, err := NewVM(vm.DisableAutoTypeCoercion())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := noCoercion.Run(instructions, nil); err == nil {
		t.Fatal("expected an error with DisableAutoTypeCoercion() and no fold - the whole point of not folding this")
	}
}

// TestFoldConstantsUnary checks unary negation/not on literals folds too.
func TestFoldConstantsUnary(t *testing.T) {
	instructions, err := Compile(`-(2 + 3) == -5 && !(false)`)
	if err != nil {
		t.Fatal(err)
	}
	// -(2+3) folds to -5; -5 == -5 folds to true; !(false) folds to
	// true; true && true is a jump, not an OpEqual/OpAdd/OpNeg/OpNot -
	// so nothing but a single OpPush(true) should remain from this
	// entirely-literal expression.
	for _, op := range []vm.OpCode{vm.OpAdd, vm.OpNeg, vm.OpEqual, vm.OpNot} {
		if got := countInstructionsOfOp(instructions, op); got != 0 {
			t.Fatalf("got %d %v instructions, want 0 (fully literal expression)", got, op)
		}
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatal(err)
	}
	got, err := mc.Run(instructions, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != true {
		t.Fatalf("got %v, want true", got)
	}
}

// TestFoldConstantsInsideLambda checks a constant sub-expression inside
// a lambda body (compiled into its own LambdaProto.Instructions, not the
// enclosing stream) still folds - map/filter/reduce predicates are
// exactly the kind of place a stray literal computation could otherwise
// re-run per element for no reason.
func TestFoldConstantsInsideLambda(t *testing.T) {
	instructions, err := Compile(`map(Ints, x => x + (2 * 3))`)
	if err != nil {
		t.Fatal(err)
	}
	var proto *vm.LambdaProto
	for _, inst := range instructions {
		if p, ok := inst.Arg.(*vm.LambdaProto); ok {
			proto = p
		}
	}
	if proto == nil {
		t.Fatal("no LambdaProto found")
	}
	if got := countInstructionsOfOp(proto.Instructions, vm.OpMul); got != 0 {
		t.Fatalf("got %d OpMul instructions inside the lambda body, want 0 (2 * 3 should be folded)", got)
	}

	mc, err := NewVM()
	if err != nil {
		t.Fatal(err)
	}
	got, err := mc.Run(instructions, map[string]any{"Ints": []int64{1, 2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	list := got.([]any)
	if len(list) != 3 || list[0] != int64(7) || list[1] != int64(8) || list[2] != int64(9) {
		t.Fatalf("got %v, want [7 8 9]", list)
	}
}

func BenchmarkArithmeticChainFolded_Plain(b *testing.B) {
	instructions, err := Compile(`2 + 3 * 4 - 1 == 13`)
	if err != nil {
		b.Fatal(err)
	}
	mc, err := NewVM()
	if err != nil {
		b.Fatal(err)
	}
	var out any
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err = mc.Run(instructions, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	sink = out
	if out != true {
		b.Fatalf("got %v, want true", out)
	}
}
