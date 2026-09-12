# Extending Owlexpr

Owlexpr has two independent extensibility surfaces, and this guide covers both:

- **New builtin functions** - `greet()`, `math.double()` - callable from an expression the same way `len()` or `string.trim()` are.
- **New native types** - teaching the VM's operators (`+`, `-`, `==`, `<`, ...) how to work with a Go type of your own, the same way it already knows `int64 + float64`.

Every extension point here is a `vm.VMOption` passed to `owlexpr.NewVM(...)`, exactly like the `stdlib` packs covered in [EMBED.md](EMBED.md) - in fact `stdlib` is written entirely in terms of these same public functions, with no special access of its own. The quickest way to see a fully worked pack is to read one, e.g. `stdlib/decimal_builtins.go` or `stdlib/time_builtins.go`; this guide builds up to that level of code, one concept at a time.

For a one-page reference table of every extension function, see [Extensibility surface](ARCHITECTURE.md#extensibility-surface) in ARCHITECTURE.md.

## Part 1: Builtin functions

### The simplest builtin

A builtin is any Go value of type `vm.BuiltinFunc`:

```go
type BuiltinFunc func(mc *vm.Machine, args ...any) (any, error)
```

`mc` is the running `*vm.Machine` - it's what gives a builtin access to everything else in this guide (`Combine`, `Call`, `CoercValue`, ...). `args` are whatever the expression passed at the call site, already evaluated to plain Go values.

```go
func greetFunc(mc *vm.Machine, args ...any) (any, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("greet() expects 1 argument, got %d", len(args))
    }
    name, ok := args[0].(string)
    if !ok {
        return nil, fmt.Errorf("greet(): argument must be a string, got %T", args[0])
    }
    return "Hello, " + name + "!", nil
}

mc, _ := owlexpr.NewVM(vm.RegisterBuiltIn("greet", greetFunc))
```

The expression `greet("World")` now evaluates to `"Hello, World!"`. `vm.RegisterBuiltIn` is a `VMOption`, so it composes with `stdlib.All()` and any other option the same way every option in [EMBED.md](EMBED.md) does.

### Argument checking and error conventions

There's no schema or arity declaration - a builtin validates its own `args` by hand, and the standard library's own functions all follow the same shape, worth matching so error messages stay consistent for expression authors:

- **Arity**: `if len(args) != N { return nil, fmt.Errorf("name() expects N argument(s), got %d", len(args)) }` - one args-are-wrong error, checked first, before touching any individual argument.
- **Type**: `fmt.Errorf("name(): argument N must be a T, got %T", args[N])` - `%T` gives the caller the concrete Go type they actually passed, which is usually enough to spot the mistake without needing owlexpr's own type names.
- **Every message is prefixed with the function's own name** (`"name(): ..."`), including when a lower-level error is wrapped with `%w` (`fmt.Errorf("name(): %w", err)`) - a caller three builtins deep in a composed expression can otherwise get an error with no idea which call actually failed.

A small helper is often worth it once a pack has more than a couple of functions - e.g. `stdlib/string_builtins.go`'s `argString(fnName string, args []any, i int) (string, error)`, which folds the bounds check and the type assertion into one call every function in that file reuses.

### Accepting a list or spread arguments

Several core builtins (`sum`, `min`, `max`) can be called two ways: `sum([1, 2, 3])` or `sum(1, 2, 3)`. The convention is simple - if there's exactly one argument, try it as a list first, and only fall back to treating `args` itself as the value list if that fails:

```go
func avgFunc(mc *vm.Machine, args ...any) (any, error) {
    values := args
    if len(args) == 1 {
        if elems, ok := owlexpr.ListElements(args[0]); ok {
            values = elems
        }
    }
    if len(values) == 0 {
        return nil, errors.New("avg(): expects at least 1 value")
    }
    ...
}
```

`owlexpr.ListElements(v)` is the exported helper behind this: it returns `v`'s elements as a `[]any` (true) if `v` is list-like - a `[]any` from a list literal, or any other slice/array type reached via reflection (a `[]int64` from the environment, say) - and `(nil, false)` otherwise. It's the same function `stdlib`'s own list-accepting builtins use (see `stdlib/list_builtins.go`), so a new pack gets identical list-recognition behavior for free rather than reimplementing the reflection fallback.

### Namespacing

A builtin doesn't have to live in the bare, global name (`greet`, `sum`) - `vm.RegisterNamespacedBuiltIn(namespace, name, fn)` registers it as `namespace.name(...)` instead, e.g. `math.double(21)`:

```go
var namespacedMathFuncs = map[string]vm.BuiltinFunc{
    "double": doubleFunc,
    "square": squareFunc,
}

func MathBuiltins() vm.VMOption {
    return func(mc *vm.Machine) error {
        for name, fn := range namespacedMathFuncs {
            mc.RegisterNamespacedBuiltin("math", name, fn)
        }
        return nil
    }
}
```

This is the shape every pack in `stdlib` actually follows: one `FooBuiltins() vm.VMOption` constructor per pack, registering a `map[string]vm.BuiltinFunc` of its functions under one namespace, rather than a boolean flag bolted onto an existing pack. Following it keeps a new pack a self-contained, optional `VMOption` an embedder can add or leave out, exactly like `stdlib.StringBuiltins()`.

Namespacing also sidesteps name collisions between independent packs - `list.reverse` and `string.reverse` can coexist under their own namespaces where a bare `reverse` couldn't belong to both. A namespace is never itself callable (`math` alone isn't a function) - only `math.double(...)` resolves, through the same ordinary member-access (`.name`) the language already uses for everything else.

### Operating generically across types: `mc.Combine`

A builtin like `sum` needs `+` to work whether the list holds `int64`, `float64`, a `decimal.Decimal` from `stdlib.DecimalBuiltins()`, or some type an embedder registered themselves - without `sum`'s own code knowing any of those types exist. That's what `mc.Combine(a, b, op)` is for: it runs the *exact* type-dispatch and coercion machinery the compiled `+`/`-`/`==`/... operators use (see Part 2 below for how a type gets registered into it), so a builtin written against `Combine` automatically supports every type anyone has ever registered on that `Machine`, with zero changes:

```go
func avgFunc(mc *vm.Machine, args ...any) (any, error) {
    // ... values, as above ...
    var total any = int64(0)
    for _, v := range values {
        var err error
        total, err = mc.Combine(total, v, vm.OpAdd)
        if err != nil {
            return nil, fmt.Errorf("avg(): %w", err)
        }
    }
    // Divide as float64 so avg([1, 2]) is 1.5, not integer-divided to 1 -
    // CoercValue converts total explicitly, the same mechanism `??`'s
    // implicit numeric promotion and stdlib's decimal() function both use.
    floatTotal, err := mc.CoercValue(total, float64(0))
    if err != nil {
        return nil, fmt.Errorf("avg(): %w", err)
    }
    return mc.Combine(floatTotal, float64(len(values)), vm.OpDiv)
}
```

`mc.CoercValue(v, exampleOfTargetType)` is `Combine`'s explicit-conversion sibling: it's `Combine(v, exampleOfTargetType, vm.OpCoerce)` under the hood, used whenever a builtin needs "convert this to that type" rather than "combine these two values" - `stdlib`'s own `decimal(x)` function is implemented as nothing more than `mc.CoercValue(args[0], decimal.Zero)`.

### Calling back into expression-supplied functions

An argument can itself be something callable from the expression - a lambda literal (`x => x * 2`), a bound struct method, or another builtin - passed in because the expression author wrote one at the call site (`items.find(x => x.Active)`) or because the environment supplied it. `mc.Call(callee, args)` invokes any of these uniformly, the same call path `OpCall` itself uses:

```go
func retryFunc(mc *vm.Machine, args ...any) (any, error) {
    if len(args) != 2 {
        return nil, fmt.Errorf("retry() expects 2 arguments (fn, n), got %d", len(args))
    }
    fn := args[0]
    n, ok := args[1].(int64)
    if !ok {
        return nil, fmt.Errorf("retry(): n must be an int, got %T", args[1])
    }
    var lastErr error
    for i := int64(0); i < n; i++ {
        result, err := mc.Call(fn, nil)
        if err == nil {
            return result, nil
        }
        lastErr = err
    }
    return nil, fmt.Errorf("retry(): all attempts failed: %w", lastErr)
}
```

`retry(() => riskyLookup(id), 3)` now calls the lambda up to three times from inside the builtin. `vm.IsCallable(v)` is available if a builtin needs to check ahead of time whether a value can be called at all, rather than just calling it and handling the error.

### Accepting a list or a map

`vm.IterateListOrMapSource(mc, source, onElement, onPair)` is what lets a builtin like `map`/`filter`/`reduce` accept either shape and adapt its callback arity automatically - `onElement` runs for a list-like source, `onPair` for a map-like one, and it stops (`ok == false`) if `source` is neither:

```go
func countIfFunc(mc *vm.Machine, args ...any) (any, error) {
    if len(args) != 2 {
        return nil, fmt.Errorf("countIf() expects 2 arguments (source, fn), got %d", len(args))
    }
    call := vm.NewReusableCall(mc, args[1]) // see the next section
    var n int64
    var callErr error
    ok := vm.IterateListOrMapSource(mc, args[0],
        func(el any) bool {
            res, err := call.Call1(el)
            if err != nil {
                callErr = err
                return false
            }
            if match, _ := res.(bool); match {
                n++
            }
            return true
        },
        func(k, v any) bool {
            res, err := call.Call2(k, v)
            if err != nil {
                callErr = err
                return false
            }
            if match, _ := res.(bool); match {
                n++
            }
            return true
        },
    )
    if !ok {
        return nil, fmt.Errorf("countIf(): first argument must be a list or map, got %T", args[0])
    }
    if callErr != nil {
        return nil, fmt.Errorf("countIf(): %w", callErr)
    }
    return n, nil
}
```

`countIf(nums, x => x > 2)` and `countIf(scores, (k, v) => v > 1)` both work through the one function. Either callback returning `false` stops the scan early - useful for a short-circuiting builtin like `any`/`find`, which don't need to visit every remaining element once they already have their answer.

`stdlib`'s `iter.*` pack (`IterBuiltins()`) has an iterator-only counterpart to this - `vm.IterateSeqSource`/`ForEachSeqElement`/`ForEachSeq2Pair` - deliberately kept separate from the list/map functions above: consuming a Go push iterator is a one-way, non-rewindable operation, so `stdlib` reserves that specifically for the `iter.` namespace rather than letting it happen implicitly inside `list.*`. Worth knowing about if your own pack needs to accept an `iter.Seq`, but not something most builtins need.

### Advanced: `vm.NewReusableCall`

`mc.Call` is the right tool for calling a function once. A builtin that calls the *same* function many times in a row - once per list element, the shape `map`/`filter`/`reduce`/`countIf` above all have - pays an avoidable cost doing that through `mc.Call` on every single element (a fresh argument slice and call frame each time). `vm.NewReusableCall(mc, fn)` sets up the call once, and its `Call1`/`Call2`/`Call3` methods (matching how many arguments `fn` needs) reuse the same buffer across every element:

```go
func squareAllFunc(mc *vm.Machine, args ...any) (any, error) {
    if len(args) != 2 {
        return nil, fmt.Errorf("squareAll() expects 2 arguments (list, fn), got %d", len(args))
    }
    elems, ok := owlexpr.ListElements(args[0])
    if !ok {
        return nil, fmt.Errorf("squareAll(): first argument must be a list, got %T", args[0])
    }

    // Set up once for the whole call, not once per element.
    call := vm.NewReusableCall(mc, args[1])

    out := make([]any, len(elems))
    for i, el := range elems {
        res, err := call.Call1(el)
        if err != nil {
            return nil, fmt.Errorf("squareAll(): %w", err)
        }
        out[i] = res
    }
    return out, nil
}
```

The one rule that makes the reuse safe: every call through a given `*vm.ReusableCall` must fully finish (result read, error checked) before the next one starts. An ordinary element-by-element loop like the one above already satisfies that automatically - just never hand the same `*vm.ReusableCall` to something that could call back into it from a second, overlapping call stack.

A more advanced pattern - a builtin that returns a lazily-evaluated pipeline rather than an immediate result, deferring the actual `ReusableCall` invocations until something later pulls from it, with errors surfaced through a `panic`/`recover` channel rather than an ordinary return (the pipeline closure has no return-error slot to put one in) - is what `stdlib`'s `iter.map`/`iter.filter` are built on. It's only needed for that specific "return an unevaluated pipeline" shape; see `stdlib/iter_builtins.go`'s `iterMapFunc` and `recoverIterErr` doc comments for the full pattern if you're building something similar.

## Part 2: New types

Nothing above required registering a type - `args[0].(string)`, `owlexpr.ListElements`, and friends work against any Go value already, since they inspect it directly rather than going through a lookup table. Type registration is only needed for one specific thing: teaching the VM's *operators* (`+`, `-`, `==`, `<`, `-x`, `abs()`, ...) how to handle a Go type of your own, the way they already handle `int64`, `float64`, and `string`.

### The basic call: `RegisterOperation`

```go
type Celsius float64

func celsiusOperations(a, b any, aTypeCode, bTypeCode vm.TypeCode, op vm.OpCode) (any, error) {
    aVal, bVal := a.(Celsius), b.(Celsius)
    switch op {
    case vm.OpAdd:
        return aVal + bVal, nil
    case vm.OpSub:
        return aVal - bVal, nil
    case vm.OpEqual:
        return aVal == bVal, nil
    case vm.OpLess:
        return aVal < bVal, nil
    case vm.OpGreater:
        return aVal > bVal, nil
    }
    return nil, errors.ErrUnsupported
}

mc, _ := owlexpr.NewVM(vm.RegisterOperation(Celsius(0), nil, celsiusOperations))
```

`c1 + c2`, `c1 < c2`, etc. now work for any `env`/argument value of type `Celsius`. `RegisterOperation`'s first argument is just a *sample* value - used once, at registration time, to identify the Go type being registered - not a value that ends up in any expression. The `nil` second argument means "no coercion with any other type": `c1 + 5.0` would still be a type error.

### Adding coercion

The second argument lists other *sample* values `Celsius` should be allowed to combine with - each one resolved to its own `TypeCode` the same way the base type is:

```go
vm.RegisterOperation(Celsius(0), []any{float64(0)}, celsiusOperations)
```

Now `c1 + 5.0` is legal too, and `celsiusOperations` needs to actually handle a `float64` arriving as `a` or `b`:

```go
func celsiusOperations(a, b any, aTypeCode, bTypeCode vm.TypeCode, op vm.OpCode) (any, error) {
    var aVal, bVal float64
    if c, ok := a.(Celsius); ok {
        aVal = float64(c)
    } else {
        aVal = a.(float64)
    }
    if c, ok := b.(Celsius); ok {
        bVal = float64(c)
    } else {
        bVal = b.(float64)
    }
    switch op {
    case vm.OpAdd:
        return Celsius(aVal + bVal), nil
    // ...
    }
    return nil, errors.ErrUnsupported
}
```

### The tricky part: `a`/`b` aren't fixed to "mine" and "theirs"

`celsiusOperations` is registered once, but the VM's dispatcher (`Combine`) calls it for *both* `c1 + 5.0` and `5.0 + c1` - `a` and `b` arrive in whatever left-to-right order the expression actually wrote them, never rearranged so "the registered type" is always `a`. A handler that assumes `a.(Celsius)` always succeeds will panic the moment someone writes the coercible operand first. That's why the pattern above checks each side independently (`if c, ok := a.(Celsius); ok { ... } else { ... }`) rather than asserting either one outright.

This matters even more once *two different* registered types share one handler, so that both directions work between them - `stdlib`'s `time.Time`/`time.Duration` support is the real example: `timeDurationOperations` is registered once for `time.Time` (coercible with `time.Duration`) and again for `time.Duration` (coercible with `time.Time`), and detects which operand is which itself:

```go
func timeDurationOperations(a, b any, aTypeCode, bTypeCode vm.TypeCode, op vm.OpCode) (any, error) {
    aTime, aIsTime := a.(time.Time)
    bTime, bIsTime := b.(time.Time)

    switch {
    case aIsTime && bIsTime: // time - time -> duration, time == time, ...
        ...
    case aIsTime && !bIsTime: // time +/- duration -> time
        bDur := b.(time.Duration)
        ...
    case !aIsTime && bIsTime: // duration + time -> time
        aDur := a.(time.Duration)
        ...
    default: // duration +/- duration -> duration, ...
        aDur, bDur := a.(time.Duration), b.(time.Duration)
        ...
    }
}
```

The rule of thumb: a Go type assertion (`a.(YourType)`) is almost always the simplest, most robust way to identify *your own* registered type inside a handler - it's exactly what every real handler in `stdlib` does. Treat `aTypeCode`/`bTypeCode` as useful for one specific thing instead: telling apart the five *core* types (compare against `vm.IntTypeCode`, `vm.Int64TypeCode`, `vm.FloatTypeCode`, `vm.StringTypeCode`, `vm.BoolTypeCode` - fixed constants, guaranteed stable), which is why `decimalOperations` in `stdlib/decimal_builtins.go` switches on `aTypeCode`/`bTypeCode` to decide whether the other operand is an `int`, an `int64`, a `float64`, or a `string`.

**Do not invent your own `TypeCode` constant and compare against it** (`const celsiusTypeCode vm.TypeCode = 2000`, say) unless you've also called `vm.RegisterTypeCoder` (below) to make that exact value what the VM actually assigns - otherwise `RegisterOperation` silently auto-assigns some *other*, unpredictable code the first time it sees your type, your comparison against 2000 never matches, and every operation on your type quietly falls through as if the operand were zero. A plain type assertion has no such trap.

### `OpCoerce`

Both `Combine` and `CoercValue` route through the same dispatcher; `CoercValue(v, exampleOfTarget)` is exactly `Combine(v, exampleOfTarget, vm.OpCoerce)`. Two things are worth knowing before writing an `OpCoerce` case:

- If `a` and `b` already have the *same* `TypeCode`, the VM returns `a` unchanged without ever calling your handler - "convert X to X" is always a no-op, whether or not your handler happens to implement `OpCoerce` at all.
- Otherwise, your handler's `OpCoerce` case is asked to convert `a` to whatever type `b` represents - `b`/`bVal` itself is normally just that example value (a `decimal.Decimal` zero value, a `""`), not something to compute with, so inspect `bTypeCode` to decide the target rather than `b`'s actual value.

### Unary operators: `OpNeg`/`OpAbs`/`OpCeil`/`OpFloor`/`OpRound`

There's no separate registration call for unary operators - `-x`, `abs(x)`, `ceil(x)`, `floor(x)`, and `round(x)` all compile down to `mc.Combine(x, x, op)`, passing the *same* value as both operands. Since that guarantees `aType == bType`, the dispatcher takes the direct same-type path straight to your existing handler - so unary support is just more `case`s in the handler you already wrote for `+`/`-`/`OpCoerce`, not a second mechanism:

```go
case vm.OpNeg:
    return Celsius(-aVal), nil
case vm.OpAbs:
    if aVal < 0 {
        return Celsius(-aVal), nil
    }
    return Celsius(aVal), nil
```

`-c1` and `abs(c1)` now work with no further registration.

### `RegisterTypeCoder`: a performance escape hatch

By default, a type registered via `RegisterOperation` alone gets an auto-assigned `TypeCode` the first time the VM sees it - fine for a handful of types, since recognizing one costs a `reflect.TypeOf`-keyed map lookup. `vm.RegisterTypeCoder(coder TypeCoder, id uuid.UUID)` installs a hand-written `func(a any) TypeCode` instead - a plain Go type-switch, cheaper per call than the map lookup, and composable: multiple independent `RegisterTypeCoder` calls (from unrelated extensions) each contribute recognition for their own types without clobbering one another. `stdlib` uses this for exactly the reason its own doc comment gives: several packs (`decimal.Decimal`, `time.Time`, `time.Duration`, ...) share one `stdLibraryTypeCoder`, registered once, ahead of any `RegisterOperation` call, so all of them get fixed, known `TypeCode`s rather than whatever got auto-assigned first. Write your own only once profiling shows the auto-assigned path actually costs something measurable - `RegisterOperation` alone is the simpler default and is what every example above uses.

### `RegisterFastSliceIterator`: a performance escape hatch for slices

`map`/`filter`/`reduce`/`in` over a `[]YourType` still work with nothing beyond `RegisterOperation` - they fall back to a generic, reflection-per-element loop. `vm.RegisterFastSliceIterator(baseType, iter FastSliceIterator)` plugs in a single bulk type assertion (`slice.([]YourType)`) instead, paid once per call rather than once per element - worth adding once a list of your type is large enough (DESIGN_NOTES.md benchmarks the crossover around 50-100 elements) for that per-element reflection to actually show up. Entirely optional - correctness doesn't depend on it.

### Starting from a clean slate

`vm.ClearOperations()` wipes the entire operator registry, including support for the five core types (`int`, `int64`, `float64`, `string`, `bool`) - useful for replacing the defaults rather than only adding to them, e.g. registering `int64` with an empty coercible-types list to get integer arithmetic with no automatic promotion to `float64`. `vm.DisableBuiltIns()` is its named-function equivalent, covered in [EMBED.md](EMBED.md#minimal-use-case).

## Where to go next

- [ARCHITECTURE.md#extensibility-surface](ARCHITECTURE.md#extensibility-surface) - the one-page table version of everything above.
- `stdlib/decimal_builtins.go` - the shortest complete real pack: one type, full coercion, all five unary ops, a fast slice iterator, and one namespaced conversion function.
- `stdlib/time_builtins.go` - two types sharing operator support in both directions (the `timeDurationOperations` example above), plus why it needed dynamic `TypeCode` registration (see DESIGN_NOTES.md's "Dynamic TypeCode registration").
- `stdlib/iter_builtins.go` - the lazy-pipeline/`panic`-`recover` pattern mentioned at the end of Part 1.
