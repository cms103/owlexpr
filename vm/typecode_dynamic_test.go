package vm

import (
	"testing"
	"uuid"
)

// operandInstruction treats a string operand as a variable name (OpLoad)
// and anything else as a literal value to push (OpPush). Sufficient for
// this file's tests, which only ever combine two variables or two
// literals, never a mix, and never use a string literal as an operand
// itself.
//
// This file builds bytecode by hand rather than going through
// Parse/Compiler (a "left op right" expression string, as it used to):
// those live in the root owlexpr package, which imports virtualmachine
// for VM/NewVM - importing it back from here would be a cycle. Every test
// below only ever needs a single binary operator over two operands, so a
// tiny hand-assembled instruction sequence is simpler than working around
// that anyway.
func operandInstruction(v any) Instruction {
	if name, ok := v.(string); ok {
		return Instruction{Op: OpLoad, Arg: name}
	}
	return Instruction{Op: OpPush, Arg: v}
}

// evalDyn runs `left op right` against vm, failing the test on error.
// left/right are either a variable name (string, resolved via env) or a
// literal value to push directly.
func evalDyn(t *testing.T, mc *Machine, env map[string]any, left, right any, op OpCode) any {
	t.Helper()
	instructions := []Instruction{operandInstruction(left), operandInstruction(right), {Op: op}}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %v %v %v: %v", left, op, right, err)
	}
	return res
}

func evalDynExpectError(t *testing.T, mc *Machine, env map[string]any, left, right any, op OpCode) {
	t.Helper()
	instructions := []Instruction{operandInstruction(left), operandInstruction(right), {Op: op}}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatalf("expected an error for %v %v %v, got none", left, op, right)
	}
}

// cents is a stand-in for "a brand-new type an extension wants to add
// operator support for" (e.g. what TimeBuiltins would do for time.Time) -
// deliberately not one of GetTypeCode's six core types.
type cents struct{ n int64 }

func centsAddHandler(a, b any, aTC, bTC TypeCode, op OpCode) (any, error) {
	return cents{a.(cents).n + b.(cents).n}, nil
}

// TestRegisterOperationAutoAssignsTypeCode is the core new capability:
// RegisterOperation on a type GetTypeCode doesn't recognize should just
// work, with no separate type-registration step and no hand-written
// TypeCoder required.
func TestRegisterOperationAutoAssignsTypeCode(t *testing.T) {
	vm, err := UnconfiguredVM(RegisterOperation(cents{}, nil, centsAddHandler))
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	env := map[string]any{"a": cents{150}, "b": cents{25}}
	got := evalDyn(t, vm, env, "a", "b", OpAdd).(cents)
	if got.n != 175 {
		t.Errorf("a + b = %v, want cents{175}", got)
	}
}

// dollars is a second, independent "extension" type, to confirm two
// separate RegisterOperation calls for two different new types don't
// clobber each other's auto-assigned TypeCodes.
type dollars struct{ n int64 }

func dollarsAddHandler(a, b any, aTC, bTC TypeCode, op OpCode) (any, error) {
	return dollars{a.(dollars).n + b.(dollars).n}, nil
}

func TestRegisterOperationComposesAcrossIndependentTypes(t *testing.T) {
	vm, err := UnconfiguredVM(
		RegisterOperation(cents{}, nil, centsAddHandler),
		RegisterOperation(dollars{}, nil, dollarsAddHandler),
	)
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	env := map[string]any{
		"c1": cents{10}, "c2": cents{20},
		"d1": dollars{1}, "d2": dollars{2},
	}
	if got := evalDyn(t, vm, env, "c1", "c2", OpAdd).(cents); got.n != 30 {
		t.Errorf("c1 + c2 = %v, want cents{30}", got)
	}
	if got := evalDyn(t, vm, env, "d1", "d2", OpAdd).(dollars); got.n != 3 {
		t.Errorf("d1 + d2 = %v, want dollars{3}", got)
	}
}

// TestRegisterTypeCoderPrecedesDynamicMap confirms typeCode's documented
// ordering: a custom coder installed via RegisterTypeCoder is consulted
// before the dynamicTypes map, and RegisterOperation reuses the custom
// coder's own code for a type it already recognizes - resolveOrAssignTypeCode
// never mints a redundant dynamicTypes entry when that happens.
func TestRegisterTypeCoderPrecedesDynamicMap(t *testing.T) {
	const centsCode TypeCode = firstDynamicTypeCode + 500
	coder := func(a any) TypeCode {
		if _, ok := a.(cents); ok {
			return centsCode
		}
		return UnSupportedType
	}
	mc, err := UnconfiguredVM(
		RegisterTypeCoder(coder, uuid.NewV4()),
		RegisterOperation(cents{}, nil, centsAddHandler),
	)
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if got := mc.typeCode(cents{}); got != centsCode {
		t.Errorf("typeCode(cents{}) = %v, want the custom coder's code %v", got, centsCode)
	}
	if len(mc.dynamicTypes) != 0 {
		t.Errorf("expected no dynamicTypes entries when a custom coder already recognized the type, got %v", mc.dynamicTypes)
	}
	env := map[string]any{"a": cents{5}, "b": cents{7}}
	if got := evalDyn(t, mc, env, "a", "b", OpAdd).(cents); got.n != 12 {
		t.Errorf("a + b = %v, want cents{12}", got)
	}
}

// TestRegisterTypeCodersCompose confirms multiple RegisterTypeCoder calls
// (independent extensions, each recognizing its own types) chain rather
// than the later one clobbering the earlier - mirroring the composability
// RegisterOperation already has, for the "advanced" hand-rolled-coder path.
func TestRegisterTypeCodersCompose(t *testing.T) {
	const centsCode TypeCode = firstDynamicTypeCode + 501
	const dollarsCode TypeCode = firstDynamicTypeCode + 502
	centsCoder := func(a any) TypeCode {
		if _, ok := a.(cents); ok {
			return centsCode
		}
		return UnSupportedType
	}
	dollarsCoder := func(a any) TypeCode {
		if _, ok := a.(dollars); ok {
			return dollarsCode
		}
		return UnSupportedType
	}
	mc, err := UnconfiguredVM(RegisterTypeCoder(centsCoder, uuid.NewV4()), RegisterTypeCoder(dollarsCoder, uuid.NewV4()))
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if got := mc.typeCode(cents{}); got != centsCode {
		t.Errorf("typeCode(cents{}) = %v, want %v", got, centsCode)
	}
	if got := mc.typeCode(dollars{}); got != dollarsCode {
		t.Errorf("typeCode(dollars{}) = %v, want %v", got, dollarsCode)
	}
	// A type neither coder recognizes still falls through cleanly.
	if got := mc.typeCode(struct{}{}); got != UnSupportedType {
		t.Errorf("typeCode(struct{}{}) = %v, want UnSupportedType", got)
	}
}

// TestClearOperationsErrorsCleanly is the regression test for the
// pre-existing nil-handler panic in operationsRouter's same-type fast
// path: with opHandlers wiped and nothing re-registered, arithmetic must
// return an error, not panic.
func TestClearOperationsErrorsCleanly(t *testing.T) {
	vm, err := UnconfiguredVM(ClearOperations())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	evalDynExpectError(t, vm, nil, int64(1), int64(2), OpAdd)
	evalDynExpectError(t, vm, nil, int64(1), int64(2), OpLess)
}

// TestClearOperationsAllowsDisablingCoercion is the user-facing scenario
// ClearOperations exists for: replace the default int64 handler (which
// coerces with float64/string by default, and with decimal too if
// stdlib.DecimalBuiltins is loaded) with one that only accepts int64, so
// cross-type arithmetic becomes a clean error instead of silently
// coercing.
func TestClearOperationsAllowsDisablingCoercion(t *testing.T) {
	vm, err := UnconfiguredVM(
		ClearOperations(),
		RegisterOperation(int64(0), nil, func(a, b any, aTC, bTC TypeCode, op OpCode) (any, error) {
			return a.(int64) + b.(int64), nil
		}),
	)
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if got := evalDyn(t, vm, nil, int64(1), int64(2), OpAdd); got != int64(3) {
		t.Errorf("1 + 2 = %v, want 3", got)
	}
	evalDynExpectError(t, vm, nil, int64(1), float64(1.5), OpAdd)
}

// TestClearOperationsPreservesTypeCoders confirms ClearOperations only
// resets operation handlers and the dynamicTypes registry, not custom
// coders installed via RegisterTypeCoder - type recognition and operation
// handling are documented as orthogonal concerns.
func TestClearOperationsPreservesTypeCoders(t *testing.T) {
	const centsCode TypeCode = firstDynamicTypeCode + 503
	coder := func(a any) TypeCode {
		if _, ok := a.(cents); ok {
			return centsCode
		}
		return UnSupportedType
	}
	mc, err := UnconfiguredVM(RegisterTypeCoder(coder, uuid.NewV4()), ClearOperations())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if got := mc.typeCode(cents{}); got != centsCode {
		t.Errorf("typeCode(cents{}) after ClearOperations() = %v, want %v (coder should survive)", got, centsCode)
	}
}

// TestRegisterOperationRejectsNilHandler is the regression test for a
// second nil-Handler hole distinct from the same-type-branch one
// ClearOperationsErrorsCleanly covers: registering a real (non-zero-value)
// operationHandlerInfo with a nil Handler but a coercible type that
// canCoerce would accept lets operationsRouter's coercion branches call
// through a nil func. Fixed by rejecting a nil handler at registration
// time instead, so a zero-value (never-registered) entry is the only way
// .Handler can ever be nil - and that path is provably unreachable from
// the coercion branches, since canCoerce is always false for a
// zero-value's nil coercionTypes slice.
func TestRegisterOperationRejectsNilHandler(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if err := mc.registerOperationalHandler(cents{}, []any{int64(0)}, nil); err == nil {
		t.Fatalf("expected registerOperationalHandler to reject a nil handler")
	}
}

// TestOverrideOperationWithoutClearing confirms the existing "last
// registered handler for a TypeCode wins" scan behavior in
// operationsRouter still holds post-refactor: registering a new int64
// handler without calling ClearOperations() first replaces the default's
// behavior for same-type dispatch, since it's found later in the scan.
func TestOverrideOperationWithoutClearing(t *testing.T) {
	vm, err := UnconfiguredVM(
		RegisterOperation(int64(0), nil, func(a, b any, aTC, bTC TypeCode, op OpCode) (any, error) {
			return int64(999), nil
		}),
	)
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if got := evalDyn(t, vm, nil, int64(1), int64(2), OpAdd); got != int64(999) {
		t.Errorf("1 + 2 = %v, want 999 (overriding handler should win)", got)
	}
}
