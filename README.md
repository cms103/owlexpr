# Owlexpr - Flexible expression evaluation in Go

Owlexpr is a high-performance flexible expression language, written in Go. It's designed to be easy to use for business analysts and configurers writing the expressions and flexible for software engineers to bake into their software.

## An Introduction to Expression Languages

Expression languages are used to enable users of your system to write simple statements to make business or policy decisions at runtime, without these being hardcoded into software.

Smoe common examples of where they are used are:

 * **Spreadsheets** - every formula cell in Excel or Google Sheets (`=A1*B2+VLOOKUP(...)`) is an expression evaluated against a grid of interdependent cells. It's the most familiar expression language there is, and the inspiration for owlexpr's own Sheet/Cell model.
 * **User-defined calculations** - letting a user type `(price * quantity) - discount` into a config field supports arbitrary calculations.
 * **Business rules** - loan approval, insurance underwriting, and pricing/discount engines externalize their logic as expressions so analysts can tune them without engineering involvement, e.g. `age >= 18 && creditScore > 650 && loanAmount <= income * 4`. 
 * **Access control and policy** - Google's Common Expression Language (CEL) drives Kubernetes admission policies, Cloud IAM conditions, and Firebase Security Rules, e.g. `resource.owner == request.auth.uid || 'admin' in request.auth.roles`.
 * **Feature flags and targeting** - platforms can evaluate targeting rules such as `country == "US" && plan == "enterprise"` to change behavior per-user at runtime, with no redeploy.
 * **Monitoring and alerting** - Prometheus alerting rules and similar systems evaluate expressions over live metrics (`rate(http_errors_total[5m]) > 0.05`) to decide when something is wrong.
 * **Infrastructure and CI/CD** - Terraform (`count = var.enabled ? 1 : 0`) and GitHub Actions (`if: github.ref == 'refs/heads/main'`) use expressions to make infrastructure and pipelines conditional and data-driven.
 * **Data transformation pipelines** - tools like JQ, JSONata, and AWS Step Functions use expressions to reshape data as it flows through a pipeline, e.g. mapping an incoming webhook payload into an internal schema before it reaches a database.

The common thread: give people who aren't writing your software a safe, constrained language to describe what they want, without having to build every configurable possibility into your software.

## Quick Start

Both examples below are complete, runnable programs - drop either one into a `.go` file inside a clone of this repository (e.g. `cmd/quickstart/main.go`) and run it with `go run ./cmd/quickstart`.

### A single expression

```go
package main

import (
	"fmt"

	"github.com/cms103/owlexpr"
)

func main() {
	env := map[string]any{
		"age":         int64(25),
		"creditScore": int64(700),
		"loanAmount":  int64(15000),
		"income":      int64(50000),
	}

	program, err := owlexpr.Compile("age >= 18 && creditScore > 650 && loanAmount <= income * 4")
	if err != nil {
		panic(err)
	}

	mc, err := owlexpr.NewVM()
	if err != nil {
		panic(err)
	}

	result, err := mc.Run(program, env)
	if err != nil {
		panic(err)
	}

	fmt.Println("approved:", result) // approved: true
}
```

▶ [Run this on the Go Playground](https://go.dev/play/p/AzNBes15Y4u)

`Compile` turns the expression text into a reusable set of instructions; `NewVM` creates a reusable machine to run them against. `env` is just a `map[string]any` - any Go value can go in it, from plain scalars to structs and slices. See [EMBED.md](docs/EMBED.md) for everything that can go into `env` and how the VM can be configured.

### A sheet

A [Sheet](docs/SHEET.md) is a set of named, interdependent expressions - each one a "cell" that can reference another cell's result via `sheet.<name>`, resolved and run in dependency order automatically:

```go
package main

import (
	"fmt"

	"github.com/cms103/owlexpr"
)

func main() {
	sheet, err := owlexpr.CompileSheet([]owlexpr.CellDef{
		{Name: "subtotal", Expression: "sum(map(items, i => i.price * i.qty))"},
		{Name: "discount", Expression: `sheet.subtotal > 100 ? sheet.subtotal * 0.1 : 0.0`},
		{Name: "total", Expression: "sheet.subtotal - sheet.discount"},
	})
	if err != nil {
		panic(err)
	}

	mc, err := owlexpr.NewVM()
	if err != nil {
		panic(err)
	}

	env := map[string]any{
		"items": []any{
			map[string]any{"price": 25.0, "qty": int64(2)},
			map[string]any{"price": 60.0, "qty": int64(1)},
		},
	}

	out, err := owlexpr.RunSheet(mc, sheet, env)
	if err != nil {
		panic(err)
	}

	fmt.Println("subtotal:", out["subtotal"]) // subtotal: 110
	fmt.Println("discount:", out["discount"]) // discount: 11
	fmt.Println("total:", out["total"])       // total: 99
}
```

▶ [Run this on the Go Playground](https://go.dev/play/p/QNu2Y2-sjqZ)

`total` is derived from `discount`, which is derived from `subtotal` - `CompileSheet` works that ordering out from the `sheet.*` references itself, and `RunSheet` returns every cell's result, not just the last one, so a caller can inspect `subtotal`/`discount` individually rather than only seeing the final `total`.


## Background and motivation

Owlexpr is heavily inspired by the excellent expr package, but with an emphasis on flexibility and extensability. It's been designed to be heavily configurable at only a modest cost to performance, using a VM for execution speed.

## Main capabilities

 * Extension mechanisms for adding builtin functions and new types
 * A small core language with the possibility to remove all builtin functions
 * Nil-safe navigation (`?.`) and nil-coalescing (`??`) operators
 * Errors instead of panics: expression failures surface as clean, catchable errors rather than crashing the host process
 * Unicode-correct string handling: indexing and slicing operate on runes, not raw bytes
 * First class lambda functions and closures
 * Namespaces for builtin methods (string.*, list.*, time.*, bytes.*, etc)
 * An extensive standard library including lazy iterator pipelines, list operations, string/bytes/time utilities, a dedicated byte type for fixed-width binary records, and arbitrary-precision fixed-point decimals (via github.com/shopspring/decimal)
 * Multi-expression spreadsheet style functionality with automatic dependency resolution


## Documentation

 * [The language](docs/LANGUAGE.md) and default built-in functions.
 * The [standard library](docs/STDLIB.md).
 * Multi-expression [Sheet and Cell](docs/SHEET.md) capabilites.
 * [A guide to embedding](docs/EMBED.md) owlexpr into your application.
 * [Extending owlexpr](docs/EXTEND.md) with new builtin functions and types.
 * [Architecture](docs/ARCHITECTURE.md) overview.

## Performance

Generally owlexpr is highly performant and there's no real need to consider performance optimisation in most cases.

If you are processing a lot of data and want to get the most out of the language, here are few things to consider:

 * The output from `owlexpr.Compile` is an immutable set of instructions. These can be both re-used many times and also provided to multiple goroutine to run in parallel.
 * The `Machine` returned by `owlexpr.NewVM` is re-usable. It can only be used by one goroutine at once, but it is safe to reuse and can run any set of instructions.
 * map[string]any is somewhat faster than using structs for presenting data - reading a struct field always costs one reflect.Value field read plus boxing the result, which a native Go map read never pays. Measured on a slice of 1000 elements, one field access each, on a reused Machine: ~49ns/element for a map vs ~103ns/element for a struct (`BenchmarkMapOverMapsForComparison`/`BenchmarkMapOverWideStructs`).
 * `Machine` automatically caches each struct's method/field name resolution the first time a given `(type, member)` pair is looked up, and reuses it for every later access to that same member on that same type - so the reflection *name search* across the width and depth of struct access is now paid at most once per Machine, not once per access.
