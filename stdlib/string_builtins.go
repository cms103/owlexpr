package stdlib

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cms103/owlexpr"
	"github.com/cms103/owlexpr/vm"
)

// StringBuiltins registers a set of string-manipulation builtins.
//
// Regex (matches/etc.) is deliberately NOT included here: a pattern
// string is user-expression-controlled, and a naive compile-and-cache
// scheme keyed by pattern text is an unbounded-memory vector if that
// input isn't trusted. That needs its own config option with explicit
// cache sizing/eviction, not a builtin bundled in here.
//
// Every position/length-sensitive operation here (indexOf, lastIndexOf,
// reverse, padStart, padEnd) works in runes, not bytes, matching
// indexValue/sliceValue/lenFunc's existing rune-based semantics so that,
// e.g., a padStart target length means "runes", not "UTF-8 bytes", and
// indexOf's result composes correctly with s[i] / s[i:j]. upper/lower/trim
// are Unicode-correct by construction, since they delegate straight to
// Go's strings package, which already operates on Unicode code points
// (case folding tables, cutset-as-runes) rather than ASCII or raw bytes.
func StringBuiltins() vm.VMOption {
	return func(mc *vm.Machine) error {
		for name, fn := range namespacedStringFuncs {
			mc.RegisterNamespacedBuiltin("string", name, fn)
		}
		return nil
	}
}

// namespacedStringFuncs is the full StringBuiltins roster keyed by its
// `string.<name>` member name - the single source of truth StringBuiltins
// loops over to register.
var namespacedStringFuncs = map[string]vm.BuiltinFunc{
	"trim":        trimFunc,
	"trimLeft":    trimLeftFunc,
	"trimRight":   trimRightFunc,
	"trimPrefix":  trimPrefixFunc,
	"trimSuffix":  trimSuffixFunc,
	"upper":       upperFunc,
	"lower":       lowerFunc,
	"split":       splitFunc,
	"splitAfter":  splitAfterFunc,
	"replace":     replaceFunc,
	"repeat":      repeatFunc,
	"indexOf":     indexOfFunc,
	"lastIndexOf": lastIndexOfFunc,
	"hasPrefix":   hasPrefixFunc,
	"hasSuffix":   hasSuffixFunc,
	"contains":    containsFunc,
	"join":        joinFunc,
	"reverse":     reverseFunc,
	"padStart":    padStartFunc,
	"padEnd":      padEndFunc,
	"count":       countFunc,
	"format":      formatFunc,
	"toBase64":    stringToBase64Func,
	"fromBase64":  stringFromBase64Func,
}

// argString and argInt centralize the "wrong arity/wrong type" error
// shape used by every function below, so a caller gets a consistent
// "fn(): argument N must be a T, got U" message no matter which builtin
// they misused.
func argString(fn string, args []any, i int) (string, error) {
	if i >= len(args) {
		return "", fmt.Errorf("%s(): expected at least %d argument(s), got %d", fn, i+1, len(args))
	}
	s, ok := args[i].(string)
	if !ok {
		return "", fmt.Errorf("%s(): argument %d must be a string, got %T", fn, i+1, args[i])
	}
	return s, nil
}

func argInt(fn string, args []any, i int) (int64, error) {
	if i >= len(args) {
		return 0, fmt.Errorf("%s(): expected at least %d argument(s), got %d", fn, i+1, len(args))
	}
	switch n := args[i].(type) {
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	}
	return 0, fmt.Errorf("%s(): argument %d must be an integer, got %T", fn, i+1, args[i])
}

func trimFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("trim() expects 1 or 2 arguments, got %d", len(args))
	}
	str, err := argString("trim", args, 0)
	if err != nil {
		return nil, err
	}
	if len(args) == 1 {
		return strings.TrimSpace(str), nil
	}
	cutset, err := argString("trim", args, 1)
	if err != nil {
		return nil, err
	}
	return strings.Trim(str, cutset), nil
}

func trimLeftFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("trimLeft() expects 1 or 2 arguments, got %d", len(args))
	}
	str, err := argString("trimLeft", args, 0)
	if err != nil {
		return nil, err
	}
	if len(args) == 1 {
		return strings.TrimLeftFunc(str, unicode.IsSpace), nil
	}
	cutset, err := argString("trimLeft", args, 1)
	if err != nil {
		return nil, err
	}
	return strings.TrimLeft(str, cutset), nil
}

func trimRightFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("trimRight() expects 1 or 2 arguments, got %d", len(args))
	}
	str, err := argString("trimRight", args, 0)
	if err != nil {
		return nil, err
	}
	if len(args) == 1 {
		return strings.TrimRightFunc(str, unicode.IsSpace), nil
	}
	cutset, err := argString("trimRight", args, 1)
	if err != nil {
		return nil, err
	}
	return strings.TrimRight(str, cutset), nil
}

func trimPrefixFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("trimPrefix() expects 2 arguments, got %d", len(args))
	}
	str, err := argString("trimPrefix", args, 0)
	if err != nil {
		return nil, err
	}
	prefix, err := argString("trimPrefix", args, 1)
	if err != nil {
		return nil, err
	}
	return strings.TrimPrefix(str, prefix), nil
}

func trimSuffixFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("trimSuffix() expects 2 arguments, got %d", len(args))
	}
	str, err := argString("trimSuffix", args, 0)
	if err != nil {
		return nil, err
	}
	suffix, err := argString("trimSuffix", args, 1)
	if err != nil {
		return nil, err
	}
	return strings.TrimSuffix(str, suffix), nil
}

func upperFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("upper() expects 1 argument, got %d", len(args))
	}
	str, err := argString("upper", args, 0)
	if err != nil {
		return nil, err
	}
	return strings.ToUpper(str), nil
}

func lowerFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("lower() expects 1 argument, got %d", len(args))
	}
	str, err := argString("lower", args, 0)
	if err != nil {
		return nil, err
	}
	return strings.ToLower(str), nil
}

// splitElements adapts a []string from the strings package into the []any
// shape the rest of owlexpr's list values use (list literals, map/filter
// output).
func splitElements(parts []string) []any {
	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = p
	}
	return out
}

func splitFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("split() expects 2 or 3 arguments, got %d", len(args))
	}
	str, err := argString("split", args, 0)
	if err != nil {
		return nil, err
	}
	delim, err := argString("split", args, 1)
	if err != nil {
		return nil, err
	}
	if len(args) == 2 {
		return splitElements(strings.Split(str, delim)), nil
	}
	n, err := argInt("split", args, 2)
	if err != nil {
		return nil, err
	}
	return splitElements(strings.SplitN(str, delim, int(n))), nil
}

func splitAfterFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("splitAfter() expects 2 or 3 arguments, got %d", len(args))
	}
	str, err := argString("splitAfter", args, 0)
	if err != nil {
		return nil, err
	}
	delim, err := argString("splitAfter", args, 1)
	if err != nil {
		return nil, err
	}
	if len(args) == 2 {
		return splitElements(strings.SplitAfter(str, delim)), nil
	}
	n, err := argInt("splitAfter", args, 2)
	if err != nil {
		return nil, err
	}
	return splitElements(strings.SplitAfterN(str, delim, int(n))), nil
}

func replaceFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("replace() expects 3 arguments, got %d", len(args))
	}
	str, err := argString("replace", args, 0)
	if err != nil {
		return nil, err
	}
	old, err := argString("replace", args, 1)
	if err != nil {
		return nil, err
	}
	newStr, err := argString("replace", args, 2)
	if err != nil {
		return nil, err
	}
	return strings.ReplaceAll(str, old, newStr), nil
}

func repeatFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("repeat() expects 2 arguments, got %d", len(args))
	}
	str, err := argString("repeat", args, 0)
	if err != nil {
		return nil, err
	}
	n, err := argInt("repeat", args, 1)
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("repeat(): count must not be negative, got %d", n)
	}
	return strings.Repeat(str, int(n)), nil
}

// runeIndex converts a byte offset from strings.Index/LastIndex into a
// rune offset, so indexOf/lastIndexOf compose correctly with the
// rune-based s[i]/s[i:j]/len(s) - see DESIGN_NOTES.md's note that any
// future string-position feature needs to report rune positions for the
// same reason len() counts runes.
func runeIndex(s string, byteIdx int) int64 {
	return int64(utf8.RuneCountInString(s[:byteIdx]))
}

func indexOfFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("indexOf() expects 2 arguments, got %d", len(args))
	}
	str, err := argString("indexOf", args, 0)
	if err != nil {
		return nil, err
	}
	substr, err := argString("indexOf", args, 1)
	if err != nil {
		return nil, err
	}
	byteIdx := strings.Index(str, substr)
	if byteIdx < 0 {
		return int64(-1), nil
	}
	return runeIndex(str, byteIdx), nil
}

func lastIndexOfFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("lastIndexOf() expects 2 arguments, got %d", len(args))
	}
	str, err := argString("lastIndexOf", args, 0)
	if err != nil {
		return nil, err
	}
	substr, err := argString("lastIndexOf", args, 1)
	if err != nil {
		return nil, err
	}
	byteIdx := strings.LastIndex(str, substr)
	if byteIdx < 0 {
		return int64(-1), nil
	}
	return runeIndex(str, byteIdx), nil
}

func hasPrefixFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("hasPrefix() expects 2 arguments, got %d", len(args))
	}
	str, err := argString("hasPrefix", args, 0)
	if err != nil {
		return nil, err
	}
	prefix, err := argString("hasPrefix", args, 1)
	if err != nil {
		return nil, err
	}
	return strings.HasPrefix(str, prefix), nil
}

func hasSuffixFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("hasSuffix() expects 2 arguments, got %d", len(args))
	}
	str, err := argString("hasSuffix", args, 0)
	if err != nil {
		return nil, err
	}
	suffix, err := argString("hasSuffix", args, 1)
	if err != nil {
		return nil, err
	}
	return strings.HasSuffix(str, suffix), nil
}

func containsFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("contains() expects 2 arguments, got %d", len(args))
	}
	str, err := argString("contains", args, 0)
	if err != nil {
		return nil, err
	}
	substr, err := argString("contains", args, 1)
	if err != nil {
		return nil, err
	}
	return strings.Contains(str, substr), nil
}

func countFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("count() expects 2 arguments, got %d", len(args))
	}
	str, err := argString("count", args, 0)
	if err != nil {
		return nil, err
	}
	substr, err := argString("count", args, 1)
	if err != nil {
		return nil, err
	}
	return int64(strings.Count(str, substr)), nil
}

// joinFunc requires every element to already be a string, matching the
// codebase's existing preference for clean type errors over silently
// stringifying (see filterFunc's map-key check) - use string(v) per
// element first if the list may hold non-string values.
func joinFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("join() expects 1 or 2 arguments, got %d", len(args))
	}
	elems, ok := owlexpr.ListElements(args[0])
	if !ok {
		return nil, fmt.Errorf("join(): first argument must be a list, got %T", args[0])
	}
	sep := ""
	if len(args) == 2 {
		var err error
		sep, err = argString("join", args, 1)
		if err != nil {
			return nil, err
		}
	}
	parts := make([]string, len(elems))
	for i, el := range elems {
		s, ok := el.(string)
		if !ok {
			return nil, fmt.Errorf("join(): element %d must be a string, got %T", i, el)
		}
		parts[i] = s
	}
	return strings.Join(parts, sep), nil
}

func reverseFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("reverse() expects 1 argument, got %d", len(args))
	}
	str, err := argString("reverse", args, 0)
	if err != nil {
		return nil, err
	}
	runes := []rune(str)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes), nil
}

// padRunes builds the []rune of padding needed to bring s up to
// targetLen runes, cycling pad as many times as necessary and truncating
// to the exact amount needed - length arithmetic is rune-based throughout
// so a multi-byte pad character pads by one character, not one byte.
func padRunes(s, pad string, targetLen int64) ([]rune, error) {
	if pad == "" {
		return nil, fmt.Errorf("pad string must not be empty")
	}
	current := int64(utf8.RuneCountInString(s))
	need := targetLen - current
	if need <= 0 {
		return nil, nil
	}
	padChars := []rune(pad)
	out := make([]rune, 0, need)
	for int64(len(out)) < need {
		out = append(out, padChars...)
	}
	return out[:need], nil
}

func padStartFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("padStart() expects 2 or 3 arguments, got %d", len(args))
	}
	str, err := argString("padStart", args, 0)
	if err != nil {
		return nil, err
	}
	targetLen, err := argInt("padStart", args, 1)
	if err != nil {
		return nil, err
	}
	pad := " "
	if len(args) == 3 {
		pad, err = argString("padStart", args, 2)
		if err != nil {
			return nil, err
		}
	}
	padding, err := padRunes(str, pad, targetLen)
	if err != nil {
		return nil, fmt.Errorf("padStart(): %w", err)
	}
	if padding == nil {
		return str, nil
	}
	return string(padding) + str, nil
}

func padEndFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("padEnd() expects 2 or 3 arguments, got %d", len(args))
	}
	str, err := argString("padEnd", args, 0)
	if err != nil {
		return nil, err
	}
	targetLen, err := argInt("padEnd", args, 1)
	if err != nil {
		return nil, err
	}
	pad := " "
	if len(args) == 3 {
		pad, err = argString("padEnd", args, 2)
		if err != nil {
			return nil, err
		}
	}
	padding, err := padRunes(str, pad, targetLen)
	if err != nil {
		return nil, fmt.Errorf("padEnd(): %w", err)
	}
	if padding == nil {
		return str, nil
	}
	return str + string(padding), nil
}

// formatFunc is a thin wrapper over fmt.Sprintf: Go's fmt package never
// panics on a verb/argument mismatch (it embeds a "%!v(...)" marker in
// the output instead), so this stays consistent with the "errors, not
// panics" rule without needing its own verb validation.
func formatFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("format() expects at least 1 argument, got %d", len(args))
	}
	str, err := argString("format", args, 0)
	if err != nil {
		return nil, err
	}
	return fmt.Sprintf(str, args[1:]...), nil
}

// stringToBase64Func/stringFromBase64Func are string.*'s own base64 pair,
// separate from bytes.toBase64/fromBase64 rather than one function
// accepting either a string or a []byte: bytesArg's own doc comment
// already rules out coercing a string into a buffer, so a dual-type
// overload would cut against that, and keeping each namespace working
// only in its own type (string in, string out here) matches every other
// function in this file. fromBase64 treats the decoded bytes as UTF-8
// text and errors if they aren't valid, consistent with this pack's
// Unicode-correctness commitment (see StringBuiltins' own doc comment) -
// use bytes.fromBase64 instead for arbitrary binary payloads.
func stringToBase64Func(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("toBase64() expects 1 argument, got %d", len(args))
	}
	str, err := argString("toBase64", args, 0)
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.EncodeToString([]byte(str)), nil
}

func stringFromBase64Func(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("fromBase64() expects 1 argument, got %d", len(args))
	}
	str, err := argString("fromBase64", args, 0)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		return nil, fmt.Errorf("fromBase64(): %w", err)
	}
	if !utf8.Valid(decoded) {
		return nil, fmt.Errorf("fromBase64(): decoded bytes are not valid UTF-8")
	}
	return string(decoded), nil
}
