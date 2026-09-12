package langtest

// Benchmarks documenting why `in` is implemented as a native operator
// rather than left as something users build out of map/filter/reduce.
// See stdlib's TestIterContainsShortCircuits for the other half of the
// argument (correctness of the early-exit behavior these numbers depend
// on) - `in` itself no longer accepts an iterator source to demonstrate
// this against directly (see DESIGN_NOTES.md's "iter/list split"), but
// iter.contains's vm.ForEachSeqElement shares the exact same
// yield-returns-false early-exit convention `in`'s own
// vm.ForEachListElement uses.

import (
	"testing"

	"github.com/cms103/owlexpr"
)

// benchRoles models a typical small membership-check list - the kind of
// data an "in" operator would realistically be checking against in a rule
// expression (role in ["admin", "editor", "owner"]).
var benchRoles = []any{"viewer", "commenter", "editor", "admin", "owner"}

// BenchmarkInViaReduceLambda is the reduce-based lambda emulation of
// `"admin" in roles` - what a user had to write before this operator
// existed. It costs one mc.Call (lambda invocation) per element and
// cannot short-circuit: reduce's contract always visits every element.
func BenchmarkInViaReduceLambda(b *testing.B) {
	env := map[string]any{"roles": benchRoles, "target": "admin"}
	instructions, err := owlexpr.Compile(`reduce(roles, (acc, x) => acc || x == target, false)`)
	if err != nil {
		b.Fatal(err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		res, err := mc.Run(instructions, env)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}

// BenchmarkInNativeOperator is the same check via the native `in`
// operator, through the full compiled pipeline (Parse+Compile once, Run
// per iteration against a reused VM) - directly comparable to the
// benchmark above since both go through mc.Run end to end.
func BenchmarkInNativeOperator(b *testing.B) {
	env := map[string]any{"roles": benchRoles, "target": "admin"}
	instructions, err := owlexpr.Compile(`target in roles`)
	if err != nil {
		b.Fatal(err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		res, err := mc.Run(instructions, env)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}

// BenchmarkInMapKeyViaFilterLen is the filter+len emulation of `"a" in
// labels` (checking map keys) - it fully iterates every pair and
// allocates the filtered map before checking its length, regardless of
// where in iteration order the match falls.
func BenchmarkInMapKeyViaFilterLen(b *testing.B) {
	env := map[string]any{
		"labels": map[string]any{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"},
	}
	instructions, err := owlexpr.Compile(`len(filter(labels, (k, v) => k == "a")) > 0`)
	if err != nil {
		b.Fatal(err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		res, err := mc.Run(instructions, env)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}

// BenchmarkInMapKeyNativeOperator is the same check via the native `in`
// operator - a single O(1) Go map lookup (map[string]any's fast path in
// inValue), not a scan over every key at all.
func BenchmarkInMapKeyNativeOperator(b *testing.B) {
	env := map[string]any{
		"labels": map[string]any{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5"},
	}
	instructions, err := owlexpr.Compile(`"a" in labels`)
	if err != nil {
		b.Fatal(err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		res, err := mc.Run(instructions, env)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}
