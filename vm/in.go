package vm

import (
	"fmt"
	"reflect"
)

// inValue implements the `in` operator: needle in haystack.
//
// For a list-like haystack ([]any or another slice/array) it scans
// elements via Combine(OpEqual) - the same type-coded, coercion-aware
// equality every other comparison operator uses (so `2 in [2.0, 3.0]`
// matches) - stopping at the first match rather than visiting the rest.
//
// For a paired haystack, membership is checked against KEYS, not values,
// matching the same convention as most expression languages' `in`. A real
// Go map (map[string]any or any other map, via reflection) is looked up
// directly - a single O(1) hash lookup, not a scan over every key - since
// that's exactly what Go maps already give for free.
func inValue(mc *Machine, needle, haystack any) (bool, error) {
	if m, ok := haystack.(map[string]any); ok {
		key, ok := needle.(string)
		if !ok {
			return false, nil
		}
		_, exists := m[key]
		return exists, nil
	}

	rv := reflect.ValueOf(haystack)
	if rv.Kind() == reflect.Map {
		return mapContainsKey(rv, needle)
	}

	var found bool
	var iterErr error
	if ok := ForEachListElement(mc, haystack, func(el any) bool {
		res, err := mc.Combine(needle, el, OpEqual)
		if err != nil {
			iterErr = err
			return false
		}
		eq, _ := res.(bool)
		if eq {
			found = true
		}
		return !eq // false stops iteration early once a match is found
	}); ok {
		return found, iterErr
	}

	return false, fmt.Errorf("'in' not supported for right-hand type %T", haystack)
}

// mapContainsKey looks needle up directly in a reflect-inspected Go map -
// a real hash lookup via MapIndex (the same mechanism `m[k]` itself
// compiles to), not a walk over MapRange. needle is converted to the
// map's key type when it isn't already exactly that type, mirroring
// indexValue's key handling; a needle that simply can't be a key of this
// map's key type (e.g. an int64 needle against a map[string]V) means
// "not found", not an error.
func mapContainsKey(rv reflect.Value, needle any) (bool, error) {
	keyType := rv.Type().Key()
	keyVal := reflect.ValueOf(needle)
	if !keyVal.IsValid() {
		return false, nil
	}

	switch {
	case keyVal.Type().AssignableTo(keyType):
		// use as-is
	case keyVal.Type().ConvertibleTo(keyType):
		keyVal = keyVal.Convert(keyType)
	default:
		return false, nil
	}

	return rv.MapIndex(keyVal).IsValid(), nil
}
