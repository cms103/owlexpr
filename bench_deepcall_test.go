package owlexpr

import "testing"

// deepRecursionSrc computes 1+2+...+N via genuine self-application
// recursion (see langtest/recursion_test.go's yCombinatorSumSrc for why
// this pattern, not letrec, is what real recursion looks like in
// owlexpr) - N nested, simultaneously-active lambda invocations, unlike
// filter/map/reduce's wide-but-shallow per-element calls (each one
// returns before the next starts). This is exactly the shape the VM's
// explicit frame stack targets - see vm/vm.go's frame doc comment.
const deepRecursionSrc = `let sum = self => n => n <= 0 ? 0 : n + self(self)(n - 1); sum(sum)(N)`

func BenchmarkDeepRecursion1000(b *testing.B) {
	instructions, err := Compile(deepRecursionSrc)
	if err != nil {
		b.Fatal(err)
	}
	mc, err := NewVM()
	if err != nil {
		b.Fatal(err)
	}
	env := map[string]any{"N": int64(1000)}
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
}
