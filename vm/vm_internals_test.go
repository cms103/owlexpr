package vm

import (
	"testing"

	"uuid"
)

// TestRegisterOperationRejectsNilBaseType covers resolveOrAssignTypeCode's
// "cannot register an operation for untyped nil" guard, surfaced through
// registerOperationalHandler's baseType-resolution error wrap.
func TestRegisterOperationRejectsNilBaseType(t *testing.T) {
	if _, err := UnconfiguredVM(RegisterOperation(nil, nil, func(a, b any, aTC, bTC TypeCode, op OpCode) (any, error) {
		return nil, nil
	})); err == nil {
		t.Error("expected RegisterOperation(nil baseType) to error")
	}
}

// TestRegisterOperationRejectsNilCoercibleType covers the same guard, hit
// instead through the coercibleTypes loop.
func TestRegisterOperationRejectsNilCoercibleType(t *testing.T) {
	handler := func(a, b any, aTC, bTC TypeCode, op OpCode) (any, error) { return nil, nil }
	if _, err := UnconfiguredVM(RegisterOperation(centsWithNeg{}, []any{nil}, handler)); err == nil {
		t.Error("expected RegisterOperation with a nil coercible type to error")
	}
}

// TestRegisterFastSliceIteratorRejectsNilBaseType mirrors the above for
// the other VMOption constructor that resolves a baseType via
// resolveOrAssignTypeCode. (RegisterNegation used to have its own such
// test here too, before negation was folded into RegisterOperation's
// dispatch - TestRegisterOperationRejectsNilBaseType above already covers
// this same guard for that mechanism.)
func TestRegisterFastSliceIteratorRejectsNilBaseType(t *testing.T) {
	if _, err := UnconfiguredVM(RegisterFastSliceIterator(nil, func(slice any, yield func(any) bool) bool { return false })); err == nil {
		t.Error("expected RegisterFastSliceIterator(nil baseType) to error")
	}
}

// TestClosureCallArgCountMismatch covers closure.call's own arg-count
// guard, reached by calling a compiled lambda with the wrong number of
// arguments via OpCall - root-package tests never do this directly since
// the compiler always emits a matching ArgCount for a literal call
// expression; this is only reachable when a closure value flows through
// something else that calls it with a different count (e.g. map/filter/
// reduce, whose own arg-count is separately guarded, or - as here - raw
// bytecode).
func TestClosureCallArgCountMismatch(t *testing.T) {
	mc := mustVM(t)
	proto := &LambdaProto{Params: []string{"x", "y"}, Instructions: []Instruction{
		{Op: OpLoadLocal, Arg: LocalRef{Depth: 0, Slot: 0}},
	}}
	instructions := []Instruction{
		{Op: OpMakeClosure, Arg: proto},
		{Op: OpPush, Arg: int64(1)},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 1}},
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Error("expected an error calling a 2-param lambda with 1 argument")
	}
}

// TestOpMakeMapRejectsNonStringKey covers OpMakeMap's own key-type guard
// at the bytecode level - unreachable via the parser/compiler (map
// literal keys always compile to a StringNode), but a real invariant of
// the bytecode format itself, worth guarding regardless of what currently
// produces it.
func TestOpMakeMapRejectsNonStringKey(t *testing.T) {
	mc := mustVM(t)
	instructions := []Instruction{
		{Op: OpPush, Arg: int64(1)}, // key (not a string)
		{Op: OpPush, Arg: "value"},
		{Op: OpMakeMap, Arg: 1},
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Error("expected OpMakeMap to reject a non-string key")
	}
}

// TestOpJumpIfFalseRequiresBool covers OpJumpIfFalse's own condition-type
// guard at the bytecode level.
func TestOpJumpIfFalseRequiresBool(t *testing.T) {
	mc := mustVM(t)
	instructions := []Instruction{
		{Op: OpPush, Arg: int64(1)},
		{Op: OpJumpIfFalse, Arg: 99},
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Error("expected OpJumpIfFalse to reject a non-bool condition")
	}
}

// TestOpLetPropagatesBodyError covers OpLet's nested mc.run() error
// propagation.
func TestOpLetPropagatesBodyError(t *testing.T) {
	mc := mustVM(t)
	instructions := []Instruction{
		{Op: OpPush, Arg: int64(1)},
		{Op: OpLet, Arg: &LetArg{Name: "x", Body: []Instruction{
			{Op: OpLoadLocal, Arg: LocalRef{Depth: 0, Slot: 0}},
			{Op: OpPush, Arg: int64(0)},
			{Op: OpDiv},
		}}},
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Error("expected the let body's division-by-zero error to propagate")
	}
}

// TestRunOnEmptyInstructionsReturnsNil covers run()'s "nothing was ever
// pushed" fallback.
func TestRunOnEmptyInstructionsReturnsNil(t *testing.T) {
	mc := mustVM(t)
	got, err := mc.Run(nil, nil)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != nil {
		t.Errorf("Run(no instructions) = %v, want nil", got)
	}
}

// TestCallReflectFuncArgCountMismatch covers callReflectFunc's non-variadic
// arg-count guard.
func TestCallReflectFuncArgCountMismatch(t *testing.T) {
	mc := mustVM(t)
	hostFunc := func(a, b int64) int64 { return a + b }
	env := map[string]any{"hostFunc": hostFunc}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "hostFunc"},
		{Op: OpPush, Arg: int64(1)},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 1}},
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Error("expected an error calling a 2-arg function with 1 argument")
	}
}

// TestCallReflectFuncNilArgument covers callReflectFunc's `arg == nil`
// branch (reflect.Zero(paramType) for a nil positional argument).
func TestCallReflectFuncNilArgument(t *testing.T) {
	mc := mustVM(t)
	hostFunc := func(p *int64) bool { return p == nil }
	env := map[string]any{"hostFunc": hostFunc}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "hostFunc"},
		{Op: OpPush, Arg: nil},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 1}},
	}
	got, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != true {
		t.Errorf("hostFunc(nil) = %v, want true (nil *int64)", got)
	}
}

// TestCallReflectFuncConvertibleArgument covers callReflectFunc's
// ConvertibleTo (not directly assignable) argument-conversion branch: an
// int64 literal passed to a plain `int` parameter.
func TestCallReflectFuncConvertibleArgument(t *testing.T) {
	mc := mustVM(t)
	hostFunc := func(n int) int { return n * 2 }
	env := map[string]any{"hostFunc": hostFunc}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "hostFunc"},
		{Op: OpPush, Arg: int64(21)},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 1}},
	}
	got, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != 42 {
		t.Errorf("hostFunc(int64(21)) = %v, want 42", got)
	}
}

// TestCallReflectFuncNonCallableFuncArgument covers isCallable's false
// branch as seen from callReflectFunc: a func-typed parameter fed a value
// that isn't assignable, convertible, NOR one of owlexpr's callable
// kinds, so it must fall through to the final "cannot use" error rather
// than trying (and panicking inside) makeReflectFuncAdapter.
func TestCallReflectFuncNonCallableFuncArgument(t *testing.T) {
	mc := mustVM(t)
	hostFunc := func(pred func(int64) bool) bool { return pred(1) }
	env := map[string]any{"hostFunc": hostFunc, "n": int64(5)}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "hostFunc"},
		{Op: OpLoad, Arg: "n"},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 1}},
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Error("expected an error passing a plain int64 where a callback func was expected")
	}
}

// TestConvertResultPanicsOnIncompatibleType covers convertResult's final
// panic branch directly: a lambda result that's neither assignable nor
// convertible to the declared output type.
func TestConvertResultPanicsOnIncompatibleType(t *testing.T) {
	mc := mustVM(t)
	hostFunc := func(transform func(int64) bool) bool {
		return transform(4)
	}
	env := map[string]any{"hostFunc": hostFunc}
	// x => [x] (a list result, which cannot convert to a bool return type)
	body := []Instruction{
		{Op: OpLoad, Arg: "x"},
		{Op: OpMakeList, Arg: 1},
	}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "hostFunc"},
		lambdaClosureInstructions(body),
		{Op: OpCall, Arg: CallMetadata{ArgCount: 1}},
	}
	defer func() {
		if recover() == nil {
			t.Error("expected a panic converting a []any lambda result to a bool return type")
		}
	}()
	_, _ = mc.Run(instructions, env)
}

// TestToIndexIntPlainInt covers toIndexInt's `case int:` branch directly -
// existing indexing tests all push int64 (what number literals compile
// to), never a plain Go int.
func TestToIndexIntPlainInt(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"xs": []any{"a", "b", "c"}}
	got, err := runIndex(t, mc, env, "xs", int(1))
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != "b" {
		t.Errorf("xs[int(1)] = %v, want %q", got, "b")
	}
}

// TestIndexValueReflectedSliceBadIndexType covers the reflect.Slice case's
// own toIndexInt error propagation (as opposed to the []any fast path's,
// already covered elsewhere).
func TestIndexValueReflectedSliceBadIndexType(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"xs": []int64{1, 2, 3}}
	if _, err := runIndex(t, mc, env, "xs", 1.5); err == nil {
		t.Error("expected an error indexing a []int64 with a float64")
	}
}

// TestRegisterTypeCoderDuplicateIDIsNoOp covers RegisterTypeCoder's
// short-circuit: registering the same coder ID a second time (e.g. an
// extension package applied twice via two overlapping VMOption sets)
// must not append a second, redundant entry.
func TestRegisterTypeCoderDuplicateIDIsNoOp(t *testing.T) {
	id := uuid.NewV4()
	coder := func(a any) TypeCode { return UnSupportedType }
	mc, err := UnconfiguredVM(RegisterTypeCoder(coder, id), RegisterTypeCoder(coder, id))
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if len(mc.extraTypers) != 1 {
		t.Errorf("extraTypers = %d entries, want exactly 1 after registering the same ID twice", len(mc.extraTypers))
	}
}

// TestIndexValueReflectedMapInconvertibleKey covers the reflect.Map case's
// "cannot use %T as key of type %s" error for a needle that's genuinely
// inconvertible to the map's key type (as opposed to merely absent).
func TestIndexValueReflectedMapInconvertibleKey(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"m": map[int]string{1: "a"}, "key": accessTarget{Name: "x"}}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "m"},
		{Op: OpLoad, Arg: "key"},
		{Op: OpIndex},
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Error("expected an error indexing a map[int]string with a struct key")
	}
}
