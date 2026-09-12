package stdlib

import (
	"errors"
	"fmt"
	"time"

	"github.com/cms103/owlexpr/vm"

	"github.com/shopspring/decimal"
)

// TimeBuiltins registers time.Time/time.Duration support: operators
// (time - time -> duration, time +/- duration -> time, duration +/-
// duration -> duration, comparisons on both) plus a handful of
// constructor/helper functions modeled on expr-lang's own date/time
// support (now, duration, date, timezone)
//
// time.Time and time.Duration are ordinary Go
// types with no literal syntax in this language, so they only ever arrive
// via env values or these constructors - exactly the case the dynamic-
// TypeCode system (RegisterOperation/RegisterTypeCoder) exists to handle:
// a type the core VM has never heard of, given arithmetic/comparison
// support entirely from outside the vm package.
//
// Deliberately NOT supported: raw int/float/decimal arithmetic against a
// Time (ambiguous units - is 5 five seconds or five days?) or implicit
// string-to-duration coercion inside +.
func TimeBuiltins() vm.VMOption {
	return func(mc *vm.Machine) error {
		// Register our stdLibraryTypeCoder
		reg := vm.RegisterTypeCoder(stdLibraryTypeCoder, stdLibraryTypeCoderId)
		err := reg(mc)
		if err != nil {
			return err
		}

		// Machine.registerOperationalHandler is unexported - from outside
		// the vm package, the only way to register an operation is to build
		// the VMOption via RegisterOperation and invoke it immediately, same
		// as the RegisterTypeCoder call above.
		if err := vm.RegisterOperation(time.Time{}, []any{time.Duration(0)}, timeDurationOperations)(mc); err != nil {
			return err
		}
		if err := vm.RegisterOperation(time.Duration(0), []any{}, timeDurationOperations)(mc); err != nil {
			return err
		}
		// time.Time.Month()/.Weekday() return named int-based types
		// (time.Month, time.Weekday), not plain int/int64 - GetTypeCode's
		// type switch doesn't match a named type against `case int:`, so
		// without this they'd be unrecognized (UnSupportedType) and
		// `t.Month() == 6` would fail with a type-dispatch error despite
		// looking like ordinary integer comparison. Year()/Day()/Hour()/
		// Minute()/Second() all return plain int and already work via the
		// existing default int<->int64 coercion - no registration needed.
		if err := vm.RegisterOperation(time.Month(0), []any{int(0), int64(0)}, namedCalendarIntOperations)(mc); err != nil {
			return err
		}
		if err := vm.RegisterOperation(time.Weekday(0), []any{int(0), int64(0)}, namedCalendarIntOperations)(mc); err != nil {
			return err
		}

		for name, fn := range namespacedTimeFuncs {
			mc.RegisterNamespacedBuiltin("time", name, fn)
		}
		return nil
	}
}

// namespacedTimeFuncs is the full TimeBuiltins roster keyed by its
// `time.<name>` member name - see StringBuiltins' doc comment for why
// every function here is namespace-only (time.now(), time.duration(),
// ...), not also registered bare.
var namespacedTimeFuncs = map[string]vm.BuiltinFunc{
	"now":          nowFunc,
	"duration":     durationFunc,
	"date":         dateFunc,
	"timezone":     timezoneFunc,
	"hours":        hoursFunc,
	"minutes":      minutesFunc,
	"seconds":      secondsFunc,
	"milliseconds": millisecondsFunc,
	"days":         daysFunc,
}

// timeDurationOperations is the single handler backing both the
// time.Time and time.Duration registrations in TimeBuiltins - Combine may
// invoke it with either operand order (whichever side's base type it
// dispatches through), so it classifies both a and b independently rather
// than assuming which one is the Time.
func timeDurationOperations(a, b any, aTypeCode, bTypeCode vm.TypeCode, op vm.OpCode) (any, error) {
	aTime, aIsTime := a.(time.Time)
	bTime, bIsTime := b.(time.Time)

	switch {
	case aIsTime && bIsTime:
		switch op {
		case vm.OpSub:
			return aTime.Sub(bTime), nil
		case vm.OpEqual:
			return aTime.Equal(bTime), nil
		case vm.OpNotEqual:
			return !aTime.Equal(bTime), nil
		case vm.OpLess:
			return aTime.Before(bTime), nil
		case vm.OpGreater:
			return aTime.After(bTime), nil
		case vm.OpLessEq:
			return !aTime.After(bTime), nil
		case vm.OpGreaterEq:
			return !aTime.Before(bTime), nil
		}
		return nil, errors.ErrUnsupported

	case aIsTime && !bIsTime:
		bDur := b.(time.Duration)
		switch op {
		case vm.OpAdd:
			return aTime.Add(bDur), nil
		case vm.OpSub:
			return aTime.Add(-bDur), nil
		}
		return nil, errors.ErrUnsupported

	case !aIsTime && bIsTime:
		aDur := a.(time.Duration)
		if op == vm.OpAdd {
			return bTime.Add(aDur), nil
		}
		return nil, errors.ErrUnsupported

	default:
		aDur, bDur := a.(time.Duration), b.(time.Duration)
		switch op {
		case vm.OpAdd:
			return aDur + bDur, nil
		case vm.OpSub:
			return aDur - bDur, nil
		case vm.OpEqual:
			return aDur == bDur, nil
		case vm.OpNotEqual:
			return aDur != bDur, nil
		case vm.OpLess:
			return aDur < bDur, nil
		case vm.OpGreater:
			return aDur > bDur, nil
		case vm.OpLessEq:
			return aDur <= bDur, nil
		case vm.OpGreaterEq:
			return aDur >= bDur, nil
		}
		return nil, errors.ErrUnsupported
	}
}

// namedCalendarIntOperations backs both time.Month and time.Weekday:
// structurally identical named-int-enum types, so one handler covers
// both. Deliberately comparisons only (==, !=, <, >, <=, >=) - Month+Month
// or Weekday*2 have no sensible meaning, unlike a genuine numeric type.
func namedCalendarIntOperations(a, b any, aTypeCode, bTypeCode vm.TypeCode, op vm.OpCode) (any, error) {
	aVal, err := calendarIntValue(a, aTypeCode)
	if err != nil {
		return nil, err
	}
	bVal, err := calendarIntValue(b, bTypeCode)
	if err != nil {
		return nil, err
	}
	switch op {
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
	}
	return nil, errors.ErrUnsupported
}

func calendarIntValue(v any, tc vm.TypeCode) (int64, error) {
	switch tc {
	case vm.IntTypeCode:
		return int64(v.(int)), nil
	case vm.Int64TypeCode:
		return v.(int64), nil
	}
	switch n := v.(type) {
	case time.Month:
		return int64(n), nil
	case time.Weekday:
		return int64(n), nil
	}
	return 0, fmt.Errorf("unsupported operand type %T for a Month/Weekday comparison", v)
}

func nowFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("now() expects no arguments, got %d", len(args))
	}
	return time.Now(), nil
}

// durationFunc parses Go's own duration syntax ("1h2m3s", "300ms",
// "-1.5h") - the same grammar the hours/minutes/seconds/days helpers
// below produce values in, so there's one mental model for a duration
// regardless of whether it came from a literal string or arithmetic.
func durationFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("duration() expects 1 argument, got %d", len(args))
	}
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("duration(): argument must be a string, got %T", args[0])
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, fmt.Errorf("duration(): %w", err)
	}
	return d, nil
}

// dateFunc parses a date string into a time.Time. Deliberately explicit
// rather than expr's multi-format auto-detection (trying several
// candidate layouts in turn): guessing which of several formats a string
// matches is exactly the kind of implicit magic this pack otherwise
// avoids (see TimeBuiltins' doc comment), and a wrong guess for an
// unusual-but-valid input would be a silent correctness bug rather than a
// clean error. With no layout given it parses RFC3339
// ("2006-01-02T15:04:05Z07:00"), the unambiguous default for a
// machine-produced timestamp; pass a Go time-package layout string
// explicitly for anything else.
func dateFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 1 || len(args) > 3 {
		return nil, fmt.Errorf("date() expects 1 to 3 arguments, got %d", len(args))
	}
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("date(): argument 1 must be a string, got %T", args[0])
	}
	layout := time.RFC3339
	if len(args) >= 2 {
		layout, ok = args[1].(string)
		if !ok {
			return nil, fmt.Errorf("date(): layout argument must be a string, got %T", args[1])
		}
	}
	if len(args) == 3 {
		tzName, ok := args[2].(string)
		if !ok {
			return nil, fmt.Errorf("date(): timezone argument must be a string, got %T", args[2])
		}
		loc, err := time.LoadLocation(tzName)
		if err != nil {
			return nil, fmt.Errorf("date(): %w", err)
		}
		t, err := time.ParseInLocation(layout, s, loc)
		if err != nil {
			return nil, fmt.Errorf("date(): %w", err)
		}
		return t, nil
	}
	t, err := time.Parse(layout, s)
	if err != nil {
		return nil, fmt.Errorf("date(): %w", err)
	}
	return t, nil
}

// timezoneFunc converts a Time to another IANA time zone. time.Time
// itself has a .In(*time.Location) method that would otherwise be
// reachable through the language's ordinary method-call syntax, but a
// owlexpr expression has no way to produce a *time.Location value on its
// own - this is the constructor that closes that gap, taking the zone
// name as a plain string instead.
func timezoneFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("timezone() expects 2 arguments, got %d", len(args))
	}
	t, ok := args[0].(time.Time)
	if !ok {
		return nil, fmt.Errorf("timezone(): argument 1 must be a time value, got %T", args[0])
	}
	tzName, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("timezone(): argument 2 must be a string, got %T", args[1])
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return nil, fmt.Errorf("timezone(): %w", err)
	}
	return t.In(loc), nil
}

// durationScale is shared by hours/minutes/seconds/milliseconds/days: n
// can be any of owlexpr's numeric types (int64 from a literal, float64
// or decimal.Decimal from arithmetic), scaled against a fixed
// time.Duration unit. days() is 24 fixed hours, not calendar-aware (no
// DST adjustment) - a deliberate, documented simplification, the same
// kind of explicit tradeoff as this package's rune-based string indexing.
func durationScale(fn string, n any, unit time.Duration) (any, error) {
	var scale float64
	switch v := n.(type) {
	case int64:
		scale = float64(v)
	case int:
		scale = float64(v)
	case float64:
		scale = v
	case decimal.Decimal:
		f, _ := v.Float64()
		scale = f
	default:
		return nil, fmt.Errorf("%s(): argument must be a number, got %T", fn, n)
	}
	return time.Duration(scale * float64(unit)), nil
}

func hoursFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("hours() expects 1 argument, got %d", len(args))
	}
	return durationScale("hours", args[0], time.Hour)
}

func minutesFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("minutes() expects 1 argument, got %d", len(args))
	}
	return durationScale("minutes", args[0], time.Minute)
}

func secondsFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("seconds() expects 1 argument, got %d", len(args))
	}
	return durationScale("seconds", args[0], time.Second)
}

func millisecondsFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("milliseconds() expects 1 argument, got %d", len(args))
	}
	return durationScale("milliseconds", args[0], time.Millisecond)
}

func daysFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("days() expects 1 argument, got %d", len(args))
	}
	return durationScale("days", args[0], 24*time.Hour)
}
