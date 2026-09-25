package owlexpr

import (
	"fmt"
	"sort"
)

// SheetReferences reports which cells expression reads via
// `<namespace>.<name>` - the same analysis CompileSheet uses to order a
// Sheet's cells - so an embedder can build or check its own dependency
// graph (e.g. across pipeline stages) without compiling a Sheet. The
// result is sorted and de-duplicated, and is empty (not nil) when the
// expression references no cells.
//
// namespace is the identifier cells are reached through; "" means the
// default, "sheet" (see the Namespace SheetOption). Like Namespace, it must
// be a single identifier and not a reserved word.
//
// The analysis is static, so it follows the same rules as CompileSheet: a
// let name or lambda parameter that shadows namespace isn't a reference,
// and a bare `<namespace>` with no member (e.g. passed whole to a function)
// adds nothing, since which cells it reads can't be known without running
// it. `<namespace>["name"]` counts the same as `<namespace>.name`, but a
// computed index such as `<namespace>[k]` is an error, since the cell it
// reads can't be resolved statically. SheetReferences doesn't check that
// the referenced cells exist.
func SheetReferences(expression, namespace string) ([]string, error) {
	if namespace == "" {
		namespace = defaultSheetNamespace
	} else if err := validateNamespaceName(namespace); err != nil {
		return nil, err
	}
	ast, err := Parse(expression)
	if err != nil {
		return nil, err
	}
	refs, err := sheetReferences(ast, namespace)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// sheetReferences returns the set of cell names expr reaches via
// `<namespace>.<name>` (namespace defaults to "sheet" - see
// defaultSheetNamespace and the Namespace SheetOption in sheet.go),
// let/lambda-aware so a local binding literally named the same as namespace
// (a let name or lambda param) is never mistaken for the reserved namespace
// CompileSheet/RunSheet inject - `let sheet = ...; sheet.x` uses the local,
// not the cell namespace, the same way any other let/lambda name would
// shadow an outer one. namespace is the only way one cell reaches another's
// value - deliberately namespace-only, no bare-name fallback, so a cell can
// never silently shadow (or be shadowed by) an env variable of the same
// name. See DESIGN_NOTES.md's "let vs. Sheet/Cell" section for the
// prior-art survey (Terraform's var./local., Rego's data. path, Excel's
// Sheet1! qualifier) that motivated this over a bare-name-based design.
//
// It returns an error, rather than panicking, for an AST node type it
// doesn't know: SheetReferences is exported, and Expr is an empty
// interface, so this must never take the host process down.
func sheetReferences(expr Expr, namespace string) (map[string]bool, error) {
	c := &refCollector{refs: make(map[string]bool)}
	collectSheetReferences(expr, namespace, nil, c)
	if c.err != nil {
		return nil, c.err
	}
	return c.refs, nil
}

// refCollector accumulates collectSheetReferences' results, plus the first
// error it hit, so the recursive walk needn't thread an error through
// every call.
type refCollector struct {
	refs map[string]bool
	err  error
}

// fail records err unless an earlier error is already recorded - the
// first problem found is the one reported.
func (c *refCollector) fail(err error) {
	if c.err == nil {
		c.err = err
	}
}

// indexReference handles `<namespace>[index]` and `<namespace>?.[index]`,
// the bracket spellings of `<namespace>.name`. It reports false, doing
// nothing, when target isn't the (unshadowed) namespace. Otherwise index
// must be a constant string - a literal, or one constant folding reduces
// to a literal, e.g. "tax" + "able" - which is recorded as a reference
// exactly as `.name` would be. A computed index (`sheet[k]`) is an error:
// which cell it reads can't be known until it runs, and at run time a
// cell only sees the cells it was statically found to depend on, so it
// could never work - it would fail with "key not found" on every run.
func (c *refCollector) indexReference(target, index Expr, namespace string, bound map[string]bool) bool {
	v, ok := target.(VarNode)
	if !ok || v.Name != namespace || bound[namespace] {
		return false
	}
	if s, ok := foldConstants(index).(StringNode); ok {
		c.refs[s.Value] = true
		return true
	}
	c.fail(fmt.Errorf("%s[...] must use a constant string, e.g. %s[\"name\"]: a cell name computed at run time can't be resolved when the sheet is compiled", namespace, namespace))
	return true
}

func collectSheetReferences(expr Expr, namespace string, bound map[string]bool, c *refCollector) {
	switch n := expr.(type) {
	case NumberNode, StringNode, BoolNode, NilNode, RegexNode:
		// leaves, no sub-expressions

	case VarNode:
		// A bare VarNode is never itself a sheet reference - only a
		// MemberAccessNode or constant-string IndexNode on an unshadowed
		// `sheet` (below) is. A bare
		// `sheet` (e.g. passed to a function as a whole map) is legal and
		// resolves at runtime, but which cells it might read can't be
		// determined statically, so it creates no dependency edge.

	case UnaryOpNode:
		collectSheetReferences(n.Operand, namespace, bound, c)

	case BinaryOpNode:
		collectSheetReferences(n.Left, namespace, bound, c)
		collectSheetReferences(n.Right, namespace, bound, c)

	case CallNode:
		collectSheetReferences(n.Callee, namespace, bound, c)
		for _, arg := range n.Args {
			collectSheetReferences(arg, namespace, bound, c)
		}

	case MemberAccessNode:
		if v, ok := n.Expr.(VarNode); ok && v.Name == namespace && !bound[namespace] {
			c.refs[n.Member] = true
			return
		}
		collectSheetReferences(n.Expr, namespace, bound, c)

	case OptMemberAccessNode:
		// sheet is always a non-nil map (RunSheet injects it fresh per
		// cell, even when a cell has zero dependencies), so `sheet?.x`
		// behaves exactly like `sheet.x` at runtime - accessMember still
		// runs and still needs x's dependency edge recorded, the ?.
		// short-circuit only ever fires here for a genuinely nil target.
		if v, ok := n.Expr.(VarNode); ok && v.Name == namespace && !bound[namespace] {
			c.refs[n.Member] = true
			return
		}
		collectSheetReferences(n.Expr, namespace, bound, c)

	case OptIndexNode:
		// As for OptMemberAccessNode above, sheet is never nil, so
		// `sheet?.["x"]` is exactly `sheet["x"]`.
		if c.indexReference(n.Target, n.Index, namespace, bound) {
			return
		}
		collectSheetReferences(n.Target, namespace, bound, c)
		collectSheetReferences(n.Index, namespace, bound, c)

	case TernaryNode:
		collectSheetReferences(n.Cond, namespace, bound, c)
		collectSheetReferences(n.Then, namespace, bound, c)
		collectSheetReferences(n.Else, namespace, bound, c)

	case IfNode:
		collectSheetReferences(n.Cond, namespace, bound, c)
		collectSheetReferences(n.Then, namespace, bound, c)
		collectSheetReferences(n.Else, namespace, bound, c)

	case ListNode:
		for _, el := range n.Elements {
			collectSheetReferences(el, namespace, bound, c)
		}

	case MapNode:
		for _, k := range n.Keys {
			collectSheetReferences(k, namespace, bound, c)
		}
		for _, v := range n.Values {
			collectSheetReferences(v, namespace, bound, c)
		}

	case IndexNode:
		if c.indexReference(n.Target, n.Index, namespace, bound) {
			return
		}
		collectSheetReferences(n.Target, namespace, bound, c)
		collectSheetReferences(n.Index, namespace, bound, c)

	case SliceNode:
		collectSheetReferences(n.Target, namespace, bound, c)
		if n.Low != nil {
			collectSheetReferences(n.Low, namespace, bound, c)
		}
		if n.High != nil {
			collectSheetReferences(n.High, namespace, bound, c)
		}

	case LambdaNode:
		collectSheetReferences(n.Body, namespace, extendBound(bound, n.Params...), c)

	case LetNode:
		// Value is evaluated in the outer scope - let is non-recursive
		// (compiler.go compiles Value before OpLet extends the scope chain
		// with Name), so only Body sees the new binding.
		collectSheetReferences(n.Value, namespace, bound, c)
		collectSheetReferences(n.Body, namespace, extendBound(bound, n.Name), c)

	default:
		// Every Expr the parser can produce is one of the cases above -
		// reaching here means a new AST node type was added without
		// updating this scanner.
		c.fail(fmt.Errorf("sheet references: unhandled AST node type %T", expr))
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
