# Sheet: multi-step calculations

A single owlexpr expression is deliberately a pure, self-contained formula - which is exactly right for a one-off rule, but starts to strain once a calculation naturally breaks into several named, dependent steps. `Sheet`/`Cell` (`sheet.go`) is the answer: a small, host-side (Go API) layer on top of the ordinary `Compile`/`Run` pipeline - no new expression syntax - for evaluating a *set* of named expressions that can reference each other's results.

## The problem

**Pricing/quote calculations.** A subtotal needs a discount applied, then tax, then a total - and a caller (an invoice, a UI, an audit log) usually wants to see the discount and tax amounts individually, not just the final number:

```go
sheet, _ := owlexpr.CompileSheet([]owlexpr.CellDef{
    {Name: "subtotal", Expression: "sum(map(items, i => i.price * i.qty))"},
    {Name: "discount", Expression: `sheet.subtotal > 100 ? sheet.subtotal * 0.1 : 0.0`},
    {Name: "taxable", Expression: "sheet.subtotal - sheet.discount"},
    {Name: "tax", Expression: "sheet.taxable * 0.08"},
    {Name: "total", Expression: "sheet.taxable + sheet.tax"},
})
```

Written as one expression instead, the same calculation still works, but `sum(map(items, ...))` has to be repeated every place `subtotal` is needed, and `discount`/`tax` disappear entirely - a caller gets `106.92` and nothing else:

```
(sum(map(items, i => i.price * i.qty))
  - (sum(map(items, i => i.price * i.qty)) > 100
       ? sum(map(items, i => i.price * i.qty)) * 0.1 : 0.0)
) * 1.08
```

**Eligibility/risk scoring.** A decision ("approve this application") is usually the last step of several intermediate, individually meaningful ones (a credit score band, a debt-to-income flag, a fraud-risk score) that compliance or support staff need to see on their own, not just folded into a single pass/fail bit.

**Multi-field data mapping.** Transforming one record shape into another where several output fields are derived, and some of those derivations depend on other derived fields rather than only on the raw input.

In every case, the common shape is: several named values, each a plain expression, where later ones reference earlier ones by name and a caller wants every intermediate value back, not just the last one.

### `let` doesn't solve this

`let name = value; body` (see [LANGUAGE.md](LANGUAGE.md#let-bindings)) already gives an expression its own local names - but strictly *within* that one expression. A `let`-bound name is never visible to anything outside the expression that bound it, and `Run` only ever returns one final value, so `let` has no way to hand back `discount` and `tax` as separate, independently addressable results the way the cells above do. `let` is for naming a subexpression you'd otherwise repeat inside one formula; `Sheet` is for naming a whole step whose value needs to be visible - to other steps, and to the caller - outside that formula. The two compose fine: a single cell's `Expression` can use `let` internally exactly like any other expression.

## Basic usage

A `Sheet` is built from a slice of `CellDef{Name, Expression string}`, then compiled once:

```go
sheet, err := owlexpr.CompileSheet([]owlexpr.CellDef{
    {Name: "subtotal", Expression: "sum(map(items, i => i.price * i.qty))"},
    {Name: "discount", Expression: `sheet.subtotal > 100 ? sheet.subtotal * 0.1 : 0.0`},
    {Name: "taxable", Expression: "sheet.subtotal - sheet.discount"},
    {Name: "tax", Expression: "sheet.taxable * 0.08"},
    {Name: "total", Expression: "sheet.taxable + sheet.tax"},
})
// err is non-nil if any cell fails to parse, references an unknown cell,
// or the cells form a dependency cycle - see "Validation" below.

mc, _ := owlexpr.NewVM()
env := map[string]any{
    "items": []any{
        map[string]any{"price": 25.0, "qty": int64(2)},
        map[string]any{"price": 60.0, "qty": int64(1)},
    },
}
out, err := owlexpr.RunSheet(mc, sheet, env)
// out == map[string]any{
//     "subtotal": 110.0, "discount": 11.0,
//     "taxable": 99.0, "tax": 7.92, "total": 106.92,
// }
```

`out` holds every cell's own result, keyed by name - not just `total`.

### `sheet.<name>` is the only way to cross-reference a cell

A cell reaches another cell's value exclusively through the reserved `sheet.` prefix, as in `discount`'s expression above. This is deliberate, not incidental: a bare name (`subtotal`, with no `sheet.` prefix) always means an ordinary env lookup, full stop, never a cell - so a cell and an env variable can safely share a name with no ambiguity at all:

```go
res, _ := owlexpr.RunSheet(mc, sheet, map[string]any{"x": int64(1)})
// cells: {Name: "x", Expression: "100"}, {Name: "y", Expression: "x + sheet.x"}
// res["x"] == int64(100)                 - the cell
// res["y"] == int64(101)                 - env x (1) + cell x (100)
```

`sheet["name"]` (or `sheet?.["name"]`) is the same reference written with brackets, handy for a cell name that isn't a valid identifier. The key must be a constant string. A computed key such as `sheet[k]` is rejected by `CompileSheet`: which cell it reads can't be known until it runs, and at run time a cell only sees the cells it was found to depend on, so it could never succeed.

A local name that happens to be spelled `sheet` - a `let sheet = ...` binding, or a lambda parameter named `sheet` - shadows the reserved namespace inside its own scope exactly the way any other `let`/lambda binding would, rather than being misread as a cross-cell reference: `let sheet = {"x": 999}; sheet.x` inside a cell body reads the local map, not another cell.

### Order doesn't matter - only dependencies do

Cells can be listed in any order; `CompileSheet` resolves the dependency graph from each cell's `sheet.<name>` references and topologically sorts them, so a cell can be declared before or after the ones it depends on:

```go
sheet, _ := owlexpr.CompileSheet([]owlexpr.CellDef{
    {Name: "d", Expression: "sheet.b + sheet.c"}, // diamond: depends on b and c
    {Name: "a", Expression: "base * 10"},
    {Name: "b", Expression: "sheet.a + 1"},        // both b and c depend on a
    {Name: "c", Expression: "sheet.a + 2"},
})
```

`a` runs before `b`/`c`, and both run before `d`, regardless of the order they were declared in above.

## Validation

Every cell is parsed and its cross-cell references checked at `CompileSheet` time, not per `RunSheet` call - a `Sheet` is reusable across many `RunSheet` calls against different envs, exactly the way `Compile`'s `[]vm.Instruction` is reusable across many `Run` calls. `CompileSheet` rejects:

- **A duplicate cell name.**
- **`sheet.<name>` referencing a cell that doesn't exist** - caught as a compile-time typo, not a runtime surprise:
  ```
  cell "a": sheet.doesNotExist references a cell that does not exist
  ```
- **`sheet[...]` with a computed key**, e.g. `sheet[k]` (see above):
  ```
  cell "b": sheet[...] must use a constant string, e.g. sheet["name"]: a cell name computed at run time can't be resolved when the sheet is compiled
  ```
- **A dependency cycle**, direct self-reference included, named explicitly in the error:
  ```
  compile sheet: dependency cycle: a -> b -> c -> a
  ```
- **An empty cell set** (`CompileSheet(nil)`) or a cell with an empty `Name`.

## Running a sheet

`RunSheet(mc, sheet, env) (map[string]any, error)` evaluates every cell in dependency order against a shared `*vm.Machine` and `env`. A few behaviors are worth knowing before relying on it:

- **`env` is never mutated or copied.** Each cell sees only its own direct dependencies' results, exposed as a small map under the reserved `"sheet"` key alongside `env` - not a merged copy of everything computed so far. Because of that, `RunSheet` refuses to run at all if `env` already has a key literally named `"sheet"`, rather than silently letting it collide with the cell-reference namespace:
  ```
  run sheet: env already defines "sheet", which collides with the reserved cell-reference namespace
  ```
- **Fail-fast, no partial results.** The first cell to error stops the whole run; its error is returned wrapped with the cell's own name, and `RunSheet`'s returned map is `nil` - there's no "here's what finished before the failure" result to inspect:
  ```go
  // cells: {Name: "a", Expression: "1 / 0"}, {Name: "b", Expression: "42"}
  out, err := owlexpr.RunSheet(mc, sheet, env)
  // out == nil, err == `cell "a": Divide by zero`
  ```
- Because evaluation follows dependency order, a cell that depends - directly or transitively - on a failed cell never runs at all. A cell with no such dependency may still have already run (and had any side effects it causes) before the failure, even though its result is discarded along with everything else once `RunSheet` returns the error.

## Cells that return a callable

A cell's expression can evaluate to a lambda, not just a plain value - useful for a step that's more naturally "a function of something" than a single number. Other cells can call it the normal way, through `sheet.<name>(...)`:

```go
sheet, _ := owlexpr.CompileSheet([]owlexpr.CellDef{
    {Name: "rate", Expression: "2"},
    {Name: "scaler", Expression: "x => x * sheet.rate"},
    {Name: "scaledTotal", Expression: "sheet.scaler(21)"}, // == 42
})
```

And the caller of `RunSheet` gets back an ordinary, directly callable Go func for `scaler` itself - not owlexpr's internal closure representation - the same conversion `Run` applies to its own result (see [EMBED.md](EMBED.md#callable-results)):

```go
out, _ := owlexpr.RunSheet(mc, sheet, map[string]any{})
scaler := out["scaler"].(func(args ...any) (any, error))
result, _ := scaler(int64(21)) // == int64(42)
```

This is safe to call even well after `RunSheet` has returned and other cells have run - each cell's own dependency map is allocated fresh and never touched again once that cell finishes, so a closure it returns keeps seeing exactly the values it closed over.

## Finding references without compiling a sheet

`SheetReferences(expression, namespace string) ([]string, error)` runs the same reference analysis `CompileSheet` uses, on a single expression, and returns the cell names it reads, sorted and de-duplicated. It's for hosts that manage the dependency graph themselves, e.g. a data pipeline that needs to know which upstream outputs a step's expression reads before scheduling it:

```go
refs, err := owlexpr.SheetReferences(`sheet.taxable + sheet.tax`, "")
// refs == []string{"tax", "taxable"}

refs, err = owlexpr.SheetReferences(`stage.clean.count > 0 ? stage.total : 0`, "stage")
// refs == []string{"clean", "total"}  (only the first member after the namespace counts)
```

- `namespace` is the identifier references go through. `""` means the default, `"sheet"`. Anything else must be a single identifier and not a reserved word, the same rule as the `Namespace` option.
- It follows `CompileSheet`'s rules exactly. A `let` name or lambda parameter that shadows the namespace isn't a reference, and a bare `sheet` with no member (e.g. `len(sheet)`) adds nothing, because which cells it reads can't be known without running it.
- The result is empty, not `nil`, when there are no references.
- `sheet["name"]` counts the same as `sheet.name`. It returns an error if the expression doesn't parse, the namespace isn't valid, or the expression uses a computed key such as `sheet[k]`. It doesn't check whether the named cells exist, since it only sees one expression.

## What `Sheet` isn't

- **Not a general workflow/orchestration engine.** Beyond fail-fast-on-error, there's no ordering guarantee for cells with no dependency relationship to each other besides "somewhere consistent with the DAG" - and cells run sequentially, not in parallel, even when nothing depends on them running in any particular order relative to each other. Reach for something purpose-built for orchestration/scheduling if that's actually what you need; `Sheet` is for a fixed set of derived values, not a task runner.
- **The cell set is fixed at `CompileSheet` time.** There's no way to add, remove, or redefine a cell at runtime - build a new `Sheet` (a cheap, one-time cost relative to however many `RunSheet` calls follow) if the set of steps itself needs to change.

## See also

- `sheet_test.go` - the full behavioral spec this document is drawn from, including every error case above.
- `cmd/sheet_demo/sheet_demo.go` - a larger runnable example mixing `Sheet` with `stdlib` functions and iterators.
- DESIGN_NOTES.md's "`let` vs. `Sheet`/`Cell`" section - the full prior-art survey (Terraform/HCL, Rego, Excel, spreadsheets) behind the `sheet.<name>`-only design.
