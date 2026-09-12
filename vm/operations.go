package vm

import (
	"errors"

	"math"
	"strconv"
)

func registerDefaultOperations(mc *Machine) {
	mc.registerOperationalHandler(string(""), []any{int(1), int64(1), float64(1.0)}, stringOperations)
	mc.registerOperationalHandler(int(1), []any{int64(1)}, intOperations[int])
	mc.registerOperationalHandler(int64(1), []any{int(1)}, intOperations[int64])
	mc.registerOperationalHandler(float64(1), []any{int(1), int64(1)}, floatOperations)
	mc.registerOperationalHandler(bool(true), nil, boolOperations)
}

func boolOperations(a, b any, aTypeCode, bTypeCode TypeCode, op OpCode) (any, error) {
	aVal, bVal := a.(bool), b.(bool)
	switch op {
	case OpEqual:
		return aVal == bVal, nil
	case OpNotEqual:
		return aVal != bVal, nil
	default:
		return nil, errors.ErrUnsupported
	}
}

func stringOperations(a, b any, aTypeCode, bTypeCode TypeCode, op OpCode) (any, error) {
	if aTypeCode != StringTypeCode {
		return nil, errors.ErrUnsupported
	}
	aVal := a.(string)
	var bVal string
	switch bTypeCode {
	case IntTypeCode:
		bVal = strconv.Itoa(b.(int))
	case Int64TypeCode:
		bVal = strconv.Itoa(int(b.(int64)))
	case FloatTypeCode:
		bVal = strconv.FormatFloat(b.(float64), 'f', -1, 64)
	case StringTypeCode:
		bVal = b.(string)
	}

	switch op {
	case OpAdd:
		return aVal + bVal, nil
	case OpEqual:
		return aVal == bVal, nil
	case OpNotEqual:
		return aVal != bVal, nil
	case OpLess:
		return aVal < bVal, nil
	case OpGreater:
		return aVal > bVal, nil
	case OpLessEq:
		return aVal <= bVal, nil
	case OpGreaterEq:
		return aVal >= bVal, nil
	case OpCoerce:
		// Special case - we need to convert value A (always a string) to type of B
		switch bTypeCode {
		case IntTypeCode:
			value, err := strconv.ParseInt(aVal, 10, 64)
			if err != nil {
				return int(0), err
			}
			return int(value), nil
		case Int64TypeCode:
			value, err := strconv.ParseInt(aVal, 10, 64)
			if err != nil {
				return int64(0), err
			}
			return value, nil
		case FloatTypeCode:
			value, err := strconv.ParseFloat(aVal, 64)
			if err != nil {
				return float64(0), err
			}
			return value, nil
		}
	}
	return nil, errors.ErrUnsupported
}

func intOperations[T int | int64](a, b any, aTypeCode, bTypeCode TypeCode, op OpCode) (any, error) {
	var aVal, bVal T

	// Mixed int and int64 are supported in either direction
	switch aTypeCode {
	case IntTypeCode:
		aVal = T(a.(int))
	case Int64TypeCode:
		aVal = T(a.(int64))
	}

	// Mixed int and int64 are supported in either direction
	switch bTypeCode {
	case IntTypeCode:
		bVal = T(b.(int))
	case Int64TypeCode:
		bVal = T(b.(int64))
	}

	switch op {
	case OpAdd:
		return aVal + bVal, nil
	case OpSub:
		return aVal - bVal, nil
	case OpDiv:
		if bVal == 0 {
			return nil, errors.New("Divide by zero")
		}
		return aVal / bVal, nil
	case OpMod:
		if bVal == 0 {
			return nil, errors.New("Modulus by zero")
		}
		return aVal % bVal, nil
	case OpPow:
		return intPow(aVal, bVal)
	case OpMul:
		return aVal * bVal, nil
	case OpEqual:
		return aVal == bVal, nil
	case OpNotEqual:
		return aVal != bVal, nil
	case OpLess:
		return aVal < bVal, nil
	case OpGreater:
		return aVal > bVal, nil
	case OpLessEq:
		return aVal <= bVal, nil
	case OpGreaterEq:
		return aVal >= bVal, nil
	case OpCoerce:
		// Special case - we need to convert value A to type of B

		// We are converting from T (int or int64) to something else.
		if aTypeCode == IntTypeCode || aTypeCode == Int64TypeCode {
			switch bTypeCode {
			case IntTypeCode:
				return int(aVal), nil
			case Int64TypeCode:
				return int64(aVal), nil
			}
		}
		// We are converting from something else to T (int or int64)
		return aVal, nil
	case OpNeg:
		return -aVal, nil
	case OpAbs:
		if aVal < 0 {
			return -aVal, nil
		}
		return aVal, nil
	case OpCeil, OpFloor, OpRound:
		// An int/int64 is already its own ceiling/floor/nearest value.
		return aVal, nil
	}
	return nil, errors.ErrUnsupported
}

// intPow computes base**exp by squaring, staying in integer arithmetic
// rather than round-tripping through float64 (which would lose precision
// for large int64 results). Negative exponents aren't representable as an
// integer result, so they're an error rather than silently truncating to 0
// the way integer division would.
func intPow[T int | int64](base, exp T) (T, error) {
	if exp < 0 {
		return 0, errors.New("negative exponent not supported for integer power")
	}
	result := T(1)
	for exp > 0 {
		if exp&1 == 1 {
			result *= base
		}
		base *= base
		exp >>= 1
	}
	return result, nil
}

func floatOperations(a, b any, aTypeCode, bTypeCode TypeCode, op OpCode) (any, error) {
	var aVal, bVal float64
	switch aTypeCode {
	case IntTypeCode:
		aVal = float64(a.(int))
	case Int64TypeCode:
		aVal = float64(a.(int64))
	case FloatTypeCode:
		aVal = a.(float64)
	}
	switch bTypeCode {
	case IntTypeCode:
		bVal = float64(b.(int))
	case Int64TypeCode:
		bVal = float64(b.(int64))
	case FloatTypeCode:
		bVal = b.(float64)
	}

	switch op {
	case OpAdd:
		return aVal + bVal, nil
	case OpSub:
		return aVal - bVal, nil
	case OpDiv:
		if bVal == 0.0 {
			return nil, errors.New("Divide by zero")
		}
		return aVal / bVal, nil
	case OpMod:
		if bVal == 0.0 {
			return nil, errors.New("Modulus by zero")
		}
		return math.Mod(aVal, bVal), nil
	case OpPow:
		return math.Pow(aVal, bVal), nil
	case OpMul:
		return aVal * bVal, nil
	case OpEqual:
		return aVal == bVal, nil
	case OpNotEqual:
		return aVal != bVal, nil
	case OpLess:
		return aVal < bVal, nil
	case OpGreater:
		return aVal > bVal, nil
	case OpLessEq:
		return aVal <= bVal, nil
	case OpGreaterEq:
		return aVal >= bVal, nil
	case OpCoerce:
		// Special case - we need to convert value A to type of B

		// We are converting from float64 to something else.
		if aTypeCode == FloatTypeCode {
			switch bTypeCode {
			case IntTypeCode:
				return int(aVal), nil
			case Int64TypeCode:
				return int64(aVal), nil
			}
		}
		// We are converting from something else to float64
		return aVal, nil
	case OpNeg:
		return -aVal, nil
	case OpAbs:
		return math.Abs(aVal), nil
	case OpCeil:
		return math.Ceil(aVal), nil
	case OpFloor:
		return math.Floor(aVal), nil
	case OpRound:
		return math.Round(aVal), nil
	}
	return nil, errors.ErrUnsupported
}
