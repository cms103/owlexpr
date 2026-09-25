package stdlib

import (
	"fmt"
	"regexp"

	"github.com/cms103/owlexpr/vm"
)

// RegexBuiltins registers the regex.* builtins: capture groups, find,
// replace and split on top of the language's own regex values.
//
// Every function takes an already-compiled *vm.Regex as its first
// argument - a re`...` literal (compiled once, at Compile() time) or an
// embedder-supplied vm.NewRegex value - never a pattern string. Nothing
// here ever calls regexp.Compile, so there's no per-pattern cache to
// size or evict, and no way for expression input to grow memory by
// feeding in an unbounded stream of distinct patterns (the concern noted
// at the top of string_builtins.go).
//
// The regex comes first, matching every other pack's "the namespace's own
// type is argument 1" convention (string.* takes the string first, list.*
// the list).
func RegexBuiltins() vm.VMOption {
	return func(mc *vm.Machine) error {
		for name, fn := range namespacedRegexFuncs {
			mc.RegisterNamespacedBuiltin("regex", name, fn)
		}
		return nil
	}
}

// namespacedRegexFuncs is the full RegexBuiltins roster keyed by its
// `regex.<name>` member name.
var namespacedRegexFuncs = map[string]vm.BuiltinFunc{
	"find":           regexFindFunc,
	"findAll":        regexFindAllFunc,
	"groups":         regexGroupsFunc,
	"capture":        regexCaptureFunc,
	"captureAll":     regexCaptureAllFunc,
	"replace":        regexReplaceFunc,
	"replaceLiteral": regexReplaceLiteralFunc,
	"split":          regexSplitFunc,
	"pattern":        regexPatternFunc,
}

// argRegex is argString's counterpart for the regex argument every
// function here takes first. A plain string gets its own message, since
// passing a pattern string is the likeliest mistake and the fix (write it
// as re`...`) isn't obvious from "got string" alone.
func argRegex(fn string, args []any, i int) (*regexp.Regexp, error) {
	if i >= len(args) {
		return nil, fmt.Errorf("%s(): expected at least %d argument(s), got %d", fn, i+1, len(args))
	}
	switch r := args[i].(type) {
	case *vm.Regex:
		if r != nil {
			return vm.RegexpOf(r), nil
		}
	case string:
		return nil, fmt.Errorf("%s(): argument %d must be a regex (re`...`), got a string", fn, i+1)
	}
	return nil, fmt.Errorf("%s(): argument %d must be a regex, got %T", fn, i+1, args[i])
}

// regexAndString reads the (re, s) pair every function here starts with,
// after checking the total argument count is within [min, max].
func regexAndString(fn string, args []any, min, max int) (*regexp.Regexp, string, error) {
	if len(args) < min || len(args) > max {
		if min == max {
			return nil, "", fmt.Errorf("%s() expects %d arguments, got %d", fn, min, len(args))
		}
		return nil, "", fmt.Errorf("%s() expects %d or %d arguments, got %d", fn, min, max, len(args))
	}
	re, err := argRegex(fn, args, 0)
	if err != nil {
		return nil, "", err
	}
	s, err := argString(fn, args, 1)
	if err != nil {
		return nil, "", err
	}
	return re, s, nil
}

// optionalLimit reads the optional trailing count argument findAll,
// captureAll and split accept, returning Go's "no limit" -1 when it's
// absent. A negative count is rejected rather than passed through as
// Go's "unlimited", so there's exactly one way to spell "no limit" (leave
// it out).
func optionalLimit(fn string, args []any, i int) (int, error) {
	if i >= len(args) {
		return -1, nil
	}
	n, err := argInt(fn, args, i)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("%s(): argument %d must not be negative, got %d", fn, i+1, n)
	}
	return int(n), nil
}

// submatchValues converts a FindStringSubmatchIndex result into the
// group values it locates - the whole match first, then each group -
// with a group that didn't take part in the match as nil rather than "".
// That's the reason this works from indices rather than
// FindStringSubmatch, which reports both cases as "": `(a)?b` against "b"
// has group 1 unmatched (nil), while `(a*)b` against "b" has it matched
// as empty (""), and `??` / `?.` should be able to tell them apart.
func submatchValues(s string, loc []int) []any {
	out := make([]any, len(loc)/2)
	for i := range out {
		start, end := loc[2*i], loc[2*i+1]
		if start < 0 {
			out[i] = nil
		} else {
			out[i] = s[start:end]
		}
	}
	return out
}

// namedGroups builds capture's map from one match's group values: one
// entry per named group, in the order they appear in the pattern. Unnamed
// groups are skipped. If a name is used more than once, the last group
// with that name that actually matched wins (so an alternation like
// `(?P<n>a)|(?P<n>b)` yields whichever branch matched).
func namedGroups(re *regexp.Regexp, values []any) map[string]any {
	out := make(map[string]any)
	for i, name := range re.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		if prev, seen := out[name]; seen && values[i] == nil && prev != nil {
			continue
		}
		out[name] = values[i]
	}
	return out
}

// regexFindFunc: regex.find(re, s) -> the first match, or nil if none.
func regexFindFunc(mc *vm.Machine, args ...any) (any, error) {
	re, s, err := regexAndString("find", args, 2, 2)
	if err != nil {
		return nil, err
	}
	loc := re.FindStringIndex(s)
	if loc == nil {
		return nil, nil
	}
	return s[loc[0]:loc[1]], nil
}

// regexFindAllFunc: regex.findAll(re, s[, n]) -> every match (at most n),
// as a list - empty, not nil, when nothing matches, so it composes
// directly with len()/list.* without a nil check.
func regexFindAllFunc(mc *vm.Machine, args ...any) (any, error) {
	re, s, err := regexAndString("findAll", args, 2, 3)
	if err != nil {
		return nil, err
	}
	n, err := optionalLimit("findAll", args, 2)
	if err != nil {
		return nil, err
	}
	matches := re.FindAllString(s, n)
	out := make([]any, len(matches))
	for i, m := range matches {
		out[i] = m
	}
	return out, nil
}

// regexGroupsFunc: regex.groups(re, s) -> [whole match, group 1, group 2,
// ...] for the first match (nil for a group that didn't participate), or
// nil if there's no match at all.
func regexGroupsFunc(mc *vm.Machine, args ...any) (any, error) {
	re, s, err := regexAndString("groups", args, 2, 2)
	if err != nil {
		return nil, err
	}
	loc := re.FindStringSubmatchIndex(s)
	if loc == nil {
		return nil, nil
	}
	return submatchValues(s, loc), nil
}

// regexCaptureFunc: regex.capture(re, s) -> a map of the first match's
// named groups ((?P<name>...) or (?<name>...)), or nil if there's no
// match. A match with no named groups in the pattern gives an empty map,
// still distinguishable from no match.
func regexCaptureFunc(mc *vm.Machine, args ...any) (any, error) {
	re, s, err := regexAndString("capture", args, 2, 2)
	if err != nil {
		return nil, err
	}
	loc := re.FindStringSubmatchIndex(s)
	if loc == nil {
		return nil, nil
	}
	return namedGroups(re, submatchValues(s, loc)), nil
}

// regexCaptureAllFunc: regex.captureAll(re, s[, n]) -> capture's map for
// every match (at most n), as a list - empty when nothing matches.
func regexCaptureAllFunc(mc *vm.Machine, args ...any) (any, error) {
	re, s, err := regexAndString("captureAll", args, 2, 3)
	if err != nil {
		return nil, err
	}
	n, err := optionalLimit("captureAll", args, 2)
	if err != nil {
		return nil, err
	}
	locs := re.FindAllStringSubmatchIndex(s, n)
	out := make([]any, len(locs))
	for i, loc := range locs {
		out[i] = namedGroups(re, submatchValues(s, loc))
	}
	return out, nil
}

// regexReplaceFunc: regex.replace(re, s, repl) replaces every match.
// repl is either a string template, where $1 / ${1} / ${name} expand to
// the corresponding group ($$ for a literal $), or a function called
// with each matched text that must return the replacement string.
func regexReplaceFunc(mc *vm.Machine, args ...any) (any, error) {
	re, s, err := regexAndString("replace", args, 3, 3)
	if err != nil {
		return nil, err
	}
	if repl, ok := args[2].(string); ok {
		return re.ReplaceAllString(s, repl), nil
	}
	if !vm.IsCallable(args[2]) {
		return nil, fmt.Errorf("replace(): argument 3 must be a string or a function, got %T", args[2])
	}
	call := vm.NewReusableCall(mc, args[2])
	// ReplaceAllStringFunc has no way to stop early, so the first error
	// is recorded and every later match is left as-is until it returns.
	var callErr error
	out := re.ReplaceAllStringFunc(s, func(m string) string {
		if callErr != nil {
			return m
		}
		res, err := call.Call1(m)
		if err != nil {
			callErr = err
			return m
		}
		str, ok := res.(string)
		if !ok {
			callErr = fmt.Errorf("replace(): replacement function must return a string, got %T", res)
			return m
		}
		return str
	})
	if callErr != nil {
		return nil, callErr
	}
	return out, nil
}

// regexReplaceLiteralFunc: regex.replaceLiteral(re, s, repl) replaces
// every match with repl exactly as written - no $ expansion.
func regexReplaceLiteralFunc(mc *vm.Machine, args ...any) (any, error) {
	re, s, err := regexAndString("replaceLiteral", args, 3, 3)
	if err != nil {
		return nil, err
	}
	repl, err := argString("replaceLiteral", args, 2)
	if err != nil {
		return nil, err
	}
	return re.ReplaceAllLiteralString(s, repl), nil
}

// regexSplitFunc: regex.split(re, s[, n]) splits s around every match,
// into at most n pieces (the last holding the unsplit remainder).
func regexSplitFunc(mc *vm.Machine, args ...any) (any, error) {
	re, s, err := regexAndString("split", args, 2, 3)
	if err != nil {
		return nil, err
	}
	n, err := optionalLimit("split", args, 2)
	if err != nil {
		return nil, err
	}
	parts := re.Split(s, n)
	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = p
	}
	return out, nil
}

// regexPatternFunc: regex.pattern(re) -> the regex's source text.
func regexPatternFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("pattern() expects 1 argument, got %d", len(args))
	}
	re, err := argRegex("pattern", args, 0)
	if err != nil {
		return nil, err
	}
	return re.String(), nil
}
