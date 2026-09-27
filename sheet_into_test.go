package owlexpr

import (
	"strings"
	"testing"
)

func TestRunSheetIntoFillsSlotsInNameOrder(t *testing.T) {
	sheet, err := CompileSheet([]CellDef{
		{Name: "total", Expression: "sheet.net + sheet.tax"},
		{Name: "net", Expression: "price * qty"},
		{Name: "tax", Expression: "sheet.net / 10"},
	})
	if err != nil {
		t.Fatalf("CompileSheet: %v", err)
	}
	if sheet.Len() != 3 {
		t.Fatalf("Len = %d, want 3", sheet.Len())
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	out := make([]any, sheet.Len())
	if err := RunSheetInto(mc, sheet, map[string]any{"price": int64(20), "qty": int64(5)}, out); err != nil {
		t.Fatalf("RunSheetInto: %v", err)
	}
	want := map[string]any{"net": int64(100), "tax": int64(10), "total": int64(110)}
	for i := range out {
		name := sheet.Name(i)
		if out[i] != want[name] {
			t.Errorf("slot %d (%s) = %v, want %v", i, name, out[i], want[name])
		}
		if idx, ok := sheet.Index(name); !ok || idx != i {
			t.Errorf("Index(%q) = %d, %v, want %d, true", name, idx, ok, i)
		}
	}
	if _, ok := sheet.Index("nope"); ok {
		t.Error("Index of an unknown cell reported ok")
	}
}

func TestRunSheetIntoRejectsWrongLength(t *testing.T) {
	sheet, err := CompileSheet([]CellDef{{Name: "a", Expression: "1"}})
	if err != nil {
		t.Fatalf("CompileSheet: %v", err)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	err = RunSheetInto(mc, sheet, nil, make([]any, 2))
	if err == nil || !strings.Contains(err.Error(), "length") {
		t.Fatalf("err = %v, want a length error", err)
	}
}

// TestRunSheetIntoReusedOutKeepsReturnedClosuresIntact: every cell of a
// run shares one results map, so a closure a cell returns must still see
// that run's values after a later run reuses out - and a later cell of
// the same run must not change what it sees.
func TestRunSheetIntoReusedOutKeepsReturnedClosuresIntact(t *testing.T) {
	sheet, err := CompileSheet([]CellDef{
		{Name: "rate", Expression: "base"},
		{Name: "scale", Expression: "x => x * sheet.rate"},
		{Name: "later", Expression: "sheet.scale(1) + 1"},
	})
	if err != nil {
		t.Fatalf("CompileSheet: %v", err)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	slot, _ := sheet.Index("scale")
	out := make([]any, sheet.Len())

	if err := RunSheetInto(mc, sheet, map[string]any{"base": int64(2)}, out); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first := out[slot].(func(args ...any) (any, error))
	if err := RunSheetInto(mc, sheet, map[string]any{"base": int64(100)}, out); err != nil {
		t.Fatalf("second run: %v", err)
	}
	for i := range out {
		out[i] = "clobbered"
	}
	got, err := first(int64(21))
	if err != nil || got != int64(42) {
		t.Fatalf("first run's closure gave %v, %v; want 42", got, err)
	}
}

// TestCompileSheetReportsCompileErrors: a cell that parses but fails to
// compile (an invalid regex literal) is an error, not a truncated cell.
func TestCompileSheetReportsCompileErrors(t *testing.T) {
	_, err := CompileSheet([]CellDef{{Name: "a", Expression: `"x" matches "["`}})
	if err == nil || !strings.Contains(err.Error(), `cell "a"`) || !strings.Contains(err.Error(), "regex") {
		t.Fatalf("err = %v, want a regex error naming cell a", err)
	}
}

// TestRunSheetIntoAllocationsDontGrowWithCells pins the point of the
// shared results map: a run's own allocations are per call, not per cell.
// Results are chosen so boxing them doesn't allocate.
func TestRunSheetIntoAllocationsDontGrowWithCells(t *testing.T) {
	allocs := func(n int) float64 {
		cells := []CellDef{{Name: "c0", Expression: "base"}}
		for i := 1; i < n; i++ {
			cells = append(cells, CellDef{Name: "c" + string(rune('a'+i)), Expression: "sheet.c0 + 1"})
		}
		sheet, err := CompileSheet(cells)
		if err != nil {
			t.Fatalf("CompileSheet: %v", err)
		}
		mc, err := NewVM()
		if err != nil {
			t.Fatalf("NewVM: %v", err)
		}
		env := map[string]any{"base": int64(1)}
		out := make([]any, sheet.Len())
		return testing.AllocsPerRun(100, func() {
			if err := RunSheetInto(mc, sheet, env, out); err != nil {
				t.Fatal(err)
			}
		})
	}
	if small, large := allocs(2), allocs(8); small != large {
		t.Fatalf("RunSheetInto allocated %v times for 2 cells but %v for 8; want the same", small, large)
	}
}
