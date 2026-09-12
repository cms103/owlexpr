package owlexpr

import (
	"math"
	"testing"

	"github.com/cms103/owlexpr/vm"
)

// sink prevents the compiler from optimizing away the computed result.
var sink any

var benchFloats = []float64{3.1, 9.4, 2.2, 7.7, 5.5, 1.1, 8.8, 4.4, 6.6, 0.9}

var benchFloatsAny = func() []any {
	out := make([]any, len(benchFloats))
	for i, v := range benchFloats {
		out[i] = v
	}
	return out
}()

var benchInts = []int64{3, 7, 2, 9, 4, 1, 8, 5, 6, 0}

var benchIntsAny = func() []any {
	out := make([]any, len(benchInts))
	for i, v := range benchInts {
		out[i] = v
	}
	return out
}()

// --- max over a uniform []float64 ---

// BenchmarkNativeMaxFloat64 is the baseline: ordinary Go, no interfaces,
// no dispatch.
func BenchmarkNativeMaxFloat64(b *testing.B) {
	for i := 0; i < b.N; i++ {
		best := benchFloats[0]
		for _, v := range benchFloats[1:] {
			best = math.Max(best, v)
		}
		sink = best
	}
}

// BenchmarkCombineMaxFloat64 does the same fold over []any-boxed floats
// using mc.Combine directly, isolating the operationsRouter dispatch cost
// (type-coding both operands + linear handler-table scan + the handler's
// own type switch) from everything else.
func BenchmarkCombineMaxFloat64(b *testing.B) {
	mc, err := NewVM()
	if err != nil {
		panic("Error creating vm")
	}
	values := benchFloatsAny
	for i := 0; i < b.N; i++ {
		best := values[0]
		for _, v := range values[1:] {
			res, err := mc.Combine(v, best, vm.OpGreater)
			if err != nil {
				b.Fatal(err)
			}
			if res.(bool) {
				best = v
			}
		}
		sink = best
	}
}

// BenchmarkBuiltinMaxFloat64 calls the registered "max" builtin directly
// (skipping Parse/Compile), adding the builtin's own arg-handling on top
// of Combine.
func BenchmarkBuiltinMaxFloat64(b *testing.B) {
	mc, err := NewVM()
	if err != nil {
		panic("Error creating vm")
	}
	values := benchFloatsAny
	for i := 0; i < b.N; i++ {
		// mc.builtins is unexported (VM now lives in the virtualmachine
		// package) - call the same underlying logic the registered "max"
		// builtin uses directly instead, since foldExtreme lives in this
		// same package (builtin_funcs.go).
		res, err := foldExtreme(mc, "max", values, vm.OpGreater)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}

// BenchmarkFullVMMaxFloat64 runs the full compiled pipeline - OpLoad,
// OpCall, stack push/pop - for `max(values)`, plus a fresh NewVM per
// iteration (matching how the demos actually use the VM). This is the
// realistic end-to-end number, not just the dispatch primitive.
func BenchmarkFullVMMaxFloat64(b *testing.B) {
	env := map[string]any{"values": benchFloats}
	instructions, err := Compile("max(values)")
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		mc, err := NewVM()
		if err != nil {
			panic("Error creating vm")
		}
		res, err := mc.Run(instructions, env)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}

// BenchmarkFullVMMaxFloat64Reused is BenchmarkFullVMMaxFloat64 but with the
// VM created once outside the timed loop, to separate NewVM's own setup
// cost (allocating the builtins map, opHandlers slice, and registering
// both) from the cost of interpreting the bytecode itself. Reuse is safe
// here because Run always leaves the stack balanced; env is passed in
// per-call, not VM state, so it could even change between iterations.
func BenchmarkFullVMMaxFloat64Reused(b *testing.B) {
	env := map[string]any{"values": benchFloats}
	instructions, err := Compile("max(values)")
	if err != nil {
		b.Fatal(err)
	}
	mc, err := NewVM()
	if err != nil {
		panic("Error creating vm")
	}
	for i := 0; i < b.N; i++ {
		res, err := mc.Run(instructions, env)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}

// Benchmarks over a mixed-type []any that includes decimal.Decimal live in
// vm_bench_decimal_test.go instead of here: they need stdlib.DecimalBuiltins(),
// and stdlib imports this package (for ListElements), so a test file that
// imports both must live in an external owlexpr_test package rather than
// this internal one - see that file's own comment.

// --- sum over a uniform []int64 ---

func BenchmarkNativeSumInt64(b *testing.B) {
	for i := 0; i < b.N; i++ {
		var acc int64
		for _, v := range benchInts {
			acc += v
		}
		sink = acc
	}
}

func BenchmarkCombineSumInt64(b *testing.B) {
	mc, err := NewVM()
	if err != nil {
		panic("Error creating vm")
	}
	values := benchIntsAny
	for i := 0; i < b.N; i++ {
		acc := values[0]
		for _, v := range values[1:] {
			res, err := mc.Combine(acc, v, vm.OpAdd)
			if err != nil {
				b.Fatal(err)
			}
			acc = res
		}
		sink = acc
	}
}

func BenchmarkFullVMSumInt64(b *testing.B) {
	env := map[string]any{"values": benchInts}
	instructions, err := Compile("sum(values)")
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		mc, err := NewVM()
		if err != nil {
			panic("Error creating vm")
		}
		res, err := mc.Run(instructions, env)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}
