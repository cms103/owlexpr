package stdlib

import (
	"reflect"
	"testing"

	"github.com/cms103/owlexpr"
)

// evalJSON parses, compiles, and runs input with JSONBuiltins() enabled,
// failing the test on any parse/compile/run error.
func evalJSON(t *testing.T, env map[string]any, input string) any {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(JSONBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	return res
}

func evalJSONExpectError(t *testing.T, env map[string]any, input string) {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("parse error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(JSONBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatalf("expected an error for %q, got none", input)
	}
}

func TestJSONNotRegisteredByDefault(t *testing.T) {
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	instructions, err := owlexpr.Compile(`json.toJSON(1)`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Fatalf("expected json.toJSON() to be undefined without JSONBuiltins()")
	}
}

func TestToJSON(t *testing.T) {
	if got := evalJSON(t, nil, `json.toJSON("hi")`); got != `"hi"` {
		t.Errorf(`json.toJSON("hi") = %v, want %q`, got, `"hi"`)
	}
	if got := evalJSON(t, nil, `json.toJSON(42)`); got != `42` {
		t.Errorf(`json.toJSON(42) = %v, want "42"`, got)
	}
	if got := evalJSON(t, nil, `json.toJSON(true)`); got != `true` {
		t.Errorf(`json.toJSON(true) = %v, want "true"`, got)
	}
	if got := evalJSON(t, nil, `json.toJSON([1, 2, 3])`); got != `[1,2,3]` {
		t.Errorf(`json.toJSON([1,2,3]) = %v, want "[1,2,3]"`, got)
	}
	env := map[string]any{"m": map[string]any{"a": int64(1), "b": "two"}}
	if got := evalJSON(t, env, `json.toJSON(m)`); got != `{"a":1,"b":"two"}` {
		t.Errorf(`json.toJSON(m) = %v, want {"a":1,"b":"two"}`, got)
	}
}

func TestFromJSON(t *testing.T) {
	if got := evalJSON(t, nil, `json.fromJSON("42")`); got != float64(42) {
		t.Errorf(`json.fromJSON("42") = %v (%T), want float64(42)`, got, got)
	}
	if got := evalJSON(t, nil, `json.fromJSON("\"hi\"")`); got != "hi" {
		t.Errorf(`json.fromJSON of a quoted string = %v, want "hi"`, got)
	}
	got := evalJSON(t, nil, `json.fromJSON("[1, 2, 3]")`)
	want := []any{float64(1), float64(2), float64(3)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf(`json.fromJSON("[1, 2, 3]") = %#v, want %#v`, got, want)
	}
	got = evalJSON(t, nil, `json.fromJSON("{\"a\": 1, \"b\": \"two\"}")`)
	wantMap := map[string]any{"a": float64(1), "b": "two"}
	if !reflect.DeepEqual(got, wantMap) {
		t.Errorf(`json.fromJSON(object) = %#v, want %#v`, got, wantMap)
	}
	evalJSONExpectError(t, nil, `json.fromJSON("not json")`)

	if got := evalJSON(t, nil, `json.fromJSON("null")`); got != nil {
		t.Errorf(`json.fromJSON("null") = %#v, want nil`, got)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	env := map[string]any{"m": map[string]any{"a": []any{float64(1), float64(2)}, "b": "two"}}
	got := evalJSON(t, env, `json.fromJSON(json.toJSON(m))`)
	if !reflect.DeepEqual(got, env["m"]) {
		t.Errorf("round trip = %#v, want %#v", got, env["m"])
	}
}

func TestJSONBuiltinArgumentErrors(t *testing.T) {
	cases := []string{
		`json.toJSON()`,
		`json.toJSON(1, 2)`,
		`json.fromJSON()`,
		`json.fromJSON(1, 2)`,
		`json.fromJSON(42)`, // wrong type, not a string
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			evalJSONExpectError(t, nil, input)
		})
	}
}
