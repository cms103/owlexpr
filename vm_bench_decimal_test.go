package owlexpr_test

import (
	"testing"

	"github.com/cms103/owlexpr"
	"github.com/cms103/owlexpr/stdlib"
	"github.com/cms103/owlexpr/vm"

	"github.com/shopspring/decimal"
)

// sink prevents the compiler from optimizing away the computed result -
// a separate copy from the internal package's own sink (vm_bench_test.go),
// since this file lives in owlexpr_test rather than owlexpr: it needs
// stdlib.DecimalBuiltins(), and stdlib imports owlexpr (for
// ListElements), so a file importing both must be an external test
// package rather than the internal one everything else here uses.
var sink any

// benchMixed has no single concrete element type - int, float64 and
// decimal.Decimal all appear - which is exactly the case Combine exists
// for: there's no math.Max that spans this.
var benchMixed = []any{
	1, 5.5, decimal.NewFromFloat(3.3), 2, decimal.NewFromFloat(9.9),
	4.4, 7, decimal.NewFromFloat(0.5), 6, 8.8,
}

// --- max over a mixed-type []any: no native Go function spans this, so
// the "native" baseline is what you'd have to hand-write without
// Combine/operationsRouter.

func nativeMixedMax(values []any) any {
	best := values[0]
	for _, v := range values[1:] {
		var greater bool
		switch bv := best.(type) {
		case int:
			switch av := v.(type) {
			case int:
				greater = av > bv
			case float64:
				greater = av > float64(bv)
			case decimal.Decimal:
				greater = av.GreaterThan(decimal.NewFromInt(int64(bv)))
			}
		case float64:
			switch av := v.(type) {
			case int:
				greater = float64(av) > bv
			case float64:
				greater = av > bv
			case decimal.Decimal:
				greater = av.GreaterThan(decimal.NewFromFloat(bv))
			}
		case decimal.Decimal:
			switch av := v.(type) {
			case int:
				greater = decimal.NewFromInt(int64(av)).GreaterThan(bv)
			case float64:
				greater = decimal.NewFromFloat(av).GreaterThan(bv)
			case decimal.Decimal:
				greater = av.GreaterThan(bv)
			}
		}
		if greater {
			best = v
		}
	}
	return best
}

func BenchmarkNativeMaxMixed(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sink = nativeMixedMax(benchMixed)
	}
}

func BenchmarkCombineMaxMixed(b *testing.B) {
	mc, err := owlexpr.NewVM(stdlib.DecimalBuiltins())
	if err != nil {
		panic("Error creating vm")
	}
	for i := 0; i < b.N; i++ {
		best := benchMixed[0]
		for _, v := range benchMixed[1:] {
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

func BenchmarkFullVMMaxMixed(b *testing.B) {
	env := map[string]any{"values": benchMixed}
	instructions, err := owlexpr.Compile("max(values)")
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < b.N; i++ {
		mc, err := owlexpr.NewVM(stdlib.DecimalBuiltins())
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
