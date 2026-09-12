package owlexpr

import (
	"testing"

	"github.com/cms103/owlexpr/vm"
)

// evalRootOpts is evalRoot's counterpart for tests that need a VMOption
// (e.g. vm.DisableAutoTypeCoercion()) rather than the plain default VM.
func evalRootOpts(t *testing.T, env map[string]any, input string, opts ...vm.VMOption) any {
	t.Helper()
	instructions, err := Compile(input)
	if err != nil {
		t.Fatalf("compile error for %q: %v", input, err)
	}
	mc, err := NewVM(opts...)
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	return res
}

func evalRootOptsExpectError(t *testing.T, env map[string]any, input string, opts ...vm.VMOption) error {
	t.Helper()
	instructions, err := Compile(input)
	if err != nil {
		t.Fatalf("compile error for %q: %v", input, err)
	}
	mc, err := NewVM(opts...)
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	_, err = mc.Run(instructions, env)
	if err == nil {
		t.Fatalf("expected an error for %q, got none", input)
	}
	return err
}

// TestIntInt64Float64Conversions covers the always-on int()/int64()/
// float64() builtins (getConvertFunc) end-to-end: truncation toward zero
// when narrowing a float, widening an int, and the identity case where the
// argument is already the target type.
func TestIntInt64Float64Conversions(t *testing.T) {
	cases := []struct {
		expr string
		want any
	}{
		{"int(3.9)", int(3)},
		{"int64(3.9)", int64(3)},
		{"int(-3.9)", int(-3)},
		{"float64(3)", float64(3)},
		{"float64(int(3))", float64(3)},
		{"int(int64(5))", int(5)},
		{"int64(int(5))", int64(5)},
		{"int(3)", int(3)},
		{"int64(3)", int64(3)},
		{"float64(3.5)", float64(3.5)},
		{`int("5")`, int(5)},
		{`int64("42")`, int64(42)},
		{`float64("3.14")`, float64(3.14)},
	}
	for _, c := range cases {
		t.Run(c.expr, func(t *testing.T) {
			got := evalRoot(t, nil, c.expr)
			if got != c.want {
				t.Errorf("%s = %v (%T), want %v (%T)", c.expr, got, got, c.want, c.want)
			}
		})
	}
}

// TestIntInt64Float64ArgumentCount covers getConvertFunc's own arity
// guard, shared by all three conversion builtins.
func TestIntInt64Float64ArgumentCount(t *testing.T) {
	cases := []string{
		"int()", "int(1, 2)",
		"int64()", "int64(1, 2)",
		"float64()", "float64(1, 2)",
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			evalRootExpectError(t, nil, input)
		})
	}
}

// TestIntInt64Float64UnsupportedConversionErrors covers CoercValue's
// error path (via getConvertFunc) for a type with no registered
// conversion into a number - a bool has no coercible types at all.
func TestIntInt64Float64UnsupportedConversionErrors(t *testing.T) {
	evalRootExpectError(t, nil, "int(true)")
	evalRootExpectError(t, nil, "int64(true)")
	evalRootExpectError(t, nil, "float64(true)")
}

// TestIntInt64Float64FromNonNumericStringErrors covers the string-parsing
// branch of the conversion builtins propagating a real strconv error for
// a non-numeric string, rather than the "unsupported operation" error that
// existed before string gained its own OpCoerce case.
func TestIntInt64Float64FromNonNumericStringErrors(t *testing.T) {
	evalRootExpectError(t, nil, `int("not a number")`)
	evalRootExpectError(t, nil, `int64("not a number")`)
	evalRootExpectError(t, nil, `float64("not a number")`)
}

// TestDisableAutoTypeCoercionEndToEnd covers vm.DisableAutoTypeCoercion()
// wired all the way through Parse/Compile/Run: with it set, an implicit
// mixed-type operator must fail, same-type operators must keep working,
// and the explicit int()/int64()/float64() conversion builtins - which go
// through CoercValue, not Combine - must still bridge the gap.
func TestDisableAutoTypeCoercionEndToEnd(t *testing.T) {
	opt := vm.DisableAutoTypeCoercion()

	if got := evalRootOpts(t, nil, "3 + 4", opt); got != int64(7) {
		t.Errorf("3 + 4 = %v, want 7 (same-type ops must still work)", got)
	}

	err := evalRootOptsExpectError(t, nil, "3 + 4.2", opt)
	t.Logf("3 + 4.2 with coercion disabled: %v", err)

	if got := evalRootOpts(t, nil, "float64(3) + 4.2", opt); got != 7.2 {
		t.Errorf("float64(3) + 4.2 = %v, want 7.2", got)
	}
	if got := evalRootOpts(t, nil, "3 + int64(4.2)", opt); got != int64(7) {
		t.Errorf("3 + int64(4.2) = %v, want 7", got)
	}

	evalRootOptsExpectError(t, nil, "3 > 2.5", opt)
	if got := evalRootOpts(t, nil, "float64(3) > 2.5", opt); got != true {
		t.Errorf("float64(3) > 2.5 = %v, want true", got)
	}
}

// TestAutoTypeCoercionEnabledByDefault confirms the option is opt-in: a
// plain NewVM() with no options still coerces mixed-type operands
// implicitly, exactly as before DisableAutoTypeCoercion existed.
func TestAutoTypeCoercionEnabledByDefault(t *testing.T) {
	if got := evalRoot(t, nil, "3 + 4.2"); got != 7.2 {
		t.Errorf("3 + 4.2 = %v, want 7.2", got)
	}
}
