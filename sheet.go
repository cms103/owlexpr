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

// cellDep is one dependency edge, resolved once at CompileSheet time.
// RunSheetInto doesn't need it - every cell reads upstream results from
// one shared map - but it records what CompileSheet found.
type cellDep struct {
	name string
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
// order, and a cell's position there is its slot in RunSheetInto's out
// slice.
type Sheet struct {
	cells     []compiledCell
	index     map[string]int
	namespace string
}

// Len reports the number of cells in s: the length RunSheetInto's out
// slice must have.
func (s *Sheet) Len() int {
	return len(s.cells)
}

// Name returns the name of the cell in slot i, 0 <= i < Len(). Slots are
// in evaluation (dependency-first) order, not the order the cells were
// given to CompileSheet.
func (s *Sheet) Name(i int) string {
	return s.cells[i].name
}

// Index returns the slot of the cell called name, and whether there is one.
func (s *Sheet) Index(name string) (int, bool) {
	i, ok := s.index[name]
	return i, ok
}

type SheetOption func(*Sheet) error

// Namespace overrides the reserved identifier CompileSheet/RunSheetInto use for
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
// scope lookup takes priority over builtins (see OpLoad, vm/vm.go).
// Pick a name unlikely to collide with any namespace your builtins
// register.
func Namespace(name string) SheetOption {
	return func(s *Sheet) error {
		if err := validateNamespaceName(name); err != nil {
			return fmt.Errorf("compile sheet: %w", err)
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
		return errors.New("namespace must not be empty")
	}
	lex := NewLexer(name)
	tok := lex.NextToken()
	if tok.Type != TokIdent || tok.Val != name {
		return fmt.Errorf("namespace %q is not a valid identifier, or is a reserved word", name)
	}
	if next := lex.NextToken(); next.Type != TokEOF {
		return fmt.Errorf("namespace %q is not a valid identifier", name)
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
		refs, err := sheetReferences(asts[cell.Name], sheet.namespace)
		if err != nil {
			return nil, fmt.Errorf("cell %q: %w", cell.Name, err)
		}
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

	// nameToIndex maps a cell name to its final position in sheet.cells,
	// for Sheet.Index.
	nameToIndex := make(map[string]int, len(cells))

	for _, name := range order {
		c := &compiler{}
		c.compile(asts[name])
		if c.err != nil {
			return nil, fmt.Errorf("cell %q: %w", name, c.err)
		}
		cellDeps := deps[name]
		resolvedDeps := make([]cellDep, len(cellDeps))
		for i, dep := range cellDeps {
			resolvedDeps[i] = cellDep{name: dep}
		}
		nameToIndex[name] = len(sheet.cells)
		sheet.cells = append(sheet.cells, compiledCell{
			name:         name,
			instructions: c.instructions,
			deps:         resolvedDeps,
		})
	}
	sheet.index = nameToIndex
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
// seeded by env, and returns each cell's result keyed by name. It's
// RunSheetInto plus building that map; see RunSheetInto for the details,
// including errors. A caller running the same Sheet over many envs should
// use RunSheetInto directly and skip the map.
func RunSheet(mc *vm.Machine, sheet *Sheet, env map[string]any) (map[string]any, error) {
	values := make([]any, len(sheet.cells))
	if err := RunSheetInto(mc, sheet, env, values); err != nil {
		return nil, err
	}
	out := make(map[string]any, len(sheet.cells))
	for i := range sheet.cells {
		out[sheet.cells[i].name] = values[i]
	}
	return out, nil
}

// RunSheetInto evaluates every cell of sheet against mc in dependency
// order, seeded by env, and stores cell i's result in out[i] - the cell
// sheet.Name(i). out must have length sheet.Len(), and may be reused
// across calls: nothing RunSheetInto leaves behind refers to it. env
// itself is never mutated or copied.
//
// Every cell runs against the same two-layer scope chain: env, under a
// scope binding the namespace ("sheet" by default) to a results map that
// each cell's value is added to once it has run. RunScopes (vm/vm.go)
// layers it over env so `sheet.x` resolves via the same generic
// map-dot-access accessMember already gives any string-keyed map. The
// map, the scope holding it and the scope chain are allocated once per
// call, not per cell, so a cell costs nothing beyond its own evaluation
// and one map insert.
//
// Sharing one map across cells is safe for the reason the per-cell maps
// used before this were fresh: a cell whose result is (or contains) a
// closure captures the map, and must keep seeing the values it was
// created with. Within a call, an entry is only ever added - a cell's
// result is written once, after it runs, and never changed - and every
// `sheet.<name>` a cell contains is a dependency CompileSheet ordered
// before it, so every read, however late a closure makes it, sees the
// value that was there when the reading cell ran. Across calls, nothing
// is shared: each call makes a new map. A cell that uses the namespace
// as a whole value (`len(sheet)`) sees every cell that has run so far,
// not just its dependencies - that isn't something callers can rely on.
//
// The map keeps each result raw, since a later cell may still need to
// call a lambda-valued cell the normal internal way via `sheet.<name>`
// (see RunScopes' own doc comment for why RunScopes itself never wraps
// its result), but what reaches out is passed through vm.WrapCallable -
// the same conversion Run applies to its own result - so a cell whose
// value is a lambda hands the caller an ordinary callable Go func instead
// of owlexpr's internal closure representation.
//
// Fails fast: the first cell to error stops execution and its error
// (wrapped with the cell's name, as `cell "name": ...`) is returned. out
// then holds partial results and should be discarded. Returns an error
// without running anything if env already defines the namespace -
// RunSheetInto refuses to silently shadow it.
func RunSheetInto(mc *vm.Machine, sheet *Sheet, env map[string]any, out []any) error {
	if len(out) != len(sheet.cells) {
		return fmt.Errorf("run sheet: out has length %d, but the sheet has %d cells", len(out), len(sheet.cells))
	}
	if _, exists := env[sheet.namespace]; exists {
		return fmt.Errorf("run sheet: env already defines %q, which collides with the reserved cell-reference namespace", sheet.namespace)
	}

	results := make(map[string]any, len(sheet.cells))
	scopes := []map[string]any{env, {sheet.namespace: results}}
	for i := range sheet.cells {
		cell := &sheet.cells[i]
		val, err := mc.RunScopes(cell.instructions, scopes)
		if err != nil {
			return fmt.Errorf("cell %q: %w", cell.name, err)
		}
		results[cell.name] = val
		out[i] = vm.WrapCallable(val)
	}
	return nil
}
