package owlexpr_test

import (
	"testing"

	"github.com/cms103/owlexpr"
	"github.com/cms103/owlexpr/stdlib"
)

// This file lives in owlexpr_test rather than owlexpr because it needs both owlexpr
// and owlexpr/stdlib in scope, and stdlib imports owlexpr - an internal
// test file can't do that without creating an import cycle in the test
// binary, but an external one, having its own distinct package identity,
// can. See vm_bench_decimal_test.go for the same constraint and its own
// package-level sink, reused here rather than redeclared.

// findInts1000 mirrors bench_expr_parity_test.go's own filterInts1000 -
// duplicated locally rather than exported from that internal-only file,
// since it's a three-line helper and not worth widening owlexpr's public
// API for.
func findInts1000() []int64 {
	arr := make([]int64, 1000)
	for i := range arr {
		arr[i] = int64(i + 1)
	}
	return arr
}

func BenchmarkListFind(b *testing.B) {
	env := map[string]any{"Ints": findInts1000()}
	instructions, err := owlexpr.Compile(`list.find(Ints, x => x % 7 == 0)`)
	if err != nil {
		b.Fatal(err)
	}
	mc, err := owlexpr.NewVM(stdlib.ListBuiltins())
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
	if out != int64(7) {
		b.Fatalf("got %v, want 7", out)
	}
}

// BenchmarkListFindLast is the short-circuiting analogue to
// bench_expr_parity_test.go's BenchmarkFilterLast, which computes
// filter(Ints, x => x % 7 == 0)[141] - the full 142-element result,
// materialized, every call (see that benchmark's own doc comment: owlexpr
// has no compiler pass turning filter(...)[literal index] into a scan that
// stops early, unlike expr's). list.findLast walks findInts1000() backwards
// and stops at the first (i.e. highest-index) match - here that's 994, six
// elements in from the end (1000, 999, ..., 994) - so this measures what
// owlexpr actually costs when a caller opts into the short-circuiting
// stdlib function instead of the filter+index idiom, rather than what the
// language does automatically with filter+index (nothing).
func BenchmarkListFindLast(b *testing.B) {
	env := map[string]any{"Ints": findInts1000()}
	instructions, err := owlexpr.Compile(`list.findLast(Ints, x => x % 7 == 0)`)
	if err != nil {
		b.Fatal(err)
	}
	mc, err := owlexpr.NewVM(stdlib.ListBuiltins())
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
	if out != int64(994) {
		b.Fatalf("got %v, want 994", out)
	}
}
