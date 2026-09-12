package vm

import (
	"reflect"
	"testing"

	"github.com/shopspring/decimal"
)

// sink prevents the compiler from optimizing away the computed result.
var sink any

// benchInts/benchFloatsAny are small local copies of the same fixtures
// vm_bench_test.go (root package) uses for its own Combine benchmarks -
// duplicated rather than shared across packages for a few lines of test
// data.
var benchInts = []int64{3, 7, 2, 9, 4, 1, 8, 5, 6, 0}

var benchFloats = []float64{3.1, 9.4, 2.2, 7.7, 5.5, 1.1, 8.8, 4.4, 6.6, 0.9}

var benchFloatsAny = func() []any {
	out := make([]any, len(benchFloats))
	for i, v := range benchFloats {
		out[i] = v
	}
	return out
}()

var benchStrings = []string{"aa", "bb", "cc", "dd", "ee", "ff", "gg", "hh", "ii", "jj"}
var benchBools = []bool{true, false, true, true, false, true, false, false, true, true}
var benchDecimals = func() []decimal.Decimal {
	out := make([]decimal.Decimal, 10)
	for i := range out {
		out[i] = decimal.NewFromFloat(float64(i) + 0.5)
	}
	return out
}()

// decimalFastSliceIterate mirrors stdlib.DecimalBuiltins' own registration
// (owlexpr/stdlib isn't importable from here - it imports this package,
// so pulling it in would be a cycle) - just enough to give
// decimal.Decimal a TypeCode and a fast slice path via
// RegisterFastSliceIterator, for benchmarking that path specifically.
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

// benchStruct is a type the default TypeCoder has never heard of - it
// always resolves to UnSupportedType, so a []benchStruct must fall back to
// the generic per-element reflect loop regardless of the TypeCode fast
// path. Used to measure that fast path's failure-case overhead.
type benchStruct struct{ n int }

var benchStructs = func() []benchStruct {
	out := make([]benchStruct, 10)
	for i := range out {
		out[i] = benchStruct{n: i}
	}
	return out
}()

// reflectOnlyIterate is ForEachListElement's reflect-based slice/array branch
// exactly as it existed before fastSliceElementIterate was added - one
// reflect.Value.Index(i).Interface() call per element, no TypeCode
// short-circuit. It's the "before" baseline these benchmarks compare
// fastSliceElementIterate-equipped ForEachListElement against.
func reflectOnlyIterate(v any, yield func(any) bool) {
	rv := reflect.ValueOf(v)
	n := rv.Len()
	for i := 0; i < n; i++ {
		if !yield(rv.Index(i).Interface()) {
			break
		}
	}
}

func benchmarkReflectOnly(b *testing.B, v any) {
	for i := 0; i < b.N; i++ {
		var acc int
		reflectOnlyIterate(v, func(el any) bool {
			acc++
			return true
		})
		sink = acc
	}
}

func benchmarkForEachListElement(b *testing.B, mc *Machine, v any) {
	for i := 0; i < b.N; i++ {
		var acc int
		ForEachListElement(mc, v, func(el any) bool {
			acc++
			return true
		})
		sink = acc
	}
}

func BenchmarkIterateStringsReflectOnly(b *testing.B) { benchmarkReflectOnly(b, benchStrings) }
func BenchmarkIterateStringsTypeCodeFast(b *testing.B) {
	vm, _ := UnconfiguredVM()
	benchmarkForEachListElement(b, vm, benchStrings)
}

func BenchmarkIterateBoolsReflectOnly(b *testing.B) { benchmarkReflectOnly(b, benchBools) }
func BenchmarkIterateBoolsTypeCodeFast(b *testing.B) {
	vm, _ := UnconfiguredVM()
	benchmarkForEachListElement(b, vm, benchBools)
}

func BenchmarkIterateDecimalsReflectOnly(b *testing.B) { benchmarkReflectOnly(b, benchDecimals) }
func BenchmarkIterateDecimalsTypeCodeFast(b *testing.B) {
	vm, _ := UnconfiguredVM(RegisterFastSliceIterator(decimal.Decimal{}, decimalFastSliceIterate))
	benchmarkForEachListElement(b, vm, benchDecimals)
}

// benchStructs: TypeCode fast path can never apply (UnSupportedType) - this
// measures its failure-case overhead (one reflect.Zero + one typer call)
// against the plain reflect loop with no pre-check at all.
func BenchmarkIterateStructsReflectOnly(b *testing.B) { benchmarkReflectOnly(b, benchStructs) }
func BenchmarkIterateStructsTypeCodeFast(b *testing.B) {
	vm, _ := UnconfiguredVM()
	benchmarkForEachListElement(b, vm, benchStructs)
}

// Already-covered types (from the earlier sum/min/max optimization) -
// confirms ForEachListElement's TypeCode path doesn't regress these versus
// direct iteration, and []any (fast-pathed before any reflection at all)
// is unaffected.
func BenchmarkIterateInt64TypeCodeFast(b *testing.B) {
	vm, _ := UnconfiguredVM()
	benchmarkForEachListElement(b, vm, benchInts)
}
func BenchmarkIterateAnySliceTypeCodeFast(b *testing.B) {
	vm, _ := UnconfiguredVM()
	benchmarkForEachListElement(b, vm, benchFloatsAny)
}

// Same comparisons at a more realistic list size (1000 elements) to see
// whether the TypeCode probe's fixed per-call cost (paid once, not once
// per element) still matters once it's amortized over more elements.
var benchStructsLarge = func() []benchStruct {
	out := make([]benchStruct, 1000)
	for i := range out {
		out[i] = benchStruct{n: i}
	}
	return out
}()

var benchBoolsLarge = func() []bool {
	out := make([]bool, 1000)
	for i := range out {
		out[i] = i%2 == 0
	}
	return out
}()

func BenchmarkIterateStructsLargeReflectOnly(b *testing.B) {
	benchmarkReflectOnly(b, benchStructsLarge)
}
func BenchmarkIterateStructsLargeTypeCodeFast(b *testing.B) {
	vm, _ := UnconfiguredVM()
	benchmarkForEachListElement(b, vm, benchStructsLarge)
}

func BenchmarkIterateBoolsLargeReflectOnly(b *testing.B) { benchmarkReflectOnly(b, benchBoolsLarge) }
func BenchmarkIterateBoolsLargeTypeCodeFast(b *testing.B) {
	vm, _ := UnconfiguredVM()
	benchmarkForEachListElement(b, vm, benchBoolsLarge)
}

var benchStringsLarge = func() []string {
	out := make([]string, 1000)
	for i := range out {
		out[i] = "xx"
	}
	return out
}()

var benchDecimalsLarge = func() []decimal.Decimal {
	out := make([]decimal.Decimal, 1000)
	for i := range out {
		out[i] = decimal.NewFromFloat(float64(i) + 0.5)
	}
	return out
}()

func BenchmarkIterateStringsLargeReflectOnly(b *testing.B) {
	benchmarkReflectOnly(b, benchStringsLarge)
}
func BenchmarkIterateStringsLargeTypeCodeFast(b *testing.B) {
	vm, _ := UnconfiguredVM()
	benchmarkForEachListElement(b, vm, benchStringsLarge)
}

func BenchmarkIterateDecimalsLargeReflectOnly(b *testing.B) {
	benchmarkReflectOnly(b, benchDecimalsLarge)
}
func BenchmarkIterateDecimalsLargeTypeCodeFast(b *testing.B) {
	vm, _ := UnconfiguredVM(RegisterFastSliceIterator(decimal.Decimal{}, decimalFastSliceIterate))
	benchmarkForEachListElement(b, vm, benchDecimalsLarge)
}
