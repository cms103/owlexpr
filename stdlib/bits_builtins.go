package stdlib

import (
	"fmt"
	"math/bits"

	"github.com/cms103/owlexpr/vm"
)

// BitsBuiltins registers int64-width bitwise functions under the `bits`
// namespace (bits.bitAnd(...), bits.shiftLeft(...), ...)
//
// This exists for the same class of real-world use case bitAnd/bitOr/etc.
// serve in the byte pack, just at int64 width instead of byte width:
// bitmask columns pulled from a database or legacy system (a "days of
// week" or "enabled features" field packed into one integer), RBAC/ACL
// permission flags (perms & WRITE != 0), and IP/CIDR range checks
// (ip & mask == network) - all cases where a rule evaluates a plain int64
// against other int64 bit patterns, not a byte buffer.
func BitsBuiltins() vm.VMOption {
	return func(mc *vm.Machine) error {
		for name, fn := range namespacedBitsFuncs {
			mc.RegisterNamespacedBuiltin("bits", name, fn)
		}
		return nil
	}
}

// namespacedBitsFuncs is the full BitsBuiltins roster keyed by its
// `bits.<name>` member name.
var namespacedBitsFuncs = map[string]vm.BuiltinFunc{
	"bitAnd":     bitsAndFunc,
	"bitOr":      bitsOrFunc,
	"bitXor":     bitsXorFunc,
	"bitNot":     bitsNotFunc,
	"shiftLeft":  bitsShiftLeftFunc,
	"shiftRight": bitsShiftRightFunc,
	"bitTest":    bitsTestFunc,
	"bitSet":     bitsSetFunc,
	"bitClear":   bitsClearFunc,
	"popCount":   bitsPopCountFunc,
}

func bitsAndFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("bitAnd() expects 2 arguments, got %d", len(args))
	}
	a, err := argInt("bitAnd", args, 0)
	if err != nil {
		return nil, err
	}
	b, err := argInt("bitAnd", args, 1)
	if err != nil {
		return nil, err
	}
	return a & b, nil
}

func bitsOrFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("bitOr() expects 2 arguments, got %d", len(args))
	}
	a, err := argInt("bitOr", args, 0)
	if err != nil {
		return nil, err
	}
	b, err := argInt("bitOr", args, 1)
	if err != nil {
		return nil, err
	}
	return a | b, nil
}

func bitsXorFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("bitXor() expects 2 arguments, got %d", len(args))
	}
	a, err := argInt("bitXor", args, 0)
	if err != nil {
		return nil, err
	}
	b, err := argInt("bitXor", args, 1)
	if err != nil {
		return nil, err
	}
	return a ^ b, nil
}

// bitsNotFunc is Go's own unary ^ on an int64 operand: a full 64-bit
// two's-complement flip, including the sign bit, matching int64's own
// semantics rather than masking to some narrower width.
func bitsNotFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("bitNot() expects 1 argument, got %d", len(args))
	}
	a, err := argInt("bitNot", args, 0)
	if err != nil {
		return nil, err
	}
	return ^a, nil
}

// bitsShiftLeftFunc/bitsShiftRightFunc use Go's native int64 shift, which
// is arithmetic (sign-extending) on the right - matching int64's own
// semantics. The bitmask/RBAC/CIDR use cases this pack targets deal in
// non-negative bit patterns in practice, where arithmetic and logical
// right shift agree; a caller working with a value that has bit 63 set
// (e.g. a uint64 wrapped negative by readUint64BE) gets Go's own signed
// shift, not a silently different logical one.
func bitsShiftLeftFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("shiftLeft() expects 2 arguments, got %d", len(args))
	}
	a, err := argInt("shiftLeft", args, 0)
	if err != nil {
		return nil, err
	}
	n, err := argInt("shiftLeft", args, 1)
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("shiftLeft(): shift amount must not be negative, got %d", n)
	}
	return a << uint(n), nil
}

func bitsShiftRightFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("shiftRight() expects 2 arguments, got %d", len(args))
	}
	a, err := argInt("shiftRight", args, 0)
	if err != nil {
		return nil, err
	}
	n, err := argInt("shiftRight", args, 1)
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("shiftRight(): shift amount must not be negative, got %d", n)
	}
	return a >> uint(n), nil
}

// bitsIndexArg validates a bit index into one int64: 0 is the
// least-significant bit (matching the byte pack's bitIndexArg and Rego's
// bits library convention), 63 the most significant.
func bitsIndexArg(fnName string, args []any, i int) (int64, error) {
	idx, err := argInt(fnName, args, i)
	if err != nil {
		return 0, err
	}
	if idx < 0 || idx > 63 {
		return 0, fmt.Errorf("%s(): bit index must be 0-63, got %d", fnName, idx)
	}
	return idx, nil
}

func bitsTestFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("bitTest() expects 2 arguments, got %d", len(args))
	}
	a, err := argInt("bitTest", args, 0)
	if err != nil {
		return nil, err
	}
	idx, err := bitsIndexArg("bitTest", args, 1)
	if err != nil {
		return nil, err
	}
	return a&(1<<uint(idx)) != 0, nil
}

func bitsSetFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("bitSet() expects 2 arguments, got %d", len(args))
	}
	a, err := argInt("bitSet", args, 0)
	if err != nil {
		return nil, err
	}
	idx, err := bitsIndexArg("bitSet", args, 1)
	if err != nil {
		return nil, err
	}
	return a | (1 << uint(idx)), nil
}

func bitsClearFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("bitClear() expects 2 arguments, got %d", len(args))
	}
	a, err := argInt("bitClear", args, 0)
	if err != nil {
		return nil, err
	}
	idx, err := bitsIndexArg("bitClear", args, 1)
	if err != nil {
		return nil, err
	}
	return a &^ (1 << uint(idx)), nil
}

func bitsPopCountFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("popCount() expects 1 argument, got %d", len(args))
	}
	a, err := argInt("popCount", args, 0)
	if err != nil {
		return nil, err
	}
	return int64(bits.OnesCount64(uint64(a))), nil
}
