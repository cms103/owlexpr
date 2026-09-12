package stdlib

// Benchmarks documenting why keys()/values() are their own builtins
// rather than left as something users spell with map()'s existing
// paired-source support - map(m, (k, v) => k) is already a correct,
// working way to get the same result today. Same shape of argument as
// langtest/in_bench_test.go makes for the `in` operator: measure the
// user-space equivalent against the native version rather than assume.

import (
	"testing"

	"github.com/cms103/owlexpr"
)

func benchStringMap(n int) map[string]any {
	m := make(map[string]any, n)
	for i := 0; i < n; i++ {
		m[string(rune('a'+i%26))+string(rune('0'+i/26))] = i
	}
	return m
}

// BenchmarkKeysViaMapLambda is `map(m, (k, v) => k)` - what a user has to
// write without a dedicated keys() builtin. Every one of the map's
// entries costs one mc.Call: a lambda invocation (closure/scope setup,
// a nested mc.run() over the lambda body) just to hand back k unchanged.
func BenchmarkKeysViaMapLambda(b *testing.B) {
	env := map[string]any{"m": benchStringMap(20)}
	instructions, err := owlexpr.Compile(`map(m, (k, v) => k)`)
	if err != nil {
		b.Fatal(err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mc.Run(instructions, env); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkKeysNativeBuiltin is the same result via keys(m) - a plain Go
// append per pair via ForEachMapPair, with no lambda/mc.Call involved at
// all, through the full compiled pipeline just like the benchmark above.
func BenchmarkKeysNativeBuiltin(b *testing.B) {
	env := map[string]any{"m": benchStringMap(20)}
	instructions, err := owlexpr.Compile(`list.keys(m)`)
	if err != nil {
		b.Fatal(err)
	}
	mc, err := owlexpr.NewVM(ListBuiltins())
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mc.Run(instructions, env); err != nil {
			b.Fatal(err)
		}
	}
}
