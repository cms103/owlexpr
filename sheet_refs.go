package owlexpr

import "fmt"

// sheetNamespace is the reserved identifier CompileSheet/RunSheet use for
// cross-cell references (`sheet.x`). It is the only way one cell reaches
// another's value - deliberately namespace-only, no bare-name fallback, so
// a cell can never silently shadow (or be shadowed by) an env variable of
// the same name. See DESIGN_NOTES.md's "let vs. Sheet/Cell" section for the
// prior-art survey (Terraform's var./local., Rego's data. path, Excel's
// Sheet1! qualifier) that motivated this over a bare-name-based design.
const sheetNamespace = "sheet"

// sheetReferences returns the set of cell names expr reaches via
// `sheet.<name>`, let/lambda-aware so a local binding literally named
// "sheet" (a let name or lambda param) is never mistaken for the reserved
// namespace CompileSheet/RunSheet inject - `let sheet = ...; sheet.x` uses
// the local, not the cell namespace, the same way any other let/lambda
// name would shadow an outer one.
func sheetReferences(expr Expr) map[string]bool {
	refs := make(map[string]bool)
	collectSheetReferences(expr, nil, refs)
	return refs
}

func collectSheetReferences(expr Expr, bound map[string]bool, refs map[string]bool) {
	switch n := expr.(type) {
	case NumberNode, StringNode, BoolNode, NilNode:
		// leaves, no sub-expressions

	case VarNode:
		// A bare VarNode is never itself a sheet reference - only a
		// MemberAccessNode on an unshadowed `sheet` (below) is. A bare
		// `sheet` (e.g. passed to a function as a whole map) is legal and
		// resolves at runtime, but which cells it might read can't be
		// determined statically, so it creates no dependency edge.

	case UnaryOpNode:
		collectSheetReferences(n.Operand, bound, refs)

	case BinaryOpNode:
		collectSheetReferences(n.Left, bound, refs)
		collectSheetReferences(n.Right, bound, refs)

	case CallNode:
		collectSheetReferences(n.Callee, bound, refs)
		for _, arg := range n.Args {
			collectSheetReferences(arg, bound, refs)
		}

	case MemberAccessNode:
		if v, ok := n.Expr.(VarNode); ok && v.Name == sheetNamespace && !bound[sheetNamespace] {
			refs[n.Member] = true
			return
		}
		collectSheetReferences(n.Expr, bound, refs)

	case OptMemberAccessNode:
		// sheet is always a non-nil map (RunSheet injects it fresh per
		// cell, even when a cell has zero dependencies), so `sheet?.x`
		// behaves exactly like `sheet.x` at runtime - accessMember still
		// runs and still needs x's dependency edge recorded, the ?.
		// short-circuit only ever fires here for a genuinely nil target.
		if v, ok := n.Expr.(VarNode); ok && v.Name == sheetNamespace && !bound[sheetNamespace] {
			refs[n.Member] = true
			return
		}
		collectSheetReferences(n.Expr, bound, refs)

	case OptIndexNode:
		collectSheetReferences(n.Target, bound, refs)
		collectSheetReferences(n.Index, bound, refs)

	case TernaryNode:
		collectSheetReferences(n.Cond, bound, refs)
		collectSheetReferences(n.Then, bound, refs)
		collectSheetReferences(n.Else, bound, refs)

	case IfNode:
		collectSheetReferences(n.Cond, bound, refs)
		collectSheetReferences(n.Then, bound, refs)
		collectSheetReferences(n.Else, bound, refs)

	case ListNode:
		for _, el := range n.Elements {
			collectSheetReferences(el, bound, refs)
		}

	case MapNode:
		for _, k := range n.Keys {
			collectSheetReferences(k, bound, refs)
		}
		for _, v := range n.Values {
			collectSheetReferences(v, bound, refs)
		}

	case IndexNode:
		collectSheetReferences(n.Target, bound, refs)
		collectSheetReferences(n.Index, bound, refs)

	case SliceNode:
		collectSheetReferences(n.Target, bound, refs)
		if n.Low != nil {
			collectSheetReferences(n.Low, bound, refs)
		}
		if n.High != nil {
			collectSheetReferences(n.High, bound, refs)
		}

	case LambdaNode:
		collectSheetReferences(n.Body, extendBound(bound, n.Params...), refs)

	case LetNode:
		// Value is evaluated in the outer scope - let is non-recursive
		// (compiler.go compiles Value before OpLet extends the scope chain
		// with Name), so only Body sees the new binding.
		collectSheetReferences(n.Value, bound, refs)
		collectSheetReferences(n.Body, extendBound(bound, n.Name), refs)

	default:
		// Every Expr the parser can produce is one of the cases above -
		// reaching here means a new AST node type was added without
		// updating this scanner.
		panic(fmt.Sprintf("sheetReferences: unhandled AST node type %T", expr))
	}
}

func extendBound(bound map[string]bool, names ...string) map[string]bool {
	next := make(map[string]bool, len(bound)+len(names))
	for k := range bound {
		next[k] = true
	}
	for _, n := range names {
		next[n] = true
	}
	return next
}
