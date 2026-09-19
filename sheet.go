package owlexpr

import (
	"errors"
	"fmt"
	"sort"

	"github.com/cms103/owlexpr/vm"
)

// defaultSheetNamespace is the namespace CompileSheet uses when the caller
// doesn't pass a Namespace option.
const defaultSheetNamespace = "sheet"

// CellDef is one named expression going into a Sheet: Expression may
// reference any other cell in the same Sheet via `sheet.<name>` (see
// sheetReferences, sheet_refs.go - "sheet" is the default namespace; pass
// the Namespace SheetOption to CompileSheet to use a different one), in
// addition to whatever env/builtins it would normally see as a standalone
// expression. A bare `<name>` never reaches a cell, even one named
// identically - only the namespaced form does, deliberately, so a cell can
// never silently shadow or be shadowed by an env variable.
type CellDef struct {
	Name       string
	Expression string
}

// cellDep is one dependency edge, resolved once at CompileSheet time to
// the depended-on cell's fixed position in Sheet.cells (see RunSheet's own
// doc comment for why an index, not a name or even a precomputed hash, is
// the right representation here).
type cellDep struct {
	name  string
	index int
}

// compiledCell is a CellDef after CompileSheet has parsed it, resolved its
// cross-cell dependencies, and compiled it to instructions. deps is sorted
// by name for deterministic error messages and scope-map construction.
type compiledCell struct {
	name         string
	instructions []vm.Instruction
	deps         []cellDep
}

// Sheet is a compiled, reusable set of named expressions ("cells") whose
// execution order has already been resolved from their cross-references to
// each other - the same relationship Compile's []vm.Instruction has to a
// single expression string. cells is stored in resolved (dependency-first)
// order.
type Sheet struct {
	cells     []compiledCell
	namespace string
}

type SheetOption func(*Sheet) error

// Namespace overrides the reserved identifier CompileSheet/RunSheet use for
// cross-cell references (`<namespace>.<name>`) - "sheet" by default (see
// defaultSheetNamespace). name must lex as a single identifier and must not
// be a language keyword (if, else, let, true, false, nil, and, or, in, not,
// matches): the lexer tokenizes those as something other than TokIdent (an
// operator, a boolean literal, ...), so no cell expression could ever spell
// a reference through a namespace named after one.
//
// Choosing a name that collides with a builtin namespace registered on the
// Machine RunSheet is later called with (e.g. "string", "json") can't be
// checked here - CompileSheet has no Machine to check against - and will
// silently shadow that builtin for every cell in the sheet, since
// env/cellScope lookup takes priority over builtins (see OpLoad, vm/vm.go).
// Pick a name unlikely to collide with any namespace your builtins
// register.
func Namespace(name string) SheetOption {
	return func(s *Sheet) error {
		if err := validateNamespaceName(name); err != nil {
			return err
		}
		s.namespace = name
		return nil
	}
}

// validateNamespaceName reports whether name can ever actually appear as
// `name.x` in a cell expression: it must lex as exactly one identifier
// token, not a language keyword (which lexes as some other token type
// entirely) and not multiple tokens (e.g. "my-name", "a b", or a name
// starting with a digit). Reusing the real lexer here, rather than
// maintaining a separate hand-picked keyword list, keeps this in sync with
// the grammar automatically as it grows.
func validateNamespaceName(name string) error {
	if name == "" {
		return errors.New("compile sheet: namespace must not be empty")
	}
	lex := NewLexer(name)
	tok := lex.NextToken()
	if tok.Type != TokIdent || tok.Val != name {
		return fmt.Errorf("compile sheet: namespace %q is not a valid identifier, or is a reserved word", name)
	}
	if next := lex.NextToken(); next.Type != TokEOF {
		return fmt.Errorf("compile sheet: namespace %q is not a valid identifier", name)
	}
	return nil
}

// CompileSheet parses and compiles every cell, statically resolves the
// dependency graph from `sheet.<name>` references (see sheetNamespace),
// topologically sorts them, and rejects duplicate names, a `sheet.<name>`
// reference to a cell that doesn't exist, and dependency cycles (including
// direct self-reference) - all at build time, not per RunSheet call. The
// result is reusable across many RunSheet calls, the same way
// []vm.Instruction is reusable across many Run calls.
func CompileSheet(cells []CellDef, options ...SheetOption) (*Sheet, error) {
	if len(cells) == 0 {
		return nil, fmt.Errorf("compile sheet: no cells provided")
	}

	sheet := &Sheet{
		cells:     make([]compiledCell, 0, len(cells)),
		namespace: defaultSheetNamespace,
	}
	for _, opt := range options {
		if err := opt(sheet); err != nil {
			return nil, err
		}
	}

	names := make(map[string]bool, len(cells))
	asts := make(map[string]Expr, len(cells))
	for _, cell := range cells {
		if cell.Name == "" {
			return nil, fmt.Errorf("compile sheet: cell has empty name")
		}
		if names[cell.Name] {
			return nil, fmt.Errorf("compile sheet: duplicate cell name %q", cell.Name)
		}
		names[cell.Name] = true

		ast, err := Parse(cell.Expression)
		if err != nil {
			return nil, fmt.Errorf("cell %q: %w", cell.Name, err)
		}
		asts[cell.Name] = ast
	}

	deps := make(map[string][]string, len(cells))
	for _, cell := range cells {
		refs := sheetReferences(asts[cell.Name], sheet.namespace)
		var cellDeps []string
		for name := range refs {
			if !names[name] {
				return nil, fmt.Errorf("cell %q: %s.%s references a cell that does not exist", cell.Name, sheet.namespace, name)
			}
			cellDeps = append(cellDeps, name)
		}
		sort.Strings(cellDeps)
		deps[cell.Name] = cellDeps
	}

	order, err := topoSortCells(cells, deps)
	if err != nil {
		return nil, err
	}

	// nameToIndex maps a cell name to its final position in sheet.cells -
	// well-defined by the time a dependent cell needs it, since order is
	// dependency-first (topoSortCells' own guarantee): every dependency of
	// "name" was already appended, and so already has an entry here,
	// before "name" itself is processed below.
	nameToIndex := make(map[string]int, len(cells))

	for _, name := range order {
		c := &compiler{}
		c.compile(asts[name])
		cellDeps := deps[name]
		resolvedDeps := make([]cellDep, len(cellDeps))
		for i, dep := range cellDeps {
			resolvedDeps[i] = cellDep{name: dep, index: nameToIndex[dep]}
		}
		nameToIndex[name] = len(sheet.cells)
		sheet.cells = append(sheet.cells, compiledCell{
			name:         name,
			instructions: c.instructions,
			deps:         resolvedDeps,
		})
	}
	return sheet, nil
}

// topoSortCells returns cell names in dependency-first order via DFS
// postorder emission: visiting a cell recurses into its dependencies first,
// then appends the cell itself, so by induction every dependency (direct or
// transitive) lands earlier in the result than the cell that needs it. The
// input order of cells is used as the visitation order for cells with no
// ordering constraint between them, so the result is deterministic.
func topoSortCells(cells []CellDef, deps map[string][]string) ([]string, error) {
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	state := make(map[string]int, len(cells))
	order := make([]string, 0, len(cells))
	var path []string

	var visit func(name string) error
	visit = func(name string) error {
		state[name] = visiting
		path = append(path, name)
		for _, dep := range deps[name] {
			switch state[dep] {
			case unvisited:
				if err := visit(dep); err != nil {
					return err
				}
			case visiting:
				idx := len(path) - 1
				for path[idx] != dep {
					idx--
				}
				cycle := append(append([]string{}, path[idx:]...), dep)
				return fmt.Errorf("compile sheet: dependency cycle: %v", joinCycle(cycle))
			case done:
				// already resolved via another path - fine.
			}
		}
		path = path[:len(path)-1]
		state[name] = done
		order = append(order, name)
		return nil
	}

	for _, cell := range cells {
		if state[cell.Name] == unvisited {
			if err := visit(cell.Name); err != nil {
				return nil, err
			}
		}
	}
	return order, nil
}

func joinCycle(names []string) string {
	s := ""
	for i, n := range names {
		if i > 0 {
			s += " -> "
		}
		s += n
	}
	return s
}

// RunSheet evaluates every cell of sheet against mc in dependency order,
// seeded by env, and returns each cell's result keyed by name. env itself
// is never mutated or copied - each cell instead sees a small, freshly
// allocated map holding only the upstream cell results it actually depends
// on, exposed under the reserved "sheet" name (RunScopes, vm/vm.go, layers
// it under env so `sheet.x` resolves via the same generic map-dot-access
// accessMember already gives any string-keyed map). Cost per cell is O(that
// cell's dependency count), independent of both env's size and the sheet's
// total cell count. That freshness matters beyond cost: a cell whose result
// is (or contains) a closure captures a reference to this map, and it's
// never touched again after the cell's RunScopes call returns - mutating a
// shared map across cells would silently corrupt an already-escaped
// closure's captured scope, the same aliasing hazard closure.call and
// let's binding scope already avoid by never mutating a scope map after
// it's handed to a closure. Fails fast: the first cell to error stops
// execution and its error (wrapped with the cell's name) is returned, with
// no partial results. Returns an error without running anything if env
// already defines "sheet" - RunSheet refuses to silently shadow it.
//
// results (as opposed to out, the returned map[string]any) is a plain
// []any indexed by each cell's fixed position in sheet.cells (see
// cellDep's own doc comment) rather than a second string-keyed map
// alongside out: a first attempt at this speedup kept results as a
// vm.PrehashedMap keyed by each cell's precomputed name hash, on the
// theory that trading a rehash for a uint64 probe would be a pure win the
// same way it was for Machine.builtins/namespaces (see
// vm/prehashed_map.go) - but benchmarking it (bench_sheet_prehash_test.go)
// showed a regression, not a win: unlike Machine.builtins/namespaces,
// which are populated once and read for the Machine's whole life, results
// here is a fresh map built and torn down every single RunSheet call, so
// every cell's PrehashedMap insert paid for a new backing bucket slice
// allocation that a plain map[string]any's own preallocated buckets don't
// need - swapping a few nanoseconds of hashing for a heap allocation per
// cell. Since cell.deps is resolved to each dependency's fixed index at
// CompileSheet time, results doesn't need to be keyed by anything at
// all - one []any of len(sheet.cells), one allocation total per RunSheet
// call - is both simpler and faster than either map form. depsMap itself
// still has to be a plain map[string]any (RunScopes takes
// []map[string]any), so populating it still pays one native hash per
// dependency; that part is unavoidable without changing the VM's scope
// representation, which vm/vm.go's own doc comment on this decision
// covers.
//
// results and out deliberately diverge on one thing: a cell result that's
// a lambda closure is kept raw in results, since a later cell may still
// need to call it the normal internal way via `sheet.<name>` (see
// RunScopes' own doc comment for why RunScopes itself never wraps its
// result), but out - what actually reaches RunSheet's caller - passes
// each value through vm.WrapCallable first, the same conversion Run
// applies to its own result, so a cell whose value is a lambda hands the
// caller an ordinary callable Go func instead of owlexpr's internal
// closure representation.
func RunSheet(mc *vm.Machine, sheet *Sheet, env map[string]any) (map[string]any, error) {
	if _, exists := env[sheet.namespace]; exists {
		return nil, fmt.Errorf("run sheet: env already defines %q, which collides with the reserved cell-reference namespace", sheet.namespace)
	}

	results := make([]any, len(sheet.cells))
	out := make(map[string]any, len(sheet.cells))
	for i, cell := range sheet.cells {
		depsMap := make(map[string]any, len(cell.deps))
		for _, dep := range cell.deps {
			depsMap[dep.name] = results[dep.index]
		}
		cellScope := map[string]any{sheet.namespace: depsMap}
		val, err := mc.RunScopes(cell.instructions, []map[string]any{env, cellScope})
		if err != nil {
			return nil, fmt.Errorf("cell %q: %w", cell.name, err)
		}
		results[i] = val
		out[cell.name] = vm.WrapCallable(val)
	}
	return out, nil
}
