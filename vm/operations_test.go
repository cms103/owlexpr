package vm

import "testing"

// TestBoolUnsupportedOperationErrors covers boolOperations' default branch:
// only == and != are supported for bool, so an arithmetic op must error
// rather than panic or silently coerce.
func TestBoolUnsupportedOperationErrors(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	evalDynExpectError(t, mc, nil, true, false, OpAdd)
}

func TestBoolEquality(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if got := evalDyn(t, mc, nil, true, true, OpEqual); got != true {
		t.Errorf("true == true = %v, want true", got)
	}
	if got := evalDyn(t, mc, nil, true, false, OpNotEqual); got != true {
		t.Errorf("true != false = %v, want true", got)
	}
}

// TestStringComparisonOperators covers stringOperations' <, >, <=, >=
// cases, none of which any existing test reached (only ==/!=/+ were
// exercised elsewhere).
func TestStringComparisonOperators(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	cases := []struct {
		a, b string
		op   OpCode
		want bool
	}{
		{"a", "b", OpLess, true},
		{"b", "a", OpGreater, true},
		{"a", "a", OpLessEq, true},
		{"b", "a", OpGreaterEq, true},
		{"a", "b", OpGreater, false},
	}
	for _, tt := range cases {
		env := map[string]any{"lhs": tt.a, "rhs": tt.b}
		if got := evalDyn(t, mc, env, "lhs", "rhs", tt.op); got != tt.want {
			t.Errorf("%q %v %q = %v, want %v", tt.a, tt.op, tt.b, got, tt.want)
		}
	}
}

// TestStringConcatWithNumericTypes covers stringOperations' bVal
// conversion switch for each numeric TypeCode it coerces from (int,
// int64, float64) - existing tests only ever concatenated string+string.
func TestStringConcatWithNumericTypes(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	env := map[string]any{"s": "n="}
	if got := evalDyn(t, mc, env, "s", int(5), OpAdd); got != "n=5" {
		t.Errorf(`"n=" + int(5) = %v, want "n=5"`, got)
	}
	if got := evalDyn(t, mc, env, "s", int64(6), OpAdd); got != "n=6" {
		t.Errorf(`"n=" + int64(6) = %v, want "n=6"`, got)
	}
	if got := evalDyn(t, mc, env, "s", float64(1.5), OpAdd); got != "n=1.5" {
		t.Errorf(`"n=" + float64(1.5) = %v, want "n=1.5"`, got)
	}
}

// TestStringOperationUnsupportedOp covers stringOperations' final
// unsupported-op fallthrough (e.g. "-" has no meaning for strings).
func TestStringOperationUnsupportedOp(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	env := map[string]any{"a": "a", "b": "b"}
	evalDynExpectError(t, mc, env, "a", "b", OpSub)
}

// TestIntOperationsBothWidths runs the full arithmetic/comparison op set
// against both int and int64 - intOperations is instantiated separately
// for each generic width (see operations.go's two registerOperationalHandler
// calls), and root-package tests only ever compile int64 literals, never
// plain Go `int` values from an environment.
func TestIntOperationsBothWidths(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	t.Run("int", func(t *testing.T) {
		if got := evalDyn(t, mc, nil, int(7), int(3), OpSub); got != int(4) {
			t.Errorf("7 - 3 (int) = %v, want 4", got)
		}
		if got := evalDyn(t, mc, nil, int(6), int(3), OpMul); got != int(18) {
			t.Errorf("6 * 3 (int) = %v, want 18", got)
		}
		evalDynExpectError(t, mc, nil, int(1), int(0), OpDiv)
		evalDynExpectError(t, mc, nil, int(1), int(0), OpMod)
		evalDynExpectError(t, mc, nil, int(2), int(-1), OpPow)
		if got := evalDyn(t, mc, nil, int(2), int(10), OpLessEq); got != true {
			t.Errorf("2 <= 10 (int) = %v, want true", got)
		}
	})
	t.Run("int64", func(t *testing.T) {
		if got := evalDyn(t, mc, nil, int64(7), int64(3), OpSub); got != int64(4) {
			t.Errorf("7 - 3 (int64) = %v, want 4", got)
		}
		evalDynExpectError(t, mc, nil, int64(1), int64(0), OpDiv)
		evalDynExpectError(t, mc, nil, int64(1), int64(0), OpMod)
		evalDynExpectError(t, mc, nil, int64(2), int64(-1), OpPow)
		if got := evalDyn(t, mc, nil, int64(2), int64(10), OpGreaterEq); got != false {
			t.Errorf("2 >= 10 (int64) = %v, want false", got)
		}
	})
}

// TestIntMixedWidthCoercion covers the int/int64 mixed-type coercion path
// registered in registerDefaultOperations (each width accepts the other as
// a coercible type).
func TestIntMixedWidthCoercion(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if got := evalDyn(t, mc, nil, int(2), int64(3), OpAdd); got != int(5) {
		t.Errorf("int(2) + int64(3) = %v, want 5", got)
	}
}

// TestFloatOperations covers floatOperations' arithmetic/comparison ops
// beyond the ** and % root-package tests already exercise - Sub, Mul,
// Div (including divide-by-zero), and every comparison operator.
func TestFloatOperations(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if got := evalDyn(t, mc, nil, 5.5, 2.0, OpSub); got != 3.5 {
		t.Errorf("5.5 - 2.0 = %v, want 3.5", got)
	}
	if got := evalDyn(t, mc, nil, 2.5, 4.0, OpMul); got != 10.0 {
		t.Errorf("2.5 * 4.0 = %v, want 10.0", got)
	}
	if got := evalDyn(t, mc, nil, 5.0, 2.0, OpDiv); got != 2.5 {
		t.Errorf("5.0 / 2.0 = %v, want 2.5", got)
	}
	evalDynExpectError(t, mc, nil, 1.0, 0.0, OpDiv)
	evalDynExpectError(t, mc, nil, 1.0, 0.0, OpMod)
	if got := evalDyn(t, mc, nil, 1.0, 2.0, OpLess); got != true {
		t.Errorf("1.0 < 2.0 = %v, want true", got)
	}
	if got := evalDyn(t, mc, nil, 2.0, 1.0, OpGreater); got != true {
		t.Errorf("2.0 > 1.0 = %v, want true", got)
	}
	if got := evalDyn(t, mc, nil, 1.0, 1.0, OpLessEq); got != true {
		t.Errorf("1.0 <= 1.0 = %v, want true", got)
	}
	if got := evalDyn(t, mc, nil, 2.0, 1.0, OpGreaterEq); got != true {
		t.Errorf("2.0 >= 1.0 = %v, want true", got)
	}
	if got := evalDyn(t, mc, nil, 1.0, 1.0, OpEqual); got != true {
		t.Errorf("1.0 == 1.0 = %v, want true", got)
	}
	if got := evalDyn(t, mc, nil, 1.0, 2.0, OpNotEqual); got != true {
		t.Errorf("1.0 != 2.0 = %v, want true", got)
	}
}

// TestFloatIntCoercion covers floatOperations' int/int64 coercion
// branches (a float combined with a plain int or int64 operand).
func TestFloatIntCoercion(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if got := evalDyn(t, mc, nil, 1.5, int(2), OpAdd); got != 3.5 {
		t.Errorf("1.5 + int(2) = %v, want 3.5", got)
	}
	if got := evalDyn(t, mc, nil, 1.5, int64(2), OpAdd); got != 3.5 {
		t.Errorf("1.5 + int64(2) = %v, want 3.5", got)
	}
}

// TestOpCodeString covers OpCode.String() end-to-end for every named
// opcode plus the OpUnknown fallback for an unrecognized byte value - this
// Stringer had no test at all despite existing purely to make error
// messages (e.g. operationsRouter's "Type operation %v ... not supported")
// readable.
func TestOpCodeString(t *testing.T) {
	named := map[OpCode]string{
		OpPush:        "OpPush",
		OpLoad:        "OpLoad",
		OpAccess:      "OpAccess",
		OpAdd:         "OpAdd",
		OpSub:         "OpSub",
		OpMul:         "OpMul",
		OpDiv:         "OpDiv",
		OpMod:         "OpMod",
		OpPow:         "OpPow",
		OpCoerce:      "OpCoerce",
		OpCall:        "OpCall",
		OpEqual:       "OpEqual",
		OpNotEqual:    "OpNotEqual",
		OpLess:        "OpLess",
		OpGreater:     "OpGreater",
		OpLessEq:      "OpLessEq",
		OpGreaterEq:   "OpGreaterEq",
		OpIn:          "OpIn",
		OpJumpIfFalse: "OpJumpIfFalse",
		OpJump:        "OpJump",
		OpNeg:         "OpNeg",
		OpNot:         "OpNot",
		OpMakeList:    "OpMakeList",
		OpMakeMap:     "OpMakeMap",
		OpIndex:       "OpIndex",
		OpSlice:       "OpSlice",
		OpMakeClosure: "OpMakeClosure",
		OpCoalesce:    "OpCoalesce",
		OpLet:         "OpLet",
	}
	for op, want := range named {
		if got := op.String(); got != want {
			t.Errorf("OpCode(%d).String() = %q, want %q", op, got, want)
		}
	}

	if got := OpCode(255).String(); got != "OpUnknown" {
		t.Errorf("OpCode(255).String() = %q, want %q", got, "OpUnknown")
	}
}
