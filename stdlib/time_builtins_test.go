package stdlib

import (
	"testing"
	"time"

	"github.com/cms103/owlexpr"

	"github.com/shopspring/decimal"
)

// evalTime parses, compiles, and runs input with TimeBuiltins() enabled,
// failing the test on any parse/compile/run error.
func evalTime(t *testing.T, env map[string]any, input string) any {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(TimeBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	return res
}

func evalTimeExpectError(t *testing.T, env map[string]any, input string) {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(TimeBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatalf("expected an error for %q, got none", input)
	}
}

func TestTimeBuiltinsNotRegisteredByDefault(t *testing.T) {
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	instructions, err := owlexpr.Compile(`time.now()`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Fatalf("expected time.now() to be undefined without TimeBuiltins()")
	}
}

func TestTimeSubtractionYieldsDuration(t *testing.T) {
	env := map[string]any{
		"start": time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		"end":   time.Date(2024, 1, 1, 12, 30, 0, 0, time.UTC),
	}
	got := evalTime(t, env, `end - start`).(time.Duration)
	if got != 2*time.Hour+30*time.Minute {
		t.Errorf("end - start = %v, want 2h30m", got)
	}
}

func TestTimePlusDuration(t *testing.T) {
	env := map[string]any{
		"start": time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		"gap":   90 * time.Minute,
	}
	got := evalTime(t, env, `start + gap`).(time.Time)
	want := time.Date(2024, 1, 1, 11, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("start + gap = %v, want %v", got, want)
	}
	// Commutative: duration + time should equal time + duration.
	got2 := evalTime(t, env, `gap + start`).(time.Time)
	if !got2.Equal(want) {
		t.Errorf("gap + start = %v, want %v", got2, want)
	}
}

func TestTimeMinusDuration(t *testing.T) {
	env := map[string]any{
		"start": time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		"gap":   30 * time.Minute,
	}
	got := evalTime(t, env, `start - gap`).(time.Time)
	want := time.Date(2024, 1, 1, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("start - gap = %v, want %v", got, want)
	}
}

func TestDurationArithmetic(t *testing.T) {
	env := map[string]any{"a": 30 * time.Minute, "b": 15 * time.Minute}
	if got := evalTime(t, env, `a + b`).(time.Duration); got != 45*time.Minute {
		t.Errorf("a + b = %v, want 45m", got)
	}
	if got := evalTime(t, env, `a - b`).(time.Duration); got != 15*time.Minute {
		t.Errorf("a - b = %v, want 15m", got)
	}
}

func TestTimeComparisons(t *testing.T) {
	env := map[string]any{
		"earlier": time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		"later":   time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC),
		"same":    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
	}
	cases := []struct {
		input string
		want  bool
	}{
		{`earlier < later`, true},
		{`later < earlier`, false},
		{`earlier <= same`, true},
		{`earlier == same`, true},
		{`earlier != later`, true},
		{`later > earlier`, true},
		{`later >= same`, true},
	}
	for _, tt := range cases {
		if got := evalTime(t, env, tt.input); got != tt.want {
			t.Errorf("%s = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDurationComparisons(t *testing.T) {
	env := map[string]any{"short": 10 * time.Minute, "long": 20 * time.Minute}
	if got := evalTime(t, env, `short < long`); got != true {
		t.Errorf("short < long = %v, want true", got)
	}
	if got := evalTime(t, env, `short == short`); got != true {
		t.Errorf("short == short = %v, want true", got)
	}
}

func TestNoRawNumberArithmeticAgainstTime(t *testing.T) {
	env := map[string]any{"t": time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
	evalTimeExpectError(t, env, `t + 5`)
	evalTimeExpectError(t, env, `t + "3h2m1s"`)
}

func TestNowReturnsRecentTime(t *testing.T) {
	before := time.Now()
	got := evalTime(t, nil, `time.now()`).(time.Time)
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Errorf("time.now() = %v, want between %v and %v", got, before, after)
	}
}

func TestDurationFunc(t *testing.T) {
	got := evalTime(t, nil, `time.duration("1h30m")`).(time.Duration)
	if got != 90*time.Minute {
		t.Errorf(`time.duration("1h30m") = %v, want 90m`, got)
	}
	evalTimeExpectError(t, nil, `time.duration("not a duration")`)
}

func TestHoursMinutesSecondsDaysHelpers(t *testing.T) {
	cases := []struct {
		input string
		want  time.Duration
	}{
		{`time.hours(2)`, 2 * time.Hour},
		{`time.minutes(90)`, 90 * time.Minute},
		{`time.seconds(45)`, 45 * time.Second},
		{`time.milliseconds(500)`, 500 * time.Millisecond},
		{`time.days(1)`, 24 * time.Hour},
		{`time.hours(1.5)`, 90 * time.Minute},
	}
	for _, tt := range cases {
		if got := evalTime(t, nil, tt.input); got != tt.want {
			t.Errorf("%s = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// TestWrapperFunctionsComposeWithTimeArithmetic is the exact idiom this
// pack was designed to make natural: start + time.hours(10).
func TestWrapperFunctionsComposeWithTimeArithmetic(t *testing.T) {
	env := map[string]any{"start": time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC)}
	got := evalTime(t, env, `start + time.hours(10)`).(time.Time)
	want := time.Date(2024, 1, 1, 18, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("start + time.hours(10) = %v, want %v", got, want)
	}
}

func TestDateFunc(t *testing.T) {
	got := evalTime(t, nil, `time.date("2024-06-15T10:30:00Z")`).(time.Time)
	want := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf(`time.date("2024-06-15T10:30:00Z") = %v, want %v`, got, want)
	}

	gotLayout := evalTime(t, nil, `time.date("2024-06-15", "2006-01-02")`).(time.Time)
	wantLayout := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	if !gotLayout.Equal(wantLayout) {
		t.Errorf(`date with layout = %v, want %v`, gotLayout, wantLayout)
	}

	evalTimeExpectError(t, nil, `time.date("not a date")`)
}

// TestDateFuncWithTimezone covers dateFunc's 3-argument form
// (layout + timezone), untested by TestDateFunc, plus its own bad-timezone
// error branch (as opposed to timezoneFunc's, which is a separate code
// path).
func TestDateFuncWithTimezone(t *testing.T) {
	got := evalTime(t, nil, `time.date("2024-06-15 10:30:00", "2006-01-02 15:04:05", "America/New_York")`).(time.Time)
	if got.Location().String() != "America/New_York" {
		t.Errorf("time.date() with timezone: location = %v, want America/New_York", got.Location())
	}
	if got.Hour() != 10 {
		t.Errorf("time.date() with timezone: hour = %d, want 10 (wall clock, not converted)", got.Hour())
	}

	evalTimeExpectError(t, nil, `time.date("2024-06-15 10:30:00", "2006-01-02 15:04:05", "Not/AZone")`)
}

// TestDateFuncArgumentErrors covers dateFunc's own arity guard and each
// positional argument's type guard (string, layout, timezone).
func TestDateFuncArgumentErrors(t *testing.T) {
	cases := []string{
		`time.date()`,
		`time.date("a", "b", "c", "d")`,
		`time.date(42)`,
		`time.date("2024-06-15", 42)`,
		`time.date("2024-06-15", "2006-01-02", 42)`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			evalTimeExpectError(t, nil, input)
		})
	}
}

// TestDurationFuncErrors covers durationFunc's arity guard, argument-type
// guard, and time.ParseDuration failure - TestDurationFunc (elsewhere in
// this file) only reaches the success path.
func TestDurationFuncErrors(t *testing.T) {
	evalTimeExpectError(t, nil, `time.duration()`)
	evalTimeExpectError(t, nil, `time.duration(42)`)
	evalTimeExpectError(t, nil, `time.duration("not a duration")`)
}

// TestNowFuncArgumentError covers nowFunc's own arity guard.
func TestNowFuncArgumentError(t *testing.T) {
	evalTimeExpectError(t, nil, `time.now(1)`)
}

// TestTimezoneFuncArgumentErrors covers timezoneFunc's arity guard and
// each positional argument's type guard.
func TestTimezoneFuncArgumentErrors(t *testing.T) {
	env := map[string]any{"t": time.Now()}
	evalTimeExpectError(t, env, `time.timezone(t)`)
	evalTimeExpectError(t, nil, `time.timezone("not a time", "UTC")`)
	evalTimeExpectError(t, env, `time.timezone(t, 42)`)
}

// TestDurationScaleWithDecimal covers durationScale's decimal.Decimal
// branch - hours/minutes/seconds/milliseconds/days accept the result of
// decimal arithmetic (DecimalBuiltins), not just a plain number literal.
func TestDurationScaleWithDecimal(t *testing.T) {
	mc, err := owlexpr.NewVM(TimeBuiltins(), DecimalBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	env := map[string]any{"d": decimal.NewFromFloat(2.5)}
	instructions, err := owlexpr.Compile(`time.hours(d)`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error: %v", err)
	}
	want := 2*time.Hour + 30*time.Minute
	if got != want {
		t.Errorf("time.hours(decimal 2.5) = %v, want %v", got, want)
	}
}

// TestDurationScaleWithBareInt covers durationScale's `case int:` branch,
// distinct from `case int64:` - unreachable from an expression literal
// (which always arrives as int64), only from an env value of Go's plain
// int type.
func TestDurationScaleWithBareInt(t *testing.T) {
	env := map[string]any{"n": int(2)}
	if got := evalTime(t, env, `time.hours(n)`); got != 2*time.Hour {
		t.Errorf("time.hours(int(2)) = %v, want 2h", got)
	}
}

// TestDurationScaleUnsupportedTypeErrors covers durationScale's final
// unsupported-type error, shared by all five of its callers.
func TestDurationScaleUnsupportedTypeErrors(t *testing.T) {
	evalTimeExpectError(t, nil, `time.hours("not a number")`)
	evalTimeExpectError(t, nil, `time.minutes("not a number")`)
	evalTimeExpectError(t, nil, `time.seconds("not a number")`)
	evalTimeExpectError(t, nil, `time.milliseconds("not a number")`)
	evalTimeExpectError(t, nil, `time.days("not a number")`)
}

// TestDurationHelperArgumentCountErrors covers each helper's own arity
// guard.
func TestDurationHelperArgumentCountErrors(t *testing.T) {
	evalTimeExpectError(t, nil, `time.hours()`)
	evalTimeExpectError(t, nil, `time.minutes(1, 2)`)
	evalTimeExpectError(t, nil, `time.seconds()`)
	evalTimeExpectError(t, nil, `time.milliseconds()`)
	evalTimeExpectError(t, nil, `time.days()`)
}

// TestCalendarValueComparison covers calendarIntValue's own time.Month/
// time.Weekday branch on BOTH sides at once (comparing two Month values
// directly), as opposed to TestMonthWeekdayComparisons, which only ever
// compares a Month/Weekday against a plain int64 literal.
func TestCalendarValueComparison(t *testing.T) {
	env := map[string]any{
		"june": time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		"july": time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
	}
	if got := evalTime(t, env, `june.Month() < july.Month()`); got != true {
		t.Errorf("june.Month() < july.Month() = %v, want true", got)
	}
	if got := evalTime(t, env, `june.Month() == june.Month()`); got != true {
		t.Errorf("june.Month() == june.Month() = %v, want true", got)
	}
	if got := evalTime(t, env, `june.Month() != july.Month()`); got != true {
		t.Errorf("june.Month() != july.Month() = %v, want true", got)
	}
	if got := evalTime(t, env, `july.Month() > june.Month()`); got != true {
		t.Errorf("july.Month() > june.Month() = %v, want true", got)
	}
	if got := evalTime(t, env, `june.Month() <= july.Month()`); got != true {
		t.Errorf("june.Month() <= july.Month() = %v, want true", got)
	}
	if got := evalTime(t, env, `july.Month() >= june.Month()`); got != true {
		t.Errorf("july.Month() >= june.Month() = %v, want true", got)
	}
}

// TestCalendarIntValueUnsupportedTypeErrors covers calendarIntValue's
// final error branch: a Month/Weekday compared against a type that's
// neither int/int64 nor another Month/Weekday.
func TestCalendarIntValueUnsupportedTypeErrors(t *testing.T) {
	env := map[string]any{"t": time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)}
	evalTimeExpectError(t, env, `t.Month() == "June"`)
}

// TestTimeDurationOperationsUnsupportedOps covers timeDurationOperations'
// per-branch unsupported-op fallthrough for all four operand-order
// combinations (time/time, time/duration, duration/time, duration/
// duration) - existing tests only ever exercise the operations each
// combination DOES support.
func TestTimeDurationOperationsUnsupportedOps(t *testing.T) {
	env := map[string]any{
		"t1": time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		"t2": time.Date(2024, 6, 2, 0, 0, 0, 0, time.UTC),
		"d1": time.Hour,
		"d2": 2 * time.Hour,
	}
	evalTimeExpectError(t, env, `t1 + t2`)  // time + time is unsupported
	evalTimeExpectError(t, env, `t1 * d1`)  // time * duration is unsupported
	evalTimeExpectError(t, env, `d1 == t1`) // duration/time comparison is unsupported
	evalTimeExpectError(t, env, `d1 * d2`)  // duration * duration is unsupported
}

func TestTimezoneFunc(t *testing.T) {
	env := map[string]any{"t": time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)}
	got := evalTime(t, env, `time.timezone(t, "America/New_York")`).(time.Time)
	if got.Location().String() != "America/New_York" {
		t.Errorf("time.timezone() location = %v, want America/New_York", got.Location())
	}
	if !got.Equal(env["t"].(time.Time)) {
		t.Errorf("time.timezone() should preserve the instant, got %v vs %v", got, env["t"])
	}
	evalTimeExpectError(t, env, `time.timezone(t, "Not/AZone")`)
}

// TestMonthWeekdayComparisons is the Month()/Weekday() gap this pack
// closes: both return named int-based types (time.Month, time.Weekday),
// not plain int/int64, so without registering them explicitly this
// comparison would fail with a type-dispatch error despite looking like
// ordinary integer comparison.
func TestMonthWeekdayComparisons(t *testing.T) {
	env := map[string]any{"t": time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)} // June 15 2024 is a Saturday
	if got := evalTime(t, env, `t.Month() == 6`); got != true {
		t.Errorf("t.Month() == 6 = %v, want true", got)
	}
	if got := evalTime(t, env, `t.Month() < 12`); got != true {
		t.Errorf("t.Month() < 12 = %v, want true", got)
	}
	if got := evalTime(t, env, `t.Weekday() == 6`); got != true { // time.Saturday == 6
		t.Errorf("t.Weekday() == 6 = %v, want true", got)
	}
	// Year/Day/Hour/Minute/Second are plain int and should already work
	// via the existing default int<->int64 coercion, no new registration.
	if got := evalTime(t, env, `t.Year() == 2024`); got != true {
		t.Errorf("t.Year() == 2024 = %v, want true", got)
	}
	if got := evalTime(t, env, `t.Day() == 15`); got != true {
		t.Errorf("t.Day() == 15 = %v, want true", got)
	}
	// str() on a Month/Weekday should call its own Stringer method - it's
	// a core builtin, so no extra VMOption beyond evalTime's own
	// TimeBuiltins() is needed.
	if got := evalTime(t, env, `str(t.Month())`); got != "June" {
		t.Errorf(`str(t.Month()) = %v, want "June"`, got)
	}
}
