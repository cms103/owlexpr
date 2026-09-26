package vm

import "slices"

// Program is a compiled expression - the instructions owlexpr.Compile
// produces, ready to run on a Machine. It's a named []Instruction rather
// than a wrapper struct so it stays interchangeable with the plain slice:
// a Program can be passed straight to Run/RunScopes, and a hand-built
// []Instruction can be converted with Program(instructions) to use the
// methods below. Like the instructions it holds, a Program is immutable
// once compiled and safe to share across goroutines.
type Program []Instruction

// Names reports the names p resolves by name when it runs - every
// identifier compiled to an OpLoad, including those in lambda bodies, let
// bodies and the left operand of `??`. The result is sorted and
// de-duplicated, and is empty (not nil) when p loads no names.
//
// This is what an embedder needs to know to build a minimal environment
// for p: a name that Names doesn't report is never looked up, so leaving
// it out of env can't change the result. Names bound inside the
// expression itself - lambda parameters and let names - are resolved
// lexically (OpLoadLocal), never looked up, so they aren't reported; a let
// or lambda that shadows a name hides it only where the binding is in
// scope (`let x = x + 1; x` still reports x, read by the let's value).
//
// Names can't tell a variable from a builtin: a builtin function or
// builtin namespace the expression uses is reported too (`round(value)`
// reports round and value, `time.now()` reports time), since which
// builtins exist depends on the Machine the Program is later run on, and
// env takes priority over builtins for the same name (see OpLoad in
// vm.go). Only the first identifier of a chain is reported - `a.b.c`
// reports a - as members are accessed on the loaded value, not looked up
// by name.
//
// Names walks the instructions on every call; call it once and keep the
// result if it's needed repeatedly.
func (p Program) Names() []string {
	seen := make(map[string]bool)
	collectLoadedNames(p, seen)
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// collectLoadedNames adds the name of every OpLoad in instructions, and in
// every instruction stream nested in them, to seen. The nested streams are
// the same ones ContainsClosureLiteral walks, plus lambda bodies:
// OpCoalesce's right-hand operand, like a conditional's branches, is
// compiled inline in the stream already being walked.
func collectLoadedNames(instructions []Instruction, seen map[string]bool) {
	for _, inst := range instructions {
		switch inst.Op {
		case OpLoad:
			// Compile always emits a HashedName, a hand-built instruction
			// may use a plain string (see asHashedName)
			switch arg := inst.Arg.(type) {
			case HashedName:
				seen[arg.Name] = true
			case string:
				seen[arg] = true
			}
		case OpMakeClosure:
			if proto, ok := inst.Arg.(*LambdaProto); ok {
				collectLoadedNames(proto.Instructions, seen)
			}
		case OpLet:
			if arg, ok := inst.Arg.(*LetArg); ok {
				collectLoadedNames(arg.Body, seen)
			}
		case OpCoalesce:
			if arg, ok := inst.Arg.(*CoalesceArg); ok {
				collectLoadedNames(arg.Left, seen)
			}
		}
	}
}
