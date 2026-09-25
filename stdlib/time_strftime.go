package stdlib

import (
	"fmt"
	"strings"
	"time"

	"github.com/cms103/owlexpr/vm"
)

// strftime support for time.parse/time.format: C/Python-style "%Y-%m-%d"
// formats, alongside time.date's Go reference-time layouts
// ("2006-01-02"), which are hard to get right for anyone who doesn't
// write Go.
//
// A format is first split into segments - directives and literal text -
// and each directive maps to the Go layout element that means the same
// thing. Formatting then works segment by segment (each directive via
// its own t.Format(element), literal text copied verbatim), so it's
// always exact. Parsing has to hand Go one whole layout, and Go layouts
// have no escape syntax: literal text that happens to spell a layout
// element ("Jan", "1", "PM", ...) would be silently parsed as a field
// instead of matched as text. strftimeParseLayout detects that and
// reports it as an error rather than misparsing - see checkLayoutFaithful.

// strftimeDirectives maps each supported directive (the text after '%')
// to its Go layout element. Deliberately limited to what Go's own
// parser can read back, so parse and format accept exactly the same set.
var strftimeDirectives = map[string]string{
	"Y":  "2006",    // 4-digit year
	"y":  "06",      // 2-digit year
	"m":  "01",      // month, 01-12
	"-m": "1",       // month, 1-12
	"d":  "02",      // day of month, 01-31
	"-d": "2",       // day of month, 1-31
	"e":  "_2",      // day of month, space-padded
	"j":  "002",     // day of year, 001-366
	"H":  "15",      // hour, 00-23
	"I":  "03",      // hour, 01-12
	"-I": "3",       // hour, 1-12
	"M":  "04",      // minute, 00-59
	"-M": "4",       // minute, 0-59
	"S":  "05",      // second, 00-59
	"-S": "5",       // second, 0-59
	"f":  "000000",  // microseconds - must follow '.' or ','
	"p":  "PM",      // AM/PM
	"b":  "Jan",     // abbreviated month name
	"h":  "Jan",     // same as %b
	"B":  "January", // full month name
	"a":  "Mon",     // abbreviated weekday name
	"A":  "Monday",  // full weekday name
	"z":  "-0700",   // UTC offset, +hhmm
	":z": "-07:00",  // UTC offset, +hh:mm
	"Z":  "MST",     // time zone abbreviation
	"F":  "2006-01-02",
	"T":  "15:04:05",
	"D":  "01/02/06",
	"R":  "15:04",
}

// strftimeSegment is one directive (goElem set) or run of literal text.
type strftimeSegment struct {
	literal   string
	directive string // e.g. "Y", "-d", ":z"; empty for literal text
	goElem    string
}

// splitStrftime splits format into directive and literal segments,
// rejecting an unknown directive or a trailing lone '%'.
func splitStrftime(fn, format string) ([]strftimeSegment, error) {
	var segs []strftimeSegment
	var lit strings.Builder
	flushLit := func() {
		if lit.Len() > 0 {
			segs = append(segs, strftimeSegment{literal: lit.String()})
			lit.Reset()
		}
	}
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			lit.WriteByte(format[i])
			continue
		}
		i++
		if i >= len(format) {
			return nil, fmt.Errorf("%s(): format %q ends with a lone '%%'", fn, format)
		}
		if format[i] == '%' {
			lit.WriteByte('%')
			continue
		}
		// One optional flag ('-' for no padding, ':' for %:z), then the
		// directive letter itself.
		key := string(format[i])
		if (format[i] == '-' || format[i] == ':') && i+1 < len(format) {
			i++
			key += string(format[i])
		}
		elem, ok := strftimeDirectives[key]
		if !ok {
			return nil, fmt.Errorf("%s(): unsupported directive %%%s in format %q", fn, key, format)
		}
		if key == "f" {
			// Go only recognizes fractional seconds as ".000000" or
			// ",000000" - the separator is part of the element.
			s := lit.String()
			if s == "" || (s[len(s)-1] != '.' && s[len(s)-1] != ',') {
				return nil, fmt.Errorf("%s(): %%f must directly follow a '.' or ',' in format %q", fn, format)
			}
		}
		flushLit()
		segs = append(segs, strftimeSegment{directive: key, goElem: elem})
	}
	flushLit()
	return segs, nil
}

// formatSegments renders t segment by segment - literal text verbatim,
// each directive on its own - so literal text can never be mistaken for
// a layout element.
func formatSegments(t time.Time, segs []strftimeSegment) string {
	var sb strings.Builder
	for _, s := range segs {
		switch s.directive {
		case "":
			sb.WriteString(s.literal)
		case "f":
			fmt.Fprintf(&sb, "%06d", t.Nanosecond()/1000)
		default:
			sb.WriteString(t.Format(s.goElem))
		}
	}
	return sb.String()
}

// layoutProbeTime is a reference time for which every Go layout element
// renders as something other than its own spelling (month 11 not "1" or
// "01", AM not "PM", zone "ABC" not "MST", offset +05:30 not -07:00, ...).
// Formatting it with a layout and with formatSegments therefore only
// agrees when the layout's literal text contains no layout elements.
var layoutProbeTime = time.Date(2037, 11, 28, 10, 48, 39, 123456789, time.FixedZone("ABC", 5*3600+30*60))

// strftimeParseLayout converts format to a Go layout for time.Parse,
// failing if the result would misread any of format's literal text.
func strftimeParseLayout(fn, format string) (string, error) {
	segs, err := splitStrftime(fn, format)
	if err != nil {
		return "", err
	}
	var layout strings.Builder
	for _, s := range segs {
		if s.directive == "" {
			layout.WriteString(s.literal)
		} else {
			layout.WriteString(s.goElem)
		}
	}
	if layoutProbeTime.Format(layout.String()) != formatSegments(layoutProbeTime, segs) {
		return "", fmt.Errorf("%s(): format %q can't be parsed: some of its literal text would be read as a date/time field (e.g. a month name, \"PM\", or a stray digit)", fn, format)
	}
	return layout.String(), nil
}

// parseFunc: time.parse(s, format[, timezoneName]) - time.date with a
// strftime format instead of a Go layout. The optional zone name places
// clock times that carry no offset of their own (no %z) in that zone,
// exactly as time.date's third argument does.
func parseFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("parse() expects 2 or 3 arguments, got %d", len(args))
	}
	s, err := argString("parse", args, 0)
	if err != nil {
		return nil, err
	}
	format, err := argString("parse", args, 1)
	if err != nil {
		return nil, err
	}
	layout, err := strftimeParseLayout("parse", format)
	if err != nil {
		return nil, err
	}
	loc := time.UTC
	if len(args) == 3 {
		tzName, err := argString("parse", args, 2)
		if err != nil {
			return nil, err
		}
		if loc, err = time.LoadLocation(tzName); err != nil {
			return nil, fmt.Errorf("parse(): %w", err)
		}
	}
	t, err := time.ParseInLocation(layout, s, loc)
	if err != nil {
		return nil, fmt.Errorf("parse(): %w", err)
	}
	return t, nil
}

// formatTimeFunc: time.format(t, format) renders t with a strftime
// format. (Not formatFunc - that's string.format's.)
func formatTimeFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("format() expects 2 arguments, got %d", len(args))
	}
	t, ok := args[0].(time.Time)
	if !ok {
		return nil, fmt.Errorf("format(): argument 1 must be a time value, got %T", args[0])
	}
	format, err := argString("format", args, 1)
	if err != nil {
		return nil, err
	}
	segs, err := splitStrftime("format", format)
	if err != nil {
		return nil, err
	}
	return formatSegments(t, segs), nil
}
