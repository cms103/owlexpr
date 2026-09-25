package stdlib

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/cms103/owlexpr"
	"github.com/cms103/owlexpr/vm"
)

// evalRegex parses, compiles, and runs input with RegexBuiltins() (plus
// StringBuiltins(), for composing with string.*) enabled, failing the
// test on any parse/compile/run error.
func evalRegex(t *testing.T, env map[string]any, input string) any {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("compile error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(RegexBuiltins(), StringBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	res, err := mc.Run(instructions, env)
	if err != nil {
		t.Fatalf("run error for %q: %v", input, err)
	}
	return res
}

// evalRegexErr is evalRegex for an input expected to fail at run time,
// returning the error so its message can be checked.
func evalRegexErr(t *testing.T, env map[string]any, input string) error {
	t.Helper()
	instructions, err := owlexpr.Compile(input)
	if err != nil {
		t.Fatalf("compile error for %q: %v", input, err)
	}
	mc, err := owlexpr.NewVM(RegexBuiltins())
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	_, err = mc.Run(instructions, env)
	if err == nil {
		t.Fatalf("expected an error for %q, got none", input)
	}
	return err
}

func TestRegexBuiltinsNotRegisteredByDefault(t *testing.T) {
	mc, err := owlexpr.NewVM()
	if err != nil {
		t.Fatalf("NewVM: %v", err)
	}
	instructions, err := owlexpr.Compile("regex.find(re`a`, \"a\")")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := mc.Run(instructions, nil); err == nil {
		t.Fatalf("expected regex.find() to be undefined without RegexBuiltins()")
	}
}

func TestRegexBuiltinsInAll(t *testing.T) {
	mc, err := owlexpr.NewVM(All())
	if err != nil {
		t.Fatalf("NewVM(All()): %v", err)
	}
	if got := evalAll(t, mc, nil, "regex.find(re`\\d+`, \"ab12cd\")"); got != "12" {
		t.Errorf("regex.find via All() = %v, want \"12\"", got)
	}
}

func TestRegexBuiltins(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  any
	}{
		// find
		{"find first", "regex.find(re`\\d+`, \"a1b22c333\")", "1"},
		{"find none", "regex.find(re`\\d+`, \"abc\")", nil},
		{"find empty match", "regex.find(re`x*`, \"abc\")", ""},

		// findAll
		{"findAll", "regex.findAll(re`\\d+`, \"a1b22c333\")", []any{"1", "22", "333"}},
		{"findAll limit", "regex.findAll(re`\\d+`, \"a1b22c333\", 2)", []any{"1", "22"}},
		{"findAll none is empty list", "regex.findAll(re`\\d+`, \"abc\")", []any{}},
		{"findAll limit zero", "regex.findAll(re`\\d+`, \"a1\", 0)", []any{}},

		// groups
		{"groups", "regex.groups(re`(\\w+)@(\\w+)`, \"me@host\")", []any{"me@host", "me", "host"}},
		{"groups no match", "regex.groups(re`(\\w+)@(\\w+)`, \"nope\")", nil},
		{"groups unmatched optional is nil", "regex.groups(re`(a)?b`, \"b\")", []any{"b", nil}},
		{"groups matched empty is empty string", "regex.groups(re`(a*)b`, \"b\")", []any{"b", ""}},

		// capture
		{
			"capture logstash request",
			"regex.capture(re`^(?P<verb>\\S+) (?P<path>\\S+) HTTP/(?P<ver>.+)$`, \"GET /index.html HTTP/1.1\")",
			map[string]any{"verb": "GET", "path": "/index.html", "ver": "1.1"},
		},
		{"capture no match", "regex.capture(re`^(?P<verb>[A-Z]+)$`, \"get\")", nil},
		{"capture (?<name>) syntax", "regex.capture(re`(?<y>\\d{4})-(?<m>\\d{2})`, \"on 2026-09\")", map[string]any{"y": "2026", "m": "09"}},
		{"capture skips unnamed groups", "regex.capture(re`(\\d+)-(?P<b>\\d+)`, \"1-2\")", map[string]any{"b": "2"}},
		{"capture no named groups is empty map", "regex.capture(re`\\d`, \"1\")", map[string]any{}},
		{"capture unmatched optional is nil", "regex.capture(re`(?P<a>x)?y`, \"y\")", map[string]any{"a": nil}},
		{"capture duplicate name takes matched branch", "regex.capture(re`(?P<n>a)|(?P<n>b)`, \"a\")", map[string]any{"n": "a"}},
		{"capture member access", "regex.capture(re`^(?P<verb>\\S+) `, \"POST /x\").verb", "POST"},
		{"capture optional chain on no match", "regex.capture(re`^(?P<verb>[A-Z]+) `, \"nope\")?.verb ?? \"none\"", "none"},

		// captureAll
		{
			"captureAll",
			"regex.captureAll(re`(?P<k>\\w+)=(?P<v>\\w+)`, \"a=1 b=2\")",
			[]any{map[string]any{"k": "a", "v": "1"}, map[string]any{"k": "b", "v": "2"}},
		},
		{"captureAll limit", "len(regex.captureAll(re`(?P<k>\\w+)=(?P<v>\\w+)`, \"a=1 b=2 c=3\", 2))", 2},
		{"captureAll none", "regex.captureAll(re`(?P<k>\\w+)=`, \"\")", []any{}},

		// replace
		{"replace template", "regex.replace(re`(\\w+)@(\\w+)`, \"me@host\", \"$2 at ${1}\")", "host at me"},
		{"replace named template", "regex.replace(re`(?P<user>\\w+)@`, \"me@host\", \"${user}#\")", "me#host"},
		{"replace dollar escape", "regex.replace(re`\\d`, \"a1\", \"$$\")", "a$"},
		{"replace all", "regex.replace(re`\\s+`, \"a  b \\t c\", \" \")", "a b c"},
		{"replace no match unchanged", "regex.replace(re`\\d`, \"abc\", \"x\")", "abc"},
		{"replace with function", "regex.replace(re`[a-z]+`, \"ab-cd\", m => string.upper(m))", "AB-CD"},

		// replaceLiteral
		{"replaceLiteral no expansion", "regex.replaceLiteral(re`(\\d)`, \"a1\", \"$1\")", "a$1"},

		// split
		{"split", "regex.split(re`\\s*,\\s*`, \"a , b,c\")", []any{"a", "b", "c"}},
		{"split limit", "regex.split(re`,`, \"a,b,c\", 2)", []any{"a", "b,c"}},
		{"split no match", "regex.split(re`,`, \"abc\")", []any{"abc"}},

		// pattern
		{"pattern", "regex.pattern(re`^a\\d$`)", "^a\\d$"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := evalRegex(t, nil, tt.input)
			if n, ok := tt.want.(int); ok {
				tt.want = int64(n)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%s = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

// TestRegexBuiltinsAcceptEnvRegex: a regex an embedder builds in Go and
// hands over via vm.NewRegex works exactly like a literal.
func TestRegexBuiltinsAcceptEnvRegex(t *testing.T) {
	env := map[string]any{"re": vm.NewRegex(regexp.MustCompile(`(?P<id>\d+)`))}
	got := evalRegex(t, env, `regex.capture(re, "order 42").id`)
	if got != "42" {
		t.Errorf("capture with env regex = %v, want \"42\"", got)
	}
}

func TestRegexBuiltinsErrors(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantMsg string
	}{
		{"string pattern rejected", `regex.find("\\d+", "a1")`, "must be a regex (re`...`), got a string"},
		{"non-regex first arg", "regex.find(1, \"a\")", "must be a regex, got int64"},
		{"non-string subject", "regex.find(re`a`, 1)", "argument 2 must be a string"},
		{"too few args", "regex.find(re`a`)", "expects 2 arguments"},
		{"too many args", "regex.findAll(re`a`, \"a\", 1, 2)", "expects 2 or 3 arguments"},
		{"negative limit", "regex.split(re`,`, \"a,b\", -1)", "must not be negative"},
		{"non-int limit", "regex.split(re`,`, \"a,b\", \"2\")", "must be an integer"},
		{"bad replacement type", "regex.replace(re`a`, \"a\", 1)", "must be a string or a function"},
		{"replacement function non-string", "regex.replace(re`a`, \"a\", m => 1)", "must return a string"},
		{"replacement function error", "regex.replace(re`a`, \"a\", m => m.nope)", "nope"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := evalRegexErr(t, nil, tt.input)
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("%s error = %q, want it to contain %q", tt.input, err, tt.wantMsg)
			}
		})
	}
}

// TestRegexToJSON: a regex value encodes as its pattern text.
func TestRegexToJSON(t *testing.T) {
	mc, err := owlexpr.NewVM(All())
	if err != nil {
		t.Fatalf("NewVM(All()): %v", err)
	}
	got := evalAll(t, mc, nil, "json.toJSON({p: re`a+`})")
	if got != `{"p":"a+"}` {
		t.Errorf("json.toJSON({p: re`a+`}) = %v, want {\"p\":\"a+\"}", got)
	}
}
