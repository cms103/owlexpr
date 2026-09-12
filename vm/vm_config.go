package vm

import "uuid"

type VMOption func(*Machine) error

// DisableBuiltIns clears the VM's builtins map, removing every named
// function registered so far (e.g. by an earlier VMOption in the chain,
// such as a root-level default) and leaving only the operators (+, -,
// ==, ...) and whatever else is registered explicitly afterward.
func DisableBuiltIns() VMOption {
	return func(mc *Machine) error {
		mc.builtins = make(PrehashedMap[BuiltinFunc])
		mc.namespaces = nil
		return nil
	}
}

// Disables type coercion - all operations require equal types on both sides
func DisableAutoTypeCoercion() VMOption {
	return func(mc *Machine) error {
		mc.disabledTypeCoercion = true
		return nil
	}
}

// DisableStructMethods turns off dot-access's ability to reach Go methods
// on env/struct values (accessMember's "look up methods first" step - see
// vm.go). Field access is unaffected: `x.Field` keeps working exactly as
// before, only `x.Method()` starts failing with a "member not found"
// style error, as if the method didn't exist. Useful when a struct passed
// into an expression's env carries data worth exposing (fields) alongside
// methods that shouldn't be callable by whoever authors the expression.
func DisableStructMethods() VMOption {
	return func(mc *Machine) error {
		mc.disableStructMethods = true
		return nil
	}
}

// ClearOperations wipes the VM's entire operation-handler registry (the
// defaults registerDefaultOperations installed, and anything registered
// on top of them) back to empty, along with the TypeCodes
// RegisterOperation auto-assigned along the way. A VM in this state
// supports no operators (+, -, ==, <, ...) at all until handlers are
// registered again via RegisterOperation - including for owlexpr's own
// core types (int, int64, float64, string, bool), which are otherwise
// pre-registered by default. This is what makes it possible to replace the
// defaults rather than merely add to them: e.g. clearing, then registering
// int64 with an empty coercible-types list, gets you int64 arithmetic with
// no automatic coercion to float64/string - useful if that coercion is a
// correctness hazard for a given embedding.
//
// Also wipes fastSliceIterators (RegisterFastSliceIterator) - same
// category of state as opHandlers, not the type-recognition category
// below. OpNeg/OpAbs/OpCeil/OpFloor/OpRound have no separate table of
// their own to wipe any more - they dispatch through opHandlers via
// Combine, so clearing opHandlers already clears their registered
// support too.
//
// Deliberately does NOT clear extraTypers (custom coders installed via
// RegisterTypeCoder).
func ClearOperations() VMOption {
	return func(mc *Machine) error {
		mc.opHandlers = nil
		mc.coreOpHandlers = [coreOpHandlersSize]operationHandlerInfo{}
		mc.fastSliceIterators = nil
		mc.dynamicTypes = nil
		mc.nextDynamicCode = firstDynamicTypeCode - 1
		return nil
	}
}

// RegisterBuiltIn adds a named function callable from expressions, e.g.
// RegisterBuiltIn("upper", myUpperFunc).
func RegisterBuiltIn(name string, theFunc BuiltinFunc) VMOption {
	return func(mc *Machine) error {
		mc.RegisterBuiltin(name, theFunc)
		return nil
	}
}

// RegisterNamespacedBuiltIn adds a named function callable from
// expressions as `namespace.name(...)`, e.g. RegisterNamespacedBuiltIn
// ("string", "trim", myTrimFunc) for `string.trim(x)`. See
// Machine.RegisterNamespacedBuiltin's doc comment for how the `.name`
// step actually resolves (accessMember's generic map-dot-access rule,
// not any namespace-specific logic).
func RegisterNamespacedBuiltIn(namespace, name string, theFunc BuiltinFunc) VMOption {
	return func(mc *Machine) error {
		mc.RegisterNamespacedBuiltin(namespace, name, theFunc)
		return nil
	}
}

// RegisterOperation registers handler for operations where at least one
// operand resolves to baseType's TypeCode (auto-assigned if it's a type
// GetTypeCode/RegisterTypeCoder don't already recognize - see
// mc.resolveOrAssignTypeCode), coercing with any of coercibleTypes.
// handler must not be nil - operationsRouter trusts every registered
// entry to be callable, so this is rejected here rather than risking a
// nil-func panic at dispatch time.
//
// This is also how a type opts into the unary ops (OpNeg/OpAbs/OpCeil/
// OpFloor/OpRound) - negateValue and the abs()/ceil()/floor()/round()
// builtins each fall back to mc.Combine(a, a, op) once their own
// hardcoded int/int64/float64 fast path doesn't match, which - since a
// passed as both operands guarantees aType == bType - reaches this exact
// handler directly. So a type wanting `-x`/abs()/etc. support adds a case
// for the relevant OpCode to the same handler it already writes for +/-/
// OpCoerce, rather than a second, unary-specific registration call.
func RegisterOperation(baseType any, coercibleTypes []any, handler OperationalHandler) VMOption {
	return func(mc *Machine) error {
		return mc.registerOperationalHandler(baseType, coercibleTypes, handler)
	}
}

// RegisterFastSliceIterator plugs a new element type into ForEachListElement's
// reflect-once-per-call fast path for slices (see FastSliceIterator's doc
// comment and fastSliceElementIterate in iteration.go), the iteration
// counterpart to RegisterOperation. Without this, a []T of a type
// GetTypeCode/RegisterTypeCoder don't hardcode still iterates correctly via
// map/filter/reduce - just through the generic per-element
// reflect.Value.Index(i).Interface() path instead of a single bulk type
// assertion, which only matters at realistic sizes - a
// benchmarked ~50-100 element threshold.
func RegisterFastSliceIterator(baseType any, iter FastSliceIterator) VMOption {
	return func(mc *Machine) error {
		return mc.registerFastSliceIterator(baseType, iter)
	}
}

// RegisterTypeCoder adds a custom TypeCoder to the VM's type-recognition
// chain (see mc.typeCode). It's additive, not a replacement for the
// default GetTypeCode switch - multiple RegisterTypeCoder calls (from
// independent extensions) compose, each contributing recognition for its
// own types, rather than the last one clobbering the others.
//
// id is just a UUID that tracks who is registering. It's only purpose
// is to ensure that RegisterTypeCoder can be called multiple times and
// only register a TypeCoder once. This is used by the stdlib so one
// type coder supports all packages.
//
// This exists as a performance escape hatch, not the primary way to add a
// new type: RegisterOperation alone already auto-assigns a working
// TypeCode to any type it doesn't recognize (see
// mc.resolveOrAssignTypeCode), no coder required. Write one only if
// profiling shows the dynamicTypes map lookup actually matters - a
// hand-rolled type-switch over your own types is cheaper than a
// reflect.TypeOf-keyed map lookup per value, but it's real code to
// maintain for a saving that will often be in the noise next to
// everything else Combine does per call.
func RegisterTypeCoder(coder TypeCoder, id uuid.UUID) VMOption {
	return func(mc *Machine) error {
		// Check to see if this is already registered
		for _, r := range mc.extraTypers {
			if r.id.Compare(id) == 0 {
				// Short-cut, this ID already registered
				return nil
			}
		}
		mc.extraTypers = append(mc.extraTypers, typeCoderRegistration{id: id, coder: coder})
		return nil
	}
}
