package owlexpr_test

import (
	"strings"
	"testing"

	"github.com/cms103/owlexpr"
	"github.com/cms103/owlexpr/stdlib"
)

// This file benchmarks the cost of resolving a name dynamically at run
// time - an OpLoad for a top-level builtin/namespace, or the OpAccess that
// follows one to reach a namespace member (e.g. the "string" and "trim" in
// string.trim(x)) - since that's exactly the lookup path a move to
// vm.PrehashedMap (see vm/prehashed_map.go) targets: a compiled program's
// OpLoad/OpAccess instructions reference the same fixed set of names every
// time they execute, so the compiler can hash each one once and have the
// VM spend a cheap uint64 probe on every execution instead of re-hashing
// the name string against a native Go map each time.
//
// Two shapes are covered, since they stress very different things:
//   - "SingleCall" mirrers vm_bench_test.go's BenchmarkFullVM* style: one
//     Run call per b.N iteration, each resolving the name exactly once.
//     Here the per-lookup hashing cost is a small fraction of Run's other
//     overhead (stack setup, instruction dispatch, ...), so a PrehashedMap
//     should barely move the needle - useful as the "stays the same" half
//     of the before/after comparison.
//   - "HotLoop" resolves the same builtin/namespace name once per element
//     of a list via map(), inside a single Run call - the shape a
//     spreadsheet-style formula or any list.map/filter/reduce pipeline
//     actually has. Here the same instruction executes thousands of times
//     per call, so a per-execution hashing cost (or the lack of one)
//     compounds directly into the benchmark's ns/op.
//
// env-variable lookups (OpLoad resolving into the caller-supplied env map)
// are deliberately NOT included here: this change leaves env as a plain
// map[string]any (see the change's own doc comment in vm/vm.go), so a
// benchmark of env lookups would show no difference before/after - the
// interesting "does this change anything" comparison is entirely on the
// builtin/namespace side.

const hotLoopSize = 1000

var hotLoopFloats = func() []float64 {
	out := make([]float64, hotLoopSize)
	for i := range out {
		out[i] = float64(i%17) - 8.5
	}
	return out
}()

var hotLoopStrings = func() []string {
	out := make([]string, hotLoopSize)
	for i := range out {
		out[i] = strings.Repeat(" ", i%5) + "value" + strings.Repeat(" ", i%3)
	}
	return out
}()

// --- bare builtin (abs), resolved via OpLoad against Machine.builtins ---

func BenchmarkOpLoadBuiltinSingleCall(b *testing.B) {
	mc, err := owlexpr.NewVM()
	if err != nil {
		b.Fatal(err)
	}
	env := map[string]any{"x": -3.5}
	instructions, err := owlexpr.Compile("abs(x)")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := mc.Run(instructions, env)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}

func BenchmarkOpLoadBuiltinHotLoop(b *testing.B) {
	mc, err := owlexpr.NewVM()
	if err != nil {
		b.Fatal(err)
	}
	env := map[string]any{"values": hotLoopFloats}
	instructions, err := owlexpr.Compile("map(values, x => abs(x))")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := mc.Run(instructions, env)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}

// --- namespaced builtin (string.trim), resolved via OpLoad against
// Machine.namespaces plus an OpAccess into the namespace's own member map ---

func BenchmarkOpLoadNamespacedSingleCall(b *testing.B) {
	mc, err := owlexpr.NewVM(stdlib.StringBuiltins())
	if err != nil {
		b.Fatal(err)
	}
	env := map[string]any{"s": "  padded  "}
	instructions, err := owlexpr.Compile("string.trim(s)")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := mc.Run(instructions, env)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}

func BenchmarkOpLoadNamespacedHotLoop(b *testing.B) {
	mc, err := owlexpr.NewVM(stdlib.StringBuiltins())
	if err != nil {
		b.Fatal(err)
	}
	env := map[string]any{"values": hotLoopStrings}
	instructions, err := owlexpr.Compile("map(values, x => string.trim(x))")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := mc.Run(instructions, env)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}
