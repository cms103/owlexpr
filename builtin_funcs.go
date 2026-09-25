package owlexpr

import (
	"fmt"
	"math"
	"reflect"
	"unicode/utf8"

	"github.com/cms103/owlexpr/vm"
)

// getConvertFunc returns a BuiltinFunc that converts to target type T
// Used for int() int64() and float64() functions
func getConvertFunc[T any](name string) vm.BuiltinFunc {
	return func(mc *vm.Machine, args ...any) (any, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("%s() expects 1 argument, got %d", name, len(args))
		}
		var target T
		// Attempt to convert the argument to the target
		return mc.CoercValue(args[0], target)
	}
}

// lenFunc supports strings directly, []any and map[string]any (the types
// our own literals produce) as a zero-reflection fast path, and falls back
// to reflection for any other slice, array, or map - so a []int64 or a
// map[string]int from the environment works too, not just our own literal
// types. It intentionally doesn't accept iter.Seq/iter.Seq2 - a length
// isn't knowable without fully draining one, which len() shouldn't do as
// a side effect.
//
// A string's length is its rune count, not its byte count (Go's own
// len(string)), to match indexValue/sliceValue's rune-based indexing - so
// that s[0:len(s)] equals s for any string, not just ASCII ones.
func lenFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("len() expects 1 argument")
	}
	switch v := args[0].(type) {
	case string:
		return int64(utf8.RuneCountInString(v)), nil
	case []any:
		return int64(len(v)), nil
	case map[string]any:
		return int64(len(v)), nil
	}

	rv := reflect.ValueOf(args[0])
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return int64(rv.Len()), nil
	}

	return nil, fmt.Errorf("len() not supported for type %T", args[0])
}

// absFunc, ceilFunc, floorFunc, and roundFunc hardcode int/int64/float64,
// exactly like negateValue's own core-type switch in vm/vm.go - these are
// as fundamental as unary `-`, not a candidate for an opt-in stdlib pack.
// ceil/floor/round on an int/int64 are a no-op (an integer is already its
// own ceiling/floor/nearest value); only float64 needs math's help.
// Anything else (decimal.Decimal, or any other type registered via
// vm.RegisterOperation) falls back to mc.Combine(v, v, vm.OpAbs) - passing
// v as both operands guarantees the same TypeCode on each side, which is
// exactly what lets operationDispatcher's direct single-handler branch
// fire (the same trick vm.OpCoerce already relies on), reaching whatever
// OpAbs case that type's own RegisterOperation handler wrote - see
// stdlib's decimalOperations. This is the same "hardcode the common
// types, fall back to Combine for the rest" shape sumFunc/foldExtreme
// already use, just applied here too.
func absFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("abs() expects 1 argument, got %d", len(args))
	}
	switch v := args[0].(type) {
	case int:
		if v < 0 {
			return -v, nil
		}
		return v, nil
	case int64:
		if v < 0 {
			return -v, nil
		}
		return v, nil
	case float64:
		return math.Abs(v), nil
	}
	res, err := mc.Combine(args[0], args[0], vm.OpAbs)
	if err != nil {
		return nil, fmt.Errorf("abs() not supported for type %T", args[0])
	}
	return res, nil
}

func ceilFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("ceil() expects 1 argument, got %d", len(args))
	}
	switch v := args[0].(type) {
	case int:
		return v, nil
	case int64:
		return v, nil
	case float64:
		return math.Ceil(v), nil
	}
	res, err := mc.Combine(args[0], args[0], vm.OpCeil)
	if err != nil {
		return nil, fmt.Errorf("ceil() not supported for type %T", args[0])
	}
	return res, nil
}

func floorFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("floor() expects 1 argument, got %d", len(args))
	}
	switch v := args[0].(type) {
	case int:
		return v, nil
	case int64:
		return v, nil
	case float64:
		return math.Floor(v), nil
	}
	res, err := mc.Combine(args[0], args[0], vm.OpFloor)
	if err != nil {
		return nil, fmt.Errorf("floor() not supported for type %T", args[0])
	}
	return res, nil
}

func roundFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("round() expects 1 argument, got %d", len(args))
	}
	switch v := args[0].(type) {
	case int:
		return v, nil
	case int64:
		return v, nil
	case float64:
		return math.Round(v), nil
	}
	res, err := mc.Combine(args[0], args[0], vm.OpRound)
	if err != nil {
		return nil, fmt.Errorf("round() not supported for type %T", args[0])
	}
	return res, nil
}

// typeFunc names v's runtime type as a string. vm.IsNilResult is checked
// first, ahead of everything else, so a typed-nil value (e.g. a nil
// *Role struct field) reports "nil" too, not its pointer type - agreeing
// with `==`/`?.`/`??`, which already treat it as nil the same way. The
// next six cases match GetTypeCode's own core set plus the two literal
// collection types ([]any/map[string]any) - zero reflection, same fast
// path lenFunc uses - plus *vm.Regex, reported as "regex" (the value a
// re`...` literal produces) rather than its Go pointer type name. vm.IsCallable comes next so a lambda closure reports
// as "function" rather than leaking its unexported *vm.closure Go type
// name. Anything else (a struct from the env, decimal.Decimal, time.Time,
// a non-nil pointer, ...) falls back to reflect: Slice/Array/Map are
// classified the same as the fast-path types above (so an env-provided
// []int64 still says "list", not "[]int64"), and everything left over
// reports its concrete Go type name via %T - informative rather than a
// generic "object", and consistent with every other builtin's own
// %T-based error messages.
func typeFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("type() expects 1 argument, got %d", len(args))
	}
	v := args[0]
	// Checked first, ahead of the type switch below: a typed-nil pointer
	// (e.g. a struct field like `Role *Role` that's nil) doesn't match a
	// bare `case nil` in a type switch - only a literal untyped nil does -
	// but it still needs to report "nil" here to agree with `==`/`?.`/
	// `??`, which already treat it as nil via this same vm.IsNilResult
	// check.
	if vm.IsNilResult(v) {
		return "nil", nil
	}
	switch v.(type) {
	case bool:
		return "bool", nil
	case int:
		return "int", nil
	case int64:
		return "int64", nil
	case float64:
		return "float64", nil
	case string:
		return "string", nil
	case []any:
		return "list", nil
	case map[string]any:
		return "map", nil
	case *vm.Regex:
		return "regex", nil
	}
	if vm.IsCallable(v) {
		return "function", nil
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Slice, reflect.Array:
		return "list", nil
	case reflect.Map:
		return "map", nil
	case reflect.Func:
		return "function", nil
	}
	return fmt.Sprintf("%T", v), nil
}

// strFunc stringifies any value, alongside the core int()/int64()/float64()
// conversions - it can't be named "string" itself, since a bare builtin and
// the opt-in `string.*` namespace share one identifier lookup (see OpLoad in
// vm/vm.go), and a core builtin registered as "string" would permanently
// shadow that namespace object wherever both are present. nil becomes ""
// rather than "nil", matching how ""+nil style concatenation would read; a
// value that's already a string passes through unchanged rather than
// picking up quotes from %v; anything else falls back to fmt.Sprintf("%v").
func strFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("str() expects 1 argument, got %d", len(args))
	}
	if args[0] == nil {
		return "", nil
	}
	if s, ok := args[0].(string); ok {
		return s, nil
	}
	return fmt.Sprintf("%v", args[0]), nil
}

func sumFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) == 1 {
		switch arr := args[0].(type) {
		case []float64:
			return sumOrdered(arr), nil
		case []int64:
			return sumOrdered(arr), nil
		case []int:
			return sumOrdered(arr), nil
		}
	}
	values := builtinValues(args)
	if len(values) == 0 {
		return int64(0), nil
	}
	acc := values[0]
	for _, v := range values[1:] {
		var err error
		acc, err = mc.Combine(acc, v, vm.OpAdd)
		if err != nil {
			return nil, fmt.Errorf("sum(): %w", err)
		}
	}
	return acc, nil
}

// registerBuiltins wires up the always-on named functions (len/sum/min/
// max/map/filter/reduce) that need no VMOption to exist. It's a plain
// function, not a method on *vm.Machine, since Go doesn't allow attaching
// methods to a type from outside its own package, and Machine is defined
// in the vm package. Root's NewVM wrapper (vm_config.go) is what actually
// calls this, injecting it as the first VMOption ahead of any the caller
// supplied - see that file's comment for why the ordering matters.
func registerBuiltins(mc *vm.Machine) error {
	mc.RegisterBuiltin("len", lenFunc)

	mc.RegisterBuiltin("sum", sumFunc)

	mc.RegisterBuiltin("max", func(mc *vm.Machine, args ...any) (any, error) {
		return foldExtreme(mc, "max", args, vm.OpGreater)
	})

	mc.RegisterBuiltin("min", func(mc *vm.Machine, args ...any) (any, error) {
		return foldExtreme(mc, "min", args, vm.OpLess)
	})

	mc.RegisterBuiltin("map", mapFunc)

	mc.RegisterBuiltin("filter", filterFunc)

	mc.RegisterBuiltin("reduce", reduceFunc)

	mc.RegisterBuiltin("abs", absFunc)
	mc.RegisterBuiltin("ceil", ceilFunc)
	mc.RegisterBuiltin("floor", floorFunc)
	mc.RegisterBuiltin("round", roundFunc)
	mc.RegisterBuiltin("type", typeFunc)

	// Now register the type conversion functions
	mc.RegisterBuiltin("int", getConvertFunc[int]("int"))
	mc.RegisterBuiltin("int64", getConvertFunc[int64]("int64"))
	mc.RegisterBuiltin("float64", getConvertFunc[float64]("float64"))
	mc.RegisterBuiltin("str", strFunc)

	return nil
}

// mapFunc, filterFunc, and reduceFunc are the higher-order builtins that
// take a lambda argument, e.g. `map(alist, x => x.f)`. The lambda (or any
// other callable value - a BuiltinFunc, a bound struct method, a plain Go
// func) is invoked via mc.Call, the same dispatch OpCall itself uses, so
// these builtins don't need to know or care whether "fn" is a lambda
// closure or something else callable.
//
// The first argument can be a list-like source (a []any or other
// slice/array) or a paired source (a Go map) - see IterateListOrMapSource.
// For a list-like source, fn is called with one argument per element; for
// a paired source, with two (key, value) - which is also why reduce's
// accumulator function takes three arguments (acc, k, v) in that case.
func mapFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("map() expects 2 arguments (list, fn), got %d", len(args))
	}
	fn := args[1]

	// call is set up once for the whole map() call, not per element - see
	// vm.ReusableCall's doc comment for why reusing it across every
	// element (rather than an ordinary mc.Call per element) is safe here.
	call := vm.NewReusableCall(mc, fn)

	var out []any
	var callErr error
	ok := vm.IterateListOrMapSource(mc, args[0],
		func(el any) bool {
			res, err := call.Call1(el)
			if err != nil {
				callErr = fmt.Errorf("map(): %w", err)
				return false
			}
			out = append(out, res)
			return true
		},
		func(k, v any) bool {
			res, err := call.Call2(k, v)
			if err != nil {
				callErr = fmt.Errorf("map(): %w", err)
				return false
			}
			out = append(out, res)
			return true
		},
	)
	if !ok {
		return nil, fmt.Errorf("map(): first argument must be a list or map, got %T", args[0])
	}
	if callErr != nil {
		return nil, callErr
	}
	if out == nil {
		out = []any{}
	}
	return out, nil
}

// filterFunc's return shape follows its source shape: filtering a list
// produces the surviving elements as a []any; filtering a map produces a
// map of the surviving pairs - the predicate decides per (k, v), but the
// shape itself isn't lost the way it would be by flattening to a list of
// values.
//
// That output map is map[string]any when every surviving key happens to
// be a string (the overwhelmingly common case - owlexpr's own map
// literals can never produce anything else, see parseMapLiteral), which
// keeps filtering an ordinary map[string]any exactly as fast and exactly
// as typed for calling Go code as it always was. It's only map[any]any
// when a surviving key isn't a string - which today means the source was
// itself a map[any]any (e.g. from stdlib's groupByFunc). Which shape
// comes back isn't known until every surviving pair has been collected,
// so pairs are buffered first and the map is only built once, rather than
// committing to a map type up front and possibly needing to rebuild it
// under a different key type partway through.
//
// A real Go map can never hold a non-comparable key to begin with (Go
// itself refuses to construct one), and this function's paired source is
// always a real Go map - IterateListOrMapSource's ForEachMapPair half is
// the only thing that ever calls the (k, v) callback below, and it never
// recognizes a bare iter.Seq2 (an arbitrary, possibly non-comparable key
// generator) as a paired source at all. So an incomparable key can never
// reach the branch below, and unlike groupBy's key function (which
// computes new keys of its own), it needs no IsComparableKey check.
func filterFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("filter() expects 2 arguments (list, fn), got %d", len(args))
	}
	fn := args[1]

	// See mapFunc's comment on call - same reasoning applies here unchanged.
	call := vm.NewReusableCall(mc, fn)

	var callErr error
	var isPaired bool
	var out []any
	type survivor struct{ k, v any }
	var survivors []survivor
	allStringKeys := true

	ok := vm.IterateListOrMapSource(mc, args[0],
		func(el any) bool {
			res, err := call.Call1(el)
			if err != nil {
				callErr = fmt.Errorf("filter(): %w", err)
				return false
			}
			keep, isBool := res.(bool)
			if !isBool {
				callErr = fmt.Errorf("filter(): predicate must return a bool, got %T", res)
				return false
			}
			if keep {
				out = append(out, el)
			}
			return true
		},
		func(k, v any) bool {
			isPaired = true
			res, err := call.Call2(k, v)
			if err != nil {
				callErr = fmt.Errorf("filter(): %w", err)
				return false
			}
			keep, isBool := res.(bool)
			if !isBool {
				callErr = fmt.Errorf("filter(): predicate must return a bool, got %T", res)
				return false
			}
			if !keep {
				return true
			}
			if _, isStr := k.(string); !isStr {
				allStringKeys = false
			}
			survivors = append(survivors, survivor{k, v})
			return true
		},
	)
	if !ok {
		return nil, fmt.Errorf("filter(): first argument must be a list or map, got %T", args[0])
	}
	if callErr != nil {
		return nil, callErr
	}

	if !isPaired {
		if out == nil {
			out = []any{}
		}
		return out, nil
	}
	if allStringKeys {
		outMap := make(map[string]any, len(survivors))
		for _, s := range survivors {
			outMap[s.k.(string)] = s.v
		}
		return outMap, nil
	}
	outMap := make(map[any]any, len(survivors))
	for _, s := range survivors {
		outMap[s.k] = s.v
	}
	return outMap, nil
}

// reduceFunc is what needs a two-param lambda: reduce(list, (acc, x) =>
// acc + x, initial). Over a paired source it needs three: reduce(m, (acc,
// k, v) => ..., initial).
func reduceFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("reduce() expects 3 arguments (list, fn, initial), got %d", len(args))
	}
	fn := args[1]
	acc := args[2]

	// See mapFunc's comment on call. Unlike map/filter, acc changes every
	// iteration, so it's passed to Call2/Call3 fresh each time rather than
	// only being set up once.
	call := vm.NewReusableCall(mc, fn)

	var callErr error
	ok := vm.IterateListOrMapSource(mc, args[0],
		func(el any) bool {
			res, err := call.Call2(acc, el)
			if err != nil {
				callErr = fmt.Errorf("reduce(): %w", err)
				return false
			}
			acc = res
			return true
		},
		func(k, v any) bool {
			res, err := call.Call3(acc, k, v)
			if err != nil {
				callErr = fmt.Errorf("reduce(): %w", err)
				return false
			}
			acc = res
			return true
		},
	)
	if !ok {
		return nil, fmt.Errorf("reduce(): first argument must be a list or map, got %T", args[0])
	}
	if callErr != nil {
		return nil, callErr
	}
	return acc, nil
}

// builtinValues lets a builtin like sum/min/max be called either as
// sum(list) with a single list-like argument, or as sum(a, b, c, ...) with
// the values spread across the call's arguments directly.
func builtinValues(args []any) []any {
	if len(args) == 1 {
		if elems, ok := ListElements(args[0]); ok {
			return elems
		}
	}
	return args
}

// ListElements returns the elements of a list-like value v as a []any, and
// true if v was in fact list-like. []any is special-cased to skip
// reflection in the common case of a value produced by our own list
// literals; any other slice or array type (e.g. a []int64 from the
// environment) is walked via reflection, so these builtins aren't limited
// to lists built from our own literal syntax. Exported so a third-party
// extension (an opt-in pack like stdlib's own StringBuiltins) can accept
// the same "list-like or map-like thing from anywhere" values this
// package's own builtins do, without reimplementing the reflection
// fallback itself - see stdlib's joinFunc for exactly that use.
func ListElements(v any) ([]any, bool) {
	if list, ok := v.([]any); ok {
		return list, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	n := rv.Len()
	out := make([]any, n)
	for i := 0; i < n; i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// IsComparableKey reports whether v is safe to insert as a Go map key.
// nil always is; the five core types (matching GetTypeCode's own set)
// get a zero-reflection fast path since they're by far the common case
// for a computed key; anything else falls back to
// reflect.TypeOf(v).Comparable()
func IsComparableKey(v any) bool {
	switch v.(type) {
	case nil, string, int, int64, float64, bool:
		return true
	}
	return reflect.TypeOf(v).Comparable()
}

// numeric is the set of element types that get a native fast path in
// sum/min/max, bypassing Combine/operationsRouter entirely: no interface
// boxing per comparison, no handler-table scan, no reflection. It's
// intentionally narrow (Go's own arithmetic types) rather than reusing
// TypeCode - this is a hand-picked set of "worth special-casing" types,
// not a general extension point the way RegisterOperationalHandler is.
type numeric interface {
	~int | ~int64 | ~float64
}

func sumOrdered[T numeric](arr []T) T {
	var acc T
	for _, v := range arr {
		acc += v
	}
	return acc
}

func foldExtremeOrdered[T numeric](name string, arr []T, cmp vm.OpCode) (any, error) {
	if len(arr) == 0 {
		return nil, fmt.Errorf("%s() expects at least 1 argument", name)
	}
	best := arr[0]
	for _, v := range arr[1:] {
		if (cmp == vm.OpGreater && v > best) || (cmp == vm.OpLess && v < best) {
			best = v
		}
	}
	return best, nil
}

// foldExtreme picks the running max/min of a list of (possibly mixed-type)
// values by repeatedly asking the VM's operationsRouter to compare two
// values with cmp (OpGreater for max, OpLess for min). Going through
// Combine, rather than a hand-rolled type switch, is what lets this
// builtin support decimal.Decimal or any other future numeric type with
// zero changes here - only a RegisterOperationalHandler call for that type.
// []float64/[]int64/[]int get a native fast path first (see numeric above)
// since those are common enough to be worth skipping Combine for entirely.
func foldExtreme(mc *vm.Machine, name string, args []any, cmp vm.OpCode) (any, error) {
	if len(args) == 1 {
		switch arr := args[0].(type) {
		case []float64:
			return foldExtremeOrdered(name, arr, cmp)
		case []int64:
			return foldExtremeOrdered(name, arr, cmp)
		case []int:
			return foldExtremeOrdered(name, arr, cmp)
		}
	}

	values := builtinValues(args)
	if len(values) == 0 {
		return nil, fmt.Errorf("%s() expects at least 1 argument", name)
	}
	best := values[0]
	for _, v := range values[1:] {
		res, err := mc.Combine(v, best, cmp)
		if err != nil {
			return nil, fmt.Errorf("%s(): %w", name, err)
		}
		better, ok := res.(bool)
		if !ok {
			return nil, fmt.Errorf("%s(): comparison did not return a bool", name)
		}
		if better {
			best = v
		}
	}
	return best, nil
}
