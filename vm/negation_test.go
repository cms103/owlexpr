package vm

import (
	"errors"
	"testing"
)

// centsWithNeg is a registered type that supports OpNeg via
// RegisterOperation, distinct from int/int64/float64's hardcoded fast
// path in negateValue.
type centsWithNeg struct{ n int64 }

// centsWithNegOperations is centsWithNeg's RegisterOperation handler. It
// only implements OpNeg (returning errors.ErrUnsupported for anything
// else) - deliberately narrow, so TestNegateValueRegisteredType can't
// accidentally pass by falling through to some other op's behavior.
func centsWithNegOperations(a, b any, aTC, bTC TypeCode, op OpCode) (any, error) {
	if op == OpNeg {
		return centsWithNeg{n: -a.(centsWithNeg).n}, nil
	}
	return nil, errors.ErrUnsupported
}

func TestNegateValueHardcodedTypes(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"i": int(5), "i64": int64(5), "f": 2.5}
	cases := []struct {
		v    string
		want any
	}{
		{"i", -5},
		{"i64", int64(-5)},
		{"f", -2.5},
	}
	for _, tt := range cases {
		instructions := []Instruction{{Op: OpLoad, Arg: tt.v}, {Op: OpNeg}}
		got, err := mc.Run(instructions, env)
		if err != nil {
			t.Fatalf("run error negating %s: %v", tt.v, err)
		}
		if got != tt.want {
			t.Errorf("-%s = %v, want %v", tt.v, got, tt.want)
		}
	}
}

// TestNegateValueRegisteredType covers negateValue's mc.Combine(a, a,
// OpNeg) fallback, reaching a handler registered via RegisterOperation -
// not reachable via the hardcoded int/int64/float64 fast path above.
func TestNegateValueRegisteredType(t *testing.T) {
	mc, err := UnconfiguredVM(RegisterOperation(centsWithNeg{}, nil, centsWithNegOperations))
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	env := map[string]any{"c": centsWithNeg{n: 150}}
	instructions := []Instruction{{Op: OpLoad, Arg: "c"}, {Op: OpNeg}}
	got, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	if got.(centsWithNeg).n != -150 {
		t.Errorf("-c = %v, want centsWithNeg{-150}", got)
	}
}

// TestNegateValueUnsupportedTypeErrors covers negateValue's final error
// for a type with no hardcoded case and no registered OpNeg handler.
func TestNegateValueUnsupportedTypeErrors(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"s": "hello"}
	instructions := []Instruction{{Op: OpLoad, Arg: "s"}, {Op: OpNeg}}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Error("expected an error negating a string")
	}
}

// TestNegateValueRegisteredTypeWithoutOpNegErrors covers a type that's
// registered (so it has a TypeCode and a handler) but whose handler
// doesn't implement OpNeg specifically - centsWithNegOperations only
// returns errors.ErrUnsupported for anything else, so this is really
// testing that negateValue's Combine fallback surfaces that as a clean
// error rather than mistaking "registered" for "supports this op".
func TestNegateValueRegisteredTypeWithoutOpNegErrors(t *testing.T) {
	mc, err := UnconfiguredVM(RegisterOperation(centsWithNeg{}, nil, centsWithNegOperations))
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Combine(centsWithNeg{n: 1}, centsWithNeg{n: 1}, OpMul); err == nil {
		t.Error("expected an error for an op centsWithNegOperations doesn't implement")
	}
}

// TestOpNotRequiresBool covers OpNot's own type guard in run() (`!x`
// requires a bool operand) - distinct from negateValue, which only
// handles unary `-`.
func TestOpNotRequiresBool(t *testing.T) {
	mc := mustVM(t)
	env := map[string]any{"n": int64(5)}
	instructions := []Instruction{{Op: OpLoad, Arg: "n"}, {Op: OpNot}}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Error("expected an error applying '!' to a non-bool")
	}
}
