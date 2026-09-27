package owlexpr

import (
	"fmt"
	"testing"
)

// This file benchmarks RunSheet's and RunSheetInto's own bookkeeping cost
// against a fixed, reusable Sheet, separately from the OpLoad/OpAccess
// instruction-resolution cost already covered by bench_prehash_test.go.
// (The file name is historical: a PrehashedMap-keyed results map was
// tried here and measured slower than a plain map[string]any.)
//
// Two synthetic shapes, since dependency fan-in is what stresses
// cross-cell reads:
//   - Chain: each cell depends on exactly the one before it - the
//     minimum possible dependency load per cell.
//   - WideDeps: each cell depends on the 4 cells before it, closer to a
//     real spreadsheet where a summary cell references several inputs.
//
// plus Record, a small Sheet shaped like a pipeline mapping node's.
//
// Each builds the Sheet once outside the timed loop (CompileSheet's own
// cost is deliberately excluded) and reuses one Machine, so what's
// measured is purely the per-call cost.

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

// The RunSheetInto variants measure the same Sheets without RunSheet's
// result map, reusing one out slice across calls the way an engine
// running a Sheet per record would.

func benchRunSheetInto(b *testing.B, sheet *Sheet, env map[string]any) {
	mc, err := NewVM()
	if err != nil {
		b.Fatal(err)
	}
	out := make([]any, sheet.Len())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := RunSheetInto(mc, sheet, env, out); err != nil {
			b.Fatal(err)
		}
	}
	sink = out
}

func BenchmarkRunSheetIntoChain(b *testing.B) {
	benchRunSheetInto(b, buildChainSheet(b), map[string]any{"base": int64(1)})
}

func BenchmarkRunSheetIntoWideDeps(b *testing.B) {
	benchRunSheetInto(b, buildWideDepsSheet(b), map[string]any{"base": int64(1)})
}

// buildRecordSheet is a Sheet shaped like a pipeline mapping node's: 15
// cells mixing env reads, cross-cell references, string concatenation and
// a lambda that reads another cell.
func buildRecordSheet(b *testing.B) *Sheet {
	b.Helper()
	sheet, err := CompileSheet([]CellDef{
		{Name: "qty", Expression: "rec.qty"},
		{Name: "price", Expression: "rec.price"},
		{Name: "net", Expression: "sheet.qty * sheet.price"},
		{Name: "rate", Expression: "rec.region == \"EU\" ? 20 : 10"},
		{Name: "tax", Expression: "sheet.net * sheet.rate / 100"},
		{Name: "gross", Expression: "sheet.net + sheet.tax"},
		{Name: "big", Expression: "sheet.gross > 1000"},
		{Name: "discount", Expression: "sheet.big ? sheet.gross / 10 : 0"},
		{Name: "total", Expression: "sheet.gross - sheet.discount"},
		{Name: "name", Expression: "rec.name"},
		{Name: "label", Expression: "sheet.name + \" (\" + rec.region + \")\""},
		{Name: "lines", Expression: "map(rec.lines, l => l * sheet.rate)"},
		{Name: "lineCount", Expression: "len(sheet.lines)"},
		{Name: "flag", Expression: "sheet.lineCount > 2 and sheet.big"},
		{Name: "summary", Expression: "sheet.flag ? sheet.label : rec.name"},
	})
	if err != nil {
		b.Fatalf("CompileSheet: %v", err)
	}
	return sheet
}

func recordSheetEnv() map[string]any {
	return map[string]any{"rec": map[string]any{
		"qty": int64(12), "price": int64(99), "region": "EU", "name": "widget",
		"lines": []any{int64(1), int64(2), int64(3)},
	}}
}

func BenchmarkRunSheetRecord(b *testing.B) {
	sheet := buildRecordSheet(b)
	mc, err := NewVM()
	if err != nil {
		b.Fatal(err)
	}
	env := recordSheetEnv()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := RunSheet(mc, sheet, env)
		if err != nil {
			b.Fatal(err)
		}
		sink = res
	}
}

func BenchmarkRunSheetIntoRecord(b *testing.B) {
	benchRunSheetInto(b, buildRecordSheet(b), recordSheetEnv())
}
