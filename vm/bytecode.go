package vm

type OpCode byte

const (
	OpPush OpCode = iota
	OpLoad
	// OpLoadLocal loads a lexically-resolved binding - a lambda parameter
	// or a let's bound name - by its compile-time-computed (Depth, Slot)
	// address (see LocalRef) instead of OpLoad's dynamic, string-keyed
	// scope-chain search. compiler.go emits this instead of OpLoad for any
	// VarNode it can prove refers to an enclosing lambda param or let;
	// OpLoad is reserved for everything else a name could mean (a
	// top-level env variable, a builtin, a namespace) - see
	// Machine.localFrame's doc comment in vm.go for the run-time side of
	// this split.
	OpLoadLocal
	OpAccess
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpPow
	// OpCoerce is used to support coercion as a Machine service
	// Not used by the VM run loop, only used to trigger operationsRouter directly
	OpCoerce
	OpCall
	// Comparison Operators
	OpEqual
	OpNotEqual
	OpLess
	OpGreater
	OpLessEq
	OpGreaterEq
	// OpIn implements the `in` operator (membership: value in list, or key
	// in map). It's not part of the operationsRouter/Combine dispatch used
	// for the other binary operators above - it has its own handler in
	// run() since it needs to iterate/look up into the right-hand operand
	// rather than combine two same-domain values.
	OpIn
	// OpMatches implements the `matches` operator (`str matches "regex"`).
	// Like OpIn, it's not part of the operationsRouter/Combine dispatch -
	// there's no third-party type that would ever want to extend "what can
	// appear on the right of matches" the way numeric types extend +/-.
	// Its right-hand operand must be a *Regex, compiled before Run ever
	// starts: a string or regex literal pattern is compiled once, at
	// Compile() time, and embedded directly as an OpPush constant (see
	// compiler.go); any other right-hand expression must evaluate to a
	// *Regex value (a let-bound regex literal, an env value built with
	// NewRegex). So there is no runtime regex compilation and no cache to
	// manage - a plain string reaching OpMatches at run time is an error,
	// not a slower runtime-compiled path.
	OpMatches
	// Control Flow Jump Instructions
	OpJumpIfFalse
	OpJump
	// Unary Operators
	OpNeg
	OpNot
	// Collection Operators
	OpMakeList
	OpMakeMap
	OpIndex
	// OpSlice implements target[low:high]. Low/high on the stack are
	// either an int/int64 bound or nil (meaning "start"/"end").
	OpSlice
	// OpMakeClosure builds a callable closure value from a LambdaProto,
	// capturing the scope chain active at the point it executes.
	OpMakeClosure
	// OpCoalesce implements the `??` operator. Its Arg (a *CoalesceArg)
	// carries the left operand's self-contained instructions - run in
	// isolation, as their own frame on the VM's explicit call stack (see
	// vm.go's frame doc comment), so an error there can be caught rather
	// than aborting the enclosing expression - and the jump target to
	// skip the right-hand operand (compiled inline, immediately
	// following) once the left operand already produced a good result.
	OpCoalesce
	// OpLet implements `let name = value; body`. It pops the already-
	// evaluated value off the stack, then runs body's instructions as
	// their own frame on the VM's explicit call stack (see vm.go's frame
	// doc comment) against the current scope chain plus one new
	// {name: value} scope on top.
	OpLet
	// OpOptAccess implements "target?.member": like OpAccess, but a nil
	// (or typed-nil, per IsNilResult) target pushes nil instead of calling
	// accessMember. Unlike OpCoalesce, this needs no sub-instruction
	// isolation or jump - the target is unconditionally evaluated either
	// way, so the nil check is a plain runtime branch inside the one
	// instruction.
	OpOptAccess
	// OpOptIndex implements "target?.[index]": like OpIndex, but a nil
	// target pushes nil instead of calling indexValue. The index
	// expression is still evaluated unconditionally before the check (it's
	// already on the stack by the time this instruction runs) - fine since
	// owlexpr expressions are pure, so there's no observable side effect
	// to skip.
	OpOptIndex
	// OpAbs, OpCeil, OpFloor, and OpRound, like OpCoerce, are not used by
	// the VM run loop - there's no dedicated bytecode instruction for
	// abs()/ceil()/floor()/round(), which compile as ordinary OpCall
	// builtin calls. These exist purely as operationsRouter/Combine
	// dispatch discriminants: a builtin calls mc.Combine(a, a, OpAbs) (the
	// same value as both operands, guaranteeing aType == bType so
	// operationDispatcher takes its direct single-handler branch, the same
	// trick OpCoerce already relies on) once its own hardcoded fast path
	// for int/int64/float64 doesn't match, letting any type registered via
	// RegisterOperation (decimal.Decimal, say) support these ops by adding
	// a case to the same handler it already wrote for +/-/OpCoerce,
	// instead of a second, unary-specific registration mechanism.
	OpAbs
	OpCeil
	OpFloor
	OpRound
)

func (op OpCode) String() string {
	switch op {
	case OpPush:
		return "OpPush"
	case OpLoad:
		return "OpLoad"
	case OpLoadLocal:
		return "OpLoadLocal"
	case OpAccess:
		return "OpAccess"
	case OpAdd:
		return "OpAdd"
	case OpSub:
		return "OpSub"
	case OpMul:
		return "OpMul"
	case OpDiv:
		return "OpDiv"
	case OpMod:
		return "OpMod"
	case OpPow:
		return "OpPow"
	case OpCoerce:
		return "OpCoerce"
	case OpCall:
		return "OpCall"
	case OpEqual:
		return "OpEqual"
	case OpNotEqual:
		return "OpNotEqual"
	case OpLess:
		return "OpLess"
	case OpGreater:
		return "OpGreater"
	case OpLessEq:
		return "OpLessEq"
	case OpGreaterEq:
		return "OpGreaterEq"
	case OpIn:
		return "OpIn"
	case OpMatches:
		return "OpMatches"
	case OpJumpIfFalse:
		return "OpJumpIfFalse"
	case OpJump:
		return "OpJump"
	case OpNeg:
		return "OpNeg"
	case OpNot:
		return "OpNot"
	case OpMakeList:
		return "OpMakeList"
	case OpMakeMap:
		return "OpMakeMap"
	case OpIndex:
		return "OpIndex"
	case OpSlice:
		return "OpSlice"
	case OpMakeClosure:
		return "OpMakeClosure"
	case OpCoalesce:
		return "OpCoalesce"
	case OpLet:
		return "OpLet"
	case OpOptAccess:
		return "OpOptAccess"
	case OpOptIndex:
		return "OpOptIndex"
	case OpAbs:
		return "OpAbs"
	case OpCeil:
		return "OpCeil"
	case OpFloor:
		return "OpFloor"
	case OpRound:
		return "OpRound"
	default:
		return "OpUnknown"
	}
}

type CallMetadata struct {
	ArgCount int
}

// LocalRef is OpLoadLocal's instruction argument: a lexical address for a
// binding introduced by an enclosing lambda parameter list or let,
// resolved once by compiler.go at compile time instead of by name at run
// time. Depth counts frame-boundaries outward from the frame active when
// this instruction runs (0 = the innermost lambda/let body, 1 = its
// immediate lexical parent, ...) and Slot is the binding's position
// within that frame's values - a lambda's params keep their declaration
// order, a let's frame always has exactly one slot. See
// Machine.localFrame's doc comment in vm.go for the run-time chain this
// walks.
type LocalRef struct {
	Depth int
	Slot  int
}

// LambdaProto is the compiled, immutable form of a lambda literal: its
// parameter names and the bytecode for evaluating its body. It carries no
// runtime state (no captured env) - that happens when OpMakeClosure runs
// and wraps a LambdaProto together with the scope chain in effect at that
// point into a closure (see vm.go). Exported because root's Compiler
// (package owlexpr) constructs these directly when compiling a lambda
// literal.
//
// Immutable is a real, load-bearing property here, not just a description:
// the same compiled []Instruction (and every LambdaProto reachable from
// it) is meant to be shared - one compile, many Run calls, including
// concurrently across separate Machines in separate goroutines (nothing
// about a Machine's own per-instance state, e.g. its stack, extends to
// the program it runs). Every field here is set once, by the compiler,
// before Compile() ever returns, and never mutated afterward - see
// NoEscape's own doc comment for why that matters even for a field that
// looks like a cacheable derived value.
type LambdaProto struct {
	Params       []string
	Instructions []Instruction

	// NoEscape reports whether this lambda's body can ever let a
	// reference to its own per-call param scope escape past the call
	// that's running it - see ReusableCall's doc comment for what that
	// enables it to skip. Computed once, by root's compiler
	// (compileLambda, via ContainsClosureLiteral), at the same time as
	// Params/Instructions, and never mutated afterward: a LambdaProto is
	// meant to be shared read-only across every Run call, including
	// concurrently across separate Machines in separate goroutines, so this
	// field can't be a lazily-computed cache that mutates itself on first
	// use without becoming a data race under exactly that deployment scenario.
	NoEscape bool
}

// ContainsClosureLiteral reports whether instructions - or any nested
// instruction stream reachable from it (a `let` body, or the left
// operand of `??`) - contains an OpMakeClosure: a lambda expression
// written inside another one, e.g. the `y => ...` in `x => y => x + y`.
// Exported so root's compiler can compute LambdaProto.NoEscape once, at
// compile time (see its own doc comment for why that matters) -
// ContainsClosureLiteral itself is the one part of that computation that
// needs to know about OpLet/OpCoalesce's own nested instruction streams,
// so it stays here rather than being reimplemented in compiler.go.
//
// Whether a lambda's body contains a nested closure literal is exactly
// what ReusableCall's reuse fast path needs to know: without that
// guarantee, reusing/overwriting the same per-call values slice across
// repeated calls (what ReusableCall does) would be observable - a lambda like
// `x => (y => x + y)` returns a closure that *captures* the outer
// param's localFrame by reference, so a caller invoking the outer lambda
// N times in a loop (map/filter/reduce's whole reason to want reuse) and
// collecting each returned inner closure would end up with every one of
// them seeing only the *last* iteration's x, instead of the x that was
// live when each was created - a real correctness bug, not just a missed
// optimization.
//
// OpCoalesce's own right-hand operand needs no separate recursion: per
// its doc comment in this file, it's compiled inline, immediately
// following, in the very same top-level stream this function is already
// walking.
func ContainsClosureLiteral(instructions []Instruction) bool {
	for _, inst := range instructions {
		switch inst.Op {
		case OpMakeClosure:
			return true
		case OpLet:
			if arg, ok := inst.Arg.(*LetArg); ok && ContainsClosureLiteral(arg.Body) {
				return true
			}
		case OpCoalesce:
			if arg, ok := inst.Arg.(*CoalesceArg); ok && ContainsClosureLiteral(arg.Left) {
				return true
			}
		}
	}
	return false
}

// CoalesceArg is OpCoalesce's instruction argument - see the OpCoalesce
// opcode comment. JumpEnd is patched in after the right-hand operand is
// compiled, the same mutate-after-the-fact pattern compileConditional
// already uses for its own jump targets. Exported for the same reason as
// LambdaProto - root's Compiler constructs these.
type CoalesceArg struct {
	Left    []Instruction
	JumpEnd int
}

// LetArg is OpLet's instruction argument: the bound name, and the
// self-contained instructions for the let's body - compiled separately,
// the same way a lambda body or "??"'s left operand is, so it runs as its
// own frame (see vm.go's frame doc comment) in its own extended scope.
// That's what makes the binding's scoping airtight with no separate
// bracket/pop bookkeeping:
// the surrounding expression's own scopes slice is never touched, so
// there's nothing to accidentally leak into a sibling call argument or
// anything after the let expression ends. Exported for the same reason as
// LambdaProto - root's Compiler constructs these.
type LetArg struct {
	Name string
	Body []Instruction
}

type Instruction struct {
	Op  OpCode
	Arg any
}
