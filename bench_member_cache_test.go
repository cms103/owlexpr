package owlexpr_test

import (
	"testing"

	"github.com/cms103/owlexpr"
)

// This file benchmarks Machine.memberCache (vm/vm.go) - accessMember's
// per-(concrete type, member name) memoization of its own
// MethodByName/FieldByName search, populated the first time a given pair
// is seen and reused for every later access to the same member on the
// same type, on the same Machine.
//
// bench_expr_parity_test.go's own BenchmarkLargeStructAccess uses a
// 2-field struct, which understates the win: a MethodByName/FieldByName
// search over 2 fields is already cheap, so most of that benchmark's
// ns/op is other Run overhead (stack ops, instruction dispatch), not the
// reflection search this cache targets. The benchmarks here use wider
// structs specifically to make that search cost - and the cache's effect
// on it - visible.

// wideStruct has enough fields that a linear/BFS FieldByName search over
// it actually costs something, unlike bench_expr_parity_test.go's 2-field
// benchLargeEnv.
type wideStruct struct {
	F00, F01, F02, F03, F04, F05, F06, F07, F08, F09 int64
	F10, F11, F12, F13, F14, F15, F16, F17, F18, F19 int64
	F20, F21, F22, F23, F24, F25, F26, F27, F28, F29 int64
	F30, F31, F32, F33, F34, F35, F36, F37, F38, F39 int64
	Field                                            int64
}

// BenchmarkWideStructAccess is BenchmarkLargeStructAccess
// (bench_expr_parity_test.go) with a 41-field struct in place of its
// 2-field one, and no other change - same expression (3 `.Field`
// comparisons per call), same reused Machine/env across b.N. Compare
// against BenchmarkLargeStructAccess to see the cache's win grow with
// struct width: `go test -bench 'BenchmarkWideStructAccess|BenchmarkLargeStructAccess$' -benchmem`.
func BenchmarkWideStructAccess(b *testing.B) {
	env := map[string]any{"Env": &wideStruct{Field: 21}}
	instructions, err := owlexpr.Compile(`Env.Field > 0 && Env.Field > 1 && Env.Field < 99`)
	if err != nil {
		b.Fatal(err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		b.Fatal(err)
	}
	var out any
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err = mc.Run(instructions, env)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	sink = out
	if out != true {
		b.Fatalf("got %v, want true", out)
	}
}

// wideItem is a struct shaped like a realistic row in a slice being
// mapped/filtered - wide enough that a name search over it costs
// something, with the field of interest last, roughly the worst case for
// a linear/BFS FieldByName search order.
type wideItem struct {
	F00, F01, F02, F03, F04, F05, F06, F07, F08, F09 int64
	F10, F11, F12, F13, F14, F15, F16, F17, F18, F19 int64
	Price                                            int64
}

// BenchmarkMapOverWideStructs is the reason this cache was actually added
// for: map()/filter() over a slice of structs, accessing the same field
// on the same type once per element.
func BenchmarkMapOverWideStructs(b *testing.B) {
	const n = 1000
	items := make([]wideItem, n)
	for i := range items {
		items[i] = wideItem{Price: int64(i)}
	}
	env := map[string]any{"Items": items}
	instructions, err := owlexpr.Compile(`map(Items, x => x.Price * 2)`)
	if err != nil {
		b.Fatal(err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		b.Fatal(err)
	}
	var out any
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err = mc.Run(instructions, env)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	sink = out
	if got := len(out.([]any)); got != n {
		b.Fatalf("got len %d, want %d", got, n)
	}
}

// BenchmarkMapOverMapsForComparison is BenchmarkMapOverWideStructs with
// map[string]any elements in place of wideItem structs - same expression,
// same element count, same reused Machine.
func BenchmarkMapOverMapsForComparison(b *testing.B) {
	const n = 1000
	items := make([]any, n)
	for i := range items {
		items[i] = map[string]any{"Price": int64(i)}
	}
	env := map[string]any{"Items": items}
	instructions, err := owlexpr.Compile(`map(Items, x => x.Price * 2)`)
	if err != nil {
		b.Fatal(err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		b.Fatal(err)
	}
	var out any
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err = mc.Run(instructions, env)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	sink = out
	if got := len(out.([]any)); got != n {
		b.Fatalf("got len %d, want %d", got, n)
	}
}
