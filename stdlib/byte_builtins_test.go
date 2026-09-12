package stdlib

import (
	"reflect"
	"testing"

	"github.com/cms103/owlexpr"
)

// evalByte parses, compiles, and runs input with ByteBuiltins() enabled,
// failing the test on any parse/compile/run error.
func evalByte(t *testing.T, env map[string]any, input string) any {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(ByteBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	return res
}

func evalByteExpectError(t *testing.T, env map[string]any, input string) {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(ByteBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatalf("expected an error for %q, got none", input)
	}
}

func TestByteNotRegisteredByDefault(t *testing.T) {
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	instructions, err := owlexpr.Compile(`bytes.fromHex("EE")`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Fatalf("expected bytes.fromHex() to be undefined without ByteBuiltins()")
	}
}

// TestByteIndexingIntoSliceWorks is the end-to-end proof that a raw Go
// []byte from env composes with the rest of the language once
// ByteBuiltins() is loaded: indexValue's existing reflect fallback hands
// back a real Go byte with no core VM changes, and byteOperations makes
// it arithmetic/comparable.
func TestByteIndexingIntoSliceWorks(t *testing.T) {
	env := map[string]any{"buf": []byte{0x10, 0x20, 0x30}}
	if got := evalByte(t, env, `buf[1]`); got != byte(0x20) {
		t.Errorf("buf[1] = %v, want 0x20", got)
	}
	if got := evalByte(t, env, `buf[1] + 1`); got != byte(0x21) {
		t.Errorf("buf[1] + 1 = %v, want 0x21", got)
	}
	if got := evalByte(t, env, `buf[1] == 32`); got != true {
		t.Errorf("buf[1] == 32 = %v, want true", got)
	}
	if got := evalByte(t, env, `buf[0] < buf[1]`); got != true {
		t.Errorf("buf[0] < buf[1] = %v, want true", got)
	}
	if got := evalByte(t, env, `len(buf)`); got != int64(3) {
		t.Errorf("len(buf) = %v, want 3", got)
	}
	// Slicing already worked before ByteBuiltins() existed (sliceValue's
	// reflect.Slice fallback), still does with it loaded.
	if got := evalByte(t, env, `buf[1:3]`); !reflect.DeepEqual(got, []byte{0x20, 0x30}) {
		t.Errorf("buf[1:3] = %v, want [0x20 0x30]", got)
	}
}

func TestByteArithmeticWraps(t *testing.T) {
	env := map[string]any{"b": byte(250)}
	if got := evalByte(t, env, `b + 10`); got != byte(4) { // 260 mod 256
		t.Errorf("b + 10 = %v, want 4 (wraparound)", got)
	}
	if got := evalByte(t, env, `b - 255`); got != byte(251) { // -5 as a byte
		t.Errorf("b - 255 = %v, want 251", got)
	}
	if got := evalByte(t, env, `b * 2`); got != byte(244) { // 500 mod 256
		t.Errorf("b * 2 = %v, want 244", got)
	}
}

func TestByteDivModByZero(t *testing.T) {
	env := map[string]any{"b": byte(10)}
	evalByteExpectError(t, env, `b / 0`)
	evalByteExpectError(t, env, `b % 0`)
}

// TestBytePow covers byteOperations' OpPow case (byte_builtins.go's own
// intPow instantiation, distinct from vm/operations.go's), untested by
// any existing test - TestByteArithmeticWraps only covers +, -, *.
func TestBytePow(t *testing.T) {
	env := map[string]any{"b": byte(3)}
	if got := evalByte(t, env, `b ** 2`); got != byte(9) {
		t.Errorf("b ** 2 = %v, want 9", got)
	}
	// Wraps the same way +/-/* do: 16 ** 2 = 256, which wraps to 0.
	env2 := map[string]any{"b": byte(16)}
	if got := evalByte(t, env2, `b ** 2`); got != byte(0) {
		t.Errorf("16 ** 2 (byte) = %v, want 0 (wraparound)", got)
	}
}

// TestByteComparisons rounds out byteOperations' comparison set - existing
// tests only cover == and <.
func TestByteComparisons(t *testing.T) {
	env := map[string]any{"a": byte(5), "b": byte(10)}
	if got := evalByte(t, env, `a != b`); got != true {
		t.Errorf("a != b = %v, want true", got)
	}
	if got := evalByte(t, env, `b > a`); got != true {
		t.Errorf("b > a = %v, want true", got)
	}
	if got := evalByte(t, env, `a <= a`); got != true {
		t.Errorf("a <= a = %v, want true", got)
	}
	if got := evalByte(t, env, `b >= a`); got != true {
		t.Errorf("b >= a = %v, want true", got)
	}
}

func TestBytesEquality(t *testing.T) {
	env := map[string]any{
		"a": []byte{1, 2, 3},
		"b": []byte{1, 2, 3},
		"c": []byte{1, 2, 4},
	}
	if got := evalByte(t, env, `a == b`); got != true {
		t.Errorf("a == b = %v, want true", got)
	}
	if got := evalByte(t, env, `a == c`); got != false {
		t.Errorf("a == c = %v, want false", got)
	}
	if got := evalByte(t, env, `a < c`); got != true {
		t.Errorf("a < c = %v, want true", got)
	}
}

// TestBytesComparisonsFull rounds out bytesOperations' comparison set
// (!=, >, <=, >=), plus its own unsupported-op fallthrough for an
// operation []byte doesn't support at all (e.g. arithmetic +).
func TestBytesComparisonsFull(t *testing.T) {
	env := map[string]any{
		"a": []byte{1, 2, 3},
		"b": []byte{1, 2, 3},
		"c": []byte{1, 2, 4},
	}
	if got := evalByte(t, env, `a != c`); got != true {
		t.Errorf("a != c = %v, want true", got)
	}
	if got := evalByte(t, env, `c > a`); got != true {
		t.Errorf("c > a = %v, want true", got)
	}
	if got := evalByte(t, env, `a <= b`); got != true {
		t.Errorf("a <= b = %v, want true", got)
	}
	if got := evalByte(t, env, `c >= a`); got != true {
		t.Errorf("c >= a = %v, want true", got)
	}
	evalByteExpectError(t, env, `a + b`)
}

func TestByteMapFilterReduce(t *testing.T) {
	env := map[string]any{"buf": []byte{1, 2, 3, 4}}
	got := evalByte(t, env, `sum(buf)`)
	if got != byte(10) && got != int64(10) {
		t.Errorf("sum(buf) = %v (%T), want 10", got, got)
	}
	filtered := evalByte(t, env, `filter(buf, x => x > 2)`)
	if !reflect.DeepEqual(filtered, []any{byte(3), byte(4)}) {
		t.Errorf("filter(buf, x => x > 2) = %v", filtered)
	}
}

func TestFromHexAndBack(t *testing.T) {
	if got := evalByte(t, nil, `bytes.fromHex("EE")`); got != byte(0xEE) {
		t.Errorf(`bytes.fromHex("EE") = %v, want 0xEE`, got)
	}
	if got := evalByte(t, nil, `bytes.toHex(bytes.fromHex("EE"))`); got != "ee" {
		t.Errorf(`bytes.toHex(...) = %v, want "ee"`, got)
	}
	evalByteExpectError(t, nil, `bytes.fromHex("E")`)    // odd length
	evalByteExpectError(t, nil, `bytes.fromHex("EEEE")`) // too long
	evalByteExpectError(t, nil, `bytes.fromHex("ZZ")`)   // not hex

	got := evalByte(t, nil, `bytes.bufFromHex("48656c6c6f")`)
	if !reflect.DeepEqual(got, []byte("Hello")) {
		t.Errorf(`bytes.bufFromHex("48656c6c6f") = %v, want "Hello" bytes`, got)
	}
	if s := evalByte(t, map[string]any{"b": []byte("Hi")}, `bytes.bufToHex(b)`); s != "4869" {
		t.Errorf(`bytes.bufToHex("Hi") = %v, want "4869"`, s)
	}
	evalByteExpectError(t, nil, `bytes.bufFromHex("abc")`) // odd length
}

func TestBase64AndBack(t *testing.T) {
	if got := evalByte(t, map[string]any{"b": []byte("Hello")}, `bytes.toBase64(b)`); got != "SGVsbG8=" {
		t.Errorf(`bytes.toBase64("Hello") = %v, want "SGVsbG8="`, got)
	}
	got := evalByte(t, nil, `bytes.fromBase64("SGVsbG8=")`)
	if !reflect.DeepEqual(got, []byte("Hello")) {
		t.Errorf(`bytes.fromBase64("SGVsbG8=") = %v, want "Hello" bytes`, got)
	}
	if got := evalByte(t, map[string]any{"b": []byte("Hello")}, `bytes.fromBase64(bytes.toBase64(b))`); !reflect.DeepEqual(got, []byte("Hello")) {
		t.Errorf(`round trip = %v, want "Hello" bytes`, got)
	}
	evalByteExpectError(t, nil, `bytes.fromBase64("not valid base64!!")`)
	evalByteExpectError(t, nil, `bytes.toBase64("Hello")`) // string, not []byte - no coercion
}

func TestBufToStringAndFromStringUTF8(t *testing.T) {
	// Default encoding, no explicit name.
	if got := evalByte(t, nil, `bytes.bufToString(bytes.bufFromHex("48656c6c6f"))`); got != "Hello" {
		t.Errorf(`bufToString(...) = %v, want "Hello"`, got)
	}
	// Explicit "UTF-8" name, and non-ASCII text round-tripping.
	if got := evalByte(t, nil, `bytes.bufToString(bytes.bufFromString("héllo", "utf-8"), "UTF-8")`); got != "héllo" {
		t.Errorf(`round trip = %v, want "héllo"`, got)
	}
	got := evalByte(t, nil, `bytes.bufFromString("hi")`)
	if !reflect.DeepEqual(got, []byte("hi")) {
		t.Errorf(`bufFromString("hi") = %v, want []byte("hi")`, got)
	}
	// Invalid UTF-8 is a clean error, not replacement characters.
	evalByteExpectError(t, nil, `bytes.bufToString(bytes.fromBase64("/w=="))`) // 0xFF alone isn't valid UTF-8
}

func TestBufToStringAndFromStringLatin1(t *testing.T) {
	// 0xE9 is "é" in Latin-1/ISO-8859-1, decoding to the same code point.
	if got := evalByte(t, nil, `bytes.bufToString(bytes.fromBase64("6Q=="), "iso-8859-1")`); got != "é" {
		t.Errorf(`bufToString(0xE9, "iso-8859-1") = %q, want "é"`, got)
	}
	// IANA-registered aliases for the same encoding all resolve the same way.
	want := []byte{0xE9}
	for _, name := range []string{"iso-8859-1", "ISO-8859-1", "latin1", "l1", "cp819"} {
		got := evalByte(t, map[string]any{"enc": name}, `bytes.bufFromString("é", enc)`)
		if !reflect.DeepEqual(got, want) {
			t.Errorf(`bufFromString("é", %q) = %v, want %v`, name, got, want)
		}
	}
	// A character outside 0-255 can't be represented in Latin-1.
	evalByteExpectError(t, nil, `bytes.bufFromString("日", "iso-8859-1")`)
}

// TestBufToStringOtherEncodings spot-checks a couple of encodings besides
// UTF-8/Latin-1, confirming this isn't hardcoded to just those two: a
// Windows code page and a Japanese double-byte encoding both work through
// the same mechanism.
func TestBufToStringOtherEncodings(t *testing.T) {
	// 0x93 0x41 0x94 is a curly-quoted "A" in Windows-1252.
	if got := evalByte(t, nil, `bytes.bufToString(bytes.bufFromHex("934194"), "windows-1252")`); got != "“A”" {
		t.Errorf(`bufToString(..., "windows-1252") = %q, want %q`, got, "“A”")
	}
	// 0x82 0xA0 is the hiragana "あ" in Shift_JIS.
	if got := evalByte(t, nil, `bytes.bufToString(bytes.bufFromHex("82a0"), "Shift_JIS")`); got != "あ" {
		t.Errorf(`bufToString(..., "Shift_JIS") = %q, want "あ"`, got)
	}
	if got := evalByte(t, nil, `bytes.bufFromString("あ", "Shift_JIS")`); !reflect.DeepEqual(got, []byte{0x82, 0xA0}) {
		t.Errorf(`bufFromString("あ", "Shift_JIS") = %v, want [0x82 0xA0]`, got)
	}
	// A Japanese character has no representation in Windows-1252.
	evalByteExpectError(t, nil, `bytes.bufFromString("あ", "windows-1252")`)
}

func TestBufToStringUnknownEncodingErrors(t *testing.T) {
	// Not a real encoding name at all.
	evalByteExpectError(t, nil, `bytes.bufToString(bytes.byte(72), "not-a-real-encoding")`)
	evalByteExpectError(t, nil, `bytes.bufFromString("hi", "not-a-real-encoding")`)
	// A name IANA registers but golang.org/x/text doesn't implement.
	evalByteExpectError(t, nil, `bytes.bufToString(bytes.byte(72), "UTF-7")`)
}

func TestBitwiseOps(t *testing.T) {
	if got := evalByte(t, nil, `bytes.bitAnd(12, 10)`); got != byte(8) {
		t.Errorf("bytes.bitAnd(12, 10) = %v, want 8", got)
	}
	if got := evalByte(t, nil, `bytes.bitOr(12, 10)`); got != byte(14) {
		t.Errorf("bytes.bitOr(12, 10) = %v, want 14", got)
	}
	if got := evalByte(t, nil, `bytes.bitXor(12, 10)`); got != byte(6) {
		t.Errorf("bytes.bitXor(12, 10) = %v, want 6", got)
	}
	if got := evalByte(t, nil, `bytes.bitNot(0)`); got != byte(255) {
		t.Errorf("bytes.bitNot(0) = %v, want 255", got)
	}
	if got := evalByte(t, nil, `bytes.shiftLeft(1, 4)`); got != byte(16) {
		t.Errorf("bytes.shiftLeft(1, 4) = %v, want 16", got)
	}
	if got := evalByte(t, nil, `bytes.shiftRight(bytes.fromHex("F0"), 4)`); got != byte(0x0F) {
		t.Errorf("bytes.shiftRight(0xF0, 4) = %v, want 0x0F", got)
	}
	if got := evalByte(t, nil, `bytes.shiftLeft(1, 8)`); got != byte(0) {
		t.Errorf("bytes.shiftLeft(1, 8) = %v, want 0 (shifted out of a byte)", got)
	}
	evalByteExpectError(t, nil, `bytes.shiftLeft(1, -1)`)
}

func TestBitTestSetClear(t *testing.T) {
	if got := evalByte(t, nil, `bytes.bitTest(bytes.fromHex("81"), 0)`); got != true { // 0x81 = 1000 0001
		t.Errorf("bytes.bitTest(0x81, 0) = %v, want true", got)
	}
	if got := evalByte(t, nil, `bytes.bitTest(bytes.fromHex("81"), 1)`); got != false {
		t.Errorf("bytes.bitTest(0x81, 1) = %v, want false", got)
	}
	if got := evalByte(t, nil, `bytes.bitSet(0, 3)`); got != byte(8) {
		t.Errorf("bytes.bitSet(0, 3) = %v, want 8", got)
	}
	if got := evalByte(t, nil, `bytes.bitClear(bytes.fromHex("FF"), 0)`); got != byte(0xFE) {
		t.Errorf("bytes.bitClear(0xFF, 0) = %v, want 0xFE", got)
	}
	evalByteExpectError(t, nil, `bytes.bitTest(0, 8)`) // out of range
	evalByteExpectError(t, nil, `bytes.bitTest(0, -1)`)
}

func TestPopCount(t *testing.T) {
	if got := evalByte(t, nil, `bytes.popCount(bytes.fromHex("FF"))`); got != int64(8) {
		t.Errorf("bytes.popCount(0xFF) = %v, want 8", got)
	}
	if got := evalByte(t, nil, `bytes.popCount(0)`); got != int64(0) {
		t.Errorf("bytes.popCount(0) = %v, want 0", got)
	}
}

func TestConcatAndReverseBuf(t *testing.T) {
	env := map[string]any{"a": []byte{1, 2}, "b": []byte{3, 4}}
	got := evalByte(t, env, `bytes.concatBuf(a, b)`)
	if !reflect.DeepEqual(got, []byte{1, 2, 3, 4}) {
		t.Errorf("bytes.concatBuf(a, b) = %v", got)
	}
	got = evalByte(t, env, `bytes.reverseBuf(a)`)
	if !reflect.DeepEqual(got, []byte{2, 1}) {
		t.Errorf("bytes.reverseBuf(a) = %v", got)
	}
	evalByteExpectError(t, map[string]any{"x": []any{1, 2}}, `bytes.concatBuf(x)`)
}

func TestPadAndTruncateBuf(t *testing.T) {
	env := map[string]any{"b": []byte{1, 2, 3}}
	if got := evalByte(t, env, `bytes.padBufStart(b, 5)`); !reflect.DeepEqual(got, []byte{0, 0, 1, 2, 3}) {
		t.Errorf("bytes.padBufStart(b, 5) = %v", got)
	}
	if got := evalByte(t, env, `bytes.padBufEnd(b, 5)`); !reflect.DeepEqual(got, []byte{1, 2, 3, 0, 0}) {
		t.Errorf("bytes.padBufEnd(b, 5) = %v", got)
	}
	if got := evalByte(t, env, `bytes.padBufEnd(b, 5, bytes.fromHex("FF"))`); !reflect.DeepEqual(got, []byte{1, 2, 3, 0xFF, 0xFF}) {
		t.Errorf("bytes.padBufEnd(b, 5, 0xFF) = %v", got)
	}
	// Already long enough - unchanged, not an error.
	if got := evalByte(t, env, `bytes.padBufStart(b, 2)`); !reflect.DeepEqual(got, []byte{1, 2, 3}) {
		t.Errorf("bytes.padBufStart(b, 2) = %v, want unchanged", got)
	}
	if got := evalByte(t, env, `bytes.truncateBuf(b, 2)`); !reflect.DeepEqual(got, []byte{1, 2}) {
		t.Errorf("bytes.truncateBuf(b, 2) = %v", got)
	}
	if got := evalByte(t, env, `bytes.truncateBuf(b, 10)`); !reflect.DeepEqual(got, []byte{1, 2, 3}) {
		t.Errorf("bytes.truncateBuf(b, 10) = %v, want unchanged", got)
	}
}

func TestFixedWidthReadWrite(t *testing.T) {
	env := map[string]any{"buf": []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}}

	if got := evalByte(t, env, `bytes.readUint16BE(buf, 0)`); got != int64(0x0001) {
		t.Errorf("bytes.readUint16BE(buf, 0) = %#x, want 0x0001", got)
	}
	if got := evalByte(t, env, `bytes.readUint16LE(buf, 0)`); got != int64(0x0100) {
		t.Errorf("bytes.readUint16LE(buf, 0) = %#x, want 0x0100", got)
	}
	if got := evalByte(t, env, `bytes.readUint32BE(buf, 2)`); got != int64(0x02030405) {
		t.Errorf("bytes.readUint32BE(buf, 2) = %#x, want 0x02030405", got)
	}
	if got := evalByte(t, env, `bytes.readUint32LE(buf, 2)`); got != int64(0x05040302) {
		t.Errorf("bytes.readUint32LE(buf, 2) = %#x, want 0x05040302", got)
	}
	if got := evalByte(t, env, `bytes.readUint64BE(buf, 0)`); got != int64(0x0001020304050607) {
		t.Errorf("bytes.readUint64BE(buf, 0) = %#x, want 0x0001020304050607", got)
	}

	evalByteExpectError(t, env, `bytes.readUint32BE(buf, 6)`)  // 6+4 > 8
	evalByteExpectError(t, env, `bytes.readUint16BE(buf, -1)`) // negative offset

	// Round-trip: write then read back, and confirm the original buffer
	// wasn't mutated (write always returns a fresh copy).
	got := evalByte(t, env, `bytes.readUint32BE(bytes.writeUint32BE(buf, 0, 305419896), 0)`) // 0x12345678
	if got != int64(0x12345678) {
		t.Errorf("round-trip writeUint32BE/readUint32BE = %#x, want 0x12345678", got)
	}
	if got := evalByte(t, env, `buf[0]`); got != byte(0x00) {
		t.Errorf("original buf was mutated by writeUint32BE: buf[0] = %v", got)
	}

	// owlexpr number literals are decimal only (no 0x syntax) - 513 is
	// 0x0201, written little-endian as bytes [0x01, 0x02].
	written := evalByte(t, env, `bytes.writeUint16LE(buf, 0, 513)`)
	want := []byte{0x01, 0x02, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	if !reflect.DeepEqual(written, want) {
		t.Errorf("bytes.writeUint16LE(buf, 0, 513) = %v, want %v", written, want)
	}

	// width=8 (the read/write factories' default switch case) - only
	// exercised above for readUint64BE, never for readUint64LE or either
	// writeUint64 direction.
	if got := evalByte(t, env, `bytes.readUint64LE(buf, 0)`); got != int64(0x0706050403020100) {
		t.Errorf("bytes.readUint64LE(buf, 0) = %#x, want 0x0706050403020100", got)
	}
	got64 := evalByte(t, env, `bytes.readUint64BE(bytes.writeUint64BE(buf, 0, 1), 0)`)
	if got64 != int64(1) {
		t.Errorf("round-trip writeUint64BE/readUint64BE = %v, want 1", got64)
	}

	evalByteExpectError(t, env, `bytes.writeUint32BE(buf, 6, 1)`) // 6+4 > 8
}

// TestToByteLikeVariants covers toByteLike's byte/int/int64 argument
// forms and its own arg-count/type-error branches - existing tests only
// ever pass a byte value (typically via bytes.fromHex(...)).
func TestToByteLikeVariants(t *testing.T) {
	if got := evalByte(t, nil, `bytes.bitNot(bytes.fromHex("00"))`); got != byte(0xFF) {
		t.Errorf(`bytes.bitNot(0x00) = %v, want 0xFF`, got)
	}
	// int and int64 literals both coerce to byte (truncating to the low
	// 8 bits), same as byteOperations' own arithmetic.
	n := int64(300)
	want := byte(n) & byte(15)
	if got := evalByte(t, nil, `bytes.bitAnd(300, 15)`); got != want {
		t.Errorf("bytes.bitAnd(300, 15) = %v, want %v", got, want)
	}
	evalByteExpectError(t, nil, `bytes.bitNot()`)
	evalByteExpectError(t, nil, `bytes.bitNot("not a byte")`)
}

// TestBytesArgRejectsNonByteSlice covers bytesArg's own type-error branch.
func TestBytesArgRejectsNonByteSlice(t *testing.T) {
	evalByteExpectError(t, nil, `bytes.reverseBuf("not bytes")`)
	evalByteExpectError(t, nil, `bytes.reverseBuf([1, 2, 3])`)
}

// TestBitIndexOutOfRangeErrors covers bitIndexArg's own 0-7 range guard,
// shared by bitTest/bitSet/bitClear.
func TestBitIndexOutOfRangeErrors(t *testing.T) {
	env := map[string]any{"b": byte(1)}
	evalByteExpectError(t, env, `bytes.bitTest(b, 8)`)
	evalByteExpectError(t, env, `bytes.bitSet(b, -1)`)
	evalByteExpectError(t, env, `bytes.bitClear(b, 100)`)
}

// TestShiftNegativeAmountErrors covers shiftLeft/shiftRight's own
// negative-shift-amount guard.
func TestShiftNegativeAmountErrors(t *testing.T) {
	env := map[string]any{"b": byte(1)}
	evalByteExpectError(t, env, `bytes.shiftLeft(b, -1)`)
	evalByteExpectError(t, env, `bytes.shiftRight(b, -1)`)
}

// TestTruncateBufNegativeLengthErrors covers truncateBufFunc's own
// negative-length guard.
func TestTruncateBufNegativeLengthErrors(t *testing.T) {
	env := map[string]any{"b": []byte{1, 2, 3}}
	evalByteExpectError(t, env, `bytes.truncateBuf(b, -1)`)
}

// TestFromHexErrors covers fromHexFunc's own decode-failure
// and wrong-length guards (as opposed to bufFromHexFunc's, a
// separate function with its own error wrapping).
func TestFromHexErrors(t *testing.T) {
	evalByteExpectError(t, nil, `bytes.fromHex("not hex")`)
	evalByteExpectError(t, nil, `bytes.fromHex("EEEE")`) // 2 bytes, not 1
	evalByteExpectError(t, nil, `bytes.bufFromHex("not hex")`)
}

// TestByteBuiltinArgumentCountErrors is a broad sweep of each remaining
// function's own arity guard, in one table rather than one test function
// each.
func TestByteBuiltinArgumentCountErrors(t *testing.T) {
	cases := []string{
		`bytes.byte()`,
		`bytes.fromHex()`,
		`bytes.bufFromHex()`,
		`bytes.toHex()`,
		`bytes.bufToHex()`,
		`bytes.toBase64()`,
		`bytes.fromBase64()`,
		`bytes.bufToString()`,
		`bytes.bufToString(bytes.fromHex("00"), "utf-8", "extra")`,
		`bytes.bufFromString()`,
		`bytes.bufFromString("hi", "utf-8", "extra")`,
		`bytes.bitAnd(1)`,
		`bytes.bitOr(1)`,
		`bytes.bitXor(1)`,
		`bytes.shiftLeft(1)`,
		`bytes.shiftRight(1)`,
		`bytes.bitTest(1)`,
		`bytes.bitSet(1)`,
		`bytes.bitClear(1)`,
		`bytes.popCount()`,
		`bytes.reverseBuf()`,
		`bytes.padBufStart(bytes.fromHex("00"))`,
		`bytes.padBufEnd(bytes.fromHex("00"))`,
		`bytes.truncateBuf(bytes.fromHex("00"))`,
		`bytes.readUint16BE(bytes.fromHex("00"))`,
		`bytes.writeUint16BE(bytes.fromHex("00"), 0)`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			evalByteExpectError(t, nil, input)
		})
	}
}

// TestBytesByteConvertsFromOtherNumericTypes covers bytes.byte()'s use of
// CoercValue/OpCoerce (byteOperations' "converting from something else to
// byte" branch) - regression coverage for a bug where that branch returned
// the raw int64 accumulator instead of byte(aVal), so bytes.byte(int64Val)
// produced an int64, not a byte, silently defeating the whole point of the
// conversion.
func TestBytesByteConvertsFromOtherNumericTypes(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  byte
	}{
		{"from int literal", `bytes.byte(200)`, 200},
		{"from int64() conversion", `bytes.byte(int64(60))`, 60},
		{"from int() conversion", `bytes.byte(int(61))`, 61},
		// byte() truncates to the low 8 bits like byteOperations' own
		// arithmetic wraparound (see its doc comment), not an error for an
		// out-of-range input.
		{"truncates like arithmetic wraparound", `bytes.byte(256)`, 0},
		{"truncates like arithmetic wraparound (257)", `bytes.byte(257)`, 1},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := evalByte(t, nil, tt.input)
			b, ok := got.(byte)
			if !ok {
				t.Fatalf("%s = %v (%T), want a byte", tt.input, got, got)
			}
			if b != tt.want {
				t.Errorf("%s = %v, want %v", tt.input, b, tt.want)
			}
		})
	}
}

// TestBytesByteIdentity covers bytes.byte() applied to a value that's
// already a byte - the aType == bType fast path in operationDispatcher.
func TestBytesByteIdentity(t *testing.T) {
	env := map[string]any{"buf": []byte{0x2A}}
	got := evalByte(t, env, `bytes.byte(buf[0])`)
	if got != byte(0x2A) {
		t.Errorf("bytes.byte(buf[0]) = %v, want 0x2A", got)
	}
}

// TestByteToIntConversions covers the reverse direction - converting a
// byte value to int/int64 via the root package's int()/int64() builtins,
// which also go through byteOperations' OpCoerce case (the "converting
// from byte to something else" branch, already correctly typed before
// this change).
func TestByteToIntConversions(t *testing.T) {
	env := map[string]any{"buf": []byte{0x10, 0x20}}
	if got := evalByte(t, env, `int64(buf[0])`); got != int64(0x10) {
		t.Errorf("int64(buf[0]) = %v (%T), want int64(16)", got, got)
	}
	if got := evalByte(t, env, `int(buf[1])`); got != int(0x20) {
		t.Errorf("int(buf[1]) = %v (%T), want int(32)", got, got)
	}
}
