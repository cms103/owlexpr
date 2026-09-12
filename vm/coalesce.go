package vm

import "reflect"

// IsNilResult reports whether v should be treated as nil for `??`/`?.`'s
// purposes: either literally nil, or a non-nil `any` wrapping a nil
// pointer, map, slice, chan, func, or interface - the classic Go
// typed-nil-interface gotcha. This matters because a struct field like
// `Role *Role` that's nil gets boxed into `any` by accessMember's
// reflection, and a boxed *Role(nil) is NOT `== nil` when compared
// as `any` even though it's exactly the "there's no role here"
// case `??` needs to catch.
//
// Exported (same reason as IsCallable/ListElements) so the VM's own `==`/
// `!=` handling and an outer-package builtin (`type()`) agree with `??`/
// `?.` on what counts as nil, rather than each reimplementing - or
// subtly disagreeing with - this same gotcha.
func IsNilResult(v any) bool {
	if v == nil {
		return true
	}
	// A deferredMember (see vm.go, near pop/popRaw) only ever wraps a
	// struct- or array-kind reflect.Value - neither of which Go lets be
	// nil - so it can never be the nil case OpOptAccess/OpOptIndex are
	// checking for. Reported directly rather than falling through to
	// reflect.ValueOf(v) below, which would wrap the deferredMember
	// *struct itself* (always non-nil, so the answer would happen to come
	// out right anyway) - but doing that would be pure waste, and settling
	// for "happens to be right" here would leave a landmine for whoever
	// wraps a second kind in deferredMember later, if that Kind can be nil.
	if _, ok := v.(deferredMember); ok {
		return false
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface, reflect.UnsafePointer:
		return rv.IsNil()
	default:
		return false
	}
}
