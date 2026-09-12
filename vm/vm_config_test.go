package vm

import (
	"testing"
)

// TestRegisterBuiltInOption covers the RegisterBuiltIn VMOption
// constructor end-to-end (OpLoad resolving it by name, then OpCall
// invoking it) - previously untested at any level; only the lower-level
// Machine.RegisterBuiltin method it wraps was exercised indirectly via
// root's own builtins.
func TestRegisterBuiltInOption(t *testing.T) {
	double := func(mc *Machine, args ...any) (any, error) {
		return args[0].(int64) * 2, nil
	}
	mc, err := UnconfiguredVM(RegisterBuiltIn("double", double))
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "double"},
		{Op: OpPush, Arg: int64(21)},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 1}},
	}
	got, err := mc.Run(instructions, nil)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != int64(42) {
		t.Errorf("double(21) = %v, want 42", got)
	}
}

// TestRegisterNamespacedBuiltInOption covers the RegisterNamespacedBuiltIn
// VMOption constructor - loading the namespace (pushing its
// map[string]BuiltinFunc), accessing the member by name (via
// accessMember's generic map-dot-access rule), and calling it.
func TestRegisterNamespacedBuiltInOption(t *testing.T) {
	trim := func(mc *Machine, args ...any) (any, error) {
		return "trimmed", nil
	}
	mc, err := UnconfiguredVM(RegisterNamespacedBuiltIn("string", "trim", trim))
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "string"},
		{Op: OpAccess, Arg: "trim"},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 0}},
	}
	got, err := mc.Run(instructions, nil)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != "trimmed" {
		t.Errorf("string.trim() = %v, want %q", got, "trimmed")
	}
}

// TestNamespaceNotCallableDirectly covers callFunction's explicit guard
// against calling a bare namespace reference (`string` with no `.member`)
// - a clearer error than the generic "cannot call target of type" that
// callReflectFunc would otherwise produce.
func TestNamespaceNotCallableDirectly(t *testing.T) {
	trim := func(mc *Machine, args ...any) (any, error) { return nil, nil }
	mc, err := UnconfiguredVM(RegisterNamespacedBuiltIn("string", "trim", trim))
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "string"},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 0}},
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Fatal("expected calling a bare namespace to error")
	}
}

// TestDisableAutoTypeCoercionOption covers the DisableAutoTypeCoercion
// VMOption: with it set, Combine (and so the compiled arithmetic/
// comparison operators) must reject any pair of operands whose TypeCodes
// differ, even though a coercion handler is registered and would normally
// succeed - same-type operands must still work, since that path in
// operationDispatcher never consults autoCoerc at all.
func TestDisableAutoTypeCoercionOption(t *testing.T) {
	mc, err := UnconfiguredVM(DisableAutoTypeCoercion())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if got := evalDyn(t, mc, nil, int64(3), int64(4), OpAdd); got != int64(7) {
		t.Errorf("int64(3) + int64(4) = %v, want 7 (same-type ops must still work)", got)
	}
	evalDynExpectError(t, mc, nil, int64(3), float64(4.2), OpAdd)
	evalDynExpectError(t, mc, nil, int64(3), float64(4.2), OpLess)

	// Without the option, the same mixed-type operation succeeds via the
	// default coercion path - confirms the failure above is actually
	// caused by DisableAutoTypeCoercion, not some other error.
	mcDefault, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if got := evalDyn(t, mcDefault, nil, int64(3), float64(4.2), OpAdd); got != 7.2 {
		t.Errorf("int64(3) + float64(4.2) = %v, want 7.2", got)
	}
}

// TestCoercValueIgnoresDisableAutoTypeCoercion covers CoercValue's own
// contract: it's explicit-conversion-on-request, so it must keep working
// even on a VM built with DisableAutoTypeCoercion() - that option is only
// meant to gate the *implicit* coercion Combine does for +, -, ==, etc.
func TestCoercValueIgnoresDisableAutoTypeCoercion(t *testing.T) {
	mc, err := UnconfiguredVM(DisableAutoTypeCoercion())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	got, err := mc.CoercValue(float64(4.9), int64(0))
	if err != nil {
		t.Fatalf("CoercValue: %v", err)
	}
	if got != int64(4) {
		t.Errorf("CoercValue(4.9, int64(0)) = %v, want int64(4)", got)
	}
}

// TestDisableStructMethodsOption covers the DisableStructMethods VMOption:
// with it set, accessMember's method lookup (vm.go) must be skipped
// entirely, so `s.Greet` fails as if the method didn't exist, while plain
// field access (`s.Name`) - which never goes through that lookup - keeps
// working. accessTarget is the same struct fixture indexing_test.go's
// TestAccessMemberOnStructAndPointer uses for the un-disabled case.
func TestDisableStructMethodsOption(t *testing.T) {
	mc, err := UnconfiguredVM(DisableStructMethods())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	env := map[string]any{"s": accessTarget{Name: "Ada"}}

	instructions := []Instruction{
		{Op: OpLoad, Arg: "s"},
		{Op: OpAccess, Arg: "Name"},
	}
	got, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != "Ada" {
		t.Errorf("s.Name = %v, want Ada", got)
	}

	instructions = []Instruction{
		{Op: OpLoad, Arg: "s"},
		{Op: OpAccess, Arg: "Greet"},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 0}},
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Error("expected DisableStructMethods to make s.Greet() fail as an unknown member")
	}

	// Without the option, the same method call succeeds - confirms the
	// failure above is actually caused by DisableStructMethods, not some
	// other error.
	mcDefault, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	got, err = mcDefault.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != "hello Ada" {
		t.Errorf("s.Greet() = %v, want %q", got, "hello Ada")
	}
}

// TestDisableStructMethodsIgnoresNamespacedBuiltins confirms
// DisableStructMethods only gates accessMember's reflect-based
// MethodByName step, not namespaced-builtin dot access (RegisterNamespacedBuiltIn)
// - the two share the `.name` syntax but resolve through entirely
// separate branches of accessMember (see its doc comment).
func TestDisableStructMethodsIgnoresNamespacedBuiltins(t *testing.T) {
	trim := func(mc *Machine, args ...any) (any, error) {
		return "trimmed", nil
	}
	mc, err := UnconfiguredVM(RegisterNamespacedBuiltIn("string", "trim", trim), DisableStructMethods())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "string"},
		{Op: OpAccess, Arg: "trim"},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 0}},
	}
	got, err := mc.Run(instructions, nil)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != "trimmed" {
		t.Errorf("string.trim() = %v, want %q", got, "trimmed")
	}
}

func TestDisableStructMethodsIgnoresEnvFuncs(t *testing.T) {
	testFunc := func(args ...any) (any, error) {
		return "testFuncReturn", nil
	}

	mc, err := UnconfiguredVM(DisableStructMethods())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}

	env := map[string]any{
		"testFunc": testFunc,
	}

	instructions := []Instruction{
		{Op: OpLoad, Arg: "testFunc"},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 0}},
	}
	got, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got != "testFuncReturn" {
		t.Errorf("string.trim() = %v, want %q", got, "testFuncReturn")
	}
}

// TestDisableBuiltInsOption covers the DisableBuiltIns VMOption
// constructor: a builtin registered earlier in the option chain must no
// longer resolve afterward.
func TestDisableBuiltInsOption(t *testing.T) {
	double := func(mc *Machine, args ...any) (any, error) {
		return args[0].(int64) * 2, nil
	}
	mc, err := UnconfiguredVM(RegisterBuiltIn("double", double), DisableBuiltIns())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	instructions := []Instruction{
		{Op: OpLoad, Arg: "double"},
		{Op: OpPush, Arg: int64(21)},
		{Op: OpCall, Arg: CallMetadata{ArgCount: 1}},
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Fatal("expected DisableBuiltIns to remove the earlier-registered builtin")
	}
}
