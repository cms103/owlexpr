package vm

import "testing"

// lambdaClosureInstructions builds a single-param closure `x => body`
// (body as raw bytecode operating on the loaded param "x") for use as a
// callback argument bridged through makeReflectFuncAdapter. Body must
// reference its param via OpLoadLocal{Depth: 0, Slot: 0} - what
// compiler.go itself would emit for a single-param lambda's own "x" -
// not OpLoad, which (since the locals/scopes split - see localFrame's
// doc comment in vm.go) no longer searches lambda-param bindings at all.
func lambdaClosureInstructions(body []Instruction) Instruction {
	proto := &LambdaProto{Params: []string{"x"}, Instructions: body}
	return Instruction{Op: OpMakeClosure, Arg: proto}
}

// loadX is the raw-bytecode spelling of "x" inside a single-param lambda
// body built via lambdaClosureInstructions - see its doc comment.
var loadX = Instruction{Op: OpLoadLocal, Arg: LocalRef{Depth: 0, Slot: 0}}

// TestReflectFuncAdapterBridgesLambdaAsCallback covers isCallable and
// makeReflectFuncAdapter's basic bridging path (a lambda closure passed
// where a plain Go function value is expected) plus convertResult's
// exact-type AssignableTo branch - none of which any existing test
// reached, since every existing callback-shaped test uses map/filter/
// reduce (which call the lambda directly via mc.Call, never through
// reflection).
func TestReflectFuncAdapterBridgesLambdaAsCallback(t *testing.T) {
	mc := mustVM(t)
	hostFunc := func(pred func(int64) bool) bool {
		return pred(10)
	}
	env := map[string]any{"hostFunc": hostFunc}
	// x => x > 5
	body := []Instruction{
		loadX,
		{Op: OpPush, Arg: int64(5)},
		{Op: OpGreater},
	}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "hostFunc"},
		lambdaClosureInstructions(body),
		{Op: OpCall, Arg: CallMetadata{ArgCount: 1}},
	}
	got, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != true {
		t.Errorf("hostFunc(x => x > 5) = %v, want true", got)
	}
}

// TestReflectFuncAdapterConvertsResultType covers convertResult's
// ConvertibleTo (not directly assignable) branch: the lambda body produces
// an int64, but the host callback's declared return type is a plain int.
func TestReflectFuncAdapterConvertsResultType(t *testing.T) {
	mc := mustVM(t)
	hostFunc := func(transform func(int64) int) int {
		return transform(4)
	}
	env := map[string]any{"hostFunc": hostFunc}
	// x => x * 2  (result is int64, adapter's declared out type is int)
	body := []Instruction{
		loadX,
		{Op: OpPush, Arg: int64(2)},
		{Op: OpMul},
	}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "hostFunc"},
		lambdaClosureInstructions(body),
		{Op: OpCall, Arg: CallMetadata{ArgCount: 1}},
	}
	got, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != 8 {
		t.Errorf("hostFunc(x => x * 2) = %v, want 8", got)
	}
}

// TestReflectFuncAdapterPropagatesErrorViaErrorOut covers
// makeReflectFuncAdapter's lastIsError branch: when the bridged callback's
// declared signature ends in `error` and the lambda body itself errors
// (mc.Call returns a non-nil error), that error is returned through the
// adapter's error output rather than panicking.
func TestReflectFuncAdapterPropagatesErrorViaErrorOut(t *testing.T) {
	mc := mustVM(t)
	var callbackErr error
	hostFunc := func(transform func(int64) (int64, error)) int64 {
		v, err := transform(4)
		callbackErr = err
		return v
	}
	env := map[string]any{"hostFunc": hostFunc}
	// x => x / 0 (errors)
	body := []Instruction{
		loadX,
		{Op: OpPush, Arg: int64(0)},
		{Op: OpDiv},
	}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "hostFunc"},
		lambdaClosureInstructions(body),
		{Op: OpCall, Arg: CallMetadata{ArgCount: 1}},
	}
	got, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != int64(0) {
		t.Errorf("hostFunc(...) = %v, want zero value on error", got)
	}
	if callbackErr == nil {
		t.Error("expected the lambda's division-by-zero error to propagate through the adapter's error output")
	}
}

// TestReflectFuncAdapterPanicsWithNoErrorOut covers makeReflectFuncAdapter's
// documented tradeoff: if the lambda body errors and the callback's
// declared signature has no error return to report it through, the
// adapter panics rather than silently swallowing the failure.
func TestReflectFuncAdapterPanicsWithNoErrorOut(t *testing.T) {
	mc := mustVM(t)
	hostFunc := func(transform func(int64) int64) int64 {
		return transform(4)
	}
	env := map[string]any{"hostFunc": hostFunc}
	// x => x / 0 (errors, but transform has no error return to catch it)
	body := []Instruction{
		loadX,
		{Op: OpPush, Arg: int64(0)},
		{Op: OpDiv},
	}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "hostFunc"},
		lambdaClosureInstructions(body),
		{Op: OpCall, Arg: CallMetadata{ArgCount: 1}},
	}

	defer func() {
		if recover() == nil {
			t.Error("expected a panic when the bridged callback has no error output to report the lambda's failure through")
		}
	}()
	_, _ = mc.Run(instructions, env)
}

// TestReflectFuncAdapterMultipleNonErrorOutputs covers
// makeReflectFuncAdapter's valueOuts>1 branch: a single `any` lambda
// result can only populate the first non-error output, so any further
// ones must be zeroed rather than left undefined.
func TestReflectFuncAdapterMultipleNonErrorOutputs(t *testing.T) {
	mc := mustVM(t)
	hostFunc := func(transform func(int64) (int64, int64)) (int64, int64) {
		return transform(4)
	}
	env := map[string]any{"hostFunc": hostFunc}
	body := []Instruction{
		loadX,
		{Op: OpPush, Arg: int64(2)},
		{Op: OpMul},
	}
	// callReflectFunc returns two non-error results as a list, so both
	// outputs are observable: the lambda's result fills the first, and the
	// second must come back zeroed (not left as garbage that would trip
	// reflect's own zero-value invariants).
	instructions := []Instruction{
		{Op: OpLoad, Arg: "hostFunc"},
		lambdaClosureInstructions(body),
		{Op: OpCall, Arg: CallMetadata{ArgCount: 1}},
	}
	got, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	list, ok := got.([]any)
	if !ok || len(list) != 2 || list[0] != int64(8) || list[1] != int64(0) {
		t.Errorf("hostFunc(x => x * 2) = %v (%T), want [8 0]", got, got)
	}
}

// TestCallableArgumentTypeMismatchErrors covers callReflectFunc's final
// "cannot use %T as %s" error when an argument is neither assignable,
// convertible, nor (for a func-typed parameter) a owlexpr callable.
func TestCallableArgumentTypeMismatchErrors(t *testing.T) {
	mc := mustVM(t)
	hostFunc := func(n int64) int64 { return n }
	env := map[string]any{"hostFunc": hostFunc, "s": "not a number"}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "hostFunc"},
		{Op: OpLoad, Arg: "s"},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 1}},
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Error("expected an error calling hostFunc(int64) with a string argument")
	}
}

// TestCallVariadicFunction covers callReflectFunc's variadic-argument-count
// guard and IsVariadic handling, untested elsewhere.
func TestCallVariadicFunction(t *testing.T) {
	mc := mustVM(t)
	sumAll := func(nums ...int64) int64 {
		var total int64
		for _, n := range nums {
			total += n
		}
		return total
	}
	env := map[string]any{"sumAll": sumAll}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "sumAll"},
		{Op: OpPush, Arg: int64(1)},
		{Op: OpPush, Arg: int64(2)},
		{Op: OpPush, Arg: int64(3)},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 3}},
	}
	got, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != int64(6) {
		t.Errorf("sumAll(1, 2, 3) = %v, want 6", got)
	}

	// Fewer args than the fixed (non-variadic) prefix requires.
	takesTwo := func(a int64, nums ...int64) int64 { return a }
	env["takesTwo"] = takesTwo
	instructions = []Instruction{
		{Op: OpLoad, Arg: "takesTwo"},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 0}},
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Error("expected an error calling a variadic function with too few arguments")
	}
}

// TestCallFunctionOnUncallableValueErrors covers callFunction's final
// "cannot call target of type" error for a value that's neither a
// BuiltinFunc, a closure, a namespace map, nor any kind of Go function.
func TestCallFunctionOnUncallableValueErrors(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"n": int64(5)}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "n"},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 0}},
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Error("expected an error calling an int64 as a function")
	}
}

// TestRunWrapsLambdaResultAsCallableFunc covers WrapCallable via Run:
// when an expression's own final value is a lambda literal - `x => x +
// 1` evaluated by itself, not called - the embedder should get back an
// ordinary Go func value it can call directly, not owlexpr's unexported
// *closure type (which nothing outside this package can even name).
func TestRunWrapsLambdaResultAsCallableFunc(t *testing.T) {
	mc := mustVM(t)
	// x => x + 1
	body := []Instruction{
		loadX,
		{Op: OpPush, Arg: int64(1)},
		{Op: OpAdd},
	}
	instructions := []Instruction{lambdaClosureInstructions(body)}

	got, err := mc.Run(instructions, nil)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	fn, ok := got.(func(args ...any) (any, error))
	if !ok {
		t.Fatalf("Run's result is %T, want func(args ...any) (any, error)", got)
	}
	out, err := fn(int64(4))
	if err != nil {
		t.Fatalf("calling wrapped lambda: %v", err)
	}
	if out != int64(5) {
		t.Errorf("wrapped(4) = %v, want 5", out)
	}
}
