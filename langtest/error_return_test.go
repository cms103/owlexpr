package langtest

import (
	"errors"
	"testing"

	"github.com/cms103/owlexpr"
	"github.com/cms103/owlexpr/vm"
)

func lookupWithError(key string) (string, error) {
	if key == "known" {
		return "found-it", nil
	}
	return "", errors.New("not found: " + key)
}

// bareError is a Go function whose ONLY return value is an error, with no
// accompanying data value - the shape callReflectFunc previously handled
// incorrectly (see TestCallReflectFuncBareErrorReturn).
func bareError(fail bool) error {
	if fail {
		return errors.New("boom")
	}
	return nil
}

func lookupBuiltin(mc *vm.Machine, args ...any) (any, error) {
	key, _ := args[0].(string)
	return lookupWithError(key)
}

// TestCoalesceWithTwoValueErrorReturn confirms "??" works with the
// standard Go (T, error) convention, for both a plain reflected function
// and a registered BuiltinFunc (already natively (any, error), so no
// reflection heuristics are involved at all).
func TestCoalesceWithTwoValueErrorReturn(t *testing.T) {
	env := map[string]any{
		"lookup":      lookupWithError,
		"lookupBuilt": vm.BuiltinFunc(lookupBuiltin),
	}

	cases := []struct {
		input string
		want  any
	}{
		{`lookup("known") ?? "fallback"`, "found-it"},
		{`lookup("missing") ?? "fallback"`, "fallback"},
		{`lookupBuilt("known") ?? "fallback"`, "found-it"},
		{`lookupBuilt("missing") ?? "fallback"`, "fallback"},
	}
	for _, tt := range cases {
		t.Run(tt.input, func(t *testing.T) {
			got := evalCoalesce(t, env, tt.input)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCallReflectFuncBareErrorReturn is the fix itself. Before it, a bare
// `func(...) error` return's ONLY broken case was failure: the non-nil
// error was handed back as if it were the call's data value (an
// *errors.errorString "result"), with no Go error raised at all - which
// meant "??" couldn't catch it either, since nothing had actually failed
// as far as the VM could tell. (The success case - out[0].Interface() on
// a nil error - already produced a plain nil both before and after this
// fix; that part was never broken, since a nil interface value already
// unwraps to nil with no special-casing needed. A function with no data
// value naturally has nothing to offer on success, and "??" coalescing
// that nil away is its ordinary documented behavior, unrelated to this
// fix.)
func TestCallReflectFuncBareErrorReturn(t *testing.T) {
	env := map[string]any{"bareError": bareError}

	t.Run("success (nil error) produces nil, not an error", func(t *testing.T) {
		got := evalCoalesce(t, env, `bareError(false)`)
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("failure (non-nil error) now propagates as a Go error, not as data", func(t *testing.T) {
		instructions, err := owlexpr.Compile(`bareError(true)`)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		mc, err := owlexpr.NewVM()
		if err != nil {
			t.Fatalf("NewVM: %v", err)
		}
		if _, err := mc.Run(instructions, env); err == nil {
			t.Fatal("expected bareError(true) to propagate as an error, got none")
		}
	})

	t.Run("?? now catches that failure and falls back", func(t *testing.T) {
		if got := evalCoalesce(t, env, `bareError(true) ?? "fallback"`); got != "fallback" {
			t.Errorf("got %v, want fallback (call failed)", got)
		}
	})
}
