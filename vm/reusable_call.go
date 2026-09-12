package vm

// ReusableCall is the call surface for a builtin that invokes the same
// callee many times in a row - once per list/map element, or once per
// pull from an iterator - without paying closure.call's normal per-call
// allocation cost (a fresh values slice and localFrame) on every single
// invocation. Construct one with NewReusableCall before the loop starts,
// then call whichever CallN matches the arity that particular call site
// needs - CallN reuses the same underlying buffer and, when eligible (see
// newCallFrame's doc comment), the same localFrame across every call made
// through this ReusableCall.
//
// callee doesn't need to be any particular shape, and different CallN
// arities may even be used across the lifetime of one ReusableCall (a
// caller like mapFunc picks whichever arity its source shape calls for,
// consistently, for the whole scan) - callReusing transparently falls
// back to an ordinary Call for anything that doesn't qualify for reuse,
// so a caller never needs its own logic to decide which path applies.
//
// Safety of the reuse depends entirely on how a ReusableCall is used, not
// just on what it wraps: every call made through one must complete - body
// fully evaluated, result consumed - before the next call through the
// same ReusableCall begins. That is exactly the pattern a builtin's own
// internal element-by-element loop naturally has (map/filter/reduce/
// list.find and friends never start element i+1 before finishing element
// i), so no caller in this codebase needs to do anything special to
// satisfy it - but a ReusableCall must never be retained and called from
// two overlapping call stacks (e.g. handed to something that recurses
// back into the same callee while a call through this ReusableCall is
// still in progress).
type ReusableCall struct {
	mc    *Machine
	fn    any
	frame *callFrame
	buf   []any
}

// NewReusableCall prepares to call fn repeatedly against mc via the
// returned ReusableCall's CallN methods. Cheap to call unconditionally
// once per builtin invocation, even when fn turns out not to be eligible
// for the reuse fast path - see newCallFrame's own doc comment.
func NewReusableCall(mc *Machine, fn any) *ReusableCall {
	return &ReusableCall{mc: mc, fn: fn, frame: newCallFrame(mc, fn)}
}

// Call1 invokes fn with a single argument, e.g. a list element or a
// filter/map predicate's own sole parameter.
func (r *ReusableCall) Call1(a any) (any, error) {
	if len(r.buf) != 1 {
		r.buf = make([]any, 1)
	}
	r.buf[0] = a
	return r.mc.callReusing(r.fn, r.buf, r.frame)
}

// Call2 invokes fn with two arguments, e.g. a map/filter pair (k, v) or
// reduce's (acc, element).
func (r *ReusableCall) Call2(a, b any) (any, error) {
	if len(r.buf) != 2 {
		r.buf = make([]any, 2)
	}
	r.buf[0], r.buf[1] = a, b
	return r.mc.callReusing(r.fn, r.buf, r.frame)
}

// Call3 invokes fn with three arguments: reduce's (acc, k, v) shape over
// a paired source.
func (r *ReusableCall) Call3(a, b, c any) (any, error) {
	if len(r.buf) != 3 {
		r.buf = make([]any, 3)
	}
	r.buf[0], r.buf[1], r.buf[2] = a, b, c
	return r.mc.callReusing(r.fn, r.buf, r.frame)
}
