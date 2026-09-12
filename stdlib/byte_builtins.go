package stdlib

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/bits"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"

	"github.com/cms103/owlexpr/vm"
)

// ByteBuiltins registers a `byte` scalar type (Go's uint8, wraparound
// arithmetic - see byteOperations) plus a set of []byte functions aimed
// at the use case a byte type earns its keep for: transformation scripts
// that need to read/write fixed-width binary records, not general text
// processing (that's StringBuiltins' job, and rune-based - see its own
// doc comment for why runes, not bytes).
//
// Registered here: fromHex/bufFromHex/toHex/bufToHex (hex conversion),
// toBase64/fromBase64 (base64, buffer-only - see their own doc comment
// for why there's no single-byte variant the way hex has one),
// bufToString/bufFromString (text conversion, encoding name optional,
// default "UTF-8", any IANA-registered encoding name golang.org/x/text
// implements otherwise - see resolveTextEncoding),
// bitAnd/bitOr/bitXor/bitNot/
// shiftLeft/shiftRight/bitTest/bitSet/bitClear/popCount (bitwise, scoped
// to one byte - see toByteLike), concatBuf/reverseBuf (the []byte
// equivalents of ListBuiltins' concat/reverseList - needed because those
// go through owlexpr.ListElements, which would reflect-copy a []byte
// into a []any of individually-boxed bytes, losing the buffer shape),
// padBufStart/padBufEnd/truncateBuf (byte-exact width, unlike
// StringBuiltins' rune-counted padStart/padEnd - a fixed-width record
// needs an exact byte count, not a character count), and
// readUint16BE/LE, readUint32BE/LE, readUint64BE/LE plus their write
// counterparts (thin wraps over encoding/binary) for fixed-width numeric
// fields.
//
// Deliberately NOT included: signed-integer or 8-bit read/write variants
// (a caller who needs those can sign-extend from the unsigned result, or
// just index/assign a single byte directly - see byteOperations), and
// any "native/host endian" option - every read/write here is explicit
// BE or LE so a owlexpr expression's result never depends on what CPU
// architecture is running it.
func ByteBuiltins() vm.VMOption {
	return func(mc *vm.Machine) error {
		reg := vm.RegisterTypeCoder(stdLibraryTypeCoder, stdLibraryTypeCoderId)
		err := reg(mc)
		if err != nil {
			return err
		}

		if err := vm.RegisterOperation(byte(0), []any{int(0), int64(0)}, byteOperations)(mc); err != nil {
			return err
		}
		if err := vm.RegisterOperation([]byte{}, nil, bytesOperations)(mc); err != nil {
			return err
		}
		if err := vm.RegisterFastSliceIterator(byte(0), byteFastSliceIterate)(mc); err != nil {
			return err
		}

		for name, fn := range namespacedByteFuncs() {
			mc.RegisterNamespacedBuiltin("bytes", name, fn)
		}

		return nil
	}
}

// namespacedByteFuncs is the full ByteBuiltins roster keyed by its
// `bytes.<name>` member name - see StringBuiltins' doc comment for why
// every function here is namespace-only (bytes.bitAnd(...),
// bytes.readUint16BE(...), ...), not also registered bare. A function,
// not a package-level var like the other packs' namespacedXFuncs maps,
// because the readUintNN/writeUintNN entries are built by
// makeReadUintFunc/makeWriteUintFunc closures rather than being plain
// top-level funcs.
func namespacedByteFuncs() map[string]vm.BuiltinFunc {
	return map[string]vm.BuiltinFunc{
		"byte":       byteFunc,
		"fromHex":    fromHexFunc,
		"bufFromHex": bufFromHexFunc,
		"toHex":      toHexFunc,
		"bufToHex":   bufToHexFunc,

		"toBase64":   toBase64Func,
		"fromBase64": fromBase64Func,

		"bufToString":   bufToStringFunc,
		"bufFromString": bufFromStringFunc,

		"bitAnd":     bitAndFunc,
		"bitOr":      bitOrFunc,
		"bitXor":     bitXorFunc,
		"bitNot":     bitNotFunc,
		"shiftLeft":  shiftLeftFunc,
		"shiftRight": shiftRightFunc,
		"bitTest":    bitTestFunc,
		"bitSet":     bitSetFunc,
		"bitClear":   bitClearFunc,
		"popCount":   popCountFunc,

		"concatBuf":   concatBufFunc,
		"reverseBuf":  reverseBufFunc,
		"padBufStart": padBufStartFunc,
		"padBufEnd":   padBufEndFunc,
		"truncateBuf": truncateBufFunc,

		"readUint16BE":  makeReadUintFunc("bytes.readUint16BE", 2, binary.BigEndian),
		"readUint16LE":  makeReadUintFunc("bytes.readUint16LE", 2, binary.LittleEndian),
		"readUint32BE":  makeReadUintFunc("bytes.readUint32BE", 4, binary.BigEndian),
		"readUint32LE":  makeReadUintFunc("bytes.readUint32LE", 4, binary.LittleEndian),
		"readUint64BE":  makeReadUintFunc("bytes.readUint64BE", 8, binary.BigEndian),
		"readUint64LE":  makeReadUintFunc("bytes.readUint64LE", 8, binary.LittleEndian),
		"writeUint16BE": makeWriteUintFunc("bytes.writeUint16BE", 2, binary.BigEndian),
		"writeUint16LE": makeWriteUintFunc("bytes.writeUint16LE", 2, binary.LittleEndian),
		"writeUint32BE": makeWriteUintFunc("bytes.writeUint32BE", 4, binary.BigEndian),
		"writeUint32LE": makeWriteUintFunc("bytes.writeUint32LE", 4, binary.LittleEndian),
		"writeUint64BE": makeWriteUintFunc("bytes.writeUint64BE", 8, binary.BigEndian),
		"writeUint64LE": makeWriteUintFunc("bytes.writeUint64LE", 8, binary.LittleEndian),
	}
}

// Converts a value to a byte
func byteFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("byte() expects 1 argument, got %d", len(args))
	}
	// Attempt to convert the argument to a byte
	return mc.CoercValue(args[0], byte(0))
}

func byteOperations(a, b any, aTypeCode, bTypeCode vm.TypeCode, op vm.OpCode) (any, error) {
	var aVal, bVal int64

	// We convert to an int64 to enable operations, then take the lowest byte on return

	// We always need to force a to an int64
	switch aTypeCode {
	case byteTypeCode:
		aVal = int64(a.(byte))
	case vm.IntTypeCode:
		aVal = int64(a.(int))
	case vm.Int64TypeCode:
		aVal = a.(int64)
	}

	// We always need to force b to an int64
	switch bTypeCode {
	case byteTypeCode:
		bVal = int64(b.(byte))
	case vm.IntTypeCode:
		bVal = int64(b.(int))
	case vm.Int64TypeCode:
		bVal = b.(int64)
	}

	// Converting to byte() always takes the lowest byte of the resulting number
	switch op {
	case vm.OpAdd:
		return byte(aVal + bVal), nil
	case vm.OpSub:
		return byte(aVal - bVal), nil
	case vm.OpDiv:
		if bVal == 0 {
			return nil, errors.New("Divide by zero")
		}
		return byte(aVal / bVal), nil
	case vm.OpMod:
		if bVal == 0 {
			return nil, errors.New("Modulus by zero")
		}
		return byte(aVal % bVal), nil
	case vm.OpPow:
		powVal, err := intPow(aVal, bVal)
		return byte(powVal), err
	case vm.OpMul:
		return byte(aVal * bVal), nil
	case vm.OpEqual:
		return aVal == bVal, nil
	case vm.OpNotEqual:
		return aVal != bVal, nil
	case vm.OpLess:
		return aVal < bVal, nil
	case vm.OpGreater:
		return aVal > bVal, nil
	case vm.OpLessEq:
		return aVal <= bVal, nil
	case vm.OpGreaterEq:
		return aVal >= bVal, nil
	case vm.OpCoerce:
		// Special case - we need to convert value A to type of B

		// We are converting from byte to something else.
		if aTypeCode == byteTypeCode {
			switch bTypeCode {
			case vm.IntTypeCode:
				return int(aVal), nil
			case vm.Int64TypeCode:
				return int64(aVal), nil
			}
		}
		// We are converting from something else to byte
		return byte(aVal), nil
	}
	return nil, errors.ErrUnsupported
}

// bytesOperations implements ==/!=/</>/<=/>= for two []byte values (e.g.
// two fixed-width record keys) via bytes.Equal/bytes.Compare - a real
// byte-by-byte comparison, not Go's own (illegal, panics) `==` on
// slices. Registered with no coercible types: comparing a []byte against
// anything but another []byte has no obvious right answer, so it's a
// clean "operation not supported" error instead of a guess, the same
// call boolOperations already makes for bool.
func bytesOperations(a, b any, aTypeCode, bTypeCode vm.TypeCode, op vm.OpCode) (any, error) {
	if aTypeCode != byteSliceTypeCode || bTypeCode != byteSliceTypeCode {
		return nil, errors.ErrUnsupported
	}
	aVal, bVal := a.([]byte), b.([]byte)
	switch op {
	case vm.OpEqual:
		return bytes.Equal(aVal, bVal), nil
	case vm.OpNotEqual:
		return !bytes.Equal(aVal, bVal), nil
	case vm.OpLess:
		return bytes.Compare(aVal, bVal) < 0, nil
	case vm.OpGreater:
		return bytes.Compare(aVal, bVal) > 0, nil
	case vm.OpLessEq:
		return bytes.Compare(aVal, bVal) <= 0, nil
	case vm.OpGreaterEq:
		return bytes.Compare(aVal, bVal) >= 0, nil
	}
	return nil, errors.ErrUnsupported
}

// intPow computes base**exp by squaring, staying in integer arithmetic
// rather than round-tripping through float64 (which would lose precision
// for large int64 results). Negative exponents aren't representable as an
// integer result, so they're an error rather than silently truncating to 0
// the way integer division would.
func intPow[T int | int64](base, exp T) (T, error) {
	if exp < 0 {
		return 0, errors.New("negative exponent not supported for integer power")
	}
	result := T(1)
	for exp > 0 {
		if exp&1 == 1 {
			result *= base
		}
		base *= base
		exp >>= 1
	}
	return result, nil
}

// byteFastSliceIterate plugs []byte into vm.ForEachListElement's
// reflect-once-per-call fast path (see vm.FastSliceIterator's doc comment),
// registered via RegisterFastSliceIterator rather than hardcoded into the
// vm package's own fastSliceElementIterate - the vm package has no
// dependency on this pack's byte type existing at all.
func byteFastSliceIterate(slice any, yield func(any) bool) bool {
	arr, matched := slice.([]byte)
	if !matched {
		return false
	}
	for _, v := range arr {
		if !yield(v) {
			break
		}
	}
	return true
}

// toByteLike accepts a byte, int, or int64 argument and normalizes it to
// byte the same way byteOperations does - truncating to the low 8 bits
// rather than erroring on an out-of-range int/int64 - so
// bitAnd(buf[i], 0x0F)-style calls compose with plain integer literals
// without forcing an explicit byte(...) conversion at every call site.
// Consistent with byteOperations' own truncate-don't-error policy, not a
// separate rule invented for these functions.
func toByteLike(fnName string, args []any, i int) (byte, error) {
	if i >= len(args) {
		return 0, fmt.Errorf("%s(): expected at least %d argument(s), got %d", fnName, i+1, len(args))
	}
	switch v := args[i].(type) {
	case byte:
		return v, nil
	case int:
		return byte(v), nil
	case int64:
		return byte(v), nil
	}
	return 0, fmt.Errorf("%s(): argument %d must be a byte or integer, got %T", fnName, i+1, args[i])
}

// bytesArg requires args[i] to already be a []byte - unlike toByteLike,
// there's no sensible coercion from some other type into a whole buffer,
// so this is a plain type assertion with a clean error, not a normalizer.
func bytesArg(fnName string, args []any, i int) ([]byte, error) {
	if i >= len(args) {
		return nil, fmt.Errorf("%s(): expected at least %d argument(s), got %d", fnName, i+1, len(args))
	}
	buf, ok := args[i].([]byte)
	if !ok {
		return nil, fmt.Errorf("%s(): argument %d must be a byte slice, got %T", fnName, i+1, args[i])
	}
	return buf, nil
}

// fromHexFunc parses exactly one byte (two hex characters) from a hex
// string, e.g. fromHex("EE") -> 0xEE. Anything other than exactly 2 hex
// characters is an error, not a silent truncation/pad - unlike
// byteOperations' arithmetic wraparound, there's no reasonable default
// for "which byte did you mean" from a string of the wrong length.
func fromHexFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("fromHex() expects 1 argument, got %d", len(args))
	}
	s, err := argString("fromHex", args, 0)
	if err != nil {
		return nil, err
	}
	decoded, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("fromHex(): %w", err)
	}
	if len(decoded) != 1 {
		return nil, fmt.Errorf("fromHex(): expected exactly 2 hex characters (1 byte), got %q", s)
	}
	return decoded[0], nil
}

// bufFromHexFunc decodes a hex string (any even length) into a []byte,
// e.g. bufFromHex("48656c6c6f") -> the bytes of "Hello".
func bufFromHexFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("bufFromHex() expects 1 argument, got %d", len(args))
	}
	s, err := argString("bufFromHex", args, 0)
	if err != nil {
		return nil, err
	}
	decoded, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("bufFromHex(): %w", err)
	}
	return decoded, nil
}

// toHexFunc and bufToHexFunc are fromHex/bufFromHex's inverse: hex-encode
// back to a string, e.g. for logging/debugging a record's contents.
// Named bytes.toHex rather than bytes.string, matching the core str(x)
// builtin - a hex dump and a generic fmt.Sprintf("%v", ...) conversion are
// different operations that happen to share a plausible name, and keeping
// them under different names keeps that distinction visible rather than
// one clobbering the other.
func toHexFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("toHex() expects 1 argument, got %d", len(args))
	}
	b, err := toByteLike("toHex", args, 0)
	if err != nil {
		return nil, err
	}
	return hex.EncodeToString([]byte{b}), nil
}

func bufToHexFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("bufToHex() expects 1 argument, got %d", len(args))
	}
	buf, err := bytesArg("bufToHex", args, 0)
	if err != nil {
		return nil, err
	}
	return hex.EncodeToString(buf), nil
}

// toBase64Func and fromBase64Func are hex's base64 equivalent, but
// buffer-only: unlike fromHex/toHex there's no single-byte variant, since
// a base64 digit encodes 6 bits, not a whole byte, so "exactly one byte's
// worth of base64" isn't a fixed, meaningful character count the way
// "exactly 2 hex characters" is for fromHex. Standard encoding
// (base64.StdEncoding, RFC 4648 with padding), matching the alphabet most
// other tools default to.
func toBase64Func(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("toBase64() expects 1 argument, got %d", len(args))
	}
	buf, err := bytesArg("toBase64", args, 0)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

func fromBase64Func(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("fromBase64() expects 1 argument, got %d", len(args))
	}
	s, err := argString("fromBase64", args, 0)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("fromBase64(): %w", err)
	}
	return decoded, nil
}

// resolveTextEncoding resolves bufToString/bufFromString's optional
// trailing encoding-name argument to a concrete text encoding, defaulting
// to "UTF-8" when the caller omits it. Names are looked up against the
// standard IANA Character Sets registry (the same registry MIME charset
// parameters and HTTP Content-Type headers use) via golang.org/x/text, so
// this supports every encoding that library implements - not just UTF-8
// and ISO-8859-1, but the rest of the ISO-8859-* family, the Windows code
// pages (windows-1252, ...), the CJK encodings (Shift_JIS, EUC-JP, GBK,
// GB18030, Big5, EUC-KR, ...), KOI8-R/U, and more - under their official
// IANA name or any of its registered aliases, matched case-insensitively.
// A name IANA registers but golang.org/x/text doesn't actually implement
// (e.g. "UTF-7") is reported as an error here too, rather than as a nil
// encoding.Encoding for every caller to have to check for individually.
func resolveTextEncoding(fnName string, args []any, i int) (encoding.Encoding, error) {
	name := "UTF-8"
	if i < len(args) {
		var err error
		name, err = argString(fnName, args, i)
		if err != nil {
			return nil, err
		}
	}
	enc, err := ianaindex.IANA.Encoding(name)
	if err != nil {
		return nil, fmt.Errorf("%s(): unknown encoding %q", fnName, name)
	}
	if enc == nil {
		return nil, fmt.Errorf("%s(): encoding %q is a recognized IANA name but isn't supported by this build", fnName, name)
	}
	return enc, nil
}

// bufToStringFunc decodes buf as text in the given encoding (default
// "UTF-8"), e.g. bufToString(bufFromHex("48656c6c6f")) -> "Hello". This is
// bytes.*'s side of the boundary with string.* - see toHex's own doc
// comment for why a []byte<->string conversion belongs in whichever pack
// owns the []byte end of it.
//
// A buffer that isn't actually valid text in the requested encoding is a
// clean error rather than silently-substituted replacement characters.
// Every decoder this resolves to represents an undecodable byte sequence
// as U+FFFD (the Unicode replacement character) rather than failing
// outright, so its presence in the decoded result is used here as the
// signal that decoding failed. The one thing this can't distinguish is
// source text that legitimately already contains a literal U+FFFD
// character - vanishingly rare in practice, and not worth the complexity
// of a stricter check for.
func bufToStringFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("bufToString() expects 1 or 2 arguments, got %d", len(args))
	}
	buf, err := bytesArg("bufToString", args, 0)
	if err != nil {
		return nil, err
	}
	enc, err := resolveTextEncoding("bufToString", args, 1)
	if err != nil {
		return nil, err
	}
	decoded, err := enc.NewDecoder().Bytes(buf)
	if err != nil {
		return nil, fmt.Errorf("bufToString(): %w", err)
	}
	if strings.ContainsRune(string(decoded), utf8.RuneError) {
		return nil, fmt.Errorf("bufToString(): buffer is not valid text in the requested encoding")
	}
	return string(decoded), nil
}

// bufFromStringFunc is bufToString's inverse: encodes s as text in the
// given encoding (default "UTF-8") and returns the raw bytes. A character
// s contains that the target encoding can't represent (e.g. a Japanese
// character encoded as ISO-8859-1) is a clean error rather than a
// silently lossy substitution - that check is built into the underlying
// library's Encoder itself, not something this function does by hand.
func bufFromStringFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("bufFromString() expects 1 or 2 arguments, got %d", len(args))
	}
	s, err := argString("bufFromString", args, 0)
	if err != nil {
		return nil, err
	}
	enc, err := resolveTextEncoding("bufFromString", args, 1)
	if err != nil {
		return nil, err
	}
	encoded, err := enc.NewEncoder().Bytes([]byte(s))
	if err != nil {
		return nil, fmt.Errorf("bufFromString(): %w", err)
	}
	return encoded, nil
}

// bitAnd/bitOr/bitXor/bitNot/shiftLeft/shiftRight/bitTest/bitSet/
// bitClear/popCount are all scoped to one byte (8 bits)
func bitAndFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("bitAnd() expects 2 arguments, got %d", len(args))
	}
	a, err := toByteLike("bitAnd", args, 0)
	if err != nil {
		return nil, err
	}
	b, err := toByteLike("bitAnd", args, 1)
	if err != nil {
		return nil, err
	}
	return a & b, nil
}

func bitOrFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("bitOr() expects 2 arguments, got %d", len(args))
	}
	a, err := toByteLike("bitOr", args, 0)
	if err != nil {
		return nil, err
	}
	b, err := toByteLike("bitOr", args, 1)
	if err != nil {
		return nil, err
	}
	return a | b, nil
}

func bitXorFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("bitXor() expects 2 arguments, got %d", len(args))
	}
	a, err := toByteLike("bitXor", args, 0)
	if err != nil {
		return nil, err
	}
	b, err := toByteLike("bitXor", args, 1)
	if err != nil {
		return nil, err
	}
	return a ^ b, nil
}

// bitNotFunc is Go's own unary ^ on a byte operand, which is already an
// 8-bit complement (XOR against all-1s of the operand's own width) with
// no extra masking needed - it only looks like it might touch higher
// bits if you're thinking in a wider type like int64.
func bitNotFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("bitNot() expects 1 argument, got %d", len(args))
	}
	a, err := toByteLike("bitNot", args, 0)
	if err != nil {
		return nil, err
	}
	return ^a, nil
}

func shiftLeftFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("shiftLeft() expects 2 arguments, got %d", len(args))
	}
	a, err := toByteLike("shiftLeft", args, 0)
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

func shiftRightFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("shiftRight() expects 2 arguments, got %d", len(args))
	}
	a, err := toByteLike("shiftRight", args, 0)
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

// bitIndexArg validates a bit index into one byte: 0 is the
// least-significant bit, matching Rego's bits library and general
// hardware convention, 7 the most significant.
func bitIndexArg(fnName string, args []any, i int) (int64, error) {
	idx, err := argInt(fnName, args, i)
	if err != nil {
		return 0, err
	}
	if idx < 0 || idx > 7 {
		return 0, fmt.Errorf("%s(): bit index must be 0-7, got %d", fnName, idx)
	}
	return idx, nil
}

func bitTestFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("bitTest() expects 2 arguments, got %d", len(args))
	}
	a, err := toByteLike("bitTest", args, 0)
	if err != nil {
		return nil, err
	}
	idx, err := bitIndexArg("bitTest", args, 1)
	if err != nil {
		return nil, err
	}
	return a&(1<<uint(idx)) != 0, nil
}

func bitSetFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("bitSet() expects 2 arguments, got %d", len(args))
	}
	a, err := toByteLike("bitSet", args, 0)
	if err != nil {
		return nil, err
	}
	idx, err := bitIndexArg("bitSet", args, 1)
	if err != nil {
		return nil, err
	}
	return a | (1 << uint(idx)), nil
}

func bitClearFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("bitClear() expects 2 arguments, got %d", len(args))
	}
	a, err := toByteLike("bitClear", args, 0)
	if err != nil {
		return nil, err
	}
	idx, err := bitIndexArg("bitClear", args, 1)
	if err != nil {
		return nil, err
	}
	return a &^ (1 << uint(idx)), nil
}

func popCountFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("popCount() expects 1 argument, got %d", len(args))
	}
	a, err := toByteLike("popCount", args, 0)
	if err != nil {
		return nil, err
	}
	return int64(bits.OnesCount8(a)), nil
}

// concatBufFunc and reverseBufFunc are the []byte-preserving
// equivalents of ListBuiltins' concat/reverseList. Those go through
// owlexpr.ListElements, which would reflect-copy a []byte argument into
// a []any of individually-boxed byte values - correct, but the wrong
// shape for anything meant to stay a buffer (re-slicing it, decoding a
// fixed-width field out of it, handing it back to Go host code). Every
// argument must already be a []byte - there's no sensible coercion from
// some other list-like value into a byte buffer.
func concatBufFunc(mc *vm.Machine, args ...any) (any, error) {
	out := []byte{}
	for i, arg := range args {
		buf, ok := arg.([]byte)
		if !ok {
			return nil, fmt.Errorf("concatBuf(): argument %d must be a byte slice, got %T", i+1, arg)
		}
		out = append(out, buf...)
	}
	return out, nil
}

func reverseBufFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("reverseBuf() expects 1 argument, got %d", len(args))
	}
	buf, err := bytesArg("reverseBuf", args, 0)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(buf))
	for i, b := range buf {
		out[len(buf)-1-i] = b
	}
	return out, nil
}

// padBufStartFunc and padBufEndFunc pad to an exact byte count, not
// StringBuiltins' padStart/padEnd's rune count (see padRunes in
// string_builtins.go) - a fixed-width record needs an exact byte width,
// and reusing the string versions would silently do the wrong thing for
// any pad byte outside plain ASCII if it were ever expressed as a
// string. pad defaults to 0x00, the conventional fill byte for binary
// records; both return a fresh copy even when no padding is needed, so
// the result is never aliased to the input's backing array.
func padBufStartFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("padBufStart() expects 2 or 3 arguments, got %d", len(args))
	}
	buf, err := bytesArg("padBufStart", args, 0)
	if err != nil {
		return nil, err
	}
	targetLen, err := argInt("padBufStart", args, 1)
	if err != nil {
		return nil, err
	}
	pad := byte(0)
	if len(args) == 3 {
		pad, err = toByteLike("padBufStart", args, 2)
		if err != nil {
			return nil, err
		}
	}
	need := targetLen - int64(len(buf))
	if need <= 0 {
		return append([]byte(nil), buf...), nil
	}
	out := make([]byte, 0, targetLen)
	for int64(len(out)) < need {
		out = append(out, pad)
	}
	return append(out, buf...), nil
}

func padBufEndFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("padBufEnd() expects 2 or 3 arguments, got %d", len(args))
	}
	buf, err := bytesArg("padBufEnd", args, 0)
	if err != nil {
		return nil, err
	}
	targetLen, err := argInt("padBufEnd", args, 1)
	if err != nil {
		return nil, err
	}
	pad := byte(0)
	if len(args) == 3 {
		pad, err = toByteLike("padBufEnd", args, 2)
		if err != nil {
			return nil, err
		}
	}
	need := targetLen - int64(len(buf))
	if need <= 0 {
		return append([]byte(nil), buf...), nil
	}
	out := append([]byte(nil), buf...)
	for int64(len(out)) < targetLen {
		out = append(out, pad)
	}
	return out, nil
}

// truncateBufFunc cuts buf down to at most n bytes - a buffer already
// at or under n bytes comes back unchanged (a fresh copy, not aliased),
// not an error, since "already short enough" is the common case when
// clamping a variable-length field to a fixed-width record's maximum.
func truncateBufFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("truncateBuf() expects 2 arguments, got %d", len(args))
	}
	buf, err := bytesArg("truncateBuf", args, 0)
	if err != nil {
		return nil, err
	}
	n, err := argInt("truncateBuf", args, 1)
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("truncateBuf(): length must not be negative, got %d", n)
	}
	if n >= int64(len(buf)) {
		return append([]byte(nil), buf...), nil
	}
	return append([]byte(nil), buf[:n]...), nil
}

// bytesAndOffsetArgs and checkedRegion are the shared argument-parsing
// and bounds-checking behind every readUintNN/writeUintNN function -
// same "expensive-to-write-nine-times, so factor it once" reasoning as
// list_builtins.go's scanElements.
func bytesAndOffsetArgs(fnName string, args []any) ([]byte, int64, error) {
	if len(args) != 2 {
		return nil, 0, fmt.Errorf("%s() expects 2 arguments (bytes, offset), got %d", fnName, len(args))
	}
	buf, err := bytesArg(fnName, args, 0)
	if err != nil {
		return nil, 0, err
	}
	offset, err := argInt(fnName, args, 1)
	if err != nil {
		return nil, 0, err
	}
	return buf, offset, nil
}

func checkedRegion(fnName string, buf []byte, offset int64, width int) ([]byte, error) {
	if offset < 0 || offset+int64(width) > int64(len(buf)) {
		return nil, fmt.Errorf("%s(): offset %d needs %d bytes, out of range for buffer of length %d", fnName, offset, width, len(buf))
	}
	return buf[offset : offset+int64(width)], nil
}

// makeReadUintFunc builds a readUintNN{BE,LE} builtin for one width
// (2/4/8 bytes) and byte order, all backed by encoding/binary - never
// encoding/binary's NativeEndian, so the result never depends on what
// CPU architecture evaluates the expression (see ByteBuiltins' doc
// comment). The decoded value is always returned as int64: a uint64
// result of 2^63 or more wraps to a negative int64 exactly the way Go's
// own int64(someUint64) conversion would, since owlexpr has no
// unsigned 64-bit type - a real, documented limitation for the widest
// width, not a bug specific to this function.
func makeReadUintFunc(fnName string, width int, order binary.ByteOrder) vm.BuiltinFunc {
	return func(mc *vm.Machine, args ...any) (any, error) {
		buf, offset, err := bytesAndOffsetArgs(fnName, args)
		if err != nil {
			return nil, err
		}
		region, err := checkedRegion(fnName, buf, offset, width)
		if err != nil {
			return nil, err
		}
		switch width {
		case 2:
			return int64(order.Uint16(region)), nil
		case 4:
			return int64(order.Uint32(region)), nil
		default:
			return int64(order.Uint64(region)), nil
		}
	}
}

// makeWriteUintFunc builds a writeUintNN{BE,LE} builtin - the write
// counterpart to makeReadUintFunc, returning a fresh []byte with the
// value written at offset rather than mutating buf, matching every other
// byte-slice function in this file (and map/filter/sort elsewhere in
// this codebase) always returning a new value.
func makeWriteUintFunc(fnName string, width int, order binary.ByteOrder) vm.BuiltinFunc {
	return func(mc *vm.Machine, args ...any) (any, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("%s() expects 3 arguments (bytes, offset, value), got %d", fnName, len(args))
		}
		buf, err := bytesArg(fnName, args, 0)
		if err != nil {
			return nil, err
		}
		offset, err := argInt(fnName, args, 1)
		if err != nil {
			return nil, err
		}
		value, err := argInt(fnName, args, 2)
		if err != nil {
			return nil, err
		}
		out := append([]byte(nil), buf...)
		region, err := checkedRegion(fnName, out, offset, width)
		if err != nil {
			return nil, err
		}
		switch width {
		case 2:
			order.PutUint16(region, uint16(value))
		case 4:
			order.PutUint32(region, uint32(value))
		default:
			order.PutUint64(region, uint64(value))
		}
		return out, nil
	}
}
