package vm

import (
	"strings"
	"testing"
)

// TestCoercValueCoreTypes covers CoercValue's dispatch through
// intOperations/floatOperations' OpCoerce cases for every pairing of the
// three core numeric types, including the identity conversions (same
// TypeCode on both sides), which operationDispatcher's aType == bType
// branch short-circuits before ever reaching a handler's OpCoerce case.
func TestCoercValueCoreTypes(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	cases := []struct {
		name   string
		value  any
		target any
		want   any
	}{
		{"int64 -> int", int64(5), int(0), int(5)},
		{"int -> int64", int(5), int64(0), int64(5)},
		{"float64 -> int64 truncates", float64(4.9), int64(0), int64(4)},
		{"float64 -> int truncates", float64(4.9), int(0), int(4)},
		{"int64 -> float64", int64(5), float64(0), float64(5)},
		{"int -> float64", int(5), float64(0), float64(5)},
		{"negative float64 -> int64 truncates toward zero", float64(-4.9), int64(0), int64(-4)},
		{"int64 -> int64 identity", int64(5), int64(0), int64(5)},
		{"int -> int identity", int(5), int(0), int(5)},
		{"float64 -> float64 identity", float64(5.5), float64(0), float64(5.5)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mc.CoercValue(tt.value, tt.target)
			if err != nil {
				t.Fatalf("CoercValue(%v, %T(0)): %v", tt.value, tt.target, err)
			}
			if got != tt.want {
				t.Errorf("CoercValue(%v, %T(0)) = %v (%T), want %v (%T)", tt.value, tt.target, got, got, tt.want, tt.want)
			}
		})
	}
}

// TestCoercValueUnsupportedPairErrors covers operationDispatcher's final
// "not supported" error for a pairing neither side's handler declares as
// coercible - bool has no coercible types at all, so converting to/from it
// must be a clean error, not a panic or a silently wrong value.
func TestCoercValueUnsupportedPairErrors(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.CoercValue(true, int64(0)); err == nil {
		t.Error("CoercValue(true, int64(0)) should error - bool has no registered coercion")
	}
	if _, err := mc.CoercValue(int64(1), true); err == nil {
		t.Error("CoercValue(int64(1), true) should error - bool has no registered coercion")
	}
}

// TestCoercValueStringToNumber covers stringOperations' OpCoerce case:
// parsing a string into int/int64/float64 via CoercValue - the direction
// that's the reverse of stringOperations' existing OpAdd case (which
// stringifies a number to append it to a string).
func TestCoercValueStringToNumber(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	cases := []struct {
		name   string
		value  string
		target any
		want   any
	}{
		{"string -> int", "5", int(0), int(5)},
		{"string -> int64", "42", int64(0), int64(42)},
		{"string -> float64", "3.14", float64(0), float64(3.14)},
		{"negative string -> int64", "-7", int64(0), int64(-7)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mc.CoercValue(tt.value, tt.target)
			if err != nil {
				t.Fatalf("CoercValue(%q, %T(0)): %v", tt.value, tt.target, err)
			}
			if got != tt.want {
				t.Errorf("CoercValue(%q, %T(0)) = %v (%T), want %v (%T)", tt.value, tt.target, got, got, tt.want, tt.want)
			}
		})
	}
}

// TestCoercValueStringToNumberParseErrors covers stringOperations'
// strconv error-propagation branches for a non-numeric string, for each of
// the three numeric target types.
func TestCoercValueStringToNumberParseErrors(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	targets := []any{int(0), int64(0), float64(0)}
	for _, target := range targets {
		t.Run("", func(t *testing.T) {
			_, err := mc.CoercValue("not a number", target)
			if err == nil {
				t.Fatalf("CoercValue(\"not a number\", %T(0)) should error", target)
			}
			if !strings.Contains(err.Error(), "not a number") {
				t.Errorf("CoercValue error = %q, want it to mention the offending input", err.Error())
			}
		})
	}
}

// TestCoercValueStringIdentity covers converting a string to a string -
// the aType == bType short-circuit, since stringOperations' OpCoerce case
// has no StringTypeCode branch of its own.
func TestCoercValueStringIdentity(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	got, err := mc.CoercValue("hello", "")
	if err != nil {
		t.Fatalf("CoercValue(\"hello\", \"\"): %v", err)
	}
	if got != "hello" {
		t.Errorf(`CoercValue("hello", "") = %v, want "hello"`, got)
	}
}

// TestCoercValueBoolIdentity covers the aType == bType short-circuit for a
// type with no coercible types at all: converting bool to bool must still
// return the original value unchanged rather than going through
// boolOperations (which has no OpCoerce case and would otherwise error).
func TestCoercValueBoolIdentity(t *testing.T) {
	mc, err := UnconfiguredVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	got, err := mc.CoercValue(true, false)
	if err != nil {
		t.Fatalf("CoercValue(true, false): %v", err)
	}
	if got != true {
		t.Errorf("CoercValue(true, false) = %v, want true", got)
	}
}
