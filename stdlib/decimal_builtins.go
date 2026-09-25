package stdlib

import (
	"errors"
	"fmt"

	"github.com/cms103/owlexpr/vm"

	"github.com/shopspring/decimal"
)

// DecimalBuiltins registers decimal.Decimal support: arithmetic/comparison
// operators coercing with int/int64/float64/string, unary `-`/abs()/
// ceil()/floor()/round() (all via the one RegisterOperation call below -
// see decimalOperations' OpNeg/OpAbs/OpCeil/OpFloor/OpRound cases and
// RegisterOperation's own doc comment for why a single handler covers
// both the binary and unary ops), and the TypeCode-driven fast
// slice-iteration path used by map/filter/reduce over a []decimal.Decimal.
// Its one function, decimal(), is a top-level builtin rather than a
// `decimal.*` namespace member: it's written so often in money-handling
// expressions that `decimal.decimal(x)` was a real burden. It keeps the
// full name rather than something shorter like dec() because `decimal`
// was already the pack's reserved name, so it's far less likely to be
// shadowed by (or to shadow) an env value or let binding.
//
// Because decimal.Decimal is registered here rather than being a core VM
// type, the core string handler's own coercion list has no reason to
// include it - the VM has no dependency on this type existing at all. A
// mixed string/decimal expression therefore always resolves through
// decimalOperations regardless of which operand is written first: "5" +
// amount and amount + "5" both parse "5" into a decimal.Decimal and add.
// This matches how every other numeric type here works - the
// numeric-typed operand always wins over implicit stringification. str() covers
// converting Decimal (or other types) to string for concatination.
func DecimalBuiltins() vm.VMOption {
	return func(mc *vm.Machine) error {
		reg := vm.RegisterTypeCoder(stdLibraryTypeCoder, stdLibraryTypeCoderId)
		err := reg(mc)
		if err != nil {
			return err
		}

		if err := vm.RegisterOperation(decimal.Decimal{}, []any{int(0), int64(0), float64(0), string("")}, decimalOperations)(mc); err != nil {
			return err
		}
		if err := vm.RegisterFastSliceIterator(decimal.Decimal{}, decimalFastSliceIterate)(mc); err != nil {
			return err
		}

		// Now register our decimal() function
		mc.RegisterBuiltin("decimal", decimalFunc)
		return nil
	}
}

// decimalFunc converts its argument to a decimal.Decimal.
func decimalFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("decimal() expects 1 argument, got %d", len(args))
	}
	// Attempt to convert the argument to a decimal
	return mc.CoercValue(args[0], decimal.Zero)
}

// decimalOperations is unchanged from when this lived in the core VM
// (vm/operations.go) - only its registration moved, not its logic.
func decimalOperations(a, b any, aTypeCode, bTypeCode vm.TypeCode, op vm.OpCode) (any, error) {
	var aVal, bVal decimal.Decimal

	// We always need to force a to a Decimal
	switch aTypeCode {
	case decimalTypeCode:
		aVal = a.(decimal.Decimal)
	case vm.IntTypeCode:
		aVal = decimal.NewFromInt(int64(a.(int)))
	case vm.Int64TypeCode:
		aVal = decimal.NewFromInt(a.(int64))
	case vm.FloatTypeCode:
		aVal = decimal.NewFromFloat(a.(float64))
	case vm.StringTypeCode:
		var err error
		aVal, err = decimal.NewFromString(a.(string))
		if err != nil {
			return nil, err
		}
	}

	// We always need to force b to a Decimal - except for OpCoerce, whose
	// switch below only ever inspects bTypeCode (never bVal itself): b
	// there is just a same-type example value (see Machine.CoercValue's
	// doc comment), not something that need parse as one, and forcing it
	// anyway made every decimal-to-string coercion fail whenever the
	// caller passed the natural "" example value, since "" doesn't parse
	// via decimal.NewFromString.
	if op != vm.OpCoerce {
		switch bTypeCode {
		case decimalTypeCode:
			bVal = b.(decimal.Decimal)
		case vm.IntTypeCode:
			bVal = decimal.NewFromInt(int64(b.(int)))
		case vm.Int64TypeCode:
			bVal = decimal.NewFromInt(b.(int64))
		case vm.FloatTypeCode:
			bVal = decimal.NewFromFloat(b.(float64))
		case vm.StringTypeCode:
			var err error
			bVal, err = decimal.NewFromString(b.(string))
			if err != nil {
				return nil, err
			}
		}
	}

	switch op {
	case vm.OpAdd:
		return aVal.Add(bVal), nil
	case vm.OpSub:
		return aVal.Sub(bVal), nil
	case vm.OpDiv:
		if bVal.Equal(decimal.Zero) {
			return nil, errors.New("Divide by zero")
		}
		return aVal.Div(bVal), nil
	case vm.OpMod:
		if bVal.Equal(decimal.Zero) {
			return nil, errors.New("Modulus by zero")
		}
		return aVal.Mod(bVal), nil
	case vm.OpPow:
		return aVal.Pow(bVal), nil
	case vm.OpMul:
		return aVal.Mul(bVal), nil
	case vm.OpEqual:
		return aVal.Equal(bVal), nil
	case vm.OpNotEqual:
		return !aVal.Equal(bVal), nil
	case vm.OpLess:
		return aVal.LessThan(bVal), nil
	case vm.OpGreater:
		return aVal.GreaterThan(bVal), nil
	case vm.OpLessEq:
		return aVal.LessThanOrEqual(bVal), nil
	case vm.OpGreaterEq:
		return aVal.GreaterThanOrEqual(bVal), nil
	case vm.OpCoerce:
		// Special case - we need to convert value A to type of B

		// We are converting from Decimal to something else.
		if aTypeCode == decimalTypeCode {
			switch bTypeCode {
			case vm.IntTypeCode:
				return int(aVal.IntPart()), nil
			case vm.Int64TypeCode:
				return aVal.IntPart(), nil
			case vm.FloatTypeCode:
				// We aren't bothered about knowing if it was exact or not
				fVal, _ := aVal.Float64()
				return fVal, nil
			case vm.StringTypeCode:
				// We aren't bothered about knowing if it was exact or not
				return aVal.String(), nil
			}
		}
		// We are converting from something else to decimal
		return aVal, nil
	case vm.OpNeg:
		return aVal.Neg(), nil
	case vm.OpAbs:
		return aVal.Abs(), nil
	case vm.OpCeil:
		return aVal.Ceil(), nil
	case vm.OpFloor:
		return aVal.Floor(), nil
	case vm.OpRound:
		// Round to the nearest integer (0 decimal places) - decimal's own
		// Round is half-away-from-zero, the same convention math.Round
		// already uses for float64 elsewhere in this codebase.
		return aVal.Round(0), nil
	}
	return nil, errors.ErrUnsupported
}

// decimalFastSliceIterate plugs []decimal.Decimal into
// vm.ForEachListElement's reflect-once-per-call fast path (see
// vm.FastSliceIterator's doc comment), registered via
// RegisterFastSliceIterator rather than hardcoded into the vm package's own
// fastSliceElementIterate - the vm package has no dependency on
// decimal.Decimal existing at all.
func decimalFastSliceIterate(slice any, yield func(any) bool) bool {
	arr, matched := slice.([]decimal.Decimal)
	if !matched {
		return false
	}
	for _, v := range arr {
		if !yield(v) {
			break
		}
	}
	return true
}
