package vm

// A TypeCoder takes any objects and returns an int that is unique to it's type.
type TypeCode int

type TypeCoder func(a any) TypeCode

const (
	UnSupportedType TypeCode = iota
	IntTypeCode
	Int64TypeCode
	FloatTypeCode
	StringTypeCode
	BoolTypeCode
)

// coreOpHandlersSize bounds Machine.coreOpHandlers, a dense array-indexed
// cache operationDispatcher uses instead of scanning opHandlers whenever
// both operand TypeCodes fall inside it - see that field's doc comment.
// Sized to comfortably cover the five built-in core codes above (0-5)
// with headroom for the rare custom type an embedder registers with a
// deliberately small TypeCode (via a custom TypeCoder - see
// RegisterTypeCoder), while staying tiny: RegisterOperation's own normal
// auto-assigned codes (resolveOrAssignTypeCode) always start at
// firstDynamicTypeCode (1000) and so never qualify - by far the common
// case for any type beyond the five below, and operationDispatcher's
// fallback scan (unchanged, still exactly as general as before) handles
// that exactly as it always has.
const coreOpHandlersSize = 8

// firstDynamicTypeCode is the first TypeCode value the VM auto-assigns to
// a type registered via RegisterOperation that GetTypeCode doesn't
// recognize (see mc.typeCode / mc.registerOperationalHandler in vm.go).
// Kept well clear of the core constants above so a future addition to
// that block can never collide with a code an already-running VM
// assigned dynamically.
const firstDynamicTypeCode TypeCode = 1000

// The default TypeCoder used by owlexpr
func GetTypeCode(a any) TypeCode {
	switch aT := a.(type) {
	case int:
		return IntTypeCode
	case bool:
		return BoolTypeCode
	case int64:
		return Int64TypeCode
	case float64:
		return FloatTypeCode
	case string:
		// To stop the compiler complaining
		_ = aT
		return StringTypeCode
	}
	return UnSupportedType
}
