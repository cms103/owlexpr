package owlexpr

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cms103/owlexpr/vm"
)

// evalRoot parses, compiles, and runs input against the default VM (no
// extra VMOptions - map/filter/reduce need none), failing the test on any
// parse/compile/run error.
func evalRoot(t *testing.T, env map[string]any, input string) any {
	t.Helper()
	instructions, err := Compile(input)
	if err != nil {
		t.Fatalf("compile error for %q: %v", input, err)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	return res
}

func evalRootExpectError(t *testing.T, env map[string]any, input string) {
	t.Helper()
	instructions, err := Compile(input)
	if err != nil {
		t.Fatalf("compile error for %q: %v", input, err)
	}
	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	if _, err := mc.Run(instructions, env); err == nil {
		t.Fatalf("expected an error for %q, got none", input)
	}
}

// TestFilterMapStringKeysStaysTyped covers filterFunc's paired-source
// branch over an ordinary string-keyed map - owlexpr's own map literals
// can never produce anything else - and checks the result is still a
// concrete map[string]any, not map[any]any, so existing Go host code
// doing result.(map[string]any) keeps working unchanged.
func TestFilterMapStringKeysStaysTyped(t *testing.T) {
	got := evalRoot(t, nil, `filter({a: 1, b: 2, c: 3}, (k, v) => v > 1)`)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("filter() returned %T, want map[string]any", got)
	}
	want := map[string]any{"b": int64(2), "c": int64(3)}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("filter() = %v, want %v", m, want)
	}
}

// TestFilterMapNonStringKeys covers filtering a map[any]any source (e.g.
// what stdlib's groupByFunc returns) - the result should come back as
// map[any]any too, since the surviving keys aren't strings.
func TestFilterMapNonStringKeys(t *testing.T) {
	env := map[string]any{"m": map[any]any{
		int64(1): "a",
		int64(2): "b",
		int64(3): "c",
	}}
	got := evalRoot(t, env, `filter(m, (k, v) => v != "b")`)
	m, ok := got.(map[any]any)
	if !ok {
		t.Fatalf("filter() returned %T, want map[any]any", got)
	}
	want := map[any]any{int64(1): "a", int64(3): "c"}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("filter() = %v, want %v", m, want)
	}
}

func TestFilterRejectsIterSeq2(t *testing.T) {
	env := map[string]any{"seq": func(yield func([]int, string) bool) {
		yield([]int{1, 2}, "x")
	}}
	evalRootExpectError(t, env, `filter(seq, (k, v) => true)`)
}

// TestAbsCeilFloorRound covers int/int64/float64 for each of the four new
// core math builtins, including that ceil/floor/round are no-ops on an
// already-integer value.
func TestAbsCeilFloorRound(t *testing.T) {
	cases := []struct {
		input string
		want  any
	}{
		{`abs(-5)`, int64(5)},
		{`abs(5)`, int64(5)},
		{`abs(-5.5)`, 5.5},
		{`ceil(4)`, int64(4)},
		{`ceil(4.1)`, 5.0},
		{`ceil(-4.1)`, -4.0},
		{`floor(4)`, int64(4)},
		{`floor(4.9)`, 4.0},
		{`floor(-4.1)`, -5.0},
		{`round(4)`, int64(4)},
		{`round(4.5)`, 5.0},
		{`round(4.4)`, 4.0},
		{`round(-4.5)`, -5.0},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalRoot(t, nil, tt.input)
			if got != tt.want {
				t.Errorf("%s = %v (%T), want %v (%T)", tt.input, got, got, tt.want, tt.want)
			}
		})
	}
}

func TestAbsCeilFloorRoundRejectUnsupportedType(t *testing.T) {
	for _, fn := range []string{"abs", "ceil", "floor", "round"} {
		evalRootExpectError(t, nil, fn+`("not a number")`)
	}
}

// cents is a custom numeric type an embedder might register via
// vm.RegisterOperation for its own arithmetic support. This test proves
// abs()/ceil()/floor()/round() extend to it "for free" through the same
// registration - no second, unary-specific registration call needed -
// by adding OpAbs/OpCeil/OpFloor/OpRound cases to the one handler
// function it already wrote for OpNeg.
type cents struct{ n int64 }

func centsOperations(a, b any, aTC, bTC vm.TypeCode, op vm.OpCode) (any, error) {
	aVal := a.(cents)
	switch op {
	case vm.OpNeg:
		return cents{n: -aVal.n}, nil
	case vm.OpAbs:
		if aVal.n < 0 {
			return cents{n: -aVal.n}, nil
		}
		return aVal, nil
	case vm.OpCeil, vm.OpFloor, vm.OpRound:
		// cents is already integral - same no-op convention int/int64
		// use for these ops.
		return aVal, nil
	}
	return nil, errors.ErrUnsupported
}

func TestAbsCeilFloorRoundExtendViaRegisterOperation(t *testing.T) {
	env := map[string]any{"neg": cents{n: -150}, "pos": cents{n: 150}}
	mc, err := NewVM(vm.RegisterOperation(cents{}, nil, centsOperations))
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	cases := []struct {
		input string
		want  int64
	}{
		{"abs(neg)", 150},
		{"abs(pos)", 150},
		{"ceil(pos)", 150},
		{"floor(pos)", 150},
		{"round(pos)", 150},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			instructions, err := Compile(tt.input)
			if err != nil {
				t.Fatalf("compile error: %v", err)
			}
			got, err := mc.Run(instructions, env)
			if err != nil {
				t.Fatalf("run error: %v", err)
			}
			if got.(cents).n != tt.want {
				t.Errorf("%s = %v, want cents{%d}", tt.input, got, tt.want)
			}
		})
	}
}

// TestType covers typeFunc's vm.IsNilResult-backed "nil" case (both a
// literal nil and a typed-nil pointer), its fast-path cases (the core
// scalar types plus []any/map[string]any), its vm.IsCallable-backed
// "function" case for a lambda closure, and its reflect-based fallback
// for a plain Go struct from the env (which isn't any of the above, so
// it reports its concrete %T name).
func TestType(t *testing.T) {
	type widget struct{ Name string }
	var typedNilPtr *widget
	env := map[string]any{"w": widget{Name: "x"}, "p": typedNilPtr}
	cases := []struct {
		input string
		want  string
	}{
		{`type(nil)`, "nil"},
		{`type(p)`, "nil"},
		{`type(true)`, "bool"},
		{`type(int64(1))`, "int64"},
		{`type(1.5)`, "float64"},
		{`type("x")`, "string"},
		{`type([1, 2])`, "list"},
		{`type({a: 1})`, "map"},
		{`type(x => x)`, "function"},
		{`type(w)`, "owlexpr.widget"},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalRoot(t, env, tt.input)
			if got != tt.want {
				t.Errorf("%s = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestStr covers str(), the core stringify function that sits alongside
// int()/int64()/float64() - moved here (formerly string.string() in the
// opt-in string.* namespace) precisely so it's always available without
// requiring StringBuiltins(), matching the always-on numeric conversions.
func TestStr(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`str(42)`, "42"},
		{`str("already")`, "already"},
		{`str(true)`, "true"},
		{`str(nil)`, ""},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalRoot(t, nil, tt.input)
			if got != tt.want {
				t.Errorf("%s = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestStrArgumentErrors(t *testing.T) {
	evalRootExpectError(t, nil, `str()`)
	evalRootExpectError(t, nil, `str(1, 2)`)
}
