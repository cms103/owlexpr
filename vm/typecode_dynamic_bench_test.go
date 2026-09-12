package vm

// Benchmarks answering a specific question raised while designing the
// dynamic-TypeCode system: is RegisterTypeCoder's "hand-roll your own
// type-switch instead of relying on the dynamicTypes map" escape hatch
// worth the added API surface and the complexity of explaining it? These
// compare the two paths for an embedder registering 10 new types - a
// number chosen as a realistic "a handful of domain types" case, not a
// worst case.

import (
	"testing"
	"uuid"
)

type dynBenchType0 struct{}
type dynBenchType1 struct{}
type dynBenchType2 struct{}
type dynBenchType3 struct{}
type dynBenchType4 struct{}
type dynBenchType5 struct{}
type dynBenchType6 struct{}
type dynBenchType7 struct{}
type dynBenchType8 struct{}
type dynBenchType9 struct{}

var dynBenchSamples = []any{
	dynBenchType0{}, dynBenchType1{}, dynBenchType2{}, dynBenchType3{}, dynBenchType4{},
	dynBenchType5{}, dynBenchType6{}, dynBenchType7{}, dynBenchType8{}, dynBenchType9{},
}

var dynBenchNoopHandler OperationalHandler = func(a, b any, aTC, bTC TypeCode, op OpCode) (any, error) {
	return nil, nil
}

func registerDynBenchTypes(b *testing.B, mc *Machine) {
	b.Helper()
	for _, s := range dynBenchSamples {
		if err := mc.registerOperationalHandler(s, nil, dynBenchNoopHandler); err != nil {
			b.Fatalf("registerOperationalHandler: %v", err)
		}
	}
}

// dynBenchSwitchCoder is the "advanced" alternative: a hand-rolled
// type-switch TypeCoder over the same 10 types registerDynBenchTypes
// uses, installed via RegisterTypeCoder ahead of NewVM's registration
// calls so it - not the dynamicTypes map - ends up owning their codes.
func dynBenchSwitchCoder(a any) TypeCode {
	switch a.(type) {
	case dynBenchType0:
		return firstDynamicTypeCode + 900
	case dynBenchType1:
		return firstDynamicTypeCode + 901
	case dynBenchType2:
		return firstDynamicTypeCode + 902
	case dynBenchType3:
		return firstDynamicTypeCode + 903
	case dynBenchType4:
		return firstDynamicTypeCode + 904
	case dynBenchType5:
		return firstDynamicTypeCode + 905
	case dynBenchType6:
		return firstDynamicTypeCode + 906
	case dynBenchType7:
		return firstDynamicTypeCode + 907
	case dynBenchType8:
		return firstDynamicTypeCode + 908
	case dynBenchType9:
		return firstDynamicTypeCode + 909
	}
	return UnSupportedType
}

// BenchmarkTypeCodeDynamicMap measures mc.typeCode's cost for a type only
// the dynamicTypes map recognizes (RegisterOperation with no accompanying
// RegisterTypeCoder) - the reflect.TypeOf + map-lookup path.
func BenchmarkTypeCodeDynamicMap(b *testing.B) {
	mc, err := UnconfiguredVM()
	if err != nil {
		b.Fatal(err)
	}
	registerDynBenchTypes(b, mc)
	sample := dynBenchType9{} // last-registered: worst case for the map's insertion-order-independent cost, included for symmetry with the switch's worst case below
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if tc := mc.typeCode(sample); tc == UnSupportedType {
			b.Fatal("expected a recognized type code")
		}
	}
}

// BenchmarkTypeCodeCustomCoder measures the same lookup with a hand-rolled
// switch-based TypeCoder (RegisterTypeCoder) installed for the same 10
// types, using the last case in the switch (its worst case).
func BenchmarkTypeCodeCustomCoder(b *testing.B) {
	mc, err := UnconfiguredVM(RegisterTypeCoder(dynBenchSwitchCoder, uuid.NewV4()))
	if err != nil {
		b.Fatal(err)
	}
	registerDynBenchTypes(b, mc)
	sample := dynBenchType9{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if tc := mc.typeCode(sample); tc == UnSupportedType {
			b.Fatal("expected a recognized type code")
		}
	}
}

// BenchmarkCombineDynamicMap and BenchmarkCombineCustomCoder repeat the
// comparison end-to-end through Combine (the opHandlers scan and handler
// call included), to show what fraction of total dispatch cost the
// typeCode step actually is - the number that determines whether
// RegisterTypeCoder is worth having at all.
func BenchmarkCombineDynamicMap(b *testing.B) {
	mc, err := UnconfiguredVM()
	if err != nil {
		b.Fatal(err)
	}
	registerDynBenchTypes(b, mc)
	x, y := dynBenchType9{}, dynBenchType9{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mc.Combine(x, y, OpAdd); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCombineCustomCoder(b *testing.B) {
	mc, err := UnconfiguredVM(RegisterTypeCoder(dynBenchSwitchCoder, uuid.NewV4()))
	if err != nil {
		b.Fatal(err)
	}
	registerDynBenchTypes(b, mc)
	x, y := dynBenchType9{}, dynBenchType9{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mc.Combine(x, y, OpAdd); err != nil {
			b.Fatal(err)
		}
	}
}
