package vm

import (
	"fmt"
)

// matchesValue implements the `matches` operator: str matches pattern.
// pattern is always a *Regex compiled at Compile() time - either directly
// by the compiler from a string or regex literal on the right-hand side
// (see compiler.go's BinaryOpNode case for "matches"), or earlier, by
// whatever produced the value a non-literal right-hand side evaluates to
// (a let-bound regex literal, an env value built with NewRegex). There is
// no runtime regexp.Compile call and nothing here to cache - a plain
// string arriving here at run time is an error, not a pattern to compile.
// str is required to be a plain string, with no implicit coercion of
// other types, matching this language's general "explicit over implicit"
// bias (see DESIGN_NOTES.md).
func matchesValue(str, pattern any) (bool, error) {
	re, ok := pattern.(*Regex)
	if !ok {
		if _, isString := pattern.(string); isString {
			return false, fmt.Errorf("'matches' requires a regex on the right-hand side, got a string - a pattern that isn't written inline must be a regex value (re`...`), not a string")
		}
		return false, fmt.Errorf("'matches' requires a regex on the right-hand side, got %T", pattern)
	}
	s, ok := str.(string)
	if !ok {
		return false, fmt.Errorf("'matches' requires a string operand on the left-hand side, got %T", str)
	}
	return re.re.MatchString(s), nil
}
