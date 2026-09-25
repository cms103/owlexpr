package vm

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"uuid"
)

// BuiltinFunc is the signature for functions registered with
// RegisterBuiltin. The vm parameter gives builtins access to the VM's
// type-dispatch machinery (see Combine) so they can operate on mixed-type
// values (int, float64, an embedder's own registered types, ...) the same
// way the compiled arithmetic operators do, extensibly - a builtin like
// sum/min/max needs no changes to support a new numeric type, only a new
// RegisterOperationalHandler call.
type BuiltinFunc func(mc *Machine, args ...any) (any, error)

// errorType is the reflect.Type of the error interface, computed once
// rather than on every callReflectFunc call - it's only ever used to ask
// "does this return value's type implement error?", which doesn't depend
// on anything call-specific.
var errorType = reflect.TypeOf((*error)(nil)).Elem()

// OperationalHandler is a function that is used to perform operations on two operands.
type OperationalHandler func(a, b any, aTypeCode, bTypeCode TypeCode, op OpCode) (any, error)

type operationHandlerInfo struct {
	baseType      TypeCode
	coercionTypes []TypeCode
	Handler       OperationalHandler
}

func (ohi operationHandlerInfo) canCoerce(test TypeCode) bool {
	for _, t := range ohi.coercionTypes {
		if t == test {
			return true
		}
	}
	return false
}

type typeCoderRegistration struct {
	id    uuid.UUID
	coder TypeCoder
}

// FastSliceIterator lets an extension plug a new element type into
// ForEachListElement's "reflect once per call, not once per element" fast
// path (see fastSliceElementIterate in iteration.go), the same way
// RegisterOperation plugs a new type into arithmetic/comparison dispatch.
// slice is the result of rv.Interface() for a reflected slice whose
// element TypeCode matched baseType's - the iterator must type-assert it
// to its own concrete []T and call yield once per element, returning
// matched=false (never panicking) if the assertion fails, mirroring the
// core hardcoded cases' behavior for a TypeCode/concrete-type mismatch
// (e.g. a custom TypeCoder aliasing some other named type onto the same
// code).
type FastSliceIterator func(slice any, yield func(any) bool) (matched bool)

type fastSliceIteratorEntry struct {
	baseType TypeCode
	iter     FastSliceIterator
}

type Machine struct {
	disabledTypeCoercion bool
	disableStructMethods bool
	stack                []any
	builtins             PrehashedMap[BuiltinFunc]
	namespaces           PrehashedMap[PrehashedMap[BuiltinFunc]]
	opHandlers           []operationHandlerInfo

	// frames is the VM's own explicit call stack.
	// It's a Machine field, not a local variable of runFrame/pump,
	// specifically so that a builtin calling back into the VM
	// (filter/map/reduce invoking a lambda via ReusableCall, once per
	// element) pushes onto the same stack that an already-in-progress
	// evaluation is using, rather than each such call
	// starting some separate, disconnected bookkeeping structure.
	frames []frame

	// coreOpHandlers mirrors opHandlers for every entry whose baseType is
	// < coreOpHandlersSize (see that constant's doc comment) - in practice
	// always the 5 built-in core types, registered by registerDefaultOperations
	// before any embedder-supplied VMOption runs. operationDispatcher
	// indexes into this directly instead of scanning opHandlers whenever
	// both operand TypeCodes qualify, which - since int/int64/float64
	// arithmetic and comparisons are by far the hottest path through
	// Combine - matters a lot more than its small size suggests. Kept in
	// sync by registerOperationalHandler (on every registration/override)
	// and ClearOperations (reset to zero values); never read or written
	// anywhere else, so those two call sites are the only ones that need
	// to know it exists.
	coreOpHandlers [coreOpHandlersSize]operationHandlerInfo

	// fastSliceIterators is, like opHandlers, per-type *handling* state (as
	// opposed to extraTypers/dynamicTypes below, which are type
	// *recognition* state)
	fastSliceIterators []fastSliceIteratorEntry

	// extraTypers and dynamicTypes are the two ways a type not recognized
	// by GetTypeCode's fast switch gets a TypeCode - see typeCode's doc
	// comment for how they compose (order matters) and
	// registerOperationalHandler for how dynamicTypes gets populated.
	extraTypers     []typeCoderRegistration
	dynamicTypes    map[reflect.Type]TypeCode
	nextDynamicCode TypeCode

	// memberCache memoizes accessMember's method/field name search, once
	// per (concrete type, member name) pair actually seen - see
	// resolveMember's doc comment. A plain, unsynchronized map is safe
	// here for the same reason the rest of Machine's mutable state is:
	// Run's own doc comment already documents that a single Machine isn't
	// safe for concurrent use to begin with (the stack and frames are
	// shared, unsynchronized state) - only sequential reuse across many
	// calls.
	//
	// Keyed by a single composite memberCacheKey{type, name} struct, not
	// map[reflect.Type]map[string]memberResolution.
	//
	// Tried and rejected: keying by (type, precomputed HashName hash)
	// instead of (type, name) - reusing OpAccess/OpOptAccess's
	// already-computed HashedName.Hash the way Machine.builtins/namespaces
	// (PrehashedMap) do, on the theory that it would avoid re-hashing
	// name's bytes on every lookup. benchstat measured this *slower* than
	// keying by name directly, not faster: 232.3ns/op, +4.87% vs the
	// name-keyed version (p=0.000, n=10) - a string-keyed struct field
	// gets Go's map runtime's dedicated string-hash fast path, while an
	// (interface, uint64) key has no equivalent fast path and pays
	// interface-hashing overhead instead. There was no wasted re-hash to
	// eliminate; left keyed by (type, name).
	memberCache map[memberCacheKey]memberResolution
}

// memberCacheKey is Machine.memberCache's single lookup key - a (concrete
// type, member name) pair - kept as one comparable struct rather than two
// nested map levels; see memberCache's doc comment for why that's not
// just a tidier representation but a measured performance choice.
type memberCacheKey struct {
	typ  reflect.Type
	name string
}

// memberResolution is accessMember's cached name-to-index resolution for
// one (concrete type, member name) pair: either a method's Method.Index
// (for use with reflect.Value.Method) or a field's StructField.Index (for
// FieldByIndex), whichever accessMember's own MethodByName/FieldByName
// search found the *first* time this exact pair was looked up.
//
// Safe to reuse indefinitely with no re-verification, unlike an
// ahead-of-time resolution built from a sampled env
// this cache is keyed by the exact reflect.Type the
// index was computed against, in Machine.memberCache, so a lookup can only
// ever hit for a value that already has exactly that type - there is
// nothing for a later call's value to have drifted from.
//
// Only ever stored for a name that actually resolved to something -
// resolveMember/cacheMember never record a miss - so the cache can never
// grow past one entry per member a type genuinely has, regardless of how
// many distinct (and possibly nonexistent) member names an expression
// probes for: a name that doesn't exist on a type costs exactly the same
// MethodByName/FieldByName search on every call it always did, cached or
// not.
type memberResolution struct {
	isMethod    bool
	methodIndex int
	fieldIndex  []int
}

// resolveMember looks up a previously-cached method/field resolution for
// name against typ - see memberResolution's doc comment. ok is false on a
// cache miss, exactly as it would be the first time this (type, name)
// pair is ever seen, or permanently for a name that doesn't resolve to
// anything on typ at all.
func (mc *Machine) resolveMember(typ reflect.Type, name string) (memberResolution, bool) {
	res, ok := mc.memberCache[memberCacheKey{typ: typ, name: name}]
	return res, ok
}

// cacheMember records a successful method/field resolution for name
// against typ, for resolveMember to find on every subsequent access to
// the same member on the same type. Never called for a name that didn't
// resolve to anything - see memberResolution's doc comment for why that
// bound matters.
func (mc *Machine) cacheMember(typ reflect.Type, name string, res memberResolution) {
	if mc.memberCache == nil {
		mc.memberCache = make(map[memberCacheKey]memberResolution)
	}
	mc.memberCache[memberCacheKey{typ: typ, name: name}] = res
}

// UnconfiguredVM creates a VM with no bound environment. A VM has no per-env state,
// so a single instance can be reused across Run calls against different
// envs and even different compiled instruction sets - see Run.
func UnconfiguredVM(options ...VMOption) (*Machine, error) {
	vm := &Machine{
		stack:           make([]any, 0),
		builtins:        make(PrehashedMap[BuiltinFunc]),
		nextDynamicCode: firstDynamicTypeCode - 1,
	}
	// Always-on named functions (len/sum/map/filter/reduce) are NOT
	// registered here: builtins are a language-level (root owlexpr
	// package) concept, defined in root's builtin_funcs.go, which this
	// package has no dependency on. Root's own NewVM wrapper injects that
	// registration as the first VMOption in the list it passes to this
	// function, ahead of any options the caller supplied - so
	// ClearOperations()/DisableBuiltIns(), passed as later options, can
	// still override the defaults, with this package never needing to know
	// builtins exist at all.
	registerDefaultOperations(vm)

	for _, config := range options {
		err := config(vm)
		if err != nil {
			return nil, err
		}
	}
	return vm, nil
}

/*
Combine applies an OpCode to two values.

The OperationHandlers are used to execute the operation.

To determine which operation handler to use, it:

 1 - Determines the TypeCodes for a and b
 2 - If they are the same, it chooses the handler with that base type
 3 - If they are different, it looks at each handler to see if it supports coercion

 If automatic coercion has been disabled, only equal types are supported.

*/

func (mc *Machine) Combine(a, b any, op OpCode) (any, error) {
	return mc.operationDispatcher(a, b, op, !mc.disabledTypeCoercion)
}

// CoercValue is used for explict conversion of a type to another
// It uses the same OperationHandlers dispatch mechanism as Combine
// targetValue is an example of the type to convert to, for example:
// CoercValue (float64(4.5), int64(0)) will attempt to convert the value 4.5 to an int64.
func (mc *Machine) CoercValue(aValue any, targetValue any) (any, error) {
	return mc.operationDispatcher(aValue, targetValue, OpCoerce, true)
}

func (mc *Machine) operationDispatcher(a, b any, op OpCode, autoCoerc bool) (any, error) {
	aType := mc.typeCode(a)
	bType := mc.typeCode(b)

	var aTypeHandler, bTypeHandler operationHandlerInfo
	var haveA, haveB bool

	// Fast path: both operand TypeCodes are "core" (small, dense - see
	// coreOpHandlers' doc comment) - index it directly instead of
	// scanning opHandlers below. This is never merely an approximation of
	// the scan: registerOperationalHandler/ClearOperations keep
	// coreOpHandlers in exact sync with opHandlers for every TypeCode in
	// this range, and a dynamic TypeCode (RegisterOperation's own default,
	// always >= firstDynamicTypeCode) never qualifies - by far the common
	// case for anything beyond the five built-in core types - so it
	// always takes the unchanged scan instead. The >= 0 checks guard a
	// pathological custom TypeCoder returning a negative code, which
	// would otherwise be a negative array index; anything outside
	// [0, coreOpHandlersSize) simply falls through to the scan, exactly
	// as it always has.

	// Was originally one test, split to speed up the "one core, one custom" path

	if aType >= 0 && int(aType) < coreOpHandlersSize {
		if h := mc.coreOpHandlers[aType]; h.Handler != nil {
			aTypeHandler = h
			haveA = true
		}
		if aType == bType {
			bTypeHandler, haveB = aTypeHandler, haveA
		}
	}
	if !haveB && bType >= 0 && int(bType) < coreOpHandlersSize {
		if h := mc.coreOpHandlers[bType]; h.Handler != nil {
			bTypeHandler = h
			haveB = true
		}
	}

	// If either A or B is missing, we need to do the opHandlers lookup
	if !haveA || !haveB {
		// opHandlers holds at most one entry per TypeCode (registration now
		// overwrites in place - see registerOperationalHandler), so the first
		// match for aType/bType is the *only* match: safe to stop as soon as
		// both are found instead of always scanning the full table.
		for i := range mc.opHandlers {
			candidate := &mc.opHandlers[i]
			if !haveA && candidate.baseType == aType {
				aTypeHandler = *candidate
				haveA = true
			}
			if !haveB && candidate.baseType == bType {
				bTypeHandler = *candidate
				haveB = true
			}
			if haveA && (aType == bType || haveB) {
				break
			}
		}
	}

	if aType == bType {
		// A value is always already its own type - true regardless of
		// whether that type's OperationalHandler happens to implement
		// OpCoerce (most don't; only the numeric types that support real
		// conversion between each other do). Without this, CoercValue on
		// same-type operands would incorrectly fail for any other
		// registered type (bool, string, time.Time, ...) whose handler's
		// OpCoerce case doesn't exist rather than returning a unchanged.
		if op == OpCoerce {
			return a, nil
		}

		// Easy - just dispatch - find the right handler, if one is
		// registered. aTypeHandler stays its zero value (nil Handler) when
		// nothing in opHandlers matched aType - e.g. after ClearOperations()
		// with no replacement registered for this type - so this must be a
		// clean error, not a call through a nil func.
		if aTypeHandler.Handler == nil {
			return nil, fmt.Errorf("Type operation %v for type code %v not supported", op, aType)
		}
		return aTypeHandler.Handler(a, b, aType, bType, op)
	}

	// See if type coercion has been disabled
	if !autoCoerc {
		return nil, fmt.Errorf("Type coercion is disabled and types differ for operation %v (types %T and %T)", op, a, b)
	}

	// Different types - choose the one that works. Unlike the aType == bType
	// branch above, these two don't need their own nil-Handler check: a
	// zero-value (unregistered) operationHandlerInfo has a nil
	// coercionTypes slice, so canCoerce is always false for it - the only
	// way to reach .Handler(...) here is via an entry that actually came
	// from registerOperationalHandler, which rejects a nil handler at
	// registration time.
	if aTypeHandler.canCoerce(bType) {
		// That works - use it
		return aTypeHandler.Handler(a, b, aType, bType, op)
	}

	if bTypeHandler.canCoerce(aType) {
		// That works - use it
		return bTypeHandler.Handler(a, b, aType, bType, op)
	}

	return nil, fmt.Errorf("Type operation %v for %v and %v type codes not supported", op, aType, bType)

}

// Call invokes any callable value the language runtime produces or
// accepts - a lambda closure (from a `x => ...` literal), a registered
// BuiltinFunc, a bound struct method (a reflect.Value from accessMember),
// or a plain Go function - with the given arguments.
// OpCall uses it internally, exported so a builtin like map/filter/reduce can
// invoke a function-valued argument the same way.
func (mc *Machine) Call(callee any, args []any) (any, error) {
	return callFunction(mc, callee, args)
}

// callFrame is a reusable per-callee invocation buffer for a loop that
// calls the *same* function value many times with different arguments -
// exactly the shape filter/map/reduce have (once per list element). Get
// one via newCallFrame before the loop starts, then call callReusing once
// per element - not mc.Call, which would pay closure.call's normal
// per-call allocation cost (a fresh values slice and localFrame) on every
// single element. See ReusableCall (reusable_call.go) for the exported,
// arity-based surface every caller outside this package actually uses -
// this type and its two functions are that surface's private
// implementation.
//
// callee doesn't need to be any particular shape: newCallFrame only
// takes the allocation-avoiding fast path for a closure literal whose
// body provably can't let its own param scope escape a single call (see
// LambdaProto.NoEscape, computed once at compile time) - for anything
// else (a BuiltinFunc, a bound method, a plain Go function, or a closure
// that fails that check), callReusing transparently falls back to the
// normal Call path, so a caller never needs its own type-switch on
// callee to decide which path applies.
type callFrame struct {
	fastPath  bool
	proto     *LambdaProto
	machine   *Machine
	envScopes []map[string]any
	locals    *localFrame
}

// newCallFrame prepares to call callee repeatedly against mc. Cheap to
// call even when the fast path doesn't apply - just a single type
// assertion and a NoEscape field read (see its own doc comment for why
// that field must already be a plain, precomputed bool) - so it's safe to
// call unconditionally once per filter/map/reduce invocation, not just
// when the caller already knows callee is a suitable closure.
func newCallFrame(mc *Machine, callee any) *callFrame {
	c, ok := callee.(*closure)
	if !ok || !c.proto.NoEscape {
		return &callFrame{fastPath: false}
	}
	return &callFrame{
		fastPath:  true,
		proto:     c.proto,
		machine:   mc,
		envScopes: c.envScopes,
		locals:    &localFrame{parent: c.locals, values: make([]any, len(c.proto.Params))},
	}
}

// callReusing invokes callee (the same value passed to newCallFrame,
// which is why callee is still an explicit parameter here rather than
// stored on frame - callReusing itself stays stateless about which
// callee it's serving) with args. On frame's fast path, this overwrites
// frame's own localFrame's values in place instead of allocating a fresh
// one - safe (nothing could ever be holding a reference to a specific
// past call's bindings to notice the reuse) for exactly the reason
// newCallFrame's fast path is conditioned on NoEscape in the first
// place. Every other case - fastPath is false, or this particular call's
// arg count doesn't match the closure's params (the same clean error
// closure.call itself would give, e.g. a lambda written for the wrong
// filter/map/reduce source shape) - is handled without touching frame's
// buffers at all.
func (mc *Machine) callReusing(callee any, args []any, frame *callFrame) (any, error) {
	if !frame.fastPath || len(args) != len(frame.proto.Params) {
		return callFunction(mc, callee, args)
	}
	copy(frame.locals.values, args)
	return frame.machine.runFrame(frame.proto.Instructions, frame.envScopes, frame.locals)
}

// typeCode is the VM's actual type-classification entry point. Type
// recognition is composed from three independent sources rather than one
// swappable coder function, so that multiple independent extensions (each
// adding support for its own new types) can each contribute recognition
// without one clobbering another's - see RegisterTypeCoder and
// RegisterOperation in vm_config.go.
//
// Dispatch order, deliberately in this sequence:
//
//  1. GetTypeCode's fast type-switch always runs first, unconditionally -
//     zero reflection cost for the five core types. A custom coder can
//     never override what those five mean: letting one reclassify a core
//     type would be a footgun (arithmetic and comparisons on int/int64/
//     float64/string/bool always mean what GetTypeCode says they mean, for
//     every Machine), so that path doesn't exist.
//  2. extraTypers (installed via RegisterTypeCoder) run next, ahead of the
//     dynamicTypes map. This is a performance escape hatch: if an
//     embedder registers many new types, a hand-rolled type-switch
//     TypeCoder over all of them is cheaper than a reflect.TypeOf-keyed
//     map lookup per call. It's also what makes a custom coder's
//     classification "win" over an auto-assigned dynamic code -
//     registerOperationalHandler calls typeCode first and only mints a
//     new dynamic code if nothing already recognizes the type, so a
//     SetTypeCoder installed before the matching RegisterOperation call
//     makes that RegisterOperation reuse the custom coder's own codes
//     instead of assigning redundant ones.
//  3. dynamicTypes is the fallback: whatever RegisterOperation had to
//     mint a fresh code for because nothing above recognized the type.
func (mc *Machine) typeCode(a any) TypeCode {
	if tc := GetTypeCode(a); tc != UnSupportedType {
		return tc
	}
	for _, registration := range mc.extraTypers {
		if tc := registration.coder(a); tc != UnSupportedType {
			return tc
		}
	}
	if rt := reflect.TypeOf(a); rt != nil {
		if tc, ok := mc.dynamicTypes[rt]; ok {
			return tc
		}
	}
	return UnSupportedType
}

// resolveOrAssignTypeCode returns sample's TypeCode if it's already
// recognized by typeCode (core switch, an extraTyper, or an earlier
// dynamic registration of this exact concrete type), or mints and
// remembers a fresh one keyed by reflect.TypeOf(sample). This is what
// lets RegisterOperation register a brand-new type with a single call -
// no separate type-registration step, no shared enum for independent
// extensions to coordinate on.
func (mc *Machine) resolveOrAssignTypeCode(sample any) (TypeCode, error) {
	if tc := mc.typeCode(sample); tc != UnSupportedType {
		return tc, nil
	}
	rt := reflect.TypeOf(sample)
	if rt == nil {
		return UnSupportedType, errors.New("cannot register an operation for untyped nil")
	}
	if mc.dynamicTypes == nil {
		mc.dynamicTypes = make(map[reflect.Type]TypeCode)
	}
	mc.nextDynamicCode++
	mc.dynamicTypes[rt] = mc.nextDynamicCode
	return mc.nextDynamicCode, nil
}

func (mc *Machine) registerOperationalHandler(baseType any, coercibleTypes []any, handler OperationalHandler) error {
	if handler == nil {
		return errors.New("handler must not be nil")
	}

	baseTypeCode, err := mc.resolveOrAssignTypeCode(baseType)
	if err != nil {
		return fmt.Errorf("baseType: %w", err)
	}

	coercibleTypesCodes := make([]TypeCode, 0, len(coercibleTypes))
	for _, aType := range coercibleTypes {
		aTypeCode, err := mc.resolveOrAssignTypeCode(aType)
		if err != nil {
			return fmt.Errorf("coercible type %v: %w", aType, err)
		}
		coercibleTypesCodes = append(coercibleTypesCodes, aTypeCode)
	}
	newEntry := operationHandlerInfo{
		baseType:      baseTypeCode,
		coercionTypes: coercibleTypesCodes,
		Handler:       handler,
	}

	// Overwrite in place if baseTypeCode is already registered (e.g. a
	// caller replacing the default int64 handler without calling
	// ClearOperations() first - see TestOverrideOperationWithoutClearing)
	// rather than appending a second entry for the same type. This keeps
	// opHandlers at most one entry per TypeCode, which is what lets
	// operationsRouter's dispatch loop stop at the first match instead of
	// having to scan to the end to find the *last* matching entry.
	overwrote := false
	for i := range mc.opHandlers {
		if mc.opHandlers[i].baseType == baseTypeCode {
			mc.opHandlers[i] = newEntry
			overwrote = true
			break
		}
	}
	if !overwrote {
		mc.opHandlers = append(mc.opHandlers, newEntry)
	}

	// Keep coreOpHandlers in sync for anything within its range - see its
	// doc comment. Every core type (int/int64/float64/string/bool) always
	// qualifies; a custom type only does if an embedder deliberately gave
	// it a small TypeCode via RegisterTypeCoder, which is unusual but not
	// wrong - see coreOpHandlersSize's doc comment.
	if int(baseTypeCode) < coreOpHandlersSize {
		mc.coreOpHandlers[baseTypeCode] = newEntry
	}
	return nil
}

func (mc *Machine) RegisterBuiltin(name string, fn BuiltinFunc) {
	mc.builtins.Put(name, fn)
}

// RegisterNamespacedBuiltin adds fn under namespace.name, callable from
// expressions as `namespace.name(...)` (e.g. RegisterNamespacedBuiltin
// ("string", "trim", trimFunc) for `string.trim(x)`) - see accessMember's
// generic map-dot-access rule, which is what actually resolves the
// `.name` step: a namespace is nothing more than a map[string]BuiltinFunc
// that OpLoad pushes and OpAccess then indexes into like any other
// string-keyed map. namespace itself is never independently callable
// (there's no bare `namespace(...)` unless something separately
// registers that exact name via RegisterBuiltin) - only its members are.
func (mc *Machine) RegisterNamespacedBuiltin(namespace, name string, fn BuiltinFunc) {
	if mc.namespaces == nil {
		mc.namespaces = make(PrehashedMap[PrehashedMap[BuiltinFunc]])
	}
	ns, ok := mc.namespaces.Get(HashName(namespace), namespace)
	if !ok {
		ns = make(PrehashedMap[BuiltinFunc])
		mc.namespaces.Put(namespace, ns)
	}
	ns.Put(name, fn)
}

// registerFastSliceIterator backs RegisterFastSliceIterator - see
// FastSliceIterator's doc comment for the contract. Resolving baseType via
// resolveOrAssignTypeCode (the same call registerOperationalHandler makes)
// is what lets this compose with RegisterOperation for the same type in
// either order: whichever call happens first mints the TypeCode, the other
// just reuses it.
func (mc *Machine) registerFastSliceIterator(baseType any, iter FastSliceIterator) error {
	if iter == nil {
		return errors.New("iterator must not be nil")
	}
	code, err := mc.resolveOrAssignTypeCode(baseType)
	if err != nil {
		return fmt.Errorf("baseType: %w", err)
	}
	mc.fastSliceIterators = append(mc.fastSliceIterators, fastSliceIteratorEntry{baseType: code, iter: iter})
	return nil
}

// closure is the runtime value produced by evaluating a lambda literal: a
// compiled LambdaProto plus the lexical environment that was in effect at
// the point the lambda was created - this is what makes `x => y => x +
// y` work, since the inner lambda's closure includes the outer lambda's
// own param binding. That environment is two independent chains, per the
// split localFrame's doc comment describes: envScopes (the dynamic,
// string-keyed env/Run-caller chain, unaffected by lambdas/lets and so
// always identical to whatever was active at the top of the current
// Run/RunScopes call) and locals (the lexically-resolved lambda-param/
// let chain, extended by exactly one frame per enclosing binding). It
// satisfies owlexpr's callable-value convention (see callFunction) the
// same way a BuiltinFunc or a bound struct method does, so it can be
// passed to builtins (map/filter/reduce), struct methods, and plain Go
// functions alike.
type closure struct {
	proto     *LambdaProto
	machine   *Machine
	envScopes []map[string]any
	locals    *localFrame
}

// call is the general (non-reused) invocation path - mc.Call, and
// anything callFunction dispatches to. args is copied into the new
// frame's own values slice rather than aliased directly: unlike OpCall's
// direct-closure fast path in pump (which builds a fresh, throwaway args
// slice for exactly this one call and nothing else), args here comes
// from a caller outside this package's control (an embedder's own
// mc.Call, or a Go callback bridged via makeReflectFuncAdapter) that may
// go on to reuse or mutate its backing array - and a nested closure
// created inside this call's body can capture this exact frame by
// reference (see LambdaProto.NoEscape), keeping it alive, and readable,
// well after call returns.
func (c *closure) call(args []any) (any, error) {
	if len(args) != len(c.proto.Params) {
		return nil, fmt.Errorf("lambda expects %d argument(s), got %d", len(c.proto.Params), len(args))
	}
	locals := &localFrame{parent: c.locals, values: append([]any(nil), args...)}
	return c.machine.runFrame(c.proto.Instructions, c.envScopes, locals)
}

// lookupScopes walks a scope chain innermost-first, so a name bound
// closer to the point of reference shadows one bound further out.
func lookupScopes(scopes []map[string]any, name string) (any, bool) {
	for i := len(scopes) - 1; i >= 0; i-- {
		if val, ok := scopes[i][name]; ok {
			return val, true
		}
	}
	return nil, false
}

// localFrame is one activation's worth of lexically-resolved bindings - a
// lambda's parameters, or a let's single bound name - addressed by
// position (OpLoadLocal's LocalRef) rather than by name, chained to the
// localFrame that was active where this one's lambda/let literal was
// written. It exists as a chain separate from envScopes because the two
// kinds of binding have fundamentally different purposes:
//
//   - envScopes ([]map[string]any - see closure's doc comment) is a
//     caller-supplied dynamic dictionary whose keys the compiler can't
//     know ahead of time (Run's env, or Sheet's per-cell layers via
//     RunScopes), so it has to stay name-based, looked up via
//     lookupScopes on every reference.
//   - Every binding the language itself introduces - a lambda's params, a
//     let's name - has a fixed, fully-known-at-compile-time name:
//     compiler.go's childWithBound/resolveLocal track exactly which frame
//     and slot a name resolves to while compiling, so pump never needs to
//     hash or search a name for these at all, and OpLet/OpMakeClosure/
//     OpCall's closure path only need to allocate one small (parent
//     pointer + values slice) struct per frame instead of a map plus a
//     scopes-slice copy - see their own cases in pump below, and
//     ReusableCall's doc comment for how the filter/map/reduce hot path
//     reuses even that.
//
// parent is nil for a lambda/let with no enclosing lambda/let of its
// own; walking Depth parents from the frame active when an OpLoadLocal
// instruction runs is exactly the lexical-addressing scheme SICP
// describes - resolved once, by the compiler, rather than searched for
// on every reference the way lookupScopes still does for env names.
type localFrame struct {
	parent *localFrame
	values []any
}

func (mc *Machine) push(val any) {
	mc.stack = append(mc.stack, val)
}

// deferredMember wraps a reflect.Value produced by accessMember/indexValue
// for a struct- or array-kind field/element, instead of eagerly boxing it
// via .Interface(). This matters because Go's reflect.Value.Interface()
// only defensively copies when the Value is *addressable* -
// the case for a field/element reached through a pointer (the
// natural, idiomatic way to put a large Go value in an env map without an
// up-front copy). Without this, `Env.Inner.Field` on a pointer-stored Env
// would box - and therefore deep-copy - the entire (possibly huge) Inner
// struct just to read one int off it, every single time the expression
// evaluates.
//
// Keeping the result as a deferredMember instead lets a following field/
// index access (accessMember/indexValue again, via OpAccess/OpIndex's use
// of popRaw below) continue the walk directly against the same
// reflect.Value - no re-reflection, and critically, no .Interface() call
// on the (large) intermediate value at all. Materialization only happens
// once, via materialize()/pop() below, at the point a value is actually
// consumed by anything other than another field/index access - by then
// it's typically a small leaf value (an int, a string, ...), for which
// the addressable-copy cost is negligible even when it does apply.
//
// This is a distinct type - not a bare reflect.Value - specifically so it
// can never collide with callFunction's existing, unrelated convention of
// passing a bound method around as a raw reflect.Value (see accessMember's
// val.MethodByName branch and IsCallable): pop()'s materialization below
// only unwraps deferredMember, so a bound method value flows through
// completely unaffected.
type deferredMember struct {
	rv reflect.Value
}

func (d deferredMember) materialize() any {
	return d.rv.Interface()
}

// pop removes and returns the top of the stack, materializing a
// deferredMember (see above) into its real, boxed value first - the
// right default for every consumer except the field/index-access chain
// itself, which uses popRaw instead to keep a deferred value deferred
// across consecutive hops.
func (mc *Machine) pop() any {
	val := mc.popRaw()
	if dm, ok := val.(deferredMember); ok {
		return dm.materialize()
	}
	return val
}

// popRaw is pop without deferredMember materialization - used only by
// OpAccess/OpOptAccess/OpIndex/OpOptIndex for their base "target" operand,
// so a chain of field/index accesses (`Env.Inner.Field`, `Data[0][0]`) can
// pass a deferredMember from one hop straight into the next without ever
// boxing the intermediate compound value.
func (mc *Machine) popRaw() any {
	idx := len(mc.stack) - 1
	val := mc.stack[idx]
	mc.stack = mc.stack[:idx]
	return val
}

// Run executes instructions against env, resolving OpLoad against env first
// (falling back to registered builtins). env is a per-call parameter rather
// than state on the VM, so one VM can be reused. Concurrent use isn't
// supported (the stack and frames are shared, unsynchronized state), but
// sequentially, across many different envs and instruction sets - without
// paying NewVM's setup cost each time.
//
// If the expression's final value is a lambda closure - e.g. Run evaluates
// `x => x * 2` itself, rather than calling it - the result is passed
// through WrapCallable before returning, so the embedder gets back an
// ordinary callable Go func value instead of owlexpr's own internal
// closure representation. Every other result type is unaffected.
func (mc *Machine) Run(instructions []Instruction, env map[string]any) (any, error) {
	result, err := mc.runFrame(instructions, []map[string]any{env}, nil)
	if err != nil {
		return nil, err
	}
	return WrapCallable(result), nil
}

// RunScopes is Run generalized to an explicit, pre-layered scope chain
// instead of a single env map. It exists for callers outside this package
// that need multiple name->value layers without merging them into one map
// up front (owlexpr's Sheet feature is the first such caller: an outer
// env layer plus a small, per-cell layer of upstream cell results,
// avoiding an O(env size) copy per cell). scopes is searched innermost-
// first, i.e. last element first - see lookupScopes. This is a pure
// forwarder to runFrame: Run's behavior and cost are unchanged, EXCEPT
// that - unlike Run - RunScopes deliberately does NOT apply
// WrapCallable to its result: a raw *closure result may still need to be
// callable the normal internal way (e.g. Sheet passing it to another
// cell's RunScopes call via `sheet.<name>`) before whatever's driving
// RunScopes is itself done with it and ready to hand a result out to an
// embedder - at which point that caller applies WrapCallable itself, the
// same way RunSheet does when it populates its final result map. The
// instructions being run start with no lexically-resolved (lambda-param/
// let) bindings of their own yet, same as Run - see localFrame's doc
// comment for why that's a separate chain from scopes.
func (mc *Machine) RunScopes(instructions []Instruction, scopes []map[string]any) (any, error) {
	return mc.runFrame(instructions, scopes, nil)
}

// frame is one *suspended* activation record on the VM's own explicit
// call stack, Machine.frames - never the currently-executing one, which
// pump keeps in plain local variables instead (see pump's own doc comment)
// Entering a lambda call, a let-binding's
// body, or a ??'s left operand doesn't recurse into a Go function the way
// a naive tree-walking evaluator would; instead, the caller's state is
// saved explicitly onto this stack (see saveContinuation) and pump's own
// locals are switched to the new instructions - a slice append/truncate
// instead of a Go function call, at any nesting depth, and restored the
// same way (a slice pop into those locals) once the nested sequence
// completes.
//
// Two things this buys, beyond a modest reduction in per-call overhead
// for genuinely nested calls:
//
//   - A deeply or accidentally-infinitely recursive lambda (a
//     factorial-style closure missing its base case, say) grows a plain
//     Go slice instead of the OS thread's call stack. Go's own stack
//     grows dynamically and would eventually die with a fatal,
//     unrecoverable "stack overflow" - not a catchable error, nothing a
//     caller embedding owlexpr inside a larger service could defend
//     against. Here it's just mc.frames getting longer, checked against
//     maxFrameDepth on every save and returned as an ordinary error the
//     moment it's exceeded - see saveContinuation.
//   - Every call site that invokes a lambda back into the VM - not just
//     a direct call inside an expression (OpCall's *closure case in
//     pump), but filter/map/reduce's per-element ReusableCall calls too -
//     shares this exact same stack with whatever's already running,
//     rather than each one starting its own independent recursion.
//
// instructions/scopes/locals/pc/coalesce are exactly pump's own local
// variables at the moment they were saved - see pump for what each one
// means during execution; here they're simply parked until restored.
//
// stackBase is the length mc.stack had when this frame's instructions
// started running. An error unwinding past this frame truncates mc.stack
// back to it, discarding whatever partial, now-abandoned work those
// instructions had pushed but not yet consumed - e.g. the `a` in
// `a + 1/0`: stranded on mc.stack the moment OpDiv fails, since OpAdd
// never runs to pop it. Without this, values like that would linger on
// mc.stack for the rest of the Machine's life, since nothing else ever
// resets it between Run() calls on a reused Machine.
//
// coalesce is non-nil only when this saved frame is itself sitting inside
// a ??'s left-hand operand (i.e. it was saved by something the left
// operand called, not by the OpCoalesce instruction itself - see pump's
// OpCoalesce case, which saves the *parent's* continuation with the
// parent's own coalesce, not a new one) - carried along so an error or a
// nested completion correctly finds its way back to whichever enclosing
// `??`, if any, is responsible for it.
type frame struct {
	instructions []Instruction
	scopes       []map[string]any
	locals       *localFrame
	pc           int
	stackBase    int
	coalesce     *CoalesceArg

	// flatCall marks a frame saved for a direct call to a NoEscape
	// closure whose args/locals live directly on mc.stack rather than in
	// a separately heap-allocated values slice - see pump's OpCall case
	// and its "flattened fast path" comment. stackBase for such a frame
	// is the index the callee itself occupied (not len(mc.stack) at call
	// time, as for every other frame kind): on error it's the correct
	// truncation point regardless, but on normal completion pump must
	// additionally compact the lone result down onto that slot - see the
	// pc >= len(instructions) branch.
	flatCall bool
}

// maxFrameDepth bounds Machine.frames - see frame's doc comment for why
// this exists at all: without it, a runaway recursive lambda would grow
// mc.frames (and, since building each new scope layer and param binding
// allocates, the Go heap behind it) without limit instead of failing
// cleanly. 10000 is generous for any realistically-authored expression,
// including a deliberately recursive one, while still catching a genuine
// bug - a missing base case - in well under a second rather than after
// exhausting available memory.
const maxFrameDepth = 10000

// runFrame is the VM's one entry point for "evaluate this instruction
// sequence against this scope chain and give me its result": Run,
// RunScopes, a lambda invoked from Go code (closure.call, which backs the
// exported Call) and ReusableCall's fast path all go through it. It's a
// thin wrapper around pump, the trampoline that actually drives execution
// - see pump's own doc comment.
func (mc *Machine) runFrame(instructions []Instruction, scopes []map[string]any, locals *localFrame) (any, error) {
	base := len(mc.frames)
	if err := mc.pump(instructions, scopes, locals, base); err != nil {
		return nil, err
	}
	if len(mc.stack) == 0 {
		return nil, nil
	}
	return mc.pop(), nil
}

// saveContinuation records instructions/scopes/locals/pc/coalesce/
// stackBase - pump's own local variables at the moment something (a let,
// a ??, a direct lambda call) is about to switch those locals to a new,
// unrelated instruction sequence - onto mc.frames, so pump can restore
// them once that new sequence finishes. This is the one explicit step
// that stands in for what a Go function call's own stack frame would give
// for free in a recursive design. The only failure mode is
// maxFrameDepth - see frame's doc comment for why that exists.
func (mc *Machine) saveContinuation(instructions []Instruction, scopes []map[string]any, locals *localFrame, pc int, coalesce *CoalesceArg, stackBase int, flatCall bool) error {
	if len(mc.frames) >= maxFrameDepth {
		return fmt.Errorf("call stack depth exceeded %d instructions deep - likely unbounded recursion", maxFrameDepth)
	}
	mc.frames = append(mc.frames, frame{
		instructions: instructions,
		scopes:       scopes,
		locals:       locals,
		pc:           pc,
		coalesce:     coalesce,
		stackBase:    stackBase,
		flatCall:     flatCall,
	})
	return nil
}

// pump is the VM's whole execution engine: one loop, executing one
// instruction at a time from whichever sequence is currently active.
// "Currently active" lives entirely in this function's own local
// variables - instructions, scopes, locals, pc, coalesce, stackBase -
// rather than being read back out of mc.frames on every single
// instruction. The handful of places that need to evaluate a nested
// sequence (OpCall into a *closure, OpLet, OpCoalesce) don't call pump
// again recursively: they save these locals onto mc.frames
// (saveContinuation) and overwrite them in place with the nested
// sequence's own instructions/scopes/locals/pc=0/stackBase, then loop
// around. When THAT sequence's pc runs off the end (or fails - see the
// two symmetric blocks below), the locals are restored by popping
// mc.frames instead of returning from a call.
//
// scopes and locals are two independent chains, not one - see
// localFrame's doc comment for why. OpLet and OpCall's direct-closure
// case below only ever extend locals (scopes is carried through
// unchanged - a let/lambda body sees exactly the same env its caller
// did); scopes only changes when execution switches into a closure whose
// own captured envScopes differs from what's currently active (a
// closure invoked well after, and possibly by a completely different
// Run() call than, the one that created it).
//
// Keeping the active sequence in local variables, rather than always
// indirecting through mc.frames, means a flat, unnested expression - still
// the overwhelming common case - never touches mc.frames at all, and only
// genuine nesting (a lambda call, a let, a "??") pays for a slice
// append/pop. See frame's own doc comment for what mc.frames holds once
// something IS nested, and why that matters beyond raw speed.
//
// base is the mc.frames depth this call started at (from runFrame) -
// pump returns once execution has unwound back down to it, one way or
// the other.
func (mc *Machine) pump(instructions []Instruction, scopes []map[string]any, locals *localFrame, base int) error {
	pc := 0
	var coalesce *CoalesceArg
	stackBase := len(mc.stack)
	flatCall := false

	for {
		if pc >= len(instructions) {
			// The active sequence just completed normally. A ??'s
			// left-hand operand (coalesce != nil) needs its result
			// checked before resuming whatever's saved below it: a
			// non-nil result means the right-hand operand - compiled
			// inline into the parent, immediately after OpCoalesce -
			// must be skipped, so the resumed pc jumps to JumpEnd
			// instead of continuing into it; a nil result is discarded
			// and falls through, exactly like the error-unwind block
			// below treats an error from the same position. Anything
			// else (a lambda call, a let body, the top-level program)
			// needs no such check: the one value this sequence left on
			// mc.stack already IS its result, sitting exactly where the
			// resumed sequence's own next instruction expects to find
			// it - UNLESS this was a flatCall (see OpCall's flattened
			// fast path): there, the wasted callee slot and the argument
			// values are still sitting on mc.stack below the result
			// (nothing popped them going in), so the result has to be
			// compacted down onto stackBase - the slot the callee itself
			// occupied - before anything resumes. coalesce and flatCall
			// never both apply to the same activation (see frame's doc
			// comment: OpCoalesce's own left operand is never entered via
			// the flattened OpCall path), so these two checks don't
			// interact.
			skipRight, jumpEnd := false, 0
			if coalesce != nil {
				if IsNilResult(mc.stack[len(mc.stack)-1]) {
					mc.pop() // discard the nil left-hand result; fall through to the right-hand operand
				} else {
					skipRight, jumpEnd = true, coalesce.JumpEnd
				}
			} else if flatCall {
				mc.stack[stackBase] = mc.stack[len(mc.stack)-1]
				mc.stack = mc.stack[:stackBase+1]
			}
			if len(mc.frames) == base {
				return nil // back to runFrame's own starting point - done
			}
			top := len(mc.frames) - 1
			f := mc.frames[top]
			mc.frames = mc.frames[:top]
			instructions, scopes, locals, pc, coalesce, stackBase, flatCall = f.instructions, f.scopes, f.locals, f.pc, f.coalesce, f.stackBase, f.flatCall
			if skipRight {
				pc = jumpEnd
			}
			continue
		}

		inst := instructions[pc]
		pc++

		var err error
		switch inst.Op {
		case OpPush:
			mc.push(inst.Arg)

		case OpLoad:
			hn := asHashedName(inst.Arg)
			if val, ok := lookupScopes(scopes, hn.Name); ok {
				mc.push(val)
			} else if builtin, ok := mc.builtins.Get(hn.Hash, hn.Name); ok {
				mc.push(builtin)
			} else if ns, ok := mc.namespaces.Get(hn.Hash, hn.Name); ok {
				mc.push(ns)
			} else {
				err = fmt.Errorf("undefined variable or function: %s", hn.Name)
			}

		case OpLoadLocal:
			ref := inst.Arg.(LocalRef)
			lf := locals
			for i := 0; i < ref.Depth; i++ {
				lf = lf.parent
			}
			mc.push(lf.values[ref.Slot])

		case OpAccess:
			target := mc.popRaw()
			var val any
			val, err = mc.accessMember(target, asHashedName(inst.Arg))
			if err == nil {
				mc.push(val)
			}

		case OpOptAccess:
			target := mc.popRaw()
			if IsNilResult(target) {
				mc.push(nil)
				break
			}
			var val any
			val, err = mc.accessMember(target, asHashedName(inst.Arg))
			if err == nil {
				mc.push(val)
			}

		case OpEqual, OpNotEqual:
			b, a := mc.pop(), mc.pop()
			// A nil (or typed-nil, per IsNilResult) operand on either side
			// is handled directly rather than via Combine/operationsRouter:
			// TypeCode-based dispatch has no sensible coercion between an
			// untyped nil and, say, a struct - and there shouldn't be one,
			// since "is this nil" needs to work against literally any
			// type. This is what lets `x == nil` work as the nil-check
			// business users would otherwise reach for `?.`/`??`/`type()`
			// to approximate, and it agrees with those on what counts as
			// nil (a typed-nil pointer field IS nil here too), rather than
			// silently disagreeing with them.
			if IsNilResult(a) || IsNilResult(b) {
				eq := IsNilResult(a) && IsNilResult(b)
				if inst.Op == OpNotEqual {
					eq = !eq
				}
				mc.push(eq)
				break
			}
			var res any
			if res, err = mc.Combine(a, b, inst.Op); err == nil {
				mc.push(res)
			}

		case OpAdd, OpSub, OpMul, OpDiv, OpMod, OpPow, OpLess, OpGreater, OpLessEq, OpGreaterEq:
			b, a := mc.pop(), mc.pop()
			var res any
			if res, err = mc.Combine(a, b, inst.Op); err == nil {
				mc.push(res)
			}

		case OpNeg:
			var res any
			if res, err = mc.negateValue(mc.pop()); err == nil {
				mc.push(res)
			}

		case OpNot:
			a := mc.pop()
			b, ok := a.(bool)
			if !ok {
				err = fmt.Errorf("'!' requires a boolean operand, got %T", a)
				break
			}
			mc.push(!b)

		case OpMakeList:
			n := inst.Arg.(int)
			list := make([]any, n)
			for i := n - 1; i >= 0; i-- {
				list[i] = mc.pop()
			}
			mc.push(list)

		case OpMakeMap:
			n := inst.Arg.(int)
			m := make(map[string]any, n)
			for i := 0; i < n; i++ {
				val := mc.pop()
				key := mc.pop()
				keyStr, ok := key.(string)
				if !ok {
					err = fmt.Errorf("map key must be a string, got %T", key)
					break
				}
				m[keyStr] = val
			}
			if err == nil {
				mc.push(m)
			}

		case OpIndex:
			idx := mc.pop()
			target := mc.popRaw()
			var val any
			if val, err = indexValue(target, idx); err == nil {
				mc.push(val)
			}

		case OpOptIndex:
			idx := mc.pop()
			target := mc.popRaw()
			if IsNilResult(target) {
				mc.push(nil)
				break
			}
			var val any
			if val, err = indexValue(target, idx); err == nil {
				mc.push(val)
			}

		case OpSlice:
			high := mc.pop()
			low := mc.pop()
			target := mc.pop()
			var val any
			if val, err = sliceValue(target, low, high); err == nil {
				mc.push(val)
			}

		case OpIn:
			haystack := mc.pop()
			needle := mc.pop()
			var found any
			if found, err = inValue(mc, needle, haystack); err == nil {
				mc.push(found)
			}

		case OpMatches:
			pattern := mc.pop()
			str := mc.pop()
			var found any
			if found, err = matchesValue(str, pattern); err == nil {
				mc.push(found)
			}

		case OpMakeClosure:
			proto := inst.Arg.(*LambdaProto)
			mc.push(&closure{proto: proto, machine: mc, envScopes: scopes, locals: locals})

		case OpLet:
			arg := inst.Arg.(*LetArg)
			val := mc.pop()

			// One small (parent pointer + 1-element values slice) frame,
			// chained onto the current locals - not scopes, which a let
			// never touches at all (a let body sees exactly the same env
			// its surrounding expression did). Nothing needs to "pop" this
			// back off afterward: the outer `locals` variable is a plain
			// pointer, left completely untouched, so once this frame's
			// body finishes and pump restores the saved continuation, it's
			// simply unreferenced again.
			bodyLocals := &localFrame{parent: locals, values: []any{val}}

			if err = mc.saveContinuation(instructions, scopes, locals, pc, coalesce, stackBase, flatCall); err == nil {
				instructions, locals, pc, coalesce, stackBase, flatCall = arg.Body, bodyLocals, 0, nil, len(mc.stack), false
			}

		case OpCoalesce:
			// Save the parent's own continuation - its instructions,
			// scopes, locals, pc already advanced past this OpCoalesce,
			// and its own coalesce - then switch the active locals to the
			// left operand, marked with arg so this function's own
			// completion/error handling above and below know to catch
			// an error or nil result here rather than letting it
			// propagate further. The right-hand operand needs no such
			// save: it's compiled inline, immediately following this
			// instruction in the parent's own instructions, so it runs
			// naturally once the parent is resumed - either at its
			// saved pc (fall-through) or at arg.JumpEnd (skipped, once
			// the left operand succeeds). scopes/locals are untouched -
			// a "??"'s left operand isn't a new binding scope of any kind,
			// just isolated for error/nil catching.
			arg := inst.Arg.(*CoalesceArg)
			if err = mc.saveContinuation(instructions, scopes, locals, pc, coalesce, stackBase, flatCall); err == nil {
				instructions, pc, coalesce, stackBase, flatCall = arg.Left, 0, arg, len(mc.stack), false
			}

		case OpJumpIfFalse:
			val := mc.pop() // Always pop the evaluated condition
			cond, ok := val.(bool)
			if !ok {
				err = fmt.Errorf("condition must be a boolean, got %T", val)
				break
			}
			if !cond {
				pc = inst.Arg.(int)
			}

		case OpJump:
			pc = inst.Arg.(int)

		case OpCall:
			meta := inst.Arg.(CallMetadata)
			argCount := meta.ArgCount

			// Flattened fast path: callee and its args are already
			// sitting on mc.stack, in call position, exactly where a
			// NoEscape closure's own param frame needs them - see
			// frame.flatCall's doc comment. Peeking (not popping) lets
			// this check happen before committing to either path.
			// Guarded on NoEscape: without it, this closure's body could
			// itself create a nested closure literal that captures
			// newLocals by reference (see LambdaProto.NoEscape) and
			// outlives this call - which a slice view into mc.stack,
			// unlike a normal heap-allocated values slice, cannot safely
			// support (this call's stack region is reused the moment the
			// call returns).
			calleeIdx := len(mc.stack) - 1 - argCount
			if c, ok := mc.stack[calleeIdx].(*closure); ok && c.proto.NoEscape && len(c.proto.Params) == argCount {
				// values is a slice view directly into mc.stack, not a
				// fresh allocation: the args are already there. Capped at
				// its own length so an (unexpected) append reallocates
				// instead of ever writing into the operand-stack space
				// that starts right after it once the body begins pushing
				// its own temporaries.
				newLocals := &localFrame{parent: c.locals, values: mc.stack[calleeIdx+1 : calleeIdx+1+argCount : calleeIdx+1+argCount]}
				if err = mc.saveContinuation(instructions, scopes, locals, pc, coalesce, stackBase, flatCall); err == nil {
					// stackBase is calleeIdx, not len(mc.stack): the
					// callee slot and its args are still physically on
					// mc.stack (nothing was popped), so on error
					// truncating to stackBase discards all of it, and on
					// normal completion the pc >= len(instructions)
					// branch compacts the lone result down onto that same
					// slot - see its own comment.
					instructions, scopes, locals, pc, coalesce, stackBase, flatCall = c.proto.Instructions, c.envScopes, newLocals, 0, nil, calleeIdx, true
				}
				break
			}

			args := make([]any, argCount)
			for i := argCount - 1; i >= 0; i-- {
				args[i] = mc.pop()
			}
			callee := mc.pop()

			if c, ok := callee.(*closure); ok && len(args) == len(c.proto.Params) {
				// A direct call to a lambda's own bytecode: save the
				// caller's continuation and switch the active locals to
				// it, instead of a real, recursive Go call - see
				// frame's doc comment. callFunction below remains the
				// path for every other callable shape (BuiltinFunc, a
				// bound method, a plain Go function) and for a closure
				// called with the wrong argument count, which needs
				// c.call's own clean error instead of silently
				// mismatching here.
				//
				// args is aliased directly into the new frame's values,
				// not copied: unlike closure.call (the general mc.Call
				// path, where a caller outside this loop might reuse or
				// mutate its own args slice after the call returns), args
				// here was just built fresh, above, solely for this one
				// call and is never touched again regardless of how long
				// a nested closure might keep this frame alive.
				newLocals := &localFrame{parent: c.locals, values: args}
				if err = mc.saveContinuation(instructions, scopes, locals, pc, coalesce, stackBase, flatCall); err == nil {
					instructions, scopes, locals, pc, coalesce, stackBase, flatCall = c.proto.Instructions, c.envScopes, newLocals, 0, nil, len(mc.stack), false
				}
				break
			}

			var res any
			if res, err = callFunction(mc, callee, args); err == nil {
				mc.push(res)
			}
		}

		if err != nil {
			// Unwind: does the currently-active sequence catch this
			// itself (it's a ??'s left-hand operand)? If not, discard
			// its partial contribution to mc.stack and keep resuming
			// saved continuations, checking each one in turn, until one
			// does or there's nothing left on this trampoline to check
			// (base reached - propagate the error out of pump
			// entirely). A frame saved by something a coalesce-left
			// operand itself called (a nested lambda call, say) always
			// carries that same coalesce marker forward (see frame's
			// doc comment), so this finds the correct enclosing `??`
			// however deep the failure occurred.
			for {
				mc.stack = mc.stack[:stackBase]
				caught := coalesce != nil
				if len(mc.frames) == base {
					return err // nothing on this trampoline catches it
				}
				top := len(mc.frames) - 1
				f := mc.frames[top]
				mc.frames = mc.frames[:top]
				instructions, scopes, locals, pc, coalesce, stackBase, flatCall = f.instructions, f.scopes, f.locals, f.pc, f.coalesce, f.stackBase, f.flatCall
				if caught {
					break // resumed exactly where saved - the right-hand operand, i.e. fall-through
				}
			}
		}
	}
}

// callFunction invokes a callable popped off the VM stack. It supports four
// kinds of callable:
//   - BuiltinFunc: the vm's own func(vm *VM, args ...any) (any, error)
//     convention, called directly with no reflection needed.
//   - *closure: a compiled lambda literal (`x => ...`), run via mc.run
//     against its captured scope chain plus one new scope for its params.
//   - reflect.Value: a bound method obtained via accessMember (e.g. from
//     `obj.Method`). The Value already wraps the callable, so it's used as
//     the reflect target directly rather than reflected over again.
//   - any other Go function value (e.g. a plain `func() string` stored in
//     the environment): reflected over generically via callReflectFunc.
func callFunction(mc *Machine, callee any, args []any) (any, error) {
	if fn, ok := callee.(BuiltinFunc); ok {
		return fn(mc, args...)
	}
	if c, ok := callee.(*closure); ok {
		return c.call(args)
	}
	// A bare reference to a namespace itself (e.g. `string` with no
	// `.member`) reaches here as the raw map OpLoad pushed - not callable
	// by itself, only its members are. Caught explicitly for a clear
	// error; without this it would fall through to callReflectFunc's
	// generic "cannot call target of type %T" below, which is accurate
	// but doesn't point the caller at the fix.
	if _, ok := callee.(PrehashedMap[BuiltinFunc]); ok {
		return nil, fmt.Errorf("this is a namespace, not a function - call one of its members instead, e.g. namespace.someFunction(...)")
	}

	var fn reflect.Value
	if v, ok := callee.(reflect.Value); ok {
		fn = v
	} else {
		fn = reflect.ValueOf(callee)
	}

	if !fn.IsValid() || fn.Kind() != reflect.Func {
		return nil, fmt.Errorf("cannot call target of type %T", callee)
	}

	return callReflectFunc(mc, fn, args)
}

// IsCallable reports whether v is one of owlexpr's own callable value
// kinds - the ones callReflectFunc's argVal.Type() checks can never match
// directly against a Go func parameter type, since they're not
// themselves Go func values with a matching signature. These are exactly
// the values callFunction itself knows how to invoke, via mc.Call.
//
// Exported for the same reason ListElements/IsComparableKey are: it lets
// a builtin outside this package (e.g. a `type()` function) classify a
// closure as "callable" without being able to name the unexported
// *closure type directly.
func IsCallable(v any) bool {
	switch v.(type) {
	case *closure, BuiltinFunc, reflect.Value:
		return true
	default:
		return false
	}
}

// WrapCallable converts a lambda closure into a plain Go function value -
// func(args ...any) (any, error) - that an embedding Go program can call
// directly, with no need for mc.Call and no way to even name the
// unexported *closure type. Any other value (including one that's
// already such a func, or any non-callable result) passes through
// unchanged, so it's safe to call unconditionally on a result whose shape
// isn't known ahead of time.
//
// Run applies this to its own final result automatically - an expression
// whose value is a lambda literal, e.g. `x => x * 2`, hands the embedder
// something they can call like any other Go func, not an opaque internal
// type. RunScopes deliberately does NOT apply this: it's also the engine
// behind Sheet's per-cell evaluation (see RunSheet, sheet.go), where a
// cell's raw *closure result may still need to be called from another
// cell via the compiler's normal OpCall path before RunSheet itself
// wraps it for the caller - wrapping it early would hide it from that
// machinery. Exported so a caller building something RunScopes-shaped of
// its own (as Sheet does) can apply the same conversion at its own
// point of finality.
func WrapCallable(v any) any {
	c, ok := v.(*closure)
	if !ok {
		return v
	}
	return func(args ...any) (any, error) {
		return c.call(args)
	}
}

// callReflectFunc calls an arbitrary Go function via reflection, converting
// arguments to the parameter types the function actually expects (e.g. our
// int64 literals into a func(int) parameter) and unpacking the results.
//
// Return value convention:
//   - no return values -> nil
//   - one return value implementing error -> (nil, that error) if non-nil,
//     else (nil, nil) - a bare `func(...) error` signals success/failure
//     only, with no data value
//   - one other return value -> that value
//   - two or more, where the LAST one is an error -> the remaining data
//     value(s), per the two rules below, plus that error
//   - exactly one data value (e.g. the idiomatic `(T, error)`) -> that value
//   - two or more data values -> all of them as a []any list, in order, so
//     e.g. time.Time's ISOWeek() gives [year, week] rather than silently
//     dropping the week
//
// Every data value, including each list element, goes through
// normaliseResult.
func callReflectFunc(mc *Machine, fn reflect.Value, args []any) (any, error) {
	fnType := fn.Type()
	numIn := fnType.NumIn()

	if fnType.IsVariadic() {
		if len(args) < numIn-1 {
			return nil, fmt.Errorf("function expects at least %d argument(s), got %d", numIn-1, len(args))
		}
	} else if len(args) != numIn {
		return nil, fmt.Errorf("function expects %d argument(s), got %d", numIn, len(args))
	}

	in := make([]reflect.Value, len(args))
	for i, arg := range args {
		var paramType reflect.Type
		if fnType.IsVariadic() && i >= numIn-1 {
			paramType = fnType.In(numIn - 1).Elem()
		} else {
			paramType = fnType.In(i)
		}

		if arg == nil {
			in[i] = reflect.Zero(paramType)
			continue
		}

		argVal := reflect.ValueOf(arg)
		switch {
		case argVal.Type().AssignableTo(paramType):
			in[i] = argVal
		case argVal.Type().ConvertibleTo(paramType):
			in[i] = argVal.Convert(paramType)
		case paramType.Kind() == reflect.Func && IsCallable(arg):
			// A lambda literal (or a builtin/bound method passed through
			// as a value) used as a callback argument to a struct method
			// or an environment function, e.g. `items.Filter(x => x.Active)`
			// where Filter expects a func(Item) bool. Bridge it into a
			// real Go function value of the expected type.
			in[i] = makeReflectFuncAdapter(mc, arg, paramType)
		default:
			return nil, fmt.Errorf("argument %d: cannot use %T as %s", i, arg, paramType)
		}
	}

	out := fn.Call(in)

	switch len(out) {
	case 0:
		return nil, nil
	case 1:
		if out[0].Type().Implements(errorType) {
			// A bare `func(...) error`-shaped return: there's no data
			// value, just success/failure. Without this check, a nil
			// error would be handed back as the call's "value" (see
			// IsNilResult's doc for why a plain == nil check on it would
			// still be wrong), and a non-nil error would be returned as
			// if it were legitimate data instead of failing the call -
			// exactly backwards from what a caller (not least "??")
			// needs.
			errVal := out[0].Interface()
			if IsNilResult(errVal) {
				return nil, nil
			}
			return nil, errVal.(error)
		}
		return mc.normaliseResult(out[0]), nil
	default:
		var err error
		if last := out[len(out)-1]; last.Type().Implements(errorType) {
			if !last.IsNil() {
				err = last.Interface().(error)
			}
			out = out[:len(out)-1]
		}
		if len(out) == 1 {
			return mc.normaliseResult(out[0]), err
		}
		list := make([]any, len(out))
		for i, v := range out {
			list[i] = mc.normaliseResult(v)
		}
		return list, err
	}
}

// normaliseResult converts a Go call's return value into a value owlexpr
// can actually work with. Go APIs routinely return integer and float kinds
// owlexpr doesn't model - e.g. a decimal's Exponent() is an int32, a
// Float32() accessor a float32 - and without this such a value would be
// opaque to arithmetic, comparisons, int()/float() conversion and output
// formatting alike. Normalising here, once, at the call boundary fixes all
// of those in one place: any unmodelled signed/unsigned integer kind
// becomes int64, any float kind float64.
//
// A value whose type the Machine already recognises is left untouched -
// the core types, and crucially anything with a registered operation
// (RegisterOperation/RegisterTypeCoder), so time.Duration (int64
// underneath), time.Month and the like keep their own semantics. A
// uint64/uint/uintptr too large for int64 is also left as-is rather than
// silently wrapping negative or losing precision as a float64.
func (mc *Machine) normaliseResult(rv reflect.Value) any {
	v := rv.Interface()
	switch v.(type) {
	case nil, int, int64, float64, string, bool:
		return v // fast path: already a core type
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if mc.typeCode(v) != UnSupportedType {
			return v
		}
		return rv.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if mc.typeCode(v) != UnSupportedType {
			return v
		}
		if u := rv.Uint(); u <= math.MaxInt64 {
			return int64(u)
		}
		return v
	case reflect.Float32, reflect.Float64:
		if mc.typeCode(v) != UnSupportedType {
			return v
		}
		return rv.Float()
	}
	return v
}

// makeReflectFuncAdapter bridges a owlexpr callable - a lambda closure,
// a BuiltinFunc, or a bound-method reflect.Value - into a real Go function
// value of paramType, via reflect.MakeFunc, so it can be passed as a
// callback argument to a struct method or a Go function stored in the
// environment. Each call into the synthesized function converts its
// native Go arguments to []any, invokes the callable through mc.Call, and
// converts the (any, error) result back to paramType's declared outputs.
//
// If mc.Call returns an error and paramType has no error among its return
// values, there's no channel to report the failure through other than a
// panic - the same tradeoff other reflect-based callback bridges make.
func makeReflectFuncAdapter(mc *Machine, callee any, paramType reflect.Type) reflect.Value {
	numOut := paramType.NumOut()
	errType := reflect.TypeOf((*error)(nil)).Elem()
	lastIsError := numOut > 0 && paramType.Out(numOut-1).Implements(errType)

	return reflect.MakeFunc(paramType, func(in []reflect.Value) []reflect.Value {
		args := make([]any, len(in))
		for i, v := range in {
			args[i] = v.Interface()
		}

		res, err := mc.Call(callee, args)
		out := make([]reflect.Value, numOut)

		if err != nil {
			if !lastIsError {
				panic(err)
			}
			for i := 0; i < numOut-1; i++ {
				out[i] = reflect.Zero(paramType.Out(i))
			}
			out[numOut-1] = reflect.ValueOf(err)
			return out
		}

		valueOuts := numOut
		if lastIsError {
			valueOuts--
			out[numOut-1] = reflect.Zero(paramType.Out(numOut - 1)) // nil error
		}
		switch {
		case valueOuts == 1:
			out[0] = convertResult(res, paramType.Out(0))
		case valueOuts > 1:
			// A single `any` result can't populate more than one
			// non-error output; zero the rest rather than guessing.
			out[0] = convertResult(res, paramType.Out(0))
			for i := 1; i < valueOuts; i++ {
				out[i] = reflect.Zero(paramType.Out(i))
			}
		}
		return out
	})
}

func convertResult(res any, outType reflect.Type) reflect.Value {
	if res == nil {
		return reflect.Zero(outType)
	}
	rv := reflect.ValueOf(res)
	if rv.Type().AssignableTo(outType) {
		return rv
	}
	if rv.Type().ConvertibleTo(outType) {
		return rv.Convert(outType)
	}
	panic(fmt.Sprintf("lambda result %T is not assignable to %s", res, outType))
}

// negateValue implements OpNeg (`-x`). int/int64/float64 stay hardcoded,
// zero registration/dispatch cost, exactly like GetTypeCode's core switch;
// anything else falls back to Combine(a, a, OpNeg) - passing a as both
// operands guarantees aType == bType, so operationDispatcher takes its
// direct single-handler branch (the same trick OpCoerce already relies
// on) rather than needing a second, unary-specific registration mechanism.
// A type registered via RegisterOperation gets `-x` support for free by
// adding an OpNeg case to the same handler it already wrote for +/-/
// OpCoerce - see stdlib's decimalOperations for exactly that.
func (mc *Machine) negateValue(a any) (any, error) {
	switch v := a.(type) {
	case int:
		return -v, nil
	case int64:
		return -v, nil
	case float64:
		return -v, nil
	}
	res, err := mc.Combine(a, a, OpNeg)
	if err != nil {
		return nil, fmt.Errorf("unary '-' not supported for type %T", a)
	}
	return res, nil
}

// indexValue implements target[idx] for list-style (int index), map-style
// (key lookup), and string (rune index) targets.
//
// []any and map[string]any - the types our own list/map literals produce -
// are handled with a direct type switch, as cheap as any other Go type
// switch and with no reflection involved. Anything else (a []int from the
// env, a map[string]int, a named slice/map type, a string, ...) falls back
// to reflection so indexing isn't limited to literal-produced values. This
// split doesn't go through the VM's TypeCode/operationsRouter machinery:
// that dispatch exists to pick a coercion between two arithmetic-like
// operands (e.g. int + decimal), which isn't the shape of this problem -
// here the target type alone decides how to index, and the index value's
// type (int vs string) is dictated by the target, not coerced against it.
//
// String indexing is rune-based, not Go's own byte-based s[i]: this
// language has no separate byte/rune type, users of an expression/rule
// language reasonably think in characters rather than UTF-8 bytes, and
// byte-slicing risks cutting a multi-byte character in half. lenFunc's
// string case uses utf8.RuneCountInString for the same reason - so that
// s[0:len(s)] stays equal to s for any string, not just ASCII ones.
func indexValue(target, idx any) (any, error) {
	switch t := target.(type) {
	case []any:
		i, err := toIndexInt(idx)
		if err != nil {
			return nil, err
		}
		if i < 0 || i >= len(t) {
			return nil, fmt.Errorf("index %d out of range for list of length %d", i, len(t))
		}
		return t[i], nil

	case map[string]any:
		key, ok := idx.(string)
		if !ok {
			return nil, fmt.Errorf("map key must be a string, got %T", idx)
		}
		val, exists := t[key]
		if !exists {
			return nil, fmt.Errorf("key %q not found in map", key)
		}
		return val, nil
	}

	// A deferredMember target is a struct/array-kind result from a prior
	// field/index hop in the same chain (e.g. the `Data[0]` in
	// `Data[0][0]`) - continue indexing directly against its reflect.Value
	// rather than re-reflecting over an .Interface()-boxed copy of it. See
	// deferredMember's doc comment (vm.go, near pop/popRaw) for why this
	// matters: boxing it here would trigger the same addressable-copy cost
	// this type exists to avoid.
	rv := reflect.ValueOf(target)
	if dm, ok := target.(deferredMember); ok {
		rv = dm.rv
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		i, err := toIndexInt(idx)
		if err != nil {
			return nil, err
		}
		if i < 0 || i >= rv.Len() {
			return nil, fmt.Errorf("index %d out of range for %s of length %d", i, rv.Type(), rv.Len())
		}
		elem := rv.Index(i)
		switch elem.Kind() {
		case reflect.Struct, reflect.Array:
			return deferredMember{rv: elem}, nil
		default:
			return elem.Interface(), nil
		}

	case reflect.String:
		runes := []rune(rv.String())
		i, err := toIndexInt(idx)
		if err != nil {
			return nil, err
		}
		if i < 0 || i >= len(runes) {
			return nil, fmt.Errorf("index %d out of range for string of length %d", i, len(runes))
		}
		return string(runes[i]), nil

	case reflect.Map:
		keyVal := reflect.ValueOf(idx)
		if !keyVal.IsValid() {
			return nil, fmt.Errorf("cannot use nil as map key")
		}
		keyType := rv.Type().Key()
		if keyVal.Type().AssignableTo(keyType) {
			// use as-is
		} else if keyVal.Type().ConvertibleTo(keyType) {
			keyVal = keyVal.Convert(keyType)
		} else {
			return nil, fmt.Errorf("cannot use %T as key of type %s", idx, keyType)
		}
		result := rv.MapIndex(keyVal)
		if !result.IsValid() {
			return nil, fmt.Errorf("key %v not found in map", idx)
		}
		return result.Interface(), nil

	default:
		if !rv.IsValid() {
			return nil, fmt.Errorf("cannot index into value of type <nil>")
		}
		return nil, fmt.Errorf("cannot index into value of type %s", rv.Type())
	}
}

func toIndexInt(idx any) (int, error) {
	switch v := idx.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("list index must be an integer, got %T", idx)
	}
}

// sliceValue implements target[low:high] - Go-style syntax (low and/or
// high may be nil, meaning "start"/"end"), but not Go's exact semantics:
// there's no negative-index wraparound (same as Go, which panics on a
// negative index too - this just errors cleanly instead), and strings
// slice by rune, not by byte, for the same reason indexValue's string
// case does (see its doc comment).
//
// []any gets the same zero-reflection fast path indexValue uses. A
// reflected slice is sliced directly via reflect.Value.Slice. A
// fixed-size array is a special case: Slice panics on a non-addressable
// value, and an array boxed into `any` from an env value or a struct
// field is exactly that - so it's copied into a real slice of the same
// element type first (reflect.MakeSlice + reflect.Copy), then sliced
// normally.
func sliceValue(target any, low, high any) (any, error) {
	if list, ok := target.([]any); ok {
		lo, hi, err := resolveSliceBounds(low, high, len(list))
		if err != nil {
			return nil, err
		}
		return list[lo:hi], nil
	}

	rv := reflect.ValueOf(target)
	switch rv.Kind() {
	case reflect.String:
		runes := []rune(rv.String())
		lo, hi, err := resolveSliceBounds(low, high, len(runes))
		if err != nil {
			return nil, err
		}
		return string(runes[lo:hi]), nil

	case reflect.Slice:
		lo, hi, err := resolveSliceBounds(low, high, rv.Len())
		if err != nil {
			return nil, err
		}
		return rv.Slice(lo, hi).Interface(), nil

	case reflect.Array:
		lo, hi, err := resolveSliceBounds(low, high, rv.Len())
		if err != nil {
			return nil, err
		}
		cp := reflect.MakeSlice(reflect.SliceOf(rv.Type().Elem()), rv.Len(), rv.Len())
		reflect.Copy(cp, rv)
		return cp.Slice(lo, hi).Interface(), nil

	default:
		return nil, fmt.Errorf("cannot slice value of type %T", target)
	}
}

// resolveSliceBounds turns possibly-nil low/high operands (nil meaning
// "start"/"end" respectively) into concrete, validated [lo:hi) bounds for
// a sequence of the given length. No negative indices - same as Go's own
// slice expressions - and an out-of-range or inverted bound is a clean
// error rather than a panic.
func resolveSliceBounds(low, high any, length int) (int, int, error) {
	lo := 0
	hi := length

	if low != nil {
		l, err := toIndexInt(low)
		if err != nil {
			return 0, 0, err
		}
		lo = l
	}
	if high != nil {
		h, err := toIndexInt(high)
		if err != nil {
			return 0, 0, err
		}
		hi = h
	}

	if lo < 0 || hi < 0 || lo > length || hi > length || lo > hi {
		return 0, 0, fmt.Errorf("slice bounds out of range [%d:%d] with length %d", lo, hi, length)
	}
	return lo, hi, nil
}

// Hooks into Go Struct fields and methods dynamically
func (mc *Machine) accessMember(target any, hn HashedName) (any, error) {
	// map[string]any - owlexpr's own literal map type, and the concrete
	// type every env and `{...}` map literal actually has - gets a
	// zero-reflection fast path, the dot-access mirror of indexValue's own
	// `case map[string]any:` fast path a few lines up in this file. `m.key`
	// and `m["key"]` are meant to behave identically (same error on a
	// missing key, see the reflect-based map branch below, which this
	// supersedes for this one concrete type), but until now only the
	// bracket form skipped reflection to get there. Skipping the
	// method-lookup step below is safe here specifically because
	// map[string]any is an unnamed type literal - Go doesn't allow
	// attaching methods to one, so there's no method this could ever
	// shadow.
	if m, ok := target.(map[string]any); ok {
		v, exists := m[hn.Name]
		if !exists {
			return nil, fmt.Errorf("key %q not found in map", hn.Name)
		}
		return v, nil
	}

	// A namespace (Machine.namespaces' PrehashedMap[BuiltinFunc] values,
	// e.g. the "string" in string.trim) gets the same treatment, checked
	// via its precomputed hash before falling into any reflection at all -
	// both to skip re-hashing hn.Name on every call of a namespaced
	// function, and because the reflect-based map branch further down
	// wouldn't even match a PrehashedMap (its key type is uint64, not
	// string) and would misreport it as "member not found".
	if ns, ok := target.(PrehashedMap[BuiltinFunc]); ok {
		fn, exists := ns.Get(hn.Hash, hn.Name)
		if !exists {
			return nil, fmt.Errorf("key %q not found in map", hn.Name)
		}
		return fn, nil
	}

	// A deferredMember target is a struct-kind result from a prior field
	// access in the same chain (e.g. the `Inner` in `Env.Inner.Field`) -
	// resolve directly against its reflect.Value instead of re-reflecting
	// over an .Interface()-boxed copy of it. See deferredMember's doc
	// comment (near pop/popRaw) for why this matters: boxing it here would
	// trigger the same addressable-copy cost this type exists to avoid.
	var val reflect.Value
	if dm, ok := target.(deferredMember); ok {
		val = dm.rv
	} else {
		val = reflect.ValueOf(target)
	}
	// A literal nil (an untyped nil any, as opposed to a typed nil
	// pointer - see below) produces an invalid reflect.Value, and
	// MethodByName/FieldByName both panic when called on one. This isn't
	// a hypothetical: `x.field` where x is nil - env["x"] = nil, or a
	// prior access step legitimately returning nil - hits this exact
	// path, so it needs a clean error, not a crash.
	if !val.IsValid() {
		return nil, fmt.Errorf("cannot access member %q on nil", hn.Name)
	}

	// rawType is val's own concrete type, captured before any pointer
	// dereference below - both a cached method resolution (found against
	// this exact type) and a cached field resolution (found after
	// dereferencing this exact type at most once, same as the miss path
	// below does) are keyed by it in mc.memberCache.
	rawType := val.Type()
	if res, ok := mc.resolveMember(rawType, hn.Name); ok {
		if res.isMethod {
			return val.Method(res.methodIndex), nil
		}
		elem := val
		if elem.Kind() == reflect.Ptr {
			elem = elem.Elem()
			if !elem.IsValid() {
				// A field resolution cached from some earlier, non-nil
				// pointer of this same type - this call's pointer happens
				// to be nil, so the field genuinely isn't reachable this
				// time. Same "describe as nil" outcome the uncached miss
				// path below reaches for exactly this situation.
				return nil, fmt.Errorf("cannot access member %q on nil", hn.Name)
			}
		}
		field := elem.FieldByIndex(res.fieldIndex)
		switch field.Kind() {
		case reflect.Struct, reflect.Array:
			return deferredMember{rv: field}, nil
		default:
			return field.Interface(), nil
		}
	}

	// Look up methods first, unless DisableStructMethods has turned that off
	// (see its VMOption doc comment) - an embedder using it wants struct
	// data reachable from expressions without also exposing whatever
	// methods happen to live on that same type, so this falls straight
	// through to the field/map checks below instead, as if the method
	// didn't exist.
	if !mc.disableStructMethods {
		if m, found := rawType.MethodByName(hn.Name); found {
			mc.cacheMember(rawType, hn.Name, memberResolution{isMethod: true, methodIndex: m.Index})
			return val.Method(m.Index), nil
		}
	}

	// Handle pointer dereferences for struct field checks
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() == reflect.Struct {
		// Unexported fields are treated as absent: reflect refuses to
		// Interface() them (a panic, not an error), and they're not part
		// of any type's public surface anyway - e.g. a *Regex's wrapped
		// *regexp.Regexp must stay unreachable (see Regex's doc comment).
		if sf, found := val.Type().FieldByName(hn.Name); found && sf.IsExported() {
			mc.cacheMember(rawType, hn.Name, memberResolution{fieldIndex: sf.Index})
			field := val.FieldByIndex(sf.Index)
			switch field.Kind() {
			case reflect.Struct, reflect.Array:
				// Defer boxing - see deferredMember's doc comment. field is
				// still reached via pointer arithmetic off val here, no
				// copy yet; .Interface() is what would copy, and doing
				// that now (rather than once, lazily, on whatever this
				// chain eventually resolves to) is exactly the cost this
				// type exists to avoid.
				return deferredMember{rv: field}, nil
			default:
				return field.Interface(), nil
			}
		}
	}

	// A string-keyed map accepts dot access as an alternate spelling of
	// bracket indexing (`m.field` == `m["field"]`), matching indexValue's
	// own map-indexing rule below (including its "error, not nil, on a
	// missing key" behavior) so `.` and `[...]` never disagree about
	// whether a key exists. The map[string]any fast path above now covers
	// the common case with no reflection at all, and the PrehashedMap
	// fast path above that covers namespaces specifically (which, being
	// keyed by uint64 hash rather than string, wouldn't even match this
	// reflect-based branch) - this remains the path for every *other*
	// string-keyed map type: a plain map[string]int from the env, a named
	// map type, ...
	if val.Kind() == reflect.Map && val.Type().Key().Kind() == reflect.String {
		key := reflect.ValueOf(hn.Name).Convert(val.Type().Key())
		entry := val.MapIndex(key)
		if entry.IsValid() {
			return entry.Interface(), nil
		}
		return nil, fmt.Errorf("key %q not found in map", hn.Name)
	}

	// val can still be invalid here: val.Elem() above turns a *typed* nil
	// pointer (e.g. a nil `Role *Role` field) into the zero Value, same as
	// a literal nil would - val.Type() panics on that, so it needs the
	// same "describe as nil" fallback the early IsValid() check above
	// gives literal nil. Kept as its own message (not merged into that
	// early return) since the member name is more useful to report here,
	// after the pointer dereference, than before it.
	if !val.IsValid() {
		return nil, fmt.Errorf("cannot access member %q on nil", hn.Name)
	}
	return nil, fmt.Errorf("member %s not found on type %s", hn.Name, val.Type())
}
