# owlexpr Architecture

owlexpr is a small expression language embedded in Go: parse a string once,
compile it to bytecode, and run that bytecode many times against different
environments. This document walks the pipeline end to end — lexer → parser →
constant folder → compiler → VM — starting from the simplest possible
expression and layering in the rest of the system's features (closures,
`let`, `??`, Sheets) as they become relevant.

For the historical reasoning behind individual design decisions (why `in` is
a native operator, why iterators and lists are split, why `matches` compiles
its regex once, and so on), see `DESIGN_NOTES.md`. This document describes
what the system *is*; DESIGN_NOTES.md is the record of how it got there.

## Contents

1. [The pipeline, in one picture](#the-pipeline-in-one-picture)
2. [Stage 1: Lexer](#stage-1-lexer)
3. [Stage 2: Parser](#stage-2-parser)
4. [Stage 3: Constant folding](#stage-3-constant-folding)
5. [Stage 4: Compiler](#stage-4-compiler)
6. [Stage 5: The VM](#stage-5-the-vm)
7. [Type dispatch: TypeCode and Combine](#type-dispatch-typecode-and-combine)
8. [Two scope chains: env vs. lexical locals](#two-scope-chains-env-vs-lexical-locals)
9. [Closures and higher-order builtins](#closures-and-higher-order-builtins)
10. [`??` and `let`: isolated instruction sequences](#-and-let-isolated-instruction-sequences)
11. [Builtins: core, namespaced, and opt-in packs](#builtins-core-namespaced-and-opt-in-packs)
12. [Sheet: a graph of interdependent expressions](#sheet-a-graph-of-interdependent-expressions)
13. [Extensibility surface](#extensibility-surface)
14. [Error handling philosophy](#error-handling-philosophy)
15. [Instruction set reference](#instruction-set-reference)
16. [Package map](#package-map)

## The pipeline, in one picture

```
                         ┌─────────────────────────────────────────────┐
                         │                  package owlexpr             │
                         │                                               │
  "a + b * 2"            │   Lexer ──▶ PrattParser ──▶ Expr (AST)         │
       │                 │                                │              │
       ▼                 │                                ▼              │
  Parse(expr)             │                       foldConstants(ast)      │
                         │                                │              │
                         │                                ▼              │
                         │                       compiler.compile(ast)    │
                         │                                │              │
                         └────────────────────────────────┼──────────────┘
                                                           ▼
                                              []vm.Instruction ("the program")
                                                           │
                         ┌─────────────────────────────────┼──────────────┐
                         │              package vm          ▼              │
                         │                                                 │
                         │   Machine.Run(instructions, env) ──▶ pump()      │
                         │        (a bytecode interpreter, one opcode      │
                         │         at a time, with its own explicit        │
                         │         call stack — see "Stage 5")             │
                         └─────────────────────────────────────────────────┘
```

Two Go packages are involved:

- **`owlexpr`** (repo root) owns the *language*: lexing, parsing, the AST,
  constant folding, compilation to bytecode, the core builtin functions
  (`len`, `map`, `filter`, `reduce`, ...), and the `Sheet` feature.
- **`vm`** owns *execution*: the instruction set, the `Machine` that runs it,
  the type-dispatch system arithmetic and comparisons go through, and the
  extension points (`RegisterOperation`, `RegisterTypeCoder`, ...) that let
  an embedder add new types without touching this package.

The split exists so that `vm.Machine` has no knowledge of owlexpr's
grammar at all — it only knows how to execute an instruction stream. This is
what makes a compiled `[]vm.Instruction` reusable: the same program can run
against many different `Machine` configurations, and a `Machine` has no
per-program state itself, so one `Machine` can run many different programs,
sequentially, without re-paying setup cost.

## Stage 1: Lexer

`Lexer` (parser.go) turns a source string into a flat stream of `Token`s.
It has no lookahead of its own — `PrattParser` is what buffers one token
ahead (`curToken`/`peekToken`) where the grammar needs it.

A few things worth knowing about the token stream it produces:

- **Two-character operators are a closed set** (`twoCharOps`): `==`, `!=`,
  `<=`, `>=`, `&&`, `||`, `**`. Anything else made of operator characters is
  exactly one token, so `1+-2` lexes as `1`, `+`, `-`, `2` — two separate
  unary/binary operators — rather than a meaningless glued `+-` token.
- **`=>` gets its own token type** (`TokArrow`), separate from the general
  operator table, because it only ever appears in one grammatical position
  (a lambda literal) and is recognized there directly rather than through
  the infix binding-power table every other operator uses.
- **Numbers are always `int64` or `float64`**, never plain `int` — a literal
  with a `.` in it is a float, everything else is an int64. This is a fixed
  rule the rest of the system (folding, type inference) can rely on.
- **Comments and whitespace are stripped in the same pass** (`skipWhitespace`
  loops so a comment can be followed by more whitespace, then another
  comment, and so on). `// line` and non-nesting `/* block */` comments are
  both supported; an unterminated `/*` consumes to end of input rather than
  looping forever.

## Stage 2: Parser

owlexpr uses a **Pratt parser** (a.k.a. top-down operator precedence
parsing): one recursive function, `ParseExpression(precedence)`, that
alternates between a *prefix* rule (how to start parsing an expression —
a literal, a variable, a unary operator, a grouping paren, `if`, `let`, ...)
and a loop of *infix/postfix* rules (how to extend an already-parsed
expression — a binary operator, `.member`, `(call)`, `[index]`, `?:`) for as
long as the next token binds more tightly than the precedence floor the
caller passed in.

### Binding power, not a single precedence number

Every infix/postfix operator has a `bindingPower{lbp, rbp}` pair rather than
one shared number:

- **`lbp`** (left binding power) is compared against the caller's precedence
  floor to decide whether the infix loop keeps absorbing tokens into `left`.
- **`rbp`** (right binding power) is the floor passed to the recursive call
  that parses the right-hand operand, and it's what encodes associativity:
  - Left-associative operators set `rbp == lbp`, so a following operator of
    the same precedence is *not* absorbed into the right-hand recursion —
    it falls back out to the enclosing loop, producing `(a+b)+c`.
  - Right-associative operators set `rbp == lbp - 1`, so a following operator
    of the same precedence *is* absorbed into the right-hand recursion,
    producing `a?b:(c?d:e)` for chained ternaries and `2**(3**2)` for `**`.

A single shared precedence number (the more common simplification) works
fine for purely left-associative arithmetic, but silently mis-associates any
operator that needs to be right-associative — which is why this parser keeps
the two numbers separate.

### Resolving genuine ambiguity: lambda parameter lists

A `(` in prefix position is ambiguous until more tokens are seen: `(a, b)`
could be a lambda's parameter list (if `=>` follows) or nothing sensible on
its own — `(a + b)` is unambiguously a grouping expression. Rather than
adding lookahead to the grammar, `tryParseParenLambdaParams` speculatively
parses a parameter list and checks for the following `=>`, restoring the
lexer/parser to its saved position byte-for-byte if that check fails, so the
normal grouping-paren / call-argument-list path runs unaffected. A bare
identifier followed directly by `=>` (`x => expr`) needs no such
backtracking — that shape is unambiguous the moment `=>` is seen.

### AST shape

Every node type is a plain, non-pointer struct implementing the empty
`Expr` interface — there is no visitor pattern; every stage that needs to
walk the tree (`foldConstants`, `compiler.compile`, `sheetReferences`) does
so with its own type switch. This keeps each pass's logic
self-contained at the cost of each of those switches needing to stay
exhaustive as node types are added — `collectSheetReferences`'s `default:
panic(...)` case exists specifically to catch that regression at test time
rather than silently miscounting cell dependencies.

## Stage 3: Constant folding

`foldConstants` (fold.go) runs once, right after parsing, before
compilation, on every `Compile` call. It recursively collapses
any sub-expression built entirely from literals — `60 * 60 * 24`, `"a" +
"b"`, `1 < 2` — into a single literal node, so compiled bytecode never
redoes that arithmetic on every `Run`.

Two properties make this always safe to apply unconditionally:

- **It only ever touches literal sub-trees.** A `VarNode`, `CallNode`,
  `MemberAccessNode`, or anything else that could depend on `env` or have a
  side effect stops the recursion from folding that branch (though it still
  recurses into the *other* operand — see `foldConstants`'s `BinaryOpNode`
  case for the short-circuit/control-flow operators, which never fold the
  node itself but still fold operands for whatever consumes them later).
- **A folding failure just leaves the node unfolded**, as an ordinary
  `BinaryOpNode`/`UnaryOpNode`. Divide-by-zero and unsupported operand
  combinations are *not* compile-time errors here — they fall through to the
  same runtime error path a non-folded expression would hit, so the error
  message points at the right place regardless of whether folding could have
  applied.

Folding hand-implements literal arithmetic rather than routing through a
`*vm.Machine`'s `Combine` — a plain `Compile()` has no `Machine` until
`Run()` is called, and a compiled program is meant to produce the same
result on *any* `Machine` that later runs it. Because of that, folding
deliberately mirrors only the *default* core-type arithmetic
(`vm/operations.go`'s `registerDefaultOperations`) and only ever folds a
pair of **identical**-type literals (`int64+int64`, `float64+float64`,
`string+string`) — never a mixed `int64`/`float64` pair, even though the
default float handler would happily coerce one at runtime. Whether that
coercion is even allowed is a per-`Machine` setting
(`DisableAutoTypeCoercion`), so folding it in unconditionally could bake in
"coercion happened" against a `Machine` that would have rejected it.

## Stage 4: Compiler

`compiler` (compiler.go) walks the (folded) AST and emits a flat
`[]vm.Instruction` — no intermediate IR, no basic blocks; control flow is
just forward/backward jumps computed as the compiler walks. `Compile`
constructs one root `compiler`, but a single top-level call can spin up many
child compilers along the way: every lambda body, every `let` body, and the
left operand of `??` compiles as its own **self-contained instruction
stream** rather than being inlined into the parent (see
[`??` and `let`](#-and-let-isolated-instruction-sequences) for why).

### Emitting expressions

Most node types compile in the obvious way: push operands, then emit the
operator. A few are worth calling out:

- **`&&`/`and` and `||`/`or` short-circuit via jumps**, not via a boolean
  `Combine`, so the right operand is never evaluated (and never even pushed
  and popped) once the left side already determines the answer.
- **`if`/ternary share one code path** (`compileConditional`): evaluate the
  condition, `OpJumpIfFalse` over the "then" branch, unconditional `OpJump`
  past the "else" branch. An `if` with no `else` compiles an implicit
  `OpPush nil` for the missing branch, so the instruction shape is identical
  either way.
- **`matches`' right-hand side must be a string literal.** The compiler
  compiles it into a real `*regexp.Regexp` once, at compile time, embedded
  directly as an `OpPush` constant — there is no runtime `regexp.Compile`
  call and nothing to cache. A non-literal pattern is a compile-time error
  rather than a slower dynamic path.
- **Variable references resolve to one of two instructions** depending on
  whether the compiler can prove, from the lexical structure of the source
  alone, that the name is bound by an enclosing `let` or lambda parameter:
  `OpLoadLocal` (a compile-time-resolved `(depth, slot)` address) if so,
  `OpLoad` (a runtime, string-keyed scope search) otherwise. See
  [Two scope chains](#two-scope-chains-env-vs-lexical-locals).

A short-circuiting "first match" operation is available as `list.find`
(an opt-in `stdlib.ListBuiltins()` builtin — see
[Builtins](#builtins-core-namespaced-and-opt-in-packs)) rather than as a
compiler-level rewrite of `filter(list, fn)[0]`: the compiler has no way to
know, from AST shape alone, which Go function a given call site's callee
will actually resolve to at runtime (`filter` itself can be shadowed or
overridden — see `TestFilterCanBeOverriddenPerVM`), so it can't soundly
assume anything about how many times, or in what pattern, that function
invokes a callable argument. That knowledge — and the buffer-reuse
optimization it enables (see
[Closures and higher-order builtins](#closures-and-higher-order-builtins))
— belongs entirely to whichever builtin actually performs the repeated
invocation.

## Stage 5: The VM

`Machine` (vm/vm.go) executes a `[]vm.Instruction` via `pump`, a **trampoline**:
one loop, one instruction at a time, with no recursive Go function calls
standing in for nested language-level evaluation. This is a deliberate
departure from the more obvious recursive-interpreter design
(`run(instructions, scopes)` calling itself for a lambda body, a `let` body,
or `??`'s left operand) — the VM keeps its own explicit call stack
(`Machine.frames`, a slice of `frame`) instead of using Go's.

### Why an explicit stack instead of Go recursion

Two things fall out of this once state that used to live in Go's own call
stack lives in a plain slice instead:

- **A runaway recursive lambda fails cleanly.** `saveContinuation` checks
  `len(mc.frames)` against `maxFrameDepth` (10000) every time a nested
  sequence is entered, returning an ordinary error instead of letting Go's
  goroutine stack grow until the process dies with an unrecoverable fatal
  error.
- **Every nested evaluation shares one stack.** A builtin that calls back
  into the VM (`filter`/`map`/`reduce` invoking a lambda once per element via
  `ReusableCall`) pushes onto the exact same `mc.frames` an already-running
  evaluation is using, rather than starting some separate, disconnected
  recursion of its own.

### How `pump` actually runs

`pump`'s currently-executing sequence lives in its own local variables —
`instructions`, `scopes`, `locals`, `pc`, `coalesce`, `stackBase` — exactly
as a recursive design would keep them on a Go stack frame. The handful of
places that would otherwise make a nested call (`OpCall` into a closure,
`OpLet`, `OpCoalesce`) instead:

1. Call `saveContinuation`, pushing the current locals onto `mc.frames`.
2. Overwrite the locals in place with the nested sequence's own
   `instructions`/`scopes`/`locals`/`pc=0`.
3. Loop around — the same `for` iteration now executes the nested sequence.

When that nested sequence's `pc` runs past its end (or fails), the saved
locals are restored by popping `mc.frames`, rather than returning from a
call. A flat, unnested expression — the overwhelming common case — never
touches `mc.frames` at all, so this costs nothing extra over a plain
recursive design for the common case, and only pays a slice
append/pop for genuine nesting.

Error unwinding walks the same frame stack: each `frame` records
`stackBase`, the VM value-stack depth when its sequence started, so an error
partway through (e.g. the `a` in `a + 1/0`, stranded on the stack when
`OpDiv` fails) truncates the stack back to a known-clean point rather than
leaking a partial evaluation's leftovers into whatever runs next on a reused
`Machine`. Unwinding stops early exactly when it reaches a frame that is
itself `??`'s left operand (`frame.coalesce != nil`) — see
[`??` and `let`](#-and-let-isolated-instruction-sequences).

## Type dispatch: TypeCode and Combine

Every binary arithmetic/comparison operator (`+`, `-`, `*`, `/`, `%`, `**`,
`<`, `>`, `<=`, `>=`, `==`, `!=`) dispatches through `Machine.Combine`,
which:

1. Classifies both operands into a `TypeCode` via `typeCode` (see below).
2. If both share one `TypeCode`, uses that type's own registered handler.
3. If they differ, checks whether either side's handler declares it can
   *coerce* the other side's type, using whichever one applies (preferring
   the left operand if both could).
4. Fails cleanly if neither of the above applies, or if type coercion has
   been disabled for this `Machine` (`DisableAutoTypeCoercion`) and the types
   differ at all.

`typeCode` itself checks three sources in order, cheapest first:

1. **`GetTypeCode`'s fixed switch** — zero-reflection recognition of the five
   core types (`int`, `int64`, `float64`, `string`, `bool`). This always
   runs first and can never be overridden, so a core type's classification
   is a system-wide invariant.
2. **`extraTypers`** — custom `TypeCoder`s installed via `RegisterTypeCoder`,
   a performance escape hatch for an embedder recognizing many types at once
   via a hand-written type-switch instead of a map lookup.
3. **`dynamicTypes`** — a `reflect.Type`-keyed map of codes auto-assigned by
   `RegisterOperation` for any type nothing above already recognizes.

For the five core types, `Combine` skips scanning the general-purpose
`opHandlers` list and indexes directly into `coreOpHandlers`, a small fixed
array — since arithmetic on the core types is by far the hottest path
through this VM, that fast path matters more than its small size suggests.

## Two scope chains: env vs. lexical locals

A running expression has **two independent chains of bindings**, not one,
and it's worth understanding why they're kept apart:

- **`scopes []map[string]any`** (aliased as `envScopes` on a closure) holds
  the caller-supplied environment (`Run`'s `env` argument, or `Sheet`'s
  per-cell layers via `RunScopes`) — a dynamic, string-keyed dictionary
  whose keys the compiler has no way to know ahead of time. Looked up via
  `lookupScopes`, innermost-first, on every `OpLoad`.
- **`locals *localFrame`** holds bindings the *language itself* introduces:
  a lambda's parameters, or a `let`'s single bound name. Unlike `env`, the
  compiler always knows exactly what these bindings are called and where
  they're used — `compiler.locals`/`resolveLocal` track, at compile time,
  exactly which enclosing frame and slot a name resolves to. That's what
  lets `OpLoadLocal` address a binding by a plain `(Depth, Slot)` pair
  (`LocalRef`) instead of hashing/searching a name at runtime, and what lets
  each new frame be a small `{parent, values}` struct instead of a fresh map
  plus a copied scope-chain slice.

A `let` or lambda call extends `locals` by exactly one frame — never
`scopes`, which a `let`/lambda body always sees unchanged from its caller.
`OpLoadLocal`'s `Depth` counts frame-boundaries outward from the frame active
when the instruction runs (0 = innermost), which is exactly the lexical
addressing scheme from *Structure and Interpretation of Computer Programs*:
resolved once by the compiler, walked (not searched) at runtime.

## Closures and higher-order builtins

A lambda literal (`x => expr`) compiles to `OpMakeClosure`, which at runtime
wraps a compiled `LambdaProto` (parameters + body instructions, immutable,
shared across every call and every `Machine` that ever runs this program)
together with the scope chain active at the point the closure is created —
both `scopes` and `locals`. That capture is what makes `x => y => x + y`
work: the inner lambda's closure includes the outer lambda's own parameter
binding.

Calling a closure normally (`closure.call`, backing `Machine.Call` and
`OpCall`'s general path) allocates a fresh `localFrame` per call. For a loop
that calls the *same* closure many times with different arguments — exactly
the shape `map`/`filter`/`reduce`, `list.find`/`list.all`/etc., and the
`iter.*` combinators all have — that per-call allocation is wasteful, so
those builtins instead drive their calls through `vm.ReusableCall`:

- `NewReusableCall(mc, fn)` takes a **fast path only when safe**: `fn`'s
  body must be provably unable to let a reference to its own parameter
  scope escape the call that's running it (`LambdaProto.NoEscape`, computed
  once at compile time by checking whether the body contains a nested
  closure literal — `ContainsClosureLiteral`). If a lambda's body creates
  and returns another closure, that inner closure captures the outer call's
  `localFrame` by reference, and reusing/overwriting that frame across
  iterations would make every returned closure see only the *last*
  iteration's arguments — a correctness bug, not just a missed optimization.
- On the fast path, each `Call1`/`Call2`/`Call3` overwrites the same
  `localFrame`'s values in place instead of allocating a new one per call.
- For anything else (a `BuiltinFunc`, a bound struct method, a plain Go
  function, or a closure that fails the `NoEscape` check), `ReusableCall`
  transparently falls back to the ordinary `Call` path — a caller never
  needs to type-switch on the callee itself to decide which applies.

Reuse is only sound because of *how* these builtins call through it: each
call is made, and its result fully consumed, before the next one begins —
exactly what a builtin's own single-threaded, one-element-at-a-time loop
already guarantees. That guarantee is inherent to whichever Go function
performs the repeated invocation, not to the call site's AST shape, which
is why this is a builtin-level mechanism (opted into via `vm.ReusableCall`)
rather than something the compiler could arrange by recognizing particular
call shapes — the compiler has no way to know, from a bare `CallNode`,
which function a callee will resolve to at runtime (it may be shadowed or
overridden — see `TestFilterCanBeOverriddenPerVM`), let alone how that
function chooses to invoke a callable argument.

`map`/`filter`/`reduce` accept either a list-like source (called with one
argument per element) or a Go map (called with two, key and value) — see
`IterateListOrMapSource`. A push-iterator source (`iter.Seq`/`iter.Seq2`,
Go 1.23's iterator shape) deliberately does **not** match either shape and
is rejected the same way any other wrong type is. Consuming a push iterator
drains it — a one-way, `env`-visible side effect that a plain `filter`/`map`
call gives no visual signal is happening — so that family is only ever
spelled `iter.map`/`iter.filter`/`iter.reduce`/etc. (see `ForEachSeqElement`,
`IterateSeqSource`, and stdlib's `iter` pack).

## `??` and `let`: isolated instruction sequences

Both `??` (nil-coalescing) and `let` compile their "risky" operand — `??`'s
left-hand side, `let`'s body — as a **separate, self-contained instruction
stream**, run via the same save-continuation/switch-locals mechanism as a
lambda call, rather than being inlined directly into the surrounding
instruction stream.

For `??`, isolation is what lets a failure in the left operand be *caught*
instead of aborting the whole expression: `OpCoalesce`'s frame carries a
`coalesce *CoalesceArg` marker, and both the normal-completion path and the
error-unwind path in `pump` check for it specifically — a non-nil result
falls through past the right-hand operand (compiled inline immediately
after `OpCoalesce`, needing no isolation of its own), while a `nil` result
or a caught error both resume evaluation right at the right-hand operand
instead of propagating further.

For `let`, isolation is what makes the binding's scope airtight with no
separate push/pop bookkeeping: the body runs against `locals` extended by
exactly one new frame, and once that frame's instructions finish, the outer
`locals` pointer — never mutated in the first place — is simply what's
active again. There's no way for the binding to leak into a sibling call
argument or anything after the `let` expression as a whole, because nothing
ever added it to the surrounding sequence's own state to begin with.

## Builtins: core, namespaced, and opt-in packs

A `Machine` has three ways to make a name callable from an expression:

- **`RegisterBuiltin`** — a bare name (`len`, `map`, `abs`, ...).
- **`RegisterNamespacedBuiltin`** — `namespace.name(...)` (`string.trim`,
  `time.parse`). A namespace is nothing more than a
  `map[string]vm.BuiltinFunc`; `OpLoad` pushes it like any other value, and
  the `.name` step resolves through the exact same generic string-keyed-map
  dot-access rule `accessMember` already applies to an ordinary
  `map[string]any` — there is no namespace-specific resolution logic
  anywhere in the VM.
- **Plain Go values in `env`** — a `func`, a bound struct method reached via
  `.Member`, anything `callFunction` can invoke.

`registerBuiltins` (root's builtin_funcs.go) wires up the small set that's
always available with no configuration: `len`, `sum`, `min`, `max`, `map`,
`filter`, `reduce`, `abs`, `ceil`, `floor`, `round`, `type`,
`int`/`int64`/`float64`. Root's `NewVM` injects this as the *first*
`VMOption`, ahead of anything the caller supplies, so `DisableBuiltIns`/
`ClearOperations` can still override the defaults — the `vm` package itself
has no hardcoded knowledge that these particular builtins exist.

Everything beyond that core set is an **opt-in pack** under `stdlib/`
(`StringBuiltins`, `ListBuiltins`, `TimeBuiltins`, `DecimalBuiltins`,
`ByteBuiltins`, `BitsBuiltins`, `JSONBuiltins`, `IterBuiltins`), each a
`vm.VMOption` a caller passes to `NewVM` explicitly. This keeps the always-on
surface small and lets an embedder pull in only the capabilities (and the
type-registration side effects, e.g. `DecimalBuiltins` registering
`decimal.Decimal` arithmetic) their particular use case needs.

## Sheet: a graph of interdependent expressions

`Sheet` (sheet.go) compiles a *set* of named expressions ("cells") that may
reference each other, resolving execution order from those references
rather than requiring the caller to sort them by hand — the same
relationship a single `Compile()`'d `[]vm.Instruction` has to one expression
string, generalized to many expressions with dependencies between them.

- A cell reaches another cell's value only via the reserved `sheet.<name>`
  form (`sheetReferences`, sheet_refs.go) — never a bare name, even one
  identical to a cell's — so a cell can never silently shadow, or be
  shadowed by, an `env` variable of the same name. This scan is
  `let`/lambda-aware: a local binding literally named `sheet` shadows the
  reserved namespace inside its own scope, the same as any other name would.
- `CompileSheet` builds the dependency graph from those references,
  topologically sorts it (`topoSortCells`, DFS postorder — visiting a cell's
  dependencies before appending the cell itself), and rejects duplicate
  names, references to nonexistent cells, and dependency cycles — all at
  compile time, once, not on every `RunSheet` call.
- `RunSheet` evaluates cells in that resolved order. Each cell sees `env`
  plus a small, freshly-allocated map holding only *its own* upstream
  cells' results, exposed under the `sheet` name via `RunScopes`'s layered
  scope chain — so a cell's evaluation cost depends on its own dependency
  count, not on the sheet's total size or `env`'s size. That map is never
  reused across cells specifically because a cell's result could be (or
  contain) a closure that captures it by reference — mutating a shared map
  after handing it to a closure would corrupt that closure's captured state.

## Extensibility surface

An embedder extends a `Machine` without modifying the `vm` package, via
`VMOption`s passed to `NewVM`/`UnconfiguredVM`:

| Function | Adds |
|---|---|
| `RegisterOperation` | Arithmetic/comparison support for a new type (also the unary ops `-`/`abs`/`ceil`/`floor`/`round`, dispatched via `Combine(v, v, op)`) |
| `RegisterTypeCoder` | A custom, composable type-classification rule (a performance escape hatch — `RegisterOperation` alone already auto-assigns working codes) |
| `RegisterFastSliceIterator` | A reflection-free iteration fast path for `[]T` of a registered type, for `map`/`filter`/`reduce`/`in` |
| `RegisterBuiltIn` / `RegisterNamespacedBuiltIn` | A callable function under a bare or `namespace.` name |
| `ClearOperations` | Wipes the operation-handler registry (including the built-in core types) back to empty |
| `DisableBuiltIns` | Wipes the named-function registry |
| `DisableAutoTypeCoercion` | Requires exact `TypeCode` matches on both operands of every operator |

Every one of these composes: multiple independent extensions can each
register their own types/builtins without one clobbering another's, which
is what lets `stdlib`'s packs be simple, independent `VMOption` values
rather than a single monolithic configuration object.

## Error handling philosophy

owlexpr treats "a legitimately-authored expression, given legitimate input,
must never crash the host process" as a hard requirement. Concretely:

- No user-reachable path in the VM lets a `panic` escape a `Run` call.
  `IsComparableKey` and the frame-depth limit exist specifically to convert
  a would-be panic into a returned `error`.
- Every type-dispatch failure, index-out-of-range, division by zero, and
  malformed-input case is a returned `error` with a message aimed at the
  person who wrote the expression, not just whoever's debugging the Go code
  underneath it.
- A `panic` inside this codebase's own logic (e.g. `collectSheetReferences`'s
  `default` case, `literalNode`'s default case) signals a bug in owlexpr
  itself — an AST shape the relevant pass didn't know how to handle — never
  a condition a well-formed user expression could trigger.

## Instruction set reference

| Opcode | Arg | Effect |
|---|---|---|
| `OpPush` | literal value | Push a constant |
| `OpLoad` | name (`string`) | Push `env`/builtin/namespace lookup by name |
| `OpLoadLocal` | `LocalRef{Depth,Slot}` | Push a lexically-resolved `let`/lambda-param binding |
| `OpAccess` / `OpOptAccess` | member (`string`) | `target.member` / `target?.member` |
| `OpAdd` `OpSub` `OpMul` `OpDiv` `OpMod` `OpPow` | — | Binary arithmetic via `Combine` |
| `OpEqual` `OpNotEqual` `OpLess` `OpGreater` `OpLessEq` `OpGreaterEq` | — | Comparisons; `Equal`/`NotEqual` special-case nil directly |
| `OpIn` | — | `needle in haystack` (own handler, not `Combine`) |
| `OpMatches` | — | `str matches <compiled regex>` (own handler, not `Combine`) |
| `OpNeg` / `OpNot` | — | Unary `-` / `!` |
| `OpJump` / `OpJumpIfFalse` | target `pc` (`int`) | Unconditional / conditional jump |
| `OpMakeList` / `OpMakeMap` | count (`int`) | Pop N values (or 2N for map) into a `[]any`/`map[string]any` |
| `OpIndex` / `OpOptIndex` | — | `target[index]` / `target?.[index]` |
| `OpSlice` | — | `target[low:high]` |
| `OpMakeClosure` | `*LambdaProto` | Build a closure over the current scope chain |
| `OpCall` | `CallMetadata{ArgCount}` | Invoke a callable value |
| `OpLet` | `*LetArg{Name, Body}` | Bind one name, run `Body` as an isolated frame |
| `OpCoalesce` | `*CoalesceArg{Left, JumpEnd}` | Run `Left` as an isolated, error/nil-catching frame |
| `OpCoerce` `OpAbs` `OpCeil` `OpFloor` `OpRound` | — | Not emitted by the compiler; dispatch discriminants used internally by builtins/`negateValue` via `Combine(v, v, op)` |

## Package map

| Path | Contents |
|---|---|
| `parser.go` | Lexer, `PrattParser`, AST node types |
| `fold.go` | `foldConstants` and the literal-arithmetic helpers behind it |
| `compiler.go` | AST → `[]vm.Instruction` |
| `builtin_funcs.go` | The always-on core builtins (`len`, `map`, `filter`, `reduce`, ...) |
| `vm_config.go` | Root's `NewVM` wrapper (injects core builtins as the default `VMOption`) |
| `sheet.go`, `sheet_refs.go` | `Sheet`/`CellDef`/`CompileSheet`/`RunSheet` |
| `vm/vm.go` | `Machine`, `pump` (the interpreter loop), `Combine`, closures, reflection-based call/access |
| `vm/reusable_call.go` | `ReusableCall`/`NewReusableCall` - the allocation-reusing call surface `map`/`filter`/`reduce`, `list.*`, and `iter.*` share |
| `vm/bytecode.go` | `OpCode` enum, instruction `Arg` payload types (`LocalRef`, `LambdaProto`, ...) |
| `vm/operations.go` | Default handlers for the five core types |
| `vm/typecode.go` | `TypeCode`, `GetTypeCode`, core/dynamic code ranges |
| `vm/iteration.go` | List/map/iterator source recognition (`ForEachListElement`, `ForEachSeqElement`, ...) |
| `vm/in.go`, `vm/coalesce.go`, `vm/matches.go` | `in`, `??`/`?.`'s nil test, `matches` |
| `vm/vm_config.go` | `VMOption` constructors (`RegisterOperation`, `RegisterTypeCoder`, ...) |
| `stdlib/*.go` | Opt-in builtin packs |
