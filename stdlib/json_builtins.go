package stdlib

import (
	"encoding/json"
	"fmt"

	"github.com/cms103/owlexpr/vm"
)

// JSONBuiltins registers toJSON/fromJSON under the `json` namespace
// (json.toJSON(x), json.fromJSON(s))
//
// toJSON delegates straight to encoding/json.Marshal, so it already
// handles map[string]any and []any (owlexpr's own map/list
// representations - see owlexpr.ListElements) with no conversion step,
// plus anything else Go's json package knows how to encode (e.g. []byte
// becomes a base64 string, per encoding/json's own default MarshalJSON
// for byte slices).
//
// fromJSON delegates to json.Unmarshal into an `any`, which already
// produces exactly owlexpr's own map[string]any/[]any shape for objects
// and arrays. JSON numbers always decode as float64, matching
// encoding/json's own untyped-decode behavior - this deliberately does
// NOT try to guess "looks like a whole number, decode as int64", since
// that heuristic would silently misclassify a legitimate float like 2.0,
// and JSON itself makes no int/float distinction to detect in the first
// place.
func JSONBuiltins() vm.VMOption {
	return func(mc *vm.Machine) error {
		for name, fn := range namespacedJSONFuncs {
			mc.RegisterNamespacedBuiltin("json", name, fn)
		}
		return nil
	}
}

// namespacedJSONFuncs is the full JSONBuiltins roster keyed by its
// `json.<name>` member name.
var namespacedJSONFuncs = map[string]vm.BuiltinFunc{
	"toJSON":   toJSONFunc,
	"fromJSON": fromJSONFunc,
}

func toJSONFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("toJSON() expects 1 argument, got %d", len(args))
	}
	out, err := json.Marshal(args[0])
	if err != nil {
		return nil, fmt.Errorf("toJSON(): %w", err)
	}
	return string(out), nil
}

func fromJSONFunc(mc *vm.Machine, args ...any) (any, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("fromJSON() expects 1 argument, got %d", len(args))
	}
	s, err := argString("fromJSON", args, 0)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("fromJSON(): %w", err)
	}
	return out, nil
}
