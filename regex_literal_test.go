package owlexpr

import (
	"strings"
	"testing"

	"github.com/cms103/owlexpr/vm"
)

// TestRegexLiteralIsRaw: the body of re`...` gets no escape processing,
// so regex escapes like \d and \. are written once, not doubled up the
// way a string-literal pattern needs.
func TestRegexLiteralIsRaw(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"digit class", "\"a12\" matches re`^a\\d+$`", true},
		{"digit class no match", "\"abc\" matches re`^a\\d+$`", false},
		{"escaped dot", "\"a.b\" matches re`^a\\.b$`", true},
		{"escaped dot is literal", "\"axb\" matches re`^a\\.b$`", false},
		{"quotes need no escaping", "'say \"hi\"' matches re`\"hi\"`", true},
		{"backtick via hex escape", "\"a`b\" matches re`a\\x60b`", true},
		{"named group syntax", "\"GET\" matches re`^(?P<verb>[A-Z]+)$`", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := evalRoot(t, nil, tt.input); got != tt.want {
				t.Errorf("%s = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestRegexLiteralCompiledOnce: a regex literal compiles to a single
// *vm.Regex OpPush constant at Compile() time, so every Run shares the
// same compiled value - no per-run compilation, nothing to cache.
func TestRegexLiteralCompiledOnce(t *testing.T) {
	instructions, err := Compile("let r = re`^[A-Z]`; name matches r")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	var consts []*vm.Regex
	for _, inst := range instructions {
		if re, ok := inst.Arg.(*vm.Regex); ok && inst.Op == vm.OpPush {
			consts = append(consts, re)
		}
	}
	if len(consts) != 1 || consts[0].String() != "^[A-Z]" {
		t.Fatalf("want exactly one *vm.Regex constant for ^[A-Z], got %v", consts)
	}

	mc, err := NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	for name, want := range map[string]bool{"Alice": true, "bob": false} {
		res, err := mc.Run(instructions, map[string]any{"name": name})
		if err != nil {
			t.Fatalf("Run(%q): %v", name, err)
		}
		if res != want {
			t.Errorf("name=%q: got %v, want %v", name, res, want)
		}
	}
}

// TestRegexLiteralCompileErrors: a malformed pattern or an unterminated
// literal is reported by Compile, never deferred to Run.
func TestRegexLiteralCompileErrors(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{"invalid pattern", "re`[`", "invalid regex literal"},
		{"invalid pattern nested in call", "len([re`(`])", "invalid regex literal"},
		{"unterminated", "\"a\" matches re`abc", "unterminated regex literal"},
		{"unterminated in trailing position", "x re`abc", "unterminated regex literal"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile(tt.input)
			if err == nil {
				t.Fatalf("Compile(%q) succeeded, want an error", tt.input)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("Compile(%q) error = %q, want it to contain %q", tt.input, err, tt.wantMsg)
			}
		})
	}
}

// TestReStillAnIdentifier: only `re` immediately followed by a backtick
// is a regex literal - `re` alone stays an ordinary name, including as a
// let binding holding a regex.
func TestReStillAnIdentifier(t *testing.T) {
	if got := evalRoot(t, map[string]any{"re": int64(5)}, "re + 1"); got != int64(6) {
		t.Errorf("re + 1 = %v, want 6", got)
	}
	if got := evalRoot(t, nil, "let re = re`^x`; \"xy\" matches re"); got != true {
		t.Errorf("let re = re`^x`; ... = %v, want true", got)
	}
}

// TestRegexValueSurface: a regex value reports its type as "regex",
// exposes only read-only methods, and never exposes the wrapped
// *regexp.Regexp (whose Longest() would mutate a shared constant).
func TestRegexValueSurface(t *testing.T) {
	if got := evalRoot(t, nil, "type(re`a`)"); got != "regex" {
		t.Errorf("type(re`a`) = %v, want \"regex\"", got)
	}
	if got := evalRoot(t, nil, "re`a+b`.String()"); got != "a+b" {
		t.Errorf("re`a+b`.String() = %v, want \"a+b\"", got)
	}
	evalRootExpectError(t, nil, "re`a`.re")
	evalRootExpectError(t, nil, "re`a`.Longest()")
}

// TestUnexportedStructFieldIsNotFound: member access naming an unexported
// field is a "not found" error, not a reflect panic.
func TestUnexportedStructFieldIsNotFound(t *testing.T) {
	type withHidden struct {
		Visible string
		hidden  string
	}
	env := map[string]any{"x": &withHidden{Visible: "v", hidden: "h"}}
	if got := evalRoot(t, env, "x.Visible"); got != "v" {
		t.Errorf("x.Visible = %v, want \"v\"", got)
	}
	evalRootExpectError(t, env, "x.hidden")
}

// TestRegexLiteralInSheet: regex literals are leaves to sheetReferences
// (no dependency edges, no panic) and work in a cell like anywhere else.
func TestRegexLiteralInSheet(t *testing.T) {
	res := runSheetT(t, map[string]any{"code": "AB-12"}, []CellDef{
		{Name: "pattern", Expression: "re`^[A-Z]+-\\d+$`"},
		{Name: "valid", Expression: "code matches sheet.pattern"},
	})
	if res["valid"] != true {
		t.Fatalf("valid = %v, want true (res %+v)", res["valid"], res)
	}
}
