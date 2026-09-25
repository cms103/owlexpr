package owlexpr

import (
	"fmt"

	"github.com/cms103/owlexpr/vm"
)

// Compile parses and compiles expression into a flat instruction sequence
// ready to run on a vm.Machine. foldConstants runs first, folding any
// pure literal sub-expression (`60 * 60 * 24`) down to a single literal
// before compilation - see its own doc comment for why that's always
// safe here, independent of which Machine eventually runs the result.
func Compile(expression string) ([]vm.Instruction, error) {
	ast, err := Parse(expression)
	if err != nil {
		return nil, err
	}
	ast = foldConstants(ast)
	c := &compiler{}
	c.compile(ast)
	if c.err != nil {
		return nil, c.err
	}
	return c.instructions, nil
}

// compiler walks an AST and emits vm.Instructions. Its state beyond the
// instructions being built is err - added for "matches", the first
// construct this compiler can fail on for a genuinely user-facing reason
// (an invalid regex literal, or a `matches` pattern that can never be a regex), as opposed to the
// panics used elsewhere for AST shapes the parser should never produce -
// and locals, the compile-time mirror of lexical scope described below.
// It's used internally - once per top-level Compile call, plus one per
// nested self-contained body (lambdas, let, ??) that needs its own
// instruction stream, each such nested compiler being a separate instance
// whose err must be propagated back to the outer one explicitly (see the
// LambdaNode/LetNode/"??" cases below) since it isn't shared state.
type compiler struct {
	instructions []vm.Instruction
	err          error

	// locals is the compile-time mirror of the VM's run-time localFrame
	// chain (see vm.go's doc comment on that type): one entry per
	// enclosing lambda parameter list or let, innermost last, each
	// holding that frame's bound names in binding order. It's what lets
	// compile's VarNode case emit vm.OpLoadLocal (a resolved (depth, slot)
	// address) instead of vm.OpLoad (a run-time, string-keyed scope-chain
	// search) for any name it can prove is lexically bound, via
	// resolveLocal below - childWithBound is the only place that extends
	// it, always by exactly one frame per call, mirroring the exactly-
	// one-frame-per-call OpLet/OpMakeClosure+OpCall push at run time (see
	// its own doc comment for why that 1:1 correspondence has to hold).
	locals []localFrameNames
}

// localFrameNames is one compiler-tracked frame's worth of bound names,
// in binding order - a lambda's parameters, or a let's single name -
// mirroring one run-time localFrame's values slice one-for-one, so a
// name's index here is exactly the Slot resolveLocal reports for it.
type localFrameNames struct {
	names []string
}

// child returns a fresh compiler for a nested, self-contained instruction
// stream ("??"'s left operand, or a lambda/let body with no additional
// binding of its own to add - see childWithBound for the ones that do)
// that still shares this compiler's locals (a read-only slice, safe since
// childWithBound never mutates it in place). err is deliberately NOT
// shared - see the LambdaNode/LetNode/"??" cases in compile, which each
// propagate a child's err back to c.err by hand once that child's
// compilation finishes.
func (c *compiler) child() *compiler {
	return &compiler{locals: c.locals}
}

// childWithBound is child, plus names added to the result's own c.locals
// - used by LambdaNode (its own parameters) and LetNode (its bound name),
// always by exactly one new localFrameNames, even when names is empty (a
// zero-param lambda): compileLambda is the only caller that ever compiles
// a lambda body, and OpMakeClosure's OpCall path at run time always
// pushes exactly one new localFrame per call regardless of param count
// (see vm.go), so the compile-time and run-time frame counts have to
// march in lockstep - never pushing here "because there's nothing to
// bind" would desynchronize every enclosing name's resolved depth by one
// as soon as a zero-param lambda sat between it and the reference. Never
// mutates c.locals itself (shared by reference with every sibling
// compile - a different lambda elsewhere, the enclosing scope - which
// must not see this one body's own bindings).
func (c *compiler) childWithBound(names ...string) *compiler {
	sub := c.child()

	locals := make([]localFrameNames, len(c.locals)+1)
	copy(locals, c.locals)
	locals[len(c.locals)] = localFrameNames{names: append([]string(nil), names...)}
	sub.locals = locals

	return sub
}

// resolveLocal reports whether name is bound by an enclosing lambda
// parameter list or let - and if so, its compile-time lexical address:
// Depth frame-boundaries out from the frame active where the reference
// compiles (0 = the innermost enclosing lambda/let, 1 = its own lexical
// parent, ...) and Slot, its position within that frame's names. Search
// runs innermost frame first, and within a frame from its last name
// backward, so a shadowing rebinding of the same name - either a nested
// lambda reusing an outer name, or (pathologically) a lambda with a
// duplicate parameter name - resolves to the innermost/last one, exactly
// matching the run-time positional-assignment order that would apply
// were duplicate names actually used (see the LambdaProto.Params
// ordering compileLambda emits).
func (c *compiler) resolveLocal(name string) (depth, slot int, ok bool) {
	for i := len(c.locals) - 1; i >= 0; i-- {
		names := c.locals[i].names
		for j := len(names) - 1; j >= 0; j-- {
			if names[j] == name {
				return len(c.locals) - 1 - i, j, true
			}
		}
	}
	return 0, 0, false
}

// compileRegex compiles a regex literal's pattern (from re`...` or a
// string literal on the right of `matches`) at Compile() time, recording
// an invalid pattern as c.err.
func (c *compiler) compileRegex(pattern string) *vm.Regex {
	re, err := vm.CompileRegex(pattern)
	if err != nil {
		c.err = fmt.Errorf("invalid regex literal %q: %w", pattern, err)
		return nil
	}
	return re
}

func (c *compiler) compile(node Expr) {
	if c.err != nil {
		return
	}
	switch n := node.(type) {
	case NumberNode:
		c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpPush, Arg: n.Value})

	case StringNode:
		c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpPush, Arg: n.Value})

	case RegexNode:
		re := c.compileRegex(n.Pattern)
		if c.err != nil {
			return
		}
		c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpPush, Arg: re})

	case BoolNode:
		c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpPush, Arg: n.Value})

	case NilNode:
		c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpPush, Arg: nil})

	case VarNode:
		if depth, slot, ok := c.resolveLocal(n.Name); ok {
			c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpLoadLocal, Arg: vm.LocalRef{Depth: depth, Slot: slot}})
		} else {
			c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpLoad, Arg: vm.HashedName{Name: n.Name, Hash: vm.HashName(n.Name)}})
		}

	case UnaryOpNode:
		c.compile(n.Operand)
		switch n.Op {
		case "-":
			c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpNeg})
		case "!", "not":
			c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpNot})
		case "+":
			// Unary plus is a numeric no-op: the operand is already on the
			// stack, so there's nothing further to emit.
		default:
			panic(fmt.Sprintf("unsupported unary operator: %s", n.Op))
		}

	case MemberAccessNode:
		c.compile(n.Expr)
		c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpAccess, Arg: vm.HashedName{Name: n.Member, Hash: vm.HashName(n.Member)}})

	case OptMemberAccessNode:
		c.compile(n.Expr)
		c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpOptAccess, Arg: vm.HashedName{Name: n.Member, Hash: vm.HashName(n.Member)}})

	case OptIndexNode:
		c.compile(n.Target)
		c.compile(n.Index)
		c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpOptIndex})

	case ListNode:
		for _, el := range n.Elements {
			c.compile(el)
		}
		c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpMakeList, Arg: len(n.Elements)})

	case MapNode:
		for i, key := range n.Keys {
			c.compile(key)
			c.compile(n.Values[i])
		}
		c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpMakeMap, Arg: len(n.Keys)})

	case IndexNode:
		c.compile(n.Target)
		c.compile(n.Index)
		c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpIndex})

	case SliceNode:
		c.compile(n.Target)
		if n.Low != nil {
			c.compile(n.Low)
		} else {
			c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpPush, Arg: nil})
		}
		if n.High != nil {
			c.compile(n.High)
		} else {
			c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpPush, Arg: nil})
		}
		c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpSlice})

	case LambdaNode:
		c.compileLambda(n)

	case LetNode:
		c.compile(n.Value)
		if c.err != nil {
			return
		}
		sub := c.childWithBound(n.Name)
		sub.compile(n.Body)
		if sub.err != nil {
			c.err = sub.err
			return
		}
		c.instructions = append(c.instructions, vm.Instruction{
			Op:  vm.OpLet,
			Arg: &vm.LetArg{Name: n.Name, Body: sub.instructions},
		})

	case BinaryOpNode:
		// Short-circuit `||` / `or`
		if n.Op == "||" || n.Op == "or" {
			c.compile(n.Left)
			jumpFalseIdx := len(c.instructions)
			c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpJumpIfFalse, Arg: 0})

			// Left side was true: push true and jump to end
			c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpPush, Arg: true})
			jumpEndIdx := len(c.instructions)
			c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpJump, Arg: 0})

			// Left side was false: compile right side
			c.instructions[jumpFalseIdx].Arg = len(c.instructions)
			c.compile(n.Right)
			c.instructions[jumpEndIdx].Arg = len(c.instructions)
			return
		}

		// Short-circuit `&&` / `and`
		if n.Op == "&&" || n.Op == "and" {
			c.compile(n.Left)
			jumpFalseIdx := len(c.instructions)
			c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpJumpIfFalse, Arg: 0})

			// Left side was true: evaluate right side
			c.compile(n.Right)
			jumpEndIdx := len(c.instructions)
			c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpJump, Arg: 0})

			// Left side was false: push false and jump to end
			c.instructions[jumpFalseIdx].Arg = len(c.instructions)
			c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpPush, Arg: false})
			c.instructions[jumpEndIdx].Arg = len(c.instructions)
			return
		}

		// `??`: the left operand is compiled into its own self-contained
		// instruction sequence (like a zero-param lambda body) rather
		// than inlined, so OpCoalesce can run it in isolation and catch
		// its error instead of letting it abort the enclosing
		// expression. The right operand IS inlined here, immediately
		// after - it needs no isolation of its own, since an error there
		// should propagate normally - and JumpEnd is patched in once its
		// length is known, skipping it when the left operand already
		// succeeded with a non-nil value.
		if n.Op == "??" {
			sub := c.child()
			sub.compile(n.Left)
			if sub.err != nil {
				c.err = sub.err
				return
			}
			arg := &vm.CoalesceArg{Left: sub.instructions}
			c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpCoalesce, Arg: arg})
			c.compile(n.Right)
			arg.JumpEnd = len(c.instructions)
			return
		}

		// "matches": a string or regex literal right-hand side is compiled
		// once here (at Compile() time) into a *vm.Regex embedded directly
		// as an OpPush constant - the same *vm.Regex value is reused,
		// already compiled, every time this instruction stream runs, so
		// the operator is cache-free. Any other non-literal right-hand
		// side (a let-bound regex, an env value) compiles normally but
		// must evaluate to a *vm.Regex at run time - a plain string there
		// is a runtime error, never compiled on the fly (see matchesValue).
		// A literal that can never be a regex (a number, bool, nil, list,
		// map or lambda) is rejected here, since it's certain to fail.
		// See DESIGN_NOTES.md's "matches" writeup for why dynamic patterns
		// are deliberately not supported.
		if n.Op == "matches" {
			switch right := n.Right.(type) {
			case StringNode:
				re := c.compileRegex(right.Value)
				if c.err != nil {
					return
				}
				c.compile(n.Left)
				c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpPush, Arg: re})
			case NumberNode, BoolNode, NilNode, ListNode, MapNode, LambdaNode:
				c.err = fmt.Errorf("right-hand side of 'matches' must be a string or regex literal, or a regex value, got %T", n.Right)
				return
			default:
				c.compile(n.Left)
				c.compile(n.Right)
			}
			if c.err != nil {
				return
			}
			c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpMatches})
			return
		}

		c.compile(n.Left)
		c.compile(n.Right)

		var op vm.OpCode
		switch n.Op {
		case "+":
			op = vm.OpAdd
		case "-":
			op = vm.OpSub
		case "*":
			op = vm.OpMul
		case "/":
			op = vm.OpDiv
		case "%":
			op = vm.OpMod
		case "**":
			op = vm.OpPow
		case "==":
			op = vm.OpEqual
		case "!=":
			op = vm.OpNotEqual
		case "<":
			op = vm.OpLess
		case ">":
			op = vm.OpGreater
		case "<=":
			op = vm.OpLessEq
		case ">=":
			op = vm.OpGreaterEq
		case "in":
			op = vm.OpIn
		default:
			panic(fmt.Sprintf("unsupported binary operator: %s", n.Op))
		}
		c.instructions = append(c.instructions, vm.Instruction{Op: op})

	case TernaryNode:
		c.compileConditional(n.Cond, n.Then, n.Else)

	case IfNode:
		c.compileConditional(n.Cond, n.Then, n.Else)

	case CallNode:
		c.compile(n.Callee)
		for _, arg := range n.Args {
			c.compile(arg)
			if c.err != nil {
				return
			}
		}
		c.instructions = append(c.instructions, vm.Instruction{
			Op: vm.OpCall,
			Arg: vm.CallMetadata{
				ArgCount: len(n.Args),
			},
		})
	}
}

// compileLambda compiles a LambdaNode into an OpMakeClosure instruction
// appended to c's own instruction stream.
//
// NoEscape is computed here, once, from the finished body, at compile
// time - see vm.LambdaProto.NoEscape's own doc comment for why it has to
// be computed once and never touched again rather than lazily. This is
// the one place a LambdaProto is ever constructed, so it's also the only
// place that needs to know how to compute it.
func (c *compiler) compileLambda(n LambdaNode) {
	sub := c.childWithBound(n.Params...)
	sub.compile(n.Body)
	if sub.err != nil {
		c.err = sub.err
		return
	}
	proto := &vm.LambdaProto{
		Params:       n.Params,
		Instructions: sub.instructions,
		NoEscape:     !vm.ContainsClosureLiteral(sub.instructions),
	}
	c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpMakeClosure, Arg: proto})
}

func (c *compiler) compileConditional(cond, thenExpr, elseExpr Expr) {
	c.compile(cond)
	jumpFalseIdx := len(c.instructions)
	c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpJumpIfFalse, Arg: 0})

	c.compile(thenExpr)
	jumpEndIdx := len(c.instructions)
	c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpJump, Arg: 0})

	c.instructions[jumpFalseIdx].Arg = len(c.instructions)
	if elseExpr != nil {
		c.compile(elseExpr)
	} else {
		c.instructions = append(c.instructions, vm.Instruction{Op: vm.OpPush, Arg: nil})
	}
	c.instructions[jumpEndIdx].Arg = len(c.instructions)
}
