package stdlib

import (
	"github.com/cms103/owlexpr/vm"
	"time"
	"uuid"

	"github.com/shopspring/decimal"
)

var stdLibraryTypeCoderId = uuid.MustParse("298e058c-9334-44aa-893a-b12aa2274918")

const timeTypeCode vm.TypeCode = 900
const durationTypeCode vm.TypeCode = 901
const monthTypeCode vm.TypeCode = 902
const weekdayTypeCode vm.TypeCode = 903
const decimalTypeCode vm.TypeCode = 904
const byteTypeCode vm.TypeCode = 905
const byteSliceTypeCode vm.TypeCode = 906

// The default TypeCoder used by owlexpr
func stdLibraryTypeCoder(a any) vm.TypeCode {
	switch aT := a.(type) {
	case time.Time:
		return timeTypeCode
	case time.Duration:
		return durationTypeCode
	case time.Month:
		return monthTypeCode
	case time.Weekday:
		_ = aT
		return weekdayTypeCode
	case decimal.Decimal:
		return decimalTypeCode
	case byte:
		return byteTypeCode
	case []byte:
		return byteSliceTypeCode

	}

	return vm.UnSupportedType
}

func All() vm.VMOption {
	return func(mc *vm.Machine) error {
		err := TimeBuiltins()(mc)
		if err != nil {
			return err
		}
		err = StringBuiltins()(mc)
		if err != nil {
			return err
		}
		err = RegexBuiltins()(mc)
		if err != nil {
			return err
		}
		err = ListBuiltins()(mc)
		if err != nil {
			return err
		}
		err = IterBuiltins()(mc)
		if err != nil {
			return err
		}
		err = DecimalBuiltins()(mc)
		if err != nil {
			return err
		}
		err = ByteBuiltins()(mc)
		if err != nil {
			return err
		}
		err = BitsBuiltins()(mc)
		if err != nil {
			return err
		}
		err = JSONBuiltins()(mc)
		return err
	}
}
