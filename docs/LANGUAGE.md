# The owlexpr Language

A owlexpr program is a single expression. There are no statements, no
semicolons-as-terminators, no top-level declarations — just one expression
that always evaluates to a value. `let` bindings and `if`/`else` are
expressions too, so they nest and compose like everything else.

This guide covers the core language: literals, operators, variables,
conditionals, `let`, lambdas, and the small set of built-in functions that
are always available. The extended standard library — `string.*`, `list.*`,
`time.*`, `bytes.*`, decimals, and so on — is opt-in and documented
separately in the standard library guide.

## Contents

1. [Literals](#literals)
2. [Variables and the environment](#variables-and-the-environment)
3. [Member access, indexing, and slicing](#member-access-indexing-and-slicing)
4. [Operators](#operators)
5. [Conditionals: `if`/`else` and `?:`](#conditionals-ifelse-and-)
6. [`let` bindings](#let-bindings)
7. [Lambdas and closures](#lambdas-and-closures)
8. [Handling missing and failing values with `??`](#handling-missing-and-failing-values-with-)
9. [Optional chaining with `?.`](#optional-chaining-with-)
10. [`in` and `matches`](#in-and-matches)
11. [Comments](#comments)
12. [Built-in functions](#built-in-functions)
13. [Built-in types](#built-in-types)
14. [Namespaced standard library](#namespaced-standard-library)

## Literals

| Kind | Syntax | Notes |
|---|---|---|
| Integer | `42` | A whole number with no `.` is an `int64`. |
| Float | `3.14` | Any number containing a `.` is a `float64`. |
| String | `"hello"` or `'hello'` | Either quote style works; use one to embed the other without escaping: `'she said "hi"'`. Supports `\n \t \r \\ \" \'`. |
| Boolean | `true`, `false` | Reserved words. |
| Nil | `nil` | Reserved word. |
| List | `[1, 2, 3]`, `[]` | Ordered, mixed-type. |
| Map | `{a: 1, "b": 2}`, `{}` | Keys are a bare identifier or a string literal — not an arbitrary expression. |
| Regex | `` re`^\d+$` `` | A regular expression (Go RE2 syntax), compiled when the expression is compiled — see [`in` and `matches`](#in-and-matches). Raw: no escape processing, so `\d` is written once, not `\\d`. Can't contain a backtick (match one with `\x60`). |

There's no hex or scientific notation, no digit separators (`1_000`), and no
string interpolation — build strings with `+` (see [Operators](#operators)).

## Variables and the environment

A bare name looks itself up, in order: any `let`/lambda-parameter binding in
scope, then the `env` map or struct passed in when the expression runs, then
the registered built-in functions and namespaces.

```
price * quantity
```

Here `price` and `quantity` come straight from the environment. Referencing a
name that isn't defined anywhere is a runtime error.

## Member access, indexing, and slicing

```
user.name              // field/key access
user["name"]           // equivalent, useful when the key is dynamic
items[0]               // list index
items[1:3]             // slice — both ends optional: items[:3], items[1:], items[:]
```

Dot access (`.`) and index access (`[]`) work the same way whether the
underlying value is a `map`, a Go struct passed in from the host, or (for
`[]`) a list. Struct methods are reachable through `.` too, unless the
embedding application has disabled that.

Strings can be indexed and sliced the same way, but by **rune**, not byte —
`"héllo"[1]` is `"é"`, intact, and `len("héllo")` is `5`. This matters mainly
for text with multi-byte characters; plain ASCII behaves exactly as you'd
expect either way.

Out-of-range indices, negative indices, and missing map keys are clean
errors rather than crashes. See [`??`](#handling-missing-and-failing-values-with-)
for how to supply a fallback instead of letting that error propagate.

## Operators

From lowest to highest precedence:

| Precedence | Operators | Associativity |
|---|---|---|
| 1 (lowest) | `? :` (ternary) | right |
| 2 | `??` (nil-coalescing) | right |
| 3 | `\|\|`, `or` | left |
| 4 | `&&`, `and` | left |
| 5 | `==`, `!=` | left |
| 6 | `<`, `>`, `<=`, `>=`, `in`, `not in`, `matches` | left |
| 7 | `+`, `-` | left |
| 8 | `*`, `/`, `%` | left |
| 9 | `**` | right |
| 10 | unary `-`, `+`, `!`, `not` | — |
| 11 (highest) | `.`, `?.`, `(...)` call, `[...]` index | — |

`and`, `or`, and `not` are exact word-form synonyms for `&&`, `||`, and `!` —
same precedence, same behavior, purely a style choice:

```
active && !archived
active and not archived     // identical
```

**Arithmetic** (`+ - * / % **`) works on `int`/`int64`/`float64`, freely
mixed — `3 + 4.0` widens to `7.0`. `/` and `%` return a clean error on
division by zero instead of panicking. `**` is exponentiation and is
right-associative, so `2 ** 3 ** 2` is `2 ** (3 ** 2)` = `512`. Ordinary
precedence rules apply across operators too: `2 * 3 ** 2` is `18`, not `36`.

**`+` on strings concatenates**, but only string with string. Mixing a
string and a number is an error in either order, so a mistake such as
`"5" + 1` fails instead of silently producing `"51"`. Convert the number
explicitly with `str()`:

```
"score: " + str(42)   // "score: 42"
"score: " + 42        // error
```

**Comparisons** (`== != < > <= >=`) work across numeric types the same way
arithmetic does, and compare strings lexically. `==`/`!=` treat `nil`
consistently regardless of exactly how a nil value arrived (a literal `nil`
and a nil pointer coming in from the environment both compare equal to
`nil`).

**Logical `&&`/`||`** genuinely short-circuit — the right-hand side is not
evaluated at all once the result is already known:

```
user != nil && user.role == "admin"   // safe: user.role is never reached if user is nil
```

There are no bitwise operators and no pipe operator.

## Conditionals: `if`/`else` and `?:`

`if`/`else` and the ternary operator are two spellings of the same thing —
pick whichever reads better at the call site:

```
if score >= 90 { "A" } else if score >= 80 { "B" } else { "C" }

score >= 90 ? "A" : score >= 80 ? "B" : "C"
```

Braces are optional for single-expression branches (`if x > 5 "big" else
"small"` is valid). An `if` with no `else` produces `nil` when the condition
is false.

## `let` bindings

`let name = value; body` binds `name` to `value` for the rest of the
expression that follows the `;`:

```
let subtotal = price * quantity;
let discount = subtotal > 100 ? subtotal * 0.1 : 0;
subtotal - discount
```

Bindings chain by nesting (each `let` introduces the scope for everything
after its `;`), are visible only inside that trailing expression, and never
mutate anything outside it — there's no reassignment, and a `let` can shadow
an outer name without affecting it. `;` has no other use in the language; it
only ever appears right after a `let` binding's value.

## Lambdas and closures

```
x => x * 2                 // one parameter, no parens needed
(a, b) => a + b             // multiple parameters
() => 42                    // zero parameters
```

A lambda body is always a single expression (which can itself be an `if`,
`let`, or ternary). Lambdas are real closures — they capture the scope they
were written in, so this works:

```
let addTo = x => y => x + y;
addTo(3)(4)     // 7
```

Lambdas are ordinary values: call them directly, `(x => x + 1)(5)`, or pass
them to a function that expects one:

```
map(items, x => x.price * x.quantity)
filter(items, x => x.price > 100)
reduce(items, (acc, x) => acc + x.price, 0)
```

`map`, `filter`, and `reduce` also accept a map instead of a list, in which
case the lambda receives `(key, value)` pairs (and, for `reduce`, `(acc,
key, value)`):

```
map(inventory, (sku, count) => count * 10)
```

## Handling missing and failing values with `??`

Real data is full of gaps: a field that's `nil`, a lookup that fails, a
divide that might be by zero. `??` is the primary tool for writing an
expression that stays correct anyway — reach for it first, before reaching
for `?.` below.

`left ?? right` evaluates `left`; if that produces `nil` **or fails with any
runtime error at all**, it falls back to `right` instead of letting the
failure propagate:

```
user.nickname ?? user.firstName ?? "Anonymous"
(1 / attempts) ?? 0
config["timeout"] ?? 30
```

This is more than a null check — it's owlexpr's in-expression error
handling. A missing map key, a divide by zero, an out-of-range index, or a
Go function that returned an error are all caught by the left side of `??`
and replaced with the right side. Only the left operand gets this
protection; if the fallback on the right also fails, that failure does
propagate. `??` is right-associative, so `a ?? b ?? c` chains fallbacks
in order until one succeeds.

## Optional chaining with `?.`

`?.` is a narrower, secondary tool for one specific situation: you have a
*chain* of lookups, any one of which might legitimately be `nil`, and you
want to short-circuit the rest of the chain to `nil` rather than erroring —
without wrapping the whole thing in `??`:

```
user?.address?.city
```

If `user` is `nil`, the whole expression is `nil` — it never attempts
`.address`. Each `?.` only guards the single hop it's attached to: it
doesn't retroactively protect a plain `.` that comes after it, and it
doesn't protect anything before it. In most other cases — a single lookup
that might be missing, or a value you want to fall back on rather than pass
through as `nil` — `??` is simpler and reads more clearly than `?.`.

## `in` and `matches`

`in` tests membership. What it checks depends on the right-hand side:

```
2 in [1, 2, 3]           // list: checks elements  -> true
"role" in {"role": "x"}  // map: checks keys, not values -> true
```

Anything else on the right-hand side (a plain scalar, for example) is a
runtime error. Negate with `not in`, either as its own operator or via plain
negation — both are equivalent:

```
"admin" not in user.roles
!("admin" in user.roles)
```

`matches` tests a string against a regular expression. The pattern is
written as a regex literal, `` re`...` ``, or as a string literal — either
way it's compiled once, when the expression is compiled, not on every run,
and an invalid pattern is a compile error:

```
email matches re`^[^@]+@[^@]+$`
code matches "^[A-Z]+-[0-9]+$"
```

Prefer `` re`...` ``: its body is raw, so regex escapes like `\d` and `\.` are
written once — in a string literal they'd need doubling (`"\\d"`).

A regex literal is also an ordinary value, so it can be bound with `let`
and reused, or passed to the `regex.*` functions in the standard library
(capture groups, find, replace, split):

```
let id = re`^[A-Z]+-\d+$`;
filter(codes, c => c matches id)
```

The right-hand side of `matches` can be any expression that produces a
regex value — a `let` binding, or one the application provides — but a
pattern can never be built from a string at run time: `x matches someString`
is a runtime error. The match is unanchored unless the pattern itself
anchors with `^`/`$`, so `` "prefix-code" matches re`code` `` is `true`.

## Comments

```
// a line comment

/* a block
   comment */
```

Block comments don't nest. Comments can appear anywhere whitespace can.

## Built-in functions

These are always available with no namespace prefix (an embedding
application can disable them, but by default they're on):

| Function | Description |
|---|---|
| `len(x)` | Length of a string (in runes), list, or map. |
| `sum(list)` / `sum(a, b, ...)` | Sum of a list, or of variadic arguments directly. |
| `min(...)`, `max(...)` | Smallest/largest of a list or of variadic arguments. |
| `map(list_or_map, fn)` | Transform each element (or `(k, v)` pair) with `fn`. |
| `filter(list_or_map, fn)` | Keep elements (or pairs) where `fn` returns `true`. |
| `reduce(list_or_map, fn, initial)` | Fold to a single value with an accumulator. |
| `abs(x)`, `ceil(x)`, `floor(x)`, `round(x)` | Standard numeric functions. |
| `type(x)` | The runtime type name of a value, as a string (`"int"`, `"string"`, `"list"`, ...). |
| `int(x)`, `int64(x)`, `float64(x)` | Explicit numeric conversion, including from numeric strings. |
| `str(x)` | Convert any value to a string (`nil` becomes `""`, not `"nil"`). |

## Built-in types

The core language understands: `int`, `int64`, `float64`, `string`, `bool`,
`nil`, list, map, regex (from a `` re`...` `` literal), and function (a
lambda, or any callable value from the environment). Any Go value from the environment — a struct, slice, or map
not already covered above — is also usable directly wherever a value of its
shape is expected.

## Namespaced standard library

Dot access also reaches namespaced built-in packs, when an embedding
application opts into them — `string.*`, `list.*`, `time.*`, `bytes.*`, and
more:

```
string.trim(name)
list.sort(scores)
time.now()
```

Syntactically these are ordinary member access on a namespace value; there's
nothing special about the dot here. The full set of namespaced functions,
and the additional types they bring with them (decimals, byte buffers,
timestamps), are covered in the standard library guide.
