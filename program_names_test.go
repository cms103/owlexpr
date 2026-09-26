package owlexpr_test

import (
	"reflect"
	"testing"

	"github.com/cms103/owlexpr"
	"github.com/cms103/owlexpr/stdlib"
	"github.com/cms103/owlexpr/vm"
)

// TestProgramNames covers vm.Program.Names on compiled expressions: sorted,
// de-duplicated names, including those nested in lambdas, let and ??, but
// not names bound by the expression itself.
func TestProgramNames(t *testing.T) {
	cases := []struct {
		expr string
		want []string
	}{
		{`1 + 2`, []string{}},
		{`b + a * b`, []string{"a", "b"}},
		{`user.address.city == "Stockholm"`, []string{"user"}},
		{`items[index]`, []string{"index", "items"}},
		{`round(value)`, []string{"round", "value"}},
		{`time.now() > created`, []string{"created", "time"}},
		{`map(items, x => x * rate)`, []string{"items", "map", "rate"}},
		{`x + map(items, x => x * 2)`, []string{"items", "map", "x"}},
		{`map(items, x => map(x, y => y + x + offset))`, []string{"items", "map", "offset"}},
		{`let limit = max; value > limit`, []string{"max", "value"}},
		{`let x = x + 1; x * y`, []string{"x", "y"}},
		{`attributes.Fee ?? fallback`, []string{"attributes", "fallback"}},
		{`(a ?? b) ?? (let c = d; c)`, []string{"a", "b", "d"}},
		{`if flag { then } else { otherwise }`, []string{"flag", "otherwise", "then"}},
		{`{"k": v, "j": [w]}[lookup]`, []string{"lookup", "v", "w"}},
		{`() => free`, []string{"free"}},
		{`true ? a : b`, []string{"a", "b"}},
	}
	for _, c := range cases {
		program, err := owlexpr.Compile(c.expr)
		if err != nil {
			t.Errorf("Compile(%q): %v", c.expr, err)
			continue
		}
		if got := program.Names(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Compile(%q).Names() = %#v, want %#v", c.expr, got, c.want)
		}
	}
}

// TestProgramNamesSufficientEnv checks the promise Names makes to an
// embedder: an env holding only the reported names (less the builtins the
// Machine provides) is enough to run the program.
func TestProgramNamesSufficientEnv(t *testing.T) {
	values := map[string]any{
		"value":      int64(7),
		"rate":       int64(3),
		"items":      []any{int64(1), int64(2)},
		"attributes": map[string]any{"Direction": "Inbound"},
		"unused":     int64(1),
	}
	cases := []struct {
		expr string
		want any
	}{
		{`value * rate`, int64(21)},
		{`sum(map(items, x => x * rate))`, int64(9)},
		{`let v = value; attributes.Direction == "Inbound" && v > 5`, true},
		{`attributes.Missing ?? value`, int64(7)},
	}
	machine, err := owlexpr.NewVM(stdlib.All())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		program, err := owlexpr.Compile(c.expr)
		if err != nil {
			t.Fatalf("Compile(%q): %v", c.expr, err)
		}
		env := make(map[string]any)
		for _, name := range program.Names() {
			if value, found := values[name]; found {
				env[name] = value
			}
		}
		if _, found := env["unused"]; found {
			t.Errorf("%q: Names reported a name the expression doesn't use", c.expr)
		}
		got, err := machine.Run(program, env)
		if err != nil {
			t.Errorf("%q with env built from Names %v: %v", c.expr, program.Names(), err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q = %#v, want %#v", c.expr, got, c.want)
		}
	}
}

// TestProgramInterchangeable checks that vm.Program and []vm.Instruction
// can be used in place of each other, and that Names handles a hand-built
// program using a plain string name.
func TestProgramInterchangeable(t *testing.T) {
	program, err := owlexpr.Compile(`a + 1`)
	if err != nil {
		t.Fatal(err)
	}
	var instructions []vm.Instruction = program
	if got := vm.Program(instructions).Names(); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("Names() of converted instructions = %#v", got)
	}

	handBuilt := vm.Program{{Op: vm.OpLoad, Arg: "b"}, {Op: vm.OpPush, Arg: int64(1)}, {Op: vm.OpAdd}}
	if got := handBuilt.Names(); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("Names() of hand-built program = %#v", got)
	}
	machine, err := owlexpr.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := machine.Run(handBuilt, map[string]any{"b": int64(2)}); err != nil || got != int64(3) {
		t.Errorf("Run(hand-built) = %v, %v", got, err)
	}
}
