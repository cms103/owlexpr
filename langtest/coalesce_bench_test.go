package langtest

// Benchmarks isolating the cost "??" adds even when it's never needed -
// the left operand always succeeds with a non-nil value, so the only
// thing being measured is the overhead of running the left operand
// through a nested, isolated mc.run() call (so its error can be caught)
// versus evaluating the exact same expression inline with no protection
// at all.

import (
	"testing"

	"github.com/cms103/owlexpr"
)

type coalBenchRole struct{ Title string }
type coalBenchUser struct{ Role *coalBenchRole }

// BenchmarkDirectFieldAccess is the baseline: a chained member access with
// no "??" at all, always succeeding.
func BenchmarkDirectFieldAccess(b *testing.B) {
	env := map[string]any{"user": coalBenchUser{Role: &coalBenchRole{Title: "Admin"}}}
	instructions, err := owlexpr.Compile(`user.Role.Title`)
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

// BenchmarkCoalesceFieldAccessNoFallbackNeeded is the same access wrapped
// in "?? fallback" - the left side always succeeds here too, so the
// fallback is never used. The difference from the benchmark above is
// purely OpCoalesce's nested mc.run() for the left operand.
func BenchmarkCoalesceFieldAccessNoFallbackNeeded(b *testing.B) {
	env := map[string]any{"user": coalBenchUser{Role: &coalBenchRole{Title: "Admin"}}}
	instructions, err := owlexpr.Compile(`user.Role.Title ?? "No Title"`)
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

// BenchmarkDirectArithmetic and BenchmarkCoalesceArithmeticNoFallbackNeeded
// repeat the same comparison for the simplest possible left operand (a
// single addition, no reflection at all), to isolate "??"'s own overhead
// from the field-access reflection cost the benchmarks above also pay.
func BenchmarkDirectArithmetic(b *testing.B) {
	env := map[string]any{"x": int64(5)}
	instructions, err := owlexpr.Compile(`x + 1`)
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

func BenchmarkCoalesceArithmeticNoFallbackNeeded(b *testing.B) {
	env := map[string]any{"x": int64(5)}
	instructions, err := owlexpr.Compile(`x + 1 ?? -1`)
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

// BenchmarkCoalesceFieldAccessFallbackUsed is the same field access, but
// this time the left side genuinely fails (nil Role), so the fallback IS
// used - the counterpart case, for comparison against the two above.
func BenchmarkCoalesceFieldAccessFallbackUsed(b *testing.B) {
	env := map[string]any{"user": coalBenchUser{Role: nil}}
	instructions, err := owlexpr.Compile(`user.Role.Title ?? "No Title"`)
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
