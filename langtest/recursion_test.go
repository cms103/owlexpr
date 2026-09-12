package langtest

import (
	"strings"
	"testing"

	"github.com/cms103/owlexpr"
)

// yCombinatorSumSrc computes the sum 1..N via genuine self-application
// recursion, with no letrec/self-reference sugar needed: `self` is bound
// as an ordinary lambda parameter, and sum(sum) passes the lambda to
// itself explicitly - the standard applicative-order self-application
// trick for recursion in a language whose `let` is non-recursive (see
// LetArg's own doc comment in vm/bytecode.go: a let's Value is compiled
// with the bound name NOT yet in scope, so `let f = ... f ... ; body`
// can't have f's own definition refer to itself directly).
const yCombinatorSumSrc = `let sum = self => n => n <= 0 ? 0 : n + self(self)(n - 1); sum(sum)(N)`

// TestDeepRecursionSucceeds exercises real, deep call nesting - not the
// wide-but-shallow nesting filter/map/reduce produce (each element's
// lambda call returns before the next one starts), but N genuinely
// nested, still-active lambda invocations at once. This is exactly the
// shape the VM's explicit frame stack (see vm/vm.go's frame doc comment)
// replaced Go's own recursive call stack for: 5000 here comfortably
// exceeds anything a real expression would need, well under
// maxFrameDepth, and must still produce the exactly correct answer, not
// just "not crash".
func TestDeepRecursionSucceeds(t *testing.T) {
	const n = 5000
	want := int64(n * (n + 1) / 2)
	got := evalCoalesce(t, map[string]any{"N": int64(n)}, yCombinatorSumSrc)
	if got != want {
		t.Fatalf("sum(1..%d) = %v, want %v", n, got, want)
	}
}

// TestUnboundedRecursionErrorsCleanly confirms a runaway recursive lambda
// (no base case - n never decreases) fails as an ordinary, catchable
// error once the VM's frame stack hits maxFrameDepth, rather than the
// fatal, unrecoverable Go "stack overflow" a real recursive mc.run() call
// per invocation used to risk - see frame's doc comment in vm/vm.go for
// why that distinction matters for anything embedding owlexpr inside a
// larger, longer-lived process.
func TestUnboundedRecursionErrorsCleanly(t *testing.T) {
	src := `let loop = self => n => n + self(self)(n); loop(loop)(1)`
	instructions, err := owlexpr.Compile(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	_, err = mc.Run(instructions, nil)
	if err == nil {
		t.Fatal("expected an error for unbounded recursion, got none")
	}
	if !strings.Contains(err.Error(), "call stack depth exceeded") {
		t.Fatalf("got error %q, want it to mention the call stack depth", err.Error())
	}
}
