package stdlib

import (
	"testing"

	"github.com/cms103/owlexpr"

	"github.com/shopspring/decimal"
)

// evalDecimal parses, compiles, and runs input with DecimalBuiltins()
// enabled, failing the test on any parse/compile/run error.
func evalDecimal(t *testing.T, env map[string]any, input string) any {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(DecimalBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	return res
}

func evalDecimalExpectError(t *testing.T, env map[string]any, input string) {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(DecimalBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatalf("expected an error for %q, got none", input)
	}
}

// TestDecimalBuiltinsNotRegisteredByDefault confirms decimal.Decimal env
// values are opaque to a plain owlexpr.NewVM() - the whole point of
// pulling decimal out of the core VM and into this opt-in pack.
func TestDecimalBuiltinsNotRegisteredByDefault(t *testing.T) {
	env := map[string]any{"amount": decimal.NewFromFloat(10)}
	instructions, err := owlexpr.Compile(`amount + 1`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatalf("expected amount + 1 to be undefined without DecimalBuiltins()")
	}
}

func TestDecimalArithmeticCoercesWithIntInt64FloatString(t *testing.T) {
	env := map[string]any{
		"amount": decimal.NewFromFloat(10.5),
		"i":      int(2),
		"i64":    int64(3),
		"f":      1.5,
		"s":      "0.5",
	}
	cases := []struct {
		expr string
		want string
	}{
		{"amount + i", "12.5"},
		{"amount + i64", "13.5"},
		{"amount + f", "12"},
		{"amount + s", "11"},
		// The other operand order still resolves through decimal
		// arithmetic (see DecimalBuiltins' doc comment on the
		// string-coercion direction change) rather than string
		// concatenation.
		{"s + amount", "11"},
	}
	for _, c := range cases {
		got := evalDecimal(t, env, c.expr)
		d, ok := got.(decimal.Decimal)
		if !ok {
			t.Errorf("%s = %v (%T), want a decimal.Decimal", c.expr, got, got)
			continue
		}
		if d.String() != c.want {
			t.Errorf("%s = %s, want %s", c.expr, d.String(), c.want)
		}
	}
}

func TestDecimalComparisons(t *testing.T) {
	env := map[string]any{"amount": decimal.NewFromFloat(10.5)}
	if got := evalDecimal(t, env, "amount > 10"); got != true {
		t.Errorf("amount > 10 = %v, want true", got)
	}
	if got := evalDecimal(t, env, "amount == 10.5"); got != true {
		t.Errorf("amount == 10.5 = %v, want true", got)
	}
}

// TestDecimalOperationsRemainingOps covers decimalOperations' Mul, Pow,
// NotEqual, LessEq, and GreaterEq cases, plus the decimal-vs-decimal
// operand shape (both sides already decimalTypeCode) - every existing
// test compares/combines a decimal against a plain int/int64/float/string
// literal, never against another decimal.Decimal value.
func TestDecimalOperationsRemainingOps(t *testing.T) {
	env := map[string]any{
		"a": decimal.NewFromFloat(2.5),
		"b": decimal.NewFromFloat(2.5),
		"c": decimal.NewFromFloat(4),
	}
	if got := evalDecimal(t, env, "a * 2"); got.(decimal.Decimal).String() != "5" {
		t.Errorf("a * 2 = %v, want 5", got)
	}
	if got := evalDecimal(t, env, "a ** 2"); got.(decimal.Decimal).String() != "6.25" {
		t.Errorf("a ** 2 = %v, want 6.25", got)
	}
	if got := evalDecimal(t, env, "a != c"); got != true {
		t.Errorf("a != c = %v, want true", got)
	}
	if got := evalDecimal(t, env, "a <= b"); got != true {
		t.Errorf("a <= b = %v, want true", got)
	}
	if got := evalDecimal(t, env, "c >= a"); got != true {
		t.Errorf("c >= a = %v, want true", got)
	}
	if got := evalDecimal(t, env, "a == b"); got != true {
		t.Errorf("a == b (decimal vs decimal) = %v, want true", got)
	}
}

// TestDecimalArithmeticInvalidStringErrors covers decimalOperations' own
// decimal.NewFromString error-propagation branch, for both operand
// positions.
func TestDecimalArithmeticInvalidStringErrors(t *testing.T) {
	env := map[string]any{"amount": decimal.NewFromFloat(10.5), "s": "not a number"}
	evalDecimalExpectError(t, env, "amount + s")
	evalDecimalExpectError(t, env, "s + amount")
}

func TestDecimalDivModByZero(t *testing.T) {
	env := map[string]any{"amount": decimal.NewFromFloat(10), "zero": 0}
	evalDecimalExpectError(t, env, "amount / zero")
	evalDecimalExpectError(t, env, "amount % zero")
}

// TestDecimalDivMod covers decimalOperations' OpDiv/OpMod happy paths -
// only their divide/modulus-by-zero error branches had any coverage
// before this, never a successful division or modulus with a value
// assertion.
func TestDecimalDivMod(t *testing.T) {
	env := map[string]any{"amount": decimal.NewFromFloat(10)}
	if got := evalDecimal(t, env, "amount / 4"); got.(decimal.Decimal).String() != "2.5" {
		t.Errorf("amount / 4 = %v, want 2.5", got)
	}
	if got := evalDecimal(t, env, "amount % 3"); got.(decimal.Decimal).String() != "1" {
		t.Errorf("amount %% 3 = %v, want 1", got)
	}
}

// TestDecimalCoerceToString covers decimalOperations' OpCoerce
// "converting from Decimal to something else" branch when the target is
// a string - unreachable from an ordinary expression since owlexpr's
// core only registers int()/int64()/float64(), not string(), so this
// calls mc.CoercValue directly the way root's getConvertFunc would.
func TestDecimalCoerceToString(t *testing.T) {
	mc, err := owlexpr.NewVM(DecimalBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	got, err := mc.CoercValue(decimal.NewFromFloat(10.5), "")
	if err != nil {
		t.Fatalf("CoercValue: %v", err)
	}
	if got != "10.5" {
		t.Errorf("CoercValue(decimal(10.5), string) = %v (%T), want \"10.5\"", got, got)
	}
}

func TestDecimalUnaryNegation(t *testing.T) {
	env := map[string]any{"amount": decimal.NewFromFloat(10.5)}
	got := evalDecimal(t, env, "-amount")
	d, ok := got.(decimal.Decimal)
	if !ok || d.String() != "-10.5" {
		t.Errorf("-amount = %v (%T), want decimal -10.5", got, got)
	}
}

// TestDecimalAbsCeilFloorRound covers decimalOperations' OpAbs/OpCeil/
// OpFloor/OpRound cases - reached via absFunc/ceilFunc/floorFunc/
// roundFunc's mc.Combine(v, v, vm.OpX) fallback once their own hardcoded
// int/int64/float64 fast path doesn't match a decimal.Decimal argument.
// These builtins are core (always registered), so evalDecimal's
// DecimalBuiltins()-only VM already has them available.
func TestDecimalAbsCeilFloorRound(t *testing.T) {
	env := map[string]any{
		"neg": decimal.NewFromFloat(-10.5),
		"pos": decimal.NewFromFloat(4.4),
	}
	cases := []struct {
		input string
		want  string
	}{
		{"abs(neg)", "10.5"},
		{"abs(pos)", "4.4"},
		{"ceil(neg)", "-10"},
		{"ceil(pos)", "5"},
		{"floor(neg)", "-11"},
		{"floor(pos)", "4"},
		{"round(neg)", "-11"}, // half-away-from-zero, but -10.5 isn't a half case - nearest integer to -10.5 rounds away from zero to -11
		{"round(pos)", "4"},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalDecimal(t, env, tt.input)
			d, ok := got.(decimal.Decimal)
			if !ok || d.String() != tt.want {
				t.Errorf("%s = %v (%T), want decimal %s", tt.input, got, got, tt.want)
			}
		})
	}
}

// TestDecimalSliceIteration exercises map()'s TypeCode-driven fast path
// for []decimal.Decimal (RegisterFastSliceIterator), not just Combine -
// the two are separate registrations DecimalBuiltins has to make.
func TestDecimalSliceIteration(t *testing.T) {
	env := map[string]any{
		"amounts": []decimal.Decimal{
			decimal.NewFromFloat(1.5),
			decimal.NewFromFloat(2.5),
			decimal.NewFromFloat(3),
		},
	}
	got := evalDecimal(t, env, "map(amounts, x => x + 1)")
	arr, ok := got.([]any)
	if !ok || len(arr) != 3 {
		t.Fatalf("map(amounts, x => x + 1) = %v (%T), want a 3-element []any", got, got)
	}
	want := []string{"2.5", "3.5", "4"}
	for i, v := range arr {
		d, ok := v.(decimal.Decimal)
		if !ok || d.String() != want[i] {
			t.Errorf("element %d = %v (%T), want decimal %s", i, v, v, want[i])
		}
	}
}

// TestDecimalDecimalConvertsFromOtherTypes covers the decimal()
// builtin (decimalFunc), which uses CoercValue/OpCoerce to convert an
// int/int64/float64/string into a decimal.Decimal - the "converting from
// something else to decimal" branch of decimalOperations' OpCoerce case.
func TestDecimalDecimalConvertsFromOtherTypes(t *testing.T) {
	env := map[string]any{"i": int(2), "i64": int64(3), "f": 1.5, "s": "12.34"}
	cases := []struct {
		expr string
		want string
	}{
		{"decimal(5)", "5"},
		{"decimal(i)", "2"},
		{"decimal(i64)", "3"},
		{"decimal(f)", "1.5"},
		{"decimal(s)", "12.34"},
	}
	for _, c := range cases {
		got := evalDecimal(t, env, c.expr)
		d, ok := got.(decimal.Decimal)
		if !ok {
			t.Errorf("%s = %v (%T), want a decimal.Decimal", c.expr, got, got)
			continue
		}
		if d.String() != c.want {
			t.Errorf("%s = %s, want %s", c.expr, d.String(), c.want)
		}
	}
}

// TestDecimalDecimalIdentity covers decimal() applied to a value
// that's already a decimal.Decimal - the aType == bType fast path in
// operationDispatcher.
func TestDecimalDecimalIdentity(t *testing.T) {
	env := map[string]any{"amount": decimal.NewFromFloat(10.5)}
	got := evalDecimal(t, env, "decimal(amount)")
	d, ok := got.(decimal.Decimal)
	if !ok || !d.Equal(decimal.NewFromFloat(10.5)) {
		t.Errorf("decimal(amount) = %v (%T), want decimal 10.5", got, got)
	}
}

// TestDecimalDecimalInvalidStringErrors covers decimalFunc's error
// propagation from decimal.NewFromString for a malformed string argument.
func TestDecimalDecimalInvalidStringErrors(t *testing.T) {
	evalDecimalExpectError(t, nil, `decimal("not a number")`)
}

// TestDecimalDecimalNamespaceRemoved: decimal() replaced
// decimal.decimal() - `decimal` is now the function itself, not a
// namespace, so the old spelling is member access on a function.
func TestDecimalDecimalNamespaceRemoved(t *testing.T) {
	evalDecimalExpectError(t, nil, `decimal.decimal(1)`)
}

// TestDecimalDecimalArgumentCount covers decimalFunc's own arity guard.
func TestDecimalDecimalArgumentCount(t *testing.T) {
	evalDecimalExpectError(t, nil, `decimal()`)
	evalDecimalExpectError(t, nil, `decimal(1, 2)`)
}

// TestDecimalToIntInt64Float64 covers the reverse direction - converting a
// decimal.Decimal to int/int64/float64 via the root package's int()/
// int64()/float64() builtins, which go through decimalOperations' OpCoerce
// case (the "converting from Decimal to something else" branch).
func TestDecimalToIntInt64Float64(t *testing.T) {
	env := map[string]any{"amount": decimal.NewFromFloat(10.7)}
	if got := evalDecimal(t, env, "int(amount)"); got != int(10) {
		t.Errorf("int(amount) = %v (%T), want int(10) (IntPart truncates)", got, got)
	}
	if got := evalDecimal(t, env, "int64(amount)"); got != int64(10) {
		t.Errorf("int64(amount) = %v (%T), want int64(10)", got, got)
	}
	if got := evalDecimal(t, env, "float64(amount)"); got != 10.7 {
		t.Errorf("float64(amount) = %v (%T), want 10.7", got, got)
	}
}

// TestDecimalFastSliceIterateDirect is a white-box unit test of
// decimalFastSliceIterate itself (same package), covering its early-exit
// (yield returns false) and type-mismatch (matched=false) branches -
// neither reachable through TestDecimalSliceIteration's map()-based
// end-to-end test, since map() always visits every element and always
// calls it with a genuine []decimal.Decimal.
func TestDecimalFastSliceIterateDirect(t *testing.T) {
	slice := []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(2), decimal.NewFromInt(3)}
	n := 0
	ok := decimalFastSliceIterate(slice, func(v any) bool {
		n++
		return n < 2
	})
	if !ok || n != 2 {
		t.Errorf("decimalFastSliceIterate stopped after %d elements (ok=%v), want 2 (early exit)", n, ok)
	}

	if ok := decimalFastSliceIterate([]int64{1, 2, 3}, func(v any) bool { return true }); ok {
		t.Error("decimalFastSliceIterate should report ok=false for a []int64")
	}
}

// TestDecimalExponentIsUsable covers a Go method returning an int32
// (decimal.Decimal.Exponent): the VM normalises it to int64 at the call
// boundary, while a method returning a registered type (Decimal itself)
// is left alone.
func TestDecimalExponentIsUsable(t *testing.T) {
	env := map[string]any{"amount": decimal.RequireFromString("12.345")}
	if got := evalDecimal(t, env, "amount.Exponent()"); got != int64(-3) {
		t.Errorf("amount.Exponent() = %v (%T), want int64(-3)", got, got)
	}
	if got := evalDecimal(t, env, "-amount.Exponent() + 1"); got != int64(4) {
		t.Errorf("-amount.Exponent() + 1 = %v (%T), want int64(4)", got, got)
	}
	if got, ok := evalDecimal(t, env, "amount.Neg()").(decimal.Decimal); !ok || !got.Equal(decimal.RequireFromString("-12.345")) {
		t.Errorf("amount.Neg() = %v (%T), want decimal.Decimal -12.345", got, got)
	}
}

// TestDecimalQuoRemReturnsList covers a two-result Decimal method: the
// VM returns [quotient, remainder] rather than just the quotient.
func TestDecimalQuoRemReturnsList(t *testing.T) {
	env := map[string]any{"d": decimal.RequireFromString("12.345"), "e": decimal.NewFromInt(2)}
	if got, ok := evalDecimal(t, env, "d.QuoRem(e, 0)[1]").(decimal.Decimal); !ok || !got.Equal(decimal.RequireFromString("0.345")) {
		t.Errorf("d.QuoRem(e, 0)[1] = %v (%T), want 0.345", got, got)
	}
	if got := evalDecimal(t, env, "d.Float64()[1]"); got != false {
		t.Errorf("d.Float64()[1] = %v (%T), want false (12.345 isn't exact as a float64)", got, got)
	}
}
