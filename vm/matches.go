package vm

import (
	"fmt"
	"regexp"
)

// matchesValue implements the `matches` operator: str matches pattern.
// pattern is always a *regexp.Regexp already compiled by the compiler at
// Compile() time from a string-literal right-hand side (see compiler.go's
// BinaryOpNode case for "matches") - there is no runtime regexp.Compile
// call and nothing here to cache. str is required to be a plain string,
// with no implicit coercion of other types, matching this language's
// general "explicit over implicit" bias (see DESIGN_NOTES.md).
func matchesValue(str, pattern any) (bool, error) {
	re, ok := pattern.(*regexp.Regexp)
	if !ok {
		return false, fmt.Errorf("'matches' requires a compiled regex on the right-hand side, got %T", pattern)
	}
	s, ok := str.(string)
	if !ok {
		return false, fmt.Errorf("'matches' requires a string operand on the left-hand side, got %T", str)
	}
	return re.MatchString(s), nil
}
