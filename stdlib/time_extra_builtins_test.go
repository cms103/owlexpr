package stdlib

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cms103/owlexpr"
)

// evalTimeErr is evalTimeExpectError that also returns the error, so its
// message can be checked.
func evalTimeErr(t *testing.T, env map[string]any, input string) error {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("compile error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(TimeBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	_, err = mc.Run(instructions, env)
	if err == nil {
		t.Fatalf("expected an error for %q, got none", input)
	}
	return err
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("time.Parse(%q): %v", s, err)
	}
	return tm
}

func TestTimeWindows(t *testing.T) {
	cases := []struct {
		name  string
		ts    string
		input string
		want  []string
	}{
		{"sliding 10m by 5m", "2024-06-15T10:07:30Z", "time.windows(ts, time.minutes(10), time.minutes(5))",
			[]string{"2024-06-15T10:00:00Z", "2024-06-15T10:05:00Z"}},
		{"on a boundary", "2024-06-15T10:05:00Z", "time.windows(ts, time.minutes(10), time.minutes(5))",
			[]string{"2024-06-15T10:00:00Z", "2024-06-15T10:05:00Z"}},
		{"tumbling default slide", "2024-06-15T10:07:30Z", "time.windows(ts, time.minutes(10))",
			[]string{"2024-06-15T10:00:00Z"}},
		// Starts are multiples of 4m from the epoch: 09:56 ends at 10:06,
		// before ts, so only 10:00 and 10:04 contain it.
		{"slide not dividing size", "2024-06-15T10:07:30Z", "time.windows(ts, time.minutes(10), time.minutes(4))",
			[]string{"2024-06-15T10:00:00Z", "2024-06-15T10:04:00Z"}},
		{"slide not dividing size, three windows", "2024-06-15T10:09:00Z", "time.windows(ts, time.minutes(10), time.minutes(4))",
			[]string{"2024-06-15T10:00:00Z", "2024-06-15T10:04:00Z", "2024-06-15T10:08:00Z"}},
		// 1970-01-01 was a Thursday; epoch alignment (like Spark), not
		// Go's year-1 Truncate alignment, which would start on a Monday.
		{"7-day windows align to the Unix epoch", "2024-06-15T10:00:00Z", "time.windows(ts, time.days(7))",
			[]string{"2024-06-13T00:00:00Z"}},
		{"before the epoch", "1969-12-31T23:59:30Z", "time.windows(ts, time.minutes(1))",
			[]string{"1969-12-31T23:59:00Z"}},
		// offset moves the boundaries: Thursday + 4 days = Monday weeks.
		{"offset to Monday weeks", "2024-06-15T10:00:00Z", "time.windows(ts, time.days(7), time.days(7), time.days(4))",
			[]string{"2024-06-10T00:00:00Z"}},
		{"negative offset same as its remainder", "2024-06-15T10:00:00Z", "time.windows(ts, time.days(7), time.days(7), time.days(-3))",
			[]string{"2024-06-10T00:00:00Z"}},
		{"offset larger than slide", "2024-06-15T10:00:00Z", "time.windows(ts, time.days(7), time.days(7), time.days(11))",
			[]string{"2024-06-10T00:00:00Z"}},
		// Days starting at local midnight in UTC-05:00.
		{"offset to a local midnight", "2024-06-15T03:00:00Z", "time.windows(ts, time.days(1), time.days(1), time.hours(5))",
			[]string{"2024-06-14T05:00:00Z"}},
		{"offset with sliding windows", "2024-06-15T10:07:30Z", "time.windows(ts, time.minutes(10), time.minutes(5), time.minutes(2))",
			[]string{"2024-06-15T10:02:00Z", "2024-06-15T10:07:00Z"}},
		{"zero offset same as none", "2024-06-15T10:07:30Z", "time.windows(ts, time.minutes(10), time.minutes(5), time.minutes(0))",
			[]string{"2024-06-15T10:00:00Z", "2024-06-15T10:05:00Z"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := evalTime(t, map[string]any{"ts": mustTime(t, tt.ts)}, tt.input)
			var want []any
			for _, w := range tt.want {
				want = append(want, mustTime(t, w))
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s = %v, want %v", tt.input, got, want)
			}
		})
	}
}

// TestTimeWindowsKeepsZone: starts are reported in t's own time zone.
func TestTimeWindowsKeepsZone(t *testing.T) {
	loc := time.FixedZone("X", 5*3600+30*60)
	ts := time.Date(2024, 6, 15, 10, 7, 0, 0, loc)
	got := evalTime(t, map[string]any{"ts": ts}, "time.windows(ts, time.hours(1))").([]any)
	start := got[0].(time.Time)
	if start.Location() != loc || !start.Equal(time.Date(2024, 6, 15, 9, 30, 0, 0, loc)) {
		t.Errorf("start = %v, want 09:30 in +05:30", start)
	}
}

// TestTimeWindowsSparkExpand is the motivating case: one output record
// per window, with no hand-written "w - 5m" arithmetic.
func TestTimeWindowsSparkExpand(t *testing.T) {
	env := map[string]any{"ts": mustTime(t, "2024-06-15T10:07:30Z")}
	got := evalTime(t, env, "map(time.windows(ts, time.minutes(10), time.minutes(5)), w => {start: w, end: w + time.minutes(10)})")
	want := []any{
		map[string]any{"start": mustTime(t, "2024-06-15T10:00:00Z"), "end": mustTime(t, "2024-06-15T10:10:00Z")},
		map[string]any{"start": mustTime(t, "2024-06-15T10:05:00Z"), "end": mustTime(t, "2024-06-15T10:15:00Z")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTimeWindowsErrors(t *testing.T) {
	env := map[string]any{"ts": mustTime(t, "2024-06-15T10:07:30Z")}
	cases := []struct {
		input   string
		wantMsg string
	}{
		{"time.windows(ts)", "expects 2 to 4 arguments"},
		{"time.windows(ts, time.minutes(1), time.minutes(1), time.minutes(1), time.minutes(1))", "expects 2 to 4 arguments"},
		{"time.windows(ts, time.minutes(1), time.minutes(1), 1)", "(offset) must be a duration"},
		{"time.windows(\"x\", time.minutes(1))", "must be a time value"},
		{"time.windows(ts, 60)", "(size) must be a duration"},
		{"time.windows(ts, time.minutes(1), 1)", "(slide) must be a duration"},
		{"time.windows(ts, time.minutes(0))", "must be positive"},
		{"time.windows(ts, time.minutes(10), time.minutes(-1))", "must be positive"},
		{"time.windows(ts, time.minutes(5), time.minutes(10))", "must not be longer than size"},
		{"time.windows(ts, time.days(1), time.milliseconds(1))", "more than the limit"},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			err := evalTimeErr(t, env, tt.input)
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("%s error = %q, want it to contain %q", tt.input, err, tt.wantMsg)
			}
		})
	}
}

func TestTimeParseStrftime(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"apache log", `time.parse("15/Jun/2024:10:07:30 -0700", "%d/%b/%Y:%H:%M:%S %z")`, "2024-06-15T17:07:30Z"},
		{"month name any case", `time.parse("15/JUN/2024", "%d/%b/%Y")`, "2024-06-15T00:00:00Z"},
		{"iso with literal T and Z", `time.parse("2024-06-15T10:07:30Z", "%Y-%m-%dT%H:%M:%SZ")`, "2024-06-15T10:07:30Z"},
		{"composite F T", `time.parse("2024-06-15 10:07:30", "%F %T")`, "2024-06-15T10:07:30Z"},
		{"fraction", `time.parse("10:07:30.250000 2024-06-15", "%H:%M:%S.%f %Y-%m-%d")`, "2024-06-15T10:07:30.25Z"},
		{"12-hour clock", `time.parse("06/15/24 03:04 PM", "%m/%d/%y %I:%M %p")`, "2024-06-15T15:04:00Z"},
		{"unpadded", `time.parse("5/6/2024", "%-d/%-m/%Y")`, "2024-06-05T00:00:00Z"},
		{"colon offset", `time.parse("2024-06-15 10:00 +05:30", "%Y-%m-%d %H:%M %:z")`, "2024-06-15T04:30:00Z"},
		{"day of year", `time.parse("2024-167", "%Y-%j")`, "2024-06-15T00:00:00Z"},
		{"literal percent", `time.parse("2024%", "%Y%%")`, "2024-01-01T00:00:00Z"},
		{"in named zone", `time.parse("2024-06-15 10:00", "%Y-%m-%d %H:%M", "America/New_York")`, "2024-06-15T14:00:00Z"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := evalTime(t, nil, tt.input).(time.Time)
			if !ok {
				t.Fatalf("%s did not return a time", tt.input)
			}
			if want := mustTime(t, tt.want); !got.Equal(want) {
				t.Errorf("%s = %v, want %v", tt.input, got, want)
			}
		})
	}
}

func TestTimeFormatStrftime(t *testing.T) {
	env := map[string]any{
		"t": time.Date(2024, 6, 5, 15, 4, 9, 250_000_000, time.FixedZone("CEST", 2*3600)),
	}
	cases := []struct {
		format string
		want   string
	}{
		{"%Y-%m-%dT%H", "2024-06-05T15"},
		{"%d/%b/%Y:%H:%M:%S %z", "05/Jun/2024:15:04:09 +0200"},
		{"%A %B %e %y", "Wednesday June  5 24"},
		{"%a %-d %-m %I:%M %p", "Wed 5 6 03:04 PM"},
		{"%F %T.%f %:z %Z", "2024-06-05 15:04:09.250000 +02:00 CEST"},
		{"%j %D %R %%", "157 06/05/24 15:04 %"},
		// Literal text that spells a Go layout element is fine for
		// format, which never builds a whole Go layout.
		{"Jan 1 PM: %d", "Jan 1 PM: 05"},
	}
	for _, tt := range cases {
		t.Run(tt.format, func(t *testing.T) {
			got := evalTime(t, env, `time.format(t, "`+tt.format+`")`)
			if got != tt.want {
				t.Errorf("format(t, %q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

// TestTimeFormatParseRoundTrip: format then parse with the same format
// gives back the same instant.
func TestTimeFormatParseRoundTrip(t *testing.T) {
	env := map[string]any{"t": time.Date(2024, 6, 5, 15, 4, 9, 250_000_000, time.FixedZone("", -7*3600))}
	got := evalTime(t, env, `let f = "%d/%b/%Y:%H:%M:%S.%f %z"; time.parse(time.format(t, f), f) == t`)
	if got != true {
		t.Errorf("round trip = %v, want true", got)
	}
}

func TestTimeStrftimeErrors(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{"unknown directive", `time.parse("2024", "%Q")`, "unsupported directive %Q"},
		{"unsupported flag combo", `time.format(time.now(), "%-H")`, "unsupported directive %-H"},
		{"trailing percent", `time.format(time.now(), "%Y%")`, "lone '%'"},
		{"f without separator", `time.format(time.now(), "%S%f")`, "%f must directly follow"},
		{"literal month name", `time.parse("Jan 2024", "Jan %Y")`, "literal text would be read"},
		{"literal digit", `time.parse("1-2024", "1-%Y")`, "literal text would be read"},
		{"input mismatch", `time.parse("2024/06/15", "%Y-%m-%d")`, "parse():"},
		{"out of range day", `time.parse("2024-02-30", "%Y-%m-%d")`, "parse():"},
		{"bad zone", `time.parse("2024", "%Y", "Nowhere/Nope")`, "parse():"},
		{"format non-time", `time.format("2024", "%Y")`, "must be a time value"},
		{"parse non-string", `time.parse(2024, "%Y")`, "must be a string"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := evalTimeErr(t, nil, tt.input)
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("%s error = %q, want it to contain %q", tt.input, err, tt.wantMsg)
			}
		})
	}
}

func TestTimeMonthWeekday(t *testing.T) {
	// 2024-06-15 was a Saturday; 2024-06-16 a Sunday.
	env := map[string]any{
		"sat": time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
		"sun": time.Date(2024, 6, 16, 12, 0, 0, 0, time.UTC),
	}
	cases := []struct {
		input string
		want  any
	}{
		{"time.month(sat)", int64(6)},
		{"time.weekday(sat)", int64(6)},
		{"time.weekday(sun)", int64(0)},
		{"time.month(sat) in [6, 7, 8]", true},
		{"time.weekday(sat) >= 1 && time.weekday(sat) <= 5", false},
		{"time.month(sat) + 1", int64(7)},
		{"type(time.month(sat))", "int64"},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			if got := evalTime(t, env, tt.input); got != tt.want {
				t.Errorf("%s = %v (%T), want %v", tt.input, got, got, tt.want)
			}
		})
	}
	evalTimeExpectError(t, nil, `time.month("2024-06-15")`)
	evalTimeExpectError(t, nil, `time.weekday()`)
}
