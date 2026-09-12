package owlexpr

import (
	"fmt"
	"testing"
)

// This file benchmarks RunSheet's own bookkeeping cost - the depsMap/
// results dance in RunSheet (sheet.go) - separately from the OpLoad/
// OpAccess instruction-resolution cost already covered by
// bench_prehash_test.go. Before this change, `results[cell.name] = val`
// and `depsMap[dep] = results[dep]` each hashed a name via Go's own map
// hash on every single RunSheet call, even though cell.name and cell.deps
// are entirely fixed once CompileSheet returns - the same Sheet gets
// re-run against many different envs (that's RunSheet's whole reason to
// exist, the same way a compiled []vm.Instruction is meant to be run
// many times rather than recompiled per call), so that hashing repeated
// needlessly on every call. compiledCell now precomputes each name's hash
// once, at CompileSheet time, and RunSheet's internal `results` uses
// vm.PrehashedMap keyed by those hashes instead.
//
// Two shapes, since dependency fan-in is what actually stresses the
// bookkeeping this change targets:
//   - Chain: each cell depends on exactly the one before it - the
//     minimum possible dependency load per cell.
//   - WideDeps: each cell depends on the 4 cells before it - 4x the
//     depsMap/results traffic per cell, closer to a real spreadsheet
//     where a summary cell references several inputs.
//
// Both build the Sheet once outside the timed loop (CompileSheet's own
// cost, including the now one-time HashName calls, is deliberately
// excluded) and reuse one Machine, so what's measured is purely
// RunSheet's per-call cost against a fixed, reusable Sheet.

const sheetBenchCellCount = 200

func buildChainSheet(b *testing.B) *Sheet {
	b.Helper()
	cells := make([]CellDef, sheetBenchCellCount)
	cells[0] = CellDef{Name: "c0", Expression: "base + 1"}
	for i := 1; i < sheetBenchCellCount; i++ {
		cells[i] = CellDef{
			Name:       fmt.Sprintf("c%d", i),
			Expression: fmt.Sprintf("sheet.c%d + base", i-1),
		}
	}
	sheet, err := CompileSheet(cells)
	if err != nil {
		b.Fatalf("CompileSheet: %v", err)
	}
	return sheet
}

func buildWideDepsSheet(b *testing.B) *Sheet {
	b.Helper()
	const fanIn = 4
	cells := make([]CellDef, sheetBenchCellCount)
	for i := 0; i < fanIn; i++ {
		cells[i] = CellDef{
			Name:       fmt.Sprintf("c%d", i),
			Expression: fmt.Sprintf("base + %d", i),
		}
	}
	for i := fanIn; i < sheetBenchCellCount; i++ {
		expr := ""
		for j := i - fanIn; j < i; j++ {
			if expr != "" {
				expr += " + "
			}
			expr += fmt.Sprintf("sheet.c%d", j)
		}
		cells[i] = CellDef{Name: fmt.Sprintf("c%d", i), Expression: expr}
	}
	sheet, err := CompileSheet(cells)
	if err != nil {
		b.Fatalf("CompileSheet: %v", err)
	}
	return sheet
}

func BenchmarkRunSheetChain(b *testing.B) {
	sheet := buildChainSheet(b)
	mc, err := NewVM()
	if err != nil {
		b.Fatal(err)
	}
	env := map[string]any{"base": int64(1)}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := RunSheet(mc, sheet, env)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}

func BenchmarkRunSheetWideDeps(b *testing.B) {
	sheet := buildWideDepsSheet(b)
	mc, err := NewVM()
	if err != nil {
		b.Fatal(err)
	}
	env := map[string]any{"base": int64(1)}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := RunSheet(mc, sheet, env)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}
