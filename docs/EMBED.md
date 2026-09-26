# Embedding Owlexpr

Expression languages can be used in a large variety of ways, everything from simple business rules through spreadsheet style calculations to complex data mapping.

To better support this range of use cases, Owlexpr provides a number of options for how it can work within an application.

## The Environment

Starting with this example:

```go
env := map[string]any{}

// Error handling removed for readability
ourProgram, _ := owlexpr.Compile(expression)
ourMachine, _ := owlexpr.NewVM(stdlib.All())
result, _ := ourMachine.Run(ourProgram, env)
```

What the expression can access depends on what you place into the env map.

### Environment Functions

A function, e.g. `env["getGreeting"] = func(args ...any) string { return "Hello" }`, can then be called in the expression `getGreeting()`.

Functions aren't limited to the `func(args ...any) T` shape above - any Go function signature works, since calls are dispatched through reflection. Arguments are converted to the parameter types the function expects where possible (e.g. an `int64` literal into a `func(int)` parameter), so a normally-typed function is usually the more convenient choice:

```go
env["getCustomer"] = func(customerID string) (*Customer, error) {
    if customerID == "" {
        return nil, errors.New("getCustomer requires a customer ID")
    }
    return customerRecord(customerID), nil
}
```

Is used in an expression as `getCustomer("1234")`.

Return values follow a fixed convention. No return values yields `nil`, and a single `error`-typed return signals success/failure only. Otherwise a trailing `error` fails the call when it's non-nil and is set aside, and the data values left over decide the result: one value (including the usual `(value, error)`, same as `getCustomer` above) is returned as-is, and two or more come back as a list in order. So `func() (int, string)` gives `[1, "a"]`, and `t.ISOWeek()` on a `time.Time` gives `[year, week]`, which an expression can index as `t.ISOWeek()[1]`. A `(value, ok bool)` function also gives a two-element list, since the VM can't tell what the `bool` means.

Each data value (including each list element) is normalised on the way back: an integer or float kind owlexpr doesn't model (e.g. the `int32` from a decimal's `Exponent()`, a `float32`, or a named type such as `type Level uint8`) is converted to `int64` or `float64`, so it works with arithmetic, comparisons and `int()`/`float64()`. Plain `int` and any type with a registered operation (e.g. `time.Duration` and `time.Month` once `TimeBuiltins` is enabled, or your own `RegisterOperation` types) are returned unchanged, as is a `uint64` value too large for `int64`.

The reflection-based argument conversion above also applies when the expected parameter type is itself a function - a callback. If a struct in the environment has a method that takes a function as one of its arguments, a lambda written in the expression can be passed straight in for it, and owlexpr bridges the call automatically.

For example, given this Go type:

```go
type Item struct {
    Name   string
    Active bool
}

type Items []Item

func (its Items) Filter(pred func(Item) bool) []Item {
    var out []Item
    for _, it := range its {
        if pred(it) {
            out = append(out, it)
        }
    }
    return out
}
```

placed into the environment as `env["items"] = someItems`, the expression `items.Filter(x => x.Active)` works with no extra setup: `x => x.Active` is a owlexpr lambda, not a Go value, but because `Filter`'s parameter is `func(Item) bool`, owlexpr wraps the lambda in an adapter of that exact type before calling `Filter` - so from `Filter`'s point of view it received an ordinary `func(Item) bool`, and calls it the same way it would any other. This works for a callback parameter on any struct method or environment function, not just this example.

### Data

Any Go type can be placed into an Environment and accessed by the expression. Typically these are slices, arrays, maps, structs, pointers to structs as well as basic types like string, int, float64.

The default slice type used by Owlexpr is []any and the default map type is map[string]any. Any other slice, array or map type can be used.

Unexported struct fields are not accessible from expressions - `x.hidden` is a "member not found" error.

A regular expression built in Go (for example from configuration) can be passed in as a `*vm.Regex`, and then works with `matches` and the `regex.*` functions exactly like a `` re`...` `` literal written in the expression:

```go
env["ticketId"] = vm.NewRegex(regexp.MustCompile(`^[A-Z]+-\d+$`))
```

`vm.Regex` exposes only read-only methods to expressions; don't call `Longest()` on the wrapped `*regexp.Regexp` after handing it over, as the same value is shared by every run. Expressions can't compile a pattern from a string at run time, so there's no regex cache to size or bound.

### Struct Methods

By default any methods that are on structs which are accessible through the environment will be callable from the expression. For example:

```go
type Customer struct { ... }

func (c *Customer) GetName() string {
    return c.Name
}

env["customer"] = &Customer{ ... }
```

The expression will be able to call `customer.GetName()`.

This is often exactly what you'd want, however there can be circumstances where structs carry methods that are not intended to be available to the authors of the expression (for example a method that mutates the struct).

Disable struct method calling by passing the vm.DisableStructMethods() option when creating the VM, e.g. `owlexpr.NewVM(stdlib.All(), vm.DisableStructMethods())`.

### Iterators

Iterators (or functions that return iterators) can be passed through the environment and used by expressions through the [iter.*](STDLIB.md#iter) package.

When iterators are used in an expression, they will typically consume the iterator to process its data. They can also be used to build a processing pipeline and return it to the embedding application:

```go
env := map[string]any{}
env["myiter"] = slices.Values([]any{1, 2, 3, 4})

ourProgram, _ := owlexpr.Compile("iter.map(myiter, x => x * 2)")
ourMachine, _ := owlexpr.NewVM(stdlib.All())

// result is now a Seq
result, _ := ourMachine.Run(ourProgram, env)
resultSeq := result.(iter.Seq[any])

for val := range resultSeq {
    fmt.Printf("Value: %v\n", val)
}
```
This will output:
```
Value: 2
Value: 4
Value: 6
Value: 8
```

The use of iterators in this fashion is probably limited to use cases where a large volume of data records need to be processed and keeping them in a slice would be wasteful.

Note: The returned Iterator is not safe for concurrent use by multiple goroutines or for concurrent use with the Machine that created it.

#### Error Handling

Pipeline stages like `iter.map` and `iter.filter` return a plain `iter.Seq[any]` closure, which has no way to return an `error` alongside each value. If the lambda passed to a stage fails when called - for example a type error inside `x => x * 2` - the stage `panic`s with an `error` value instead.

When the expression itself calls a terminal operation such as `iter.reduce` or `iter.toList`, that panic is recovered internally and surfaced normally as the `error` returned from `Run`. But when `Run` returns an undriven `Seq` for the embedder to consume later, as in the example above, `Run` has already returned successfully by the time the embedder starts ranging over it - there is nothing left on the call stack to recover the panic. This means a failure encountered while consuming the `Seq` will panic out of the embedder's `for range` loop (or out of a callback invocation, if the `Seq` is driven that way):

```go
func consume(seq iter.Seq[any]) (err error) {
    defer func() {
        if r := recover(); r != nil {
            if e, ok := r.(error); ok {
                err = e
                return
            }
            panic(r) // not an owlexpr error - a real bug, don't swallow it
        }
    }()

    for val := range seq {
        fmt.Printf("Value: %v\n", val)
    }
    return nil
}
```

Only recover an `error`-typed panic value and re-panic anything else, mirroring the convention owlexpr itself uses internally - a non-`error` panic means something has gone wrong outside of expression evaluation, and swallowing it would hide a real bug.

### Callable Results

An expression can itself evaluate to a function rather than a plain value, e.g. an expression that compiles a piece of reusable logic for the embedder to call repeatedly with different inputs. When the final result is a lambda literal, `Run` automatically converts it into an ordinary Go func the embedder can call directly, with no owlexpr-internal type involved:

```go
ourProgram, _ := owlexpr.Compile("x => x * 2")
result, _ := ourMachine.Run(ourProgram, env)

double := result.(func(...any) (any, error))
out, _ := double(int64(21)) // out == int64(42)
```

Note: The returned callable is not safe for concurrent use by multiple goroutines or for concurrent use with the Machine that created it.

### Finding the Names an Expression Uses

`Compile` returns a `vm.Program`, whose `Names` method reports the names the expression looks up in its environment when it runs, sorted and de-duplicated:

```go
ourProgram, _ := owlexpr.Compile(`let limit = max; attributes.Fee ?? round(value * rate) > limit`)
fmt.Println(ourProgram.Names()) // [attributes max rate round value]
```

This is useful when building the environment is expensive - for example when running the same expression over many records, only the values an expression actually uses need to be placed into `env`. It can also be used to check, when an expression is saved, that it only refers to names your application provides.

Names bound inside the expression (lambda parameters and `let` names, such as `limit` above) aren't reported, and only the first identifier of a chain is: `attributes.Fee` reports `attributes`. Builtin functions and namespaces are reported alongside variables (`round` above), as which builtins exist depends on the Machine the program is run on - remove any your Machine provides that you don't expect to supply in `env`.

`vm.Program` is a named `[]vm.Instruction`, so it can be used anywhere the plain slice is expected, and existing instructions can be converted with `vm.Program(instructions)`.

## Minimal Use Case

Expressions can be used for very simple business rule evaluation, e.g. should this order get free shipping: `customer.IsVIP || order.value > 1000`.

When the expression evaluation is being done against simple, flat data like this, there's no need to provide complex capabilities from the standard library or even the [builtin functions](LANGUAGE.md#built-in-functions).

Creating the Machine without the `stdlib.All()` option leaves out the standard library, and the option `vm.DisableBuiltIns()` results in just the core language, with no functions provided.

## Partial standard library

The standard library is fairly broad and includes capabilities that most business users would never need (e.g. `bytes`, `iter`, `json`, `bits`). When calling `NewVM()`, instead of passing `stdlib.All()`, you can select which packages should be made available:

| Function            | Namespace   | Covers                                                      |
| ------------------- | ----------- | ----------------------------------------------------------- |
| `StringBuiltins()`  | `string.*`  | String manipulation                                         |
| `RegexBuiltins()`   | `regex.*`   | Regex capture, find, replace and split                      |
| `ListBuiltins()`    | `list.*`    | List/map processing (map, filter, reduce, sort, ...)        |
| `TimeBuiltins()`    | `time.*`    | Times and durations                                         |
| `DecimalBuiltins()` | `decimal()` | Arbitrary precision numbers                                 |
| `IterBuiltins()`    | `iter.*`    | Lazy, push-iterator pipelines (see [Iterators](#iterators)) |
| `ByteBuiltins()`    | `bytes.*`   | Byte/byte-slice conversion and manipulation                 |
| `BitsBuiltins()`    | `bits.*`    | Bitwise operations                                          |
| `JSONBuiltins()`    | `json.*`    | JSON encode/decode                                          |

`StringBuiltins()` and `ListBuiltins()` will be the most broadly useful, along with `TimeBuiltins()` for handling times and durations and `DecimalBuiltins()` for arbitrary precision numbers.

Each is an independent `vm.VMOption`, so pass as many as you need, e.g. `owlexpr.NewVM(stdlib.StringBuiltins(), stdlib.ListBuiltins())`.

## Type Coercion

By default, Owlexpr will coerce types so that expression authors do not need to spend time learning about them. For example: `decimal("3.50") + 4 + 6.7` will evaluate to a Decimal result of 14.2, without the user needing to explicitly cast one or more operands to decimal.

If your use case for Owlexpr is very sensitive to the types being used, you can disable this coercion by passing `vm.DisableAutoTypeCoercion()`. To disable or change what type coercion is supported on a type-by-type basis, or even selectively remove / change operators, see [extending Owlexpr](EXTEND.md#starting-from-a-clean-slate).

## Concurrency

A `*vm.Machine` is not safe for concurrent use - `Run`/`RunScopes` mutate unsynchronized state on the Machine itself, so two goroutines must not share one. A compiled program (the `[]vm.Instruction` returned by `owlexpr.Compile`) has no such restriction: it's immutable once built, so it's safe to compile once and share across goroutines, each running it through its own `*Machine` (built with the same `vm.VMOption`s passed to `NewVM`).

`*Machine` is relatively cheap to create, so just build one per-goroutine. Note: It is not safe to pass labmda or iterators created in one Machine to another running in a different goroutine.

## Adding new builtin functions and types

The standard library is implemented entirely using the same public interfaces available to embedding applications - there's nothing `stdlib` can do that your own code can't, whether that's a new function like `math.double()` or teaching the VM's operators about a Go type of your own.

See [EXTEND.md](EXTEND.md) for a full walkthrough, from the simplest builtin through to registering new operators.
