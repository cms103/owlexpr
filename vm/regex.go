package vm

import "regexp"

// Regex is the run-time value of a regex literal (re`...`): an
// already-compiled pattern, produced once by the compiler at Compile()
// time and embedded in the instruction stream as an OpPush constant, so
// it's shared - unchanged - by every Run of that program, on every
// Machine, concurrently. It's what `matches` accepts on its right-hand
// side and what every regex.* builtin (stdlib.RegexBuiltins) takes as its
// first argument.
//
// It wraps *regexp.Regexp rather than being one, deliberately: struct
// methods on env values are callable from expressions by default (see
// DisableStructMethods), and *regexp.Regexp's method set includes
// Longest(), which mutates the regex in place - on a shared, compiled-once
// constant that would silently change the behavior of every later Run
// (and race with any concurrent one). Regex's only exported methods are
// read-only. The wrapped *regexp.Regexp is reachable from Go via RegexpOf,
// a package-level function rather than a method, precisely so an
// expression can't reach it too.
//
// An embedder can hand an expression a regex built in Go (a pattern from
// config, say) by putting NewRegex(...) in the env - it then works with
// `matches` and regex.* exactly like a literal does.
type Regex struct {
	re *regexp.Regexp
}

// NewRegex wraps an already-compiled *regexp.Regexp for use as an
// expression value. The caller must not call Longest() on re afterwards
// (see Regex's doc comment for why).
func NewRegex(re *regexp.Regexp) *Regex {
	return &Regex{re: re}
}

// CompileRegex compiles pattern (Go RE2 syntax) into a Regex.
func CompileRegex(pattern string) (*Regex, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &Regex{re: re}, nil
}

// RegexpOf returns r's underlying *regexp.Regexp, for Go code (a builtin,
// an embedder) that needs the full regexp API. The result is shared with
// every other user of r - treat it as read-only.
func RegexpOf(r *Regex) *regexp.Regexp {
	return r.re
}

// String returns the source pattern.
func (r *Regex) String() string {
	return r.re.String()
}

// MarshalText makes a Regex encode as its source pattern (e.g. through
// json.toJSON) rather than as an empty object.
func (r *Regex) MarshalText() ([]byte, error) {
	return []byte(r.re.String()), nil
}
