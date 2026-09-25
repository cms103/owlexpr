package owlexpr

import (
	"fmt"
	"math"
)

// foldConstants recursively folds pure literal arithmetic/comparison/
// unary sub-expressions into a single literal node, so compiled bytecode
// never redoes at run time work that has no dependency on env at all -
// the same class of optimization expr's own optimizer/fold.go does.
// Applied unconditionally by Compile (always safe: it only ever touches
// sub-trees built entirely from literals - no
// VarNode, CallNode, MemberAccessNode, IndexNode, or anything else that
// could depend on env or have a side effect appears anywhere inside a
// folded sub-tree), right after parsing and before compilation.
//
// A folding failure (divide-by-zero, or an operator/type combination the
// runtime wouldn't support either) is deliberately NOT an error here -
// the node is left unfolded, an ordinary BinaryOpNode/UnaryOpNode, so the
// existing runtime error path fires exactly where the user's source says
// the problem is, instead of this pass turning a would-be runtime error
// into a confusing compile-time one.
//
// foldArith/foldCompare hand-implement literal arithmetic rather than
// running it through a *vm.Machine's Combine: a plain Compile() has no
// Machine at all until Run(), and folding must produce the same answer
// regardless of which Machine eventually runs the result - the same
// "one compiled program, many possible Machines" property the rest of
// Compile()'s design depends on (see TestFilterCanBeOverriddenPerVM).
// This does mean folding assumes the *default* core int64/float64/
// string/bool arithmetic (vm/operations.go's registerDefaultOperations)
// - if an embedder replaces int64's own handler via RegisterOperation/
// ClearOperations with different semantics, a folded int64 literal
// expression uses the default semantics, not that override. That's a
// narrower, more defensible version of the same assumption Compile()
// already makes about literals (their TypeCode is always one of the 5
// core ones, per GetTypeCode's fixed switch, itself never
// overridable) - and it's the same trade-off expr's own fold.go makes.
func foldConstants(node Expr) Expr {
	switch n := node.(type) {
	case UnaryOpNode:
		n.Operand = foldConstants(n.Operand)
		if folded, ok := foldUnary(n.Op, n.Operand); ok {
			return folded
		}
		return n

	case BinaryOpNode:
		switch n.Op {
		// Short-circuit/control-flow operators compile to jumps, not a
		// Combine dispatch - there's no dispatch cost folding would
		// eliminate here, so operands are still recursed into (a folded
		// operand still helps whatever consumes it) but the node itself
		// is never folded. "matches"' right side must stay a literal
		// StringNode (or RegexNode) for the compiler's own compile-time
		// regex handling, which it already structurally is - nothing to
		// fold there either.
		// "in"'s right side is typically a list, not a foldable scalar.
		case "&&", "and", "||", "or", "??", "in", "matches":
			n.Left = foldConstants(n.Left)
			n.Right = foldConstants(n.Right)
			return n
		}
		n.Left = foldConstants(n.Left)
		n.Right = foldConstants(n.Right)
		if folded, ok := foldBinary(n.Op, n.Left, n.Right); ok {
			return folded
		}
		return n

	case CallNode:
		n.Callee = foldConstants(n.Callee)
		for i := range n.Args {
			n.Args[i] = foldConstants(n.Args[i])
		}
		return n

	case ListNode:
		for i := range n.Elements {
			n.Elements[i] = foldConstants(n.Elements[i])
		}
		return n

	case MapNode:
		for i := range n.Values {
			n.Values[i] = foldConstants(n.Values[i])
		}
		return n

	case MemberAccessNode:
		n.Expr = foldConstants(n.Expr)
		return n

	case OptMemberAccessNode:
		n.Expr = foldConstants(n.Expr)
		return n

	case IndexNode:
		n.Target = foldConstants(n.Target)
		n.Index = foldConstants(n.Index)
		return n

	case OptIndexNode:
		n.Target = foldConstants(n.Target)
		n.Index = foldConstants(n.Index)
		return n

	case SliceNode:
		n.Target = foldConstants(n.Target)
		if n.Low != nil {
			n.Low = foldConstants(n.Low)
		}
		if n.High != nil {
			n.High = foldConstants(n.High)
		}
		return n

	case TernaryNode:
		n.Cond = foldConstants(n.Cond)
		n.Then = foldConstants(n.Then)
		n.Else = foldConstants(n.Else)
		return n

	case IfNode:
		n.Cond = foldConstants(n.Cond)
		n.Then = foldConstants(n.Then)
		if n.Else != nil {
			n.Else = foldConstants(n.Else)
		}
		return n

	case LambdaNode:
		n.Body = foldConstants(n.Body)
		return n

	case LetNode:
		n.Value = foldConstants(n.Value)
		n.Body = foldConstants(n.Body)
		return n
	}
	return node
}

// literalOf extracts e's literal value when e is already a literal node -
// the base case foldUnary/foldBinary need to know both operands are
// foldable at all before attempting arithmetic on them.
func literalOf(e Expr) (any, bool) {
	switch v := e.(type) {
	case NumberNode:
		return v.Value, true
	case StringNode:
		return v.Value, true
	case BoolNode:
		return v.Value, true
	}
	return nil, false
}

// literalNode is literalOf's inverse: box a folded Go value back into the
// AST node shape it came from. Panics on a type this package's own
// arithmetic could never actually produce - a bug in foldArith/
// foldCompare, not a reachable user-facing condition.
func literalNode(v any) Expr {
	switch v := v.(type) {
	case int64:
		return NumberNode{Value: v}
	case float64:
		return NumberNode{Value: v}
	case string:
		return StringNode{Value: v}
	case bool:
		return BoolNode{Value: v}
	}
	panic(fmt.Sprintf("foldConstants: unexpected literal type %T", v))
}

func foldUnary(op string, operand Expr) (Expr, bool) {
	val, ok := literalOf(operand)
	if !ok {
		return nil, false
	}
	switch op {
	case "-":
		switch v := val.(type) {
		case int64:
			return literalNode(-v), true
		case float64:
			return literalNode(-v), true
		}
	case "!", "not":
		if v, ok := val.(bool); ok {
			return literalNode(!v), true
		}
	case "+":
		switch val.(type) {
		case int64, float64:
			return literalNode(val), true
		}
	}
	return nil, false
}

func foldBinary(op string, left, right Expr) (Expr, bool) {
	lv, lok := literalOf(left)
	rv, rok := literalOf(right)
	if !lok || !rok {
		return nil, false
	}
	switch op {
	case "+", "-", "*", "/", "%", "**":
		return foldArith(op, lv, rv)
	case "==", "!=", "<", ">", "<=", ">=":
		return foldCompare(op, lv, rv)
	}
	return nil, false
}

// foldArith only ever folds a pair of *identical*-type literals
// (int64+int64, float64+float64, string+string) - deliberately never a
// mixed int64/float64 pair, even though vm/operations.go's
// floatOperations would happily coerce one. Whether that coercion is
// even allowed is itself a runtime Machine setting
// (DisableAutoTypeCoercion) that operationDispatcher only consults on
// its *different*-TypeCode branch (its same-type branch never checks
// autoCoerc at all) - so folding `3 + 4.2` unconditionally would bake in
// "coercion happened" at compile time, silently producing a valid
// result even against a Machine that would have errored at run time
// instead. This pass's safety bar is "matches what any possible Machine
// would have done", not just "matches the default Machine" - see
// TestDisableAutoTypeCoercionEndToEnd. owlexpr's literals are never
// plain `int` (only int64/float64 - see parser.go's number-literal
// rule), so unlike operations.go's own generic intOperations[T], there's
// no int/int64 case to consider here either, only this one exact-match
// rule.
func foldArith(op string, lv, rv any) (Expr, bool) {
	switch l := lv.(type) {
	case int64:
		if r, ok := rv.(int64); ok {
			return foldIntArith(op, l, r)
		}
	case float64:
		if r, ok := rv.(float64); ok {
			return foldFloatArith(op, l, r)
		}
	case string:
		if r, ok := rv.(string); ok && op == "+" {
			return literalNode(l + r), true
		}
	}
	return nil, false
}

func foldIntArith(op string, l, r int64) (Expr, bool) {
	switch op {
	case "+":
		return literalNode(l + r), true
	case "-":
		return literalNode(l - r), true
	case "*":
		return literalNode(l * r), true
	case "/":
		if r == 0 {
			return nil, false // leave for the runtime "Divide by zero" error
		}
		return literalNode(l / r), true
	case "%":
		if r == 0 {
			return nil, false // leave for the runtime "Modulus by zero" error
		}
		return literalNode(l % r), true
	case "**":
		res, ok := foldIntPow(l, r)
		if !ok {
			return nil, false // negative exponent - leave for the runtime error
		}
		return literalNode(res), true
	}
	return nil, false
}

// foldIntPow mirrors vm/operations.go's intPow[T int | int64] exactly
// (exponentiation by squaring), specialized to int64 since a literal is
// never plain int - duplicated rather than imported since vm/operations.go's
// intPow is unexported, and this pass deliberately has no *vm.Machine
// dependency at all (see foldConstants' own doc comment). Keep in sync
// with intPow if its algorithm ever changes.
func foldIntPow(base, exp int64) (int64, bool) {
	if exp < 0 {
		return 0, false
	}
	result := int64(1)
	for exp > 0 {
		if exp&1 == 1 {
			result *= base
		}
		base *= base
		exp >>= 1
	}
	return result, true
}

func foldFloatArith(op string, l, r float64) (Expr, bool) {
	switch op {
	case "+":
		return literalNode(l + r), true
	case "-":
		return literalNode(l - r), true
	case "*":
		return literalNode(l * r), true
	case "/":
		if r == 0.0 {
			return nil, false // leave for the runtime "Divide by zero" error
		}
		return literalNode(l / r), true
	case "%":
		if r == 0.0 {
			return nil, false // leave for the runtime "Modulus by zero" error
		}
		return literalNode(math.Mod(l, r)), true
	case "**":
		return literalNode(math.Pow(l, r)), true
	}
	return nil, false
}

// foldCompare, like foldArith, only ever folds a pair of identical-type
// literals - see foldArith's doc comment for why a mixed int64/float64
// pair is deliberately excluded even though it looks foldable: whether
// that comparison would even be allowed (vs. erroring under
// DisableAutoTypeCoercion) is a runtime Machine setting this pass has no
// way to know at compile time.
func foldCompare(op string, lv, rv any) (Expr, bool) {
	switch l := lv.(type) {
	case int64:
		if r, ok := rv.(int64); ok {
			return literalNode(compareOrdered(op, l, r)), true
		}
	case float64:
		if r, ok := rv.(float64); ok {
			return literalNode(compareOrdered(op, l, r)), true
		}
	case string:
		if r, ok := rv.(string); ok {
			return literalNode(compareOrdered(op, l, r)), true
		}
	case bool:
		if r, ok := rv.(bool); ok {
			switch op {
			case "==":
				return literalNode(l == r), true
			case "!=":
				return literalNode(l != r), true
			}
		}
	}
	return nil, false
}

func compareOrdered[T int64 | float64 | string](op string, l, r T) bool {
	switch op {
	case "==":
		return l == r
	case "!=":
		return l != r
	case "<":
		return l < r
	case ">":
		return l > r
	case "<=":
		return l <= r
	case ">=":
		return l >= r
	}
	panic("foldConstants: unreachable comparison operator " + op)
}
