package owlexpr

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// --- AST Node Definitions ---

type Expr interface{}

type NumberNode struct{ Value any }
type StringNode struct{ Value string }
type BoolNode struct{ Value bool }

// RegexNode is a regex literal: re`pattern`. Pattern is the raw source
// text between the backticks - no escape processing at all, so `\d` means
// what it means to the regex engine without the double-escaping a string
// literal would need. The compiler compiles it once, at Compile() time,
// into a *vm.Regex constant (an invalid pattern is a compile error).
type RegexNode struct{ Pattern string }

// NilNode is the "nil" literal. It carries no fields - there's only one
// nil - and compiles to the same OpPush(nil) any other literal does.
type NilNode struct{}

type VarNode struct{ Name string }

// UnaryOpNode represents a prefix operator: -x, +x, !x
type UnaryOpNode struct {
	Op      string
	Operand Expr
}

type BinaryOpNode struct {
	Op    string
	Left  Expr
	Right Expr
}

type CallNode struct {
	Callee Expr
	Args   []Expr
}

type MemberAccessNode struct {
	Expr   Expr
	Member string
}

// OptMemberAccessNode is a postfix optional-chain member access: target?.member.
// Unlike MemberAccessNode, a nil (or typed-nil, per isNilResult) target
// produces nil instead of erroring. Chaining several ("a?.b?.c") cascades
// correctly with no cross-node coordination needed: each node's Target is
// simply the previous node's already-nil-or-value result. This is
// deliberately narrower than JS's "?." poisons the rest of the chain"
// semantics - a plain "." after a "?." still errors on nil, same as
// today - since that gap is already covered by wrapping the whole
// expression in the existing "??" operator when a hard default is wanted.
type OptMemberAccessNode struct {
	Expr   Expr
	Member string
}

// OptIndexNode is a postfix optional-chain index access: target?.[index].
// Same nil-short-circuits-to-nil rule as OptMemberAccessNode, one level
// only - it does not also guard against Index itself producing an error
// (e.g. an out-of-range index on a non-nil target still errors normally).
type OptIndexNode struct {
	Target Expr
	Index  Expr
}

type TernaryNode struct {
	Cond Expr
	Then Expr
	Else Expr
}

// ListNode is a list literal: [a, b, c].
type ListNode struct{ Elements []Expr }

// MapNode is a map literal: {a: 1, b: 2}. Keys are parsed as bare
// identifiers or string literals, both of which compile down to a
// StringNode - there is no support for computed keys.
type MapNode struct {
	Keys   []Expr
	Values []Expr
}

// IndexNode is a postfix index expression: target[index].
type IndexNode struct {
	Target Expr
	Index  Expr
}

// SliceNode is a Go-style slice expression: target[low:high], where Low
// and/or High may be nil when omitted (target[1:], target[:3], target[:]).
// There's no 3-index (capacity-controlling) form.
type SliceNode struct {
	Target Expr
	Low    Expr
	High   Expr
}

// LambdaNode is an arrow-function literal: x => expr, or (a, b) => expr.
// It's always a single expression body - there's no statement/block form.
type LambdaNode struct {
	Params []string
	Body   Expr
}

// LetNode is a local binding: `let name = value; body`. Bindings are
// pure - each `let` introduces a fresh binding, never mutates an
// existing one - and scoped to `body` only: they shadow, but never
// overwrite, an environment variable of the same name, and they never
// leak past `body` (not to sibling call arguments, not to anything
// after the let expression as a whole ends).
type LetNode struct {
	Name  string
	Value Expr
	Body  Expr
}

type IfNode struct {
	Cond Expr
	Then Expr
	Else Expr
}

// --- Lexer / Tokenizer ---

type TokenType int

const (
	TokEOF TokenType = iota
	TokNumber
	TokString
	TokIdent
	TokBool
	TokOp
	TokLParen
	TokRParen
	TokLBrace
	TokRBrace
	TokLBracket
	TokRBracket
	TokQuestion
	TokOptDot
	TokColon
	TokComma
	TokDot
	TokArrow
	TokSemicolon
	TokIf
	TokElse
	TokLet
	TokNil
	TokRegex
	// TokIllegal is a lexing error (e.g. an unterminated regex literal),
	// with the message in Val - surfaced as a parse error by parsePrefix.
	TokIllegal
)

type Token struct {
	Type    TokenType
	Val     string
	Literal any
}

type Lexer struct {
	input []rune
	pos   int
}

func NewLexer(input string) *Lexer {
	return &Lexer{input: []rune(input)}
}

// twoCharOps is the closed set of operators that are allowed to span two
// characters. Anything else made of "op chars" is exactly one character.
// This is what stops something like "1+-2" from being lexed as a single,
// meaningless "+-" token: '+' and '-' are each valid on their own, but
// "+-" is not a recognized operator, so it must not be glued together.
var twoCharOps = map[string]bool{
	"==": true,
	"!=": true,
	"<=": true,
	">=": true,
	"&&": true,
	"||": true,
	"**": true,
}

func (l *Lexer) NextToken() Token {
	l.skipWhitespace()
	if l.pos >= len(l.input) {
		return Token{Type: TokEOF}
	}

	ch := l.input[l.pos]

	switch ch {
	case '(':
		l.pos++
		return Token{Type: TokLParen, Val: "("}
	case ')':
		l.pos++
		return Token{Type: TokRParen, Val: ")"}
	case '{':
		l.pos++
		return Token{Type: TokLBrace, Val: "{"}
	case '}':
		l.pos++
		return Token{Type: TokRBrace, Val: "}"}
	case '[':
		l.pos++
		return Token{Type: TokLBracket, Val: "["}
	case ']':
		l.pos++
		return Token{Type: TokRBracket, Val: "]"}
	case '?':
		if l.pos+1 < len(l.input) && l.input[l.pos+1] == '?' {
			l.pos += 2
			return Token{Type: TokOp, Val: "??"}
		}
		if l.pos+1 < len(l.input) && l.input[l.pos+1] == '.' {
			l.pos += 2
			return Token{Type: TokOptDot, Val: "?."}
		}
		l.pos++
		return Token{Type: TokQuestion, Val: "?"}
	case ':':
		l.pos++
		return Token{Type: TokColon, Val: ":"}
	case ';':
		l.pos++
		return Token{Type: TokSemicolon, Val: ";"}
	case ',':
		l.pos++
		return Token{Type: TokComma, Val: ","}
	case '.':
		l.pos++
		return Token{Type: TokDot, Val: "."}
	case '"', '\'':
		return l.readString(ch)
	}

	if unicode.IsDigit(ch) {
		return l.readNumber()
	}

	if isIdentStart(ch) {
		return l.readIdent()
	}

	if isOpChar(ch) {
		// Only merge into a two-character operator if that exact pair is a
		// known operator. Otherwise emit a single-character op token and
		// let the next call to NextToken lex whatever follows on its own
		// terms (this is what lets "-" after "+" be seen as its own,
		// separate unary-minus token instead of being swallowed).
		if l.pos+1 < len(l.input) {
			pair := string(l.input[l.pos : l.pos+2])
			// "=>" gets its own token type rather than joining twoCharOps:
			// unlike ==, !=, etc. it isn't a general infix operator - it
			// only ever appears in one place (a lambda literal), and is
			// recognized there by dedicated parser logic, not the infix
			// binding-power table.
			if pair == "=>" {
				l.pos += 2
				return Token{Type: TokArrow, Val: pair}
			}
			if twoCharOps[pair] {
				l.pos += 2
				return Token{Type: TokOp, Val: pair}
			}
		}
		l.pos++
		return Token{Type: TokOp, Val: string(ch)}
	}

	l.pos++
	return Token{Type: TokOp, Val: string(ch)}
}

// skipWhitespace skips both whitespace and comments ("// to end of line"
// and "/* ... */", non-nesting) - looping rather than a single pass since
// either can be followed by more of either (whitespace, then a comment,
// then more whitespace, ...). An unterminated "/*" consumes to EOF rather
// than looping forever looking for a "*/" that never arrives.
func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) {
		if unicode.IsSpace(l.input[l.pos]) {
			l.pos++
			continue
		}
		if l.pos+1 < len(l.input) && l.input[l.pos] == '/' && l.input[l.pos+1] == '/' {
			l.pos += 2
			for l.pos < len(l.input) && l.input[l.pos] != '\n' {
				l.pos++
			}
			continue
		}
		if l.pos+1 < len(l.input) && l.input[l.pos] == '/' && l.input[l.pos+1] == '*' {
			l.pos += 2
			for l.pos+1 < len(l.input) && !(l.input[l.pos] == '*' && l.input[l.pos+1] == '/') {
				l.pos++
			}
			if l.pos+1 < len(l.input) {
				l.pos += 2 // consume closing "*/"
			} else {
				l.pos = len(l.input) // unterminated - consume to EOF
			}
			continue
		}
		break
	}
}

// readString reads a quoted string literal, processing backslash escapes
// (\n, \t, \r, \\, \", \') along the way. A literal, unescaped newline byte
// in the source passes through as-is - nothing here rejects it - so a
// string can span multiple lines by containing one directly; \n is what
// lets a caller express one from a source that's itself constrained to a
// single line (a single-line UI field, a JSON value, ...). An unrecognized
// escape (\d, say) is kept as both characters literally - the backslash is
// not dropped - so an escape sequence outside the recognized set is inert
// rather than silently mangled.
func (l *Lexer) readString(quote rune) Token {
	l.pos++ // consume opening quote

	var sb strings.Builder
	for l.pos < len(l.input) && l.input[l.pos] != quote {
		ch := l.input[l.pos]
		if ch == '\\' && l.pos+1 < len(l.input) {
			l.pos++
			switch escaped := l.input[l.pos]; escaped {
			case 'n':
				sb.WriteRune('\n')
			case 't':
				sb.WriteRune('\t')
			case 'r':
				sb.WriteRune('\r')
			case '\\', '"', '\'':
				sb.WriteRune(escaped)
			default:
				sb.WriteRune('\\')
				sb.WriteRune(escaped)
			}
			l.pos++
			continue
		}
		sb.WriteRune(ch)
		l.pos++
	}

	strVal := sb.String()
	if l.pos < len(l.input) {
		l.pos++ // consume closing quote
	}
	return Token{Type: TokString, Val: strVal, Literal: strVal}
}

func (l *Lexer) readNumber() Token {
	start := l.pos
	isFloat := false
	for l.pos < len(l.input) && (unicode.IsDigit(l.input[l.pos]) || l.input[l.pos] == '.') {
		if l.input[l.pos] == '.' {
			if isFloat {
				break
			}
			isFloat = true
		}
		l.pos++
	}
	raw := string(l.input[start:l.pos])
	if isFloat {
		v, _ := strconv.ParseFloat(raw, 64)
		return Token{Type: TokNumber, Val: raw, Literal: v}
	}
	v, _ := strconv.ParseInt(raw, 10, 64)
	return Token{Type: TokNumber, Val: raw, Literal: v}
}

func (l *Lexer) readIdent() Token {
	start := l.pos
	for l.pos < len(l.input) && isIdentPart(l.input[l.pos]) {
		l.pos++
	}
	val := string(l.input[start:l.pos])

	// `re` immediately followed by a backtick opens a regex literal. A
	// backtick has no other meaning in the language, so this never
	// changes how any previously valid expression lexes - `re` on its
	// own is still an ordinary identifier.
	if val == "re" && l.pos < len(l.input) && l.input[l.pos] == '`' {
		return l.readRegex()
	}

	switch val {
	case "if":
		return Token{Type: TokIf, Val: val}
	case "else":
		return Token{Type: TokElse, Val: val}
	case "let":
		return Token{Type: TokLet, Val: val}
	case "true":
		return Token{Type: TokBool, Val: val, Literal: true}
	case "false":
		return Token{Type: TokBool, Val: val, Literal: false}
	case "nil":
		return Token{Type: TokNil, Val: val}
	case "and", "or", "in", "not", "matches":
		return Token{Type: TokOp, Val: val}
	default:
		return Token{Type: TokIdent, Val: val}
	}
}

// readRegex reads the body of a regex literal (re`...`), starting at its
// opening backtick. The body is raw - backslashes are passed through
// untouched for the regex engine to interpret - so it can't contain a
// backtick itself (match one with \x60). Unlike readString, a missing
// closing delimiter is an error rather than consuming to EOF: a regex
// silently extended to the end of the input would match something quite
// different from what was written.
func (l *Lexer) readRegex() Token {
	l.pos++ // consume opening backtick
	start := l.pos
	for l.pos < len(l.input) && l.input[l.pos] != '`' {
		l.pos++
	}
	if l.pos >= len(l.input) {
		return Token{Type: TokIllegal, Val: "unterminated regex literal: missing closing '`'"}
	}
	pattern := string(l.input[start:l.pos])
	l.pos++ // consume closing backtick
	return Token{Type: TokRegex, Val: pattern, Literal: pattern}
}

func isIdentStart(ch rune) bool {
	return unicode.IsLetter(ch) || ch == '_'
}

func isIdentPart(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_'
}

func isOpChar(ch rune) bool {
	switch ch {
	case '+', '-', '*', '/', '%', '=', '!', '<', '>', '&', '|':
		return true
	default:
		return false
	}
}

// --- Pratt Parser ---

type Precedence int

const (
	LOWEST            Precedence = iota
	PREC_TERNARY                 // ?:
	PREC_NIL_COALESCE            // ??
	PREC_OR                      // or, ||
	PREC_AND                     // and, &&
	PREC_EQUALS                  // ==, !=
	PREC_LESSGREATER             // >, <, >=, <=
	PREC_SUM                     // +, -
	PREC_PRODUCT                 // *, /, %
	PREC_EXPONENT                // **
	PREC_PREFIX                  // unary -, +, !
	PREC_CALL                    // .member, func(...)
)

// bindingPower gives an infix/postfix operator two independent precedence
// numbers rather than one, because left- and right-associativity can't both
// be expressed with a single shared value:
//
//   - lbp (left binding power): how strongly an operator binds to the
//     expression on its LEFT. This is what the infix loop compares against
//     the current call's precedence "floor" to decide whether to keep
//     absorbing tokens into `left`, versus returning and letting an outer,
//     lower-precedence call take over.
//
//   - rbp (right binding power): the precedence floor handed to the
//     recursive call that parses the expression on the RIGHT of the
//     operator. This is what actually encodes associativity:
//
//   - left-associative operator: rbp == lbp, so a following operator of
//     the same precedence is NOT absorbed by the right-hand recursion,
//     and instead falls back out to the enclosing loop, which attaches
//     it on the left (`(a+b)+c`, not `a+(b+c)`).
//
//   - right-associative operator: rbp == lbp - 1, so a following
//     operator of the same precedence IS absorbed into the right-hand
//     recursion instead of being handed back to the caller
//     (`a?b:c?d:e` becomes `a?b:(c?d:e)`, not `(a?b:c)?d:e`).
//
// A single shared number per operator works for the purely left-associative
// arithmetic operators, but can't also express the two operators that need
// to be right-associative: chained ternaries (see parseTernary) and `**`.
type bindingPower struct {
	lbp Precedence
	rbp Precedence
}

var infixBindingPowers = map[string]bindingPower{
	".":  {PREC_CALL, PREC_CALL},
	"?.": {PREC_CALL, PREC_CALL},
	"(":  {PREC_CALL, PREC_CALL},
	"[":  {PREC_CALL, PREC_CALL},
	"?":  {PREC_TERNARY, PREC_TERNARY}, // rbp handled specially in parseTernary (right-assoc)
	// "??" is right-associative (a ?? b ?? c == a ?? (b ?? c)), via the
	// same rbp=lbp-1 trick as "?:". Binding it above PREC_TERNARY but
	// below PREC_OR means its fallback (right) side can itself be a full
	// or/and/comparison/arithmetic expression with no parens needed
	// ("a ?? b || c" == "a ?? (b || c)"), while the coalesced result
	// still composes into an enclosing ternary condition with no parens
	// either ("a ?? b ? c : d" == "(a ?? b) ? c : d").
	"??":      {PREC_NIL_COALESCE, PREC_NIL_COALESCE - 1},
	"||":      {PREC_OR, PREC_OR},
	"or":      {PREC_OR, PREC_OR},
	"&&":      {PREC_AND, PREC_AND},
	"and":     {PREC_AND, PREC_AND},
	"==":      {PREC_EQUALS, PREC_EQUALS},
	"!=":      {PREC_EQUALS, PREC_EQUALS},
	"<":       {PREC_LESSGREATER, PREC_LESSGREATER},
	">":       {PREC_LESSGREATER, PREC_LESSGREATER},
	"<=":      {PREC_LESSGREATER, PREC_LESSGREATER},
	">=":      {PREC_LESSGREATER, PREC_LESSGREATER},
	"in":      {PREC_LESSGREATER, PREC_LESSGREATER},
	"matches": {PREC_LESSGREATER, PREC_LESSGREATER},
	// "not" only has infix binding power so that "a not in b" can be
	// recognized here at all (the infix loop wouldn't even look at a
	// token with no entry in this map - it'd just stop). It's not a
	// real operator on its own; the TokOp case below requires the very
	// next token to be "in" and desugars the pair straight into
	// !(a in b), so it shares "in"'s precedence rather than getting a
	// precedence of its own.
	"not": {PREC_LESSGREATER, PREC_LESSGREATER},
	"+":       {PREC_SUM, PREC_SUM},
	"-":       {PREC_SUM, PREC_SUM},
	"*":       {PREC_PRODUCT, PREC_PRODUCT},
	"/":       {PREC_PRODUCT, PREC_PRODUCT},
	"%":       {PREC_PRODUCT, PREC_PRODUCT},
	// "**" is right-associative (2**3**2 == 2**(3**2) == 512, not
	// (2**3)**2 == 64), via the same rbp=lbp-1 trick as "?:" below, and
	// binds tighter than * / % (2*3**2 == 2*9 == 18).
	"**": {PREC_EXPONENT, PREC_EXPONENT - 1},
}

// prefixOps is the set of operators allowed in prefix (unary) position, and
// their binding power on the right. PREC_PREFIX sits between PREC_EXPONENT
// and PREC_CALL, so "-a*b" parses as "(-a)*b" (unary binds tighter than
// binary `*`), "-a.b" parses as "-(a.b)" (member access binds tighter than
// unary), and "-a**b" parses as "(-a)**b" (unary binds tighter than `**`
// too - unlike Python, where "**" binds tighter than unary minus; this
// keeps unary consistent with how it already relates to every other
// binary operator here, rather than carving out an exception for one).
var prefixOps = map[string]Precedence{
	"-":   PREC_PREFIX,
	"+":   PREC_PREFIX,
	"!":   PREC_PREFIX,
	"not": PREC_PREFIX, // exact synonym for "!", same precedence - like "and"/"or" are for "&&"/"||", not Python's looser-than-comparisons "not"
}

type PrattParser struct {
	lexer     *Lexer
	curToken  Token
	peekToken Token
}

func NewPrattParser(lexer *Lexer) *PrattParser {
	p := &PrattParser{lexer: lexer}
	p.nextToken()
	p.nextToken()
	return p
}

func (p *PrattParser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.lexer.NextToken()
}

func (p *PrattParser) curLBP() Precedence {
	if bp, ok := infixBindingPowers[p.curToken.Val]; ok {
		return bp.lbp
	}
	return LOWEST
}

func (p *PrattParser) curRBP() Precedence {
	if bp, ok := infixBindingPowers[p.curToken.Val]; ok {
		return bp.rbp
	}
	return LOWEST
}

func (p *PrattParser) ParseExpression(precedence Precedence) (Expr, error) {
	left, err := p.parsePrefix()
	if err != nil {
		return nil, err
	}

	// Infix & Postfix Loop Parsing
	for p.curToken.Type != TokEOF && precedence < p.curLBP() {
		switch p.curToken.Type {
		case TokOp:
			op := p.curToken.Val
			if op == "not" {
				// Infix "not" only ever legally continues as "not in"
				// (a not in b == !(a in b)); prefix "not"/"!" negation
				// is handled entirely by parsePrefix and never reaches
				// here. Desugaring immediately into existing AST nodes
				// means the compiler and VM never need to know "not in"
				// exists as its own syntax.
				if p.peekToken.Type != TokOp || p.peekToken.Val != "in" {
					return nil, fmt.Errorf("expected 'in' after 'not', got %q", p.peekToken.Val)
				}
				rbp := p.curRBP()
				p.nextToken() // consume "not"
				p.nextToken() // consume "in"
				right, err := p.ParseExpression(rbp)
				if err != nil {
					return nil, err
				}
				left = UnaryOpNode{Op: "!", Operand: BinaryOpNode{Op: "in", Left: left, Right: right}}
				break
			}
			rbp := p.curRBP()
			p.nextToken()
			right, err := p.ParseExpression(rbp)
			if err != nil {
				return nil, err
			}
			left = BinaryOpNode{Op: op, Left: left, Right: right}

		case TokDot:
			p.nextToken()
			if p.curToken.Type != TokIdent {
				return nil, fmt.Errorf("expected identifier after '.'")
			}
			member := p.curToken.Val
			p.nextToken()
			left = MemberAccessNode{Expr: left, Member: member}

		case TokOptDot:
			// "?.member" (optional member access) or "?.[index]" (optional
			// index access) - the two postfix forms a plain "." and "["
			// already support, guarded so a nil target produces nil
			// instead of erroring. There's no "?.(" optional-call form:
			// calling a possibly-nil function value isn't a case this
			// language's target domain (traversing possibly-absent struct/
			// map data) actually hits.
			p.nextToken()
			if p.curToken.Type == TokLBracket {
				p.nextToken() // consume '['
				index, err := p.ParseExpression(LOWEST)
				if err != nil {
					return nil, err
				}
				if p.curToken.Type != TokRBracket {
					return nil, fmt.Errorf("expected ']' after '?.[' index expression, got %q", p.curToken.Val)
				}
				p.nextToken()
				left = OptIndexNode{Target: left, Index: index}
				break
			}
			if p.curToken.Type != TokIdent {
				return nil, fmt.Errorf("expected identifier or '[' after '?.'")
			}
			member := p.curToken.Val
			p.nextToken()
			left = OptMemberAccessNode{Expr: left, Member: member}

		case TokLParen:
			// Postfix '(' is a function call on whatever we've parsed so
			// far (`left`). This is distinct from '(' in prefix position,
			// which is a grouping parenthesis with no preceding operand
			// (see parsePrefix). The two are told apart purely by
			// position, same as in ordinary expression grammars.
			p.nextToken()
			args, err := p.parseCallArguments()
			if err != nil {
				return nil, err
			}
			left = CallNode{Callee: left, Args: args}

		case TokLBracket:
			// Postfix '[' is an index or slice operation on whatever
			// we've parsed so far (`left`), e.g. `alist[2]`, `amap["b"]`,
			// or `alist[1:3]`. As with the call-postfix '(' above, this
			// is distinct from '[' in prefix position, which starts a
			// list literal (see parsePrefix); the two are told apart
			// purely by position.
			//
			// Go-style slicing: '[' (Expr)? (':' (Expr)?)? ']'. Both the
			// low and high bounds are optional independently of each
			// other (alist[1:], alist[:3], alist[:]), and there's no
			// 3-index (capacity-controlling) form.
			p.nextToken() // consume '['

			var low Expr
			if p.curToken.Type != TokColon && p.curToken.Type != TokRBracket {
				var err error
				low, err = p.ParseExpression(LOWEST)
				if err != nil {
					return nil, err
				}
			}

			if p.curToken.Type == TokColon {
				p.nextToken() // consume ':'
				var high Expr
				if p.curToken.Type != TokRBracket {
					var err error
					high, err = p.ParseExpression(LOWEST)
					if err != nil {
						return nil, err
					}
				}
				if p.curToken.Type != TokRBracket {
					return nil, fmt.Errorf("expected ']' after slice expression, got %q", p.curToken.Val)
				}
				p.nextToken()
				left = SliceNode{Target: left, Low: low, High: high}
			} else {
				if low == nil {
					return nil, fmt.Errorf("expected index or slice expression, got %q", p.curToken.Val)
				}
				if p.curToken.Type != TokRBracket {
					return nil, fmt.Errorf("expected ']' after index expression, got %q", p.curToken.Val)
				}
				p.nextToken()
				left = IndexNode{Target: left, Index: low}
			}

		case TokQuestion:
			ternary, err := p.parseTernary(left)
			if err != nil {
				return nil, err
			}
			left = ternary

		default:
			return left, nil
		}
	}

	return left, nil
}

// parsePrefix handles every expression that can start a (sub)expression:
// literals, identifiers, grouping parens, if-expressions, and now unary
// operators.
func (p *PrattParser) parsePrefix() (Expr, error) {
	switch p.curToken.Type {
	case TokNumber:
		n := NumberNode{Value: p.curToken.Literal}
		p.nextToken()
		return n, nil

	case TokString:
		s := StringNode{Value: p.curToken.Literal.(string)}
		p.nextToken()
		return s, nil

	case TokRegex:
		r := RegexNode{Pattern: p.curToken.Literal.(string)}
		p.nextToken()
		return r, nil

	case TokIllegal:
		return nil, fmt.Errorf("%s", p.curToken.Val)

	case TokBool:
		b := BoolNode{Value: p.curToken.Literal.(bool)}
		p.nextToken()
		return b, nil

	case TokNil:
		p.nextToken()
		return NilNode{}, nil

	case TokIdent:
		// A bare identifier immediately followed by '=>' is a single-param
		// lambda (`x => expr`), not a variable reference. There's no other
		// valid construct "ident =>" could mean, so this check is
		// unambiguous - no lookahead/backtracking needed, unlike the
		// parenthesized-params case below.
		if p.peekToken.Type == TokArrow {
			param := p.curToken.Val
			p.nextToken() // consume the identifier
			p.nextToken() // consume '=>'
			body, err := p.ParseExpression(LOWEST)
			if err != nil {
				return nil, err
			}
			return LambdaNode{Params: []string{param}, Body: body}, nil
		}
		v := VarNode{Name: p.curToken.Val}
		p.nextToken()
		return v, nil

	case TokOp:
		// Unary prefix operator: -x, +x, !x. prefixOps is consulted (rather
		// than falling through to the default "unexpected token" case)
		// specifically so ordinary input like "-5" or "-(1+2)" parses.
		op := p.curToken.Val
		bp, ok := prefixOps[op]
		if !ok {
			return nil, fmt.Errorf("unexpected token %q", p.curToken.Val)
		}
		p.nextToken()
		operand, err := p.ParseExpression(bp)
		if err != nil {
			return nil, err
		}
		return UnaryOpNode{Op: op, Operand: operand}, nil

	case TokLParen:
		// A parenthesized, comma-separated identifier list followed by
		// '=>' is a multi-param (or zero-param) lambda: `(a, b) => expr`.
		// This is genuinely ambiguous with a grouping paren at the point
		// we see '(' - `(a, b)` looks like a param list, `(a + b)` looks
		// like a grouped expression - so tryParseParenLambdaParams
		// speculatively parses and backtracks (restoring the lexer/parser
		// state exactly) if it turns out not to be followed by '=>'.
		if params, ok := p.tryParseParenLambdaParams(); ok {
			p.nextToken() // consume '=>'
			body, err := p.ParseExpression(LOWEST)
			if err != nil {
				return nil, err
			}
			return LambdaNode{Params: params, Body: body}, nil
		}

		// Grouping parenthesis: consumes '(' EXPR ')' and produces EXPR
		// itself (no node of its own - the parens only ever affect
		// grouping/precedence, they don't survive into the AST). Because
		// this only runs when '(' appears where an operand is expected,
		// it can never be confused with the call-postfix '(' handled in
		// ParseExpression's infix loop, which only runs after `left` is
		// already set to a preceding expression.
		p.nextToken() // consume '('
		if p.curToken.Type == TokRParen {
			return nil, fmt.Errorf("empty parentheses '()' are not a valid expression")
		}
		inner, err := p.ParseExpression(LOWEST)
		if err != nil {
			return nil, err
		}
		if p.curToken.Type != TokRParen {
			return nil, fmt.Errorf("expected closing parenthesis ')', got %q", p.curToken.Val)
		}
		p.nextToken() // consume ')'
		return inner, nil

	case TokIf:
		return p.parseIfExpression()

	case TokLet:
		return p.parseLetExpression()

	case TokLBracket:
		return p.parseListLiteral()

	case TokLBrace:
		// '{' in prefix (operand) position is a map literal. This never
		// collides with the '{' used for if/else blocks, since those are
		// consumed directly by parseBlockOrSingle and never go through
		// parsePrefix.
		return p.parseMapLiteral()

	default:
		return nil, fmt.Errorf("unexpected token %q", p.curToken.Val)
	}
}

// parseListLiteral parses a list literal: '[' (expr (',' expr)*)? ']'.
func (p *PrattParser) parseListLiteral() (Expr, error) {
	p.nextToken() // consume '['

	var elements []Expr
	if p.curToken.Type == TokRBracket {
		p.nextToken()
		return ListNode{Elements: elements}, nil
	}

	for {
		el, err := p.ParseExpression(LOWEST)
		if err != nil {
			return nil, err
		}
		elements = append(elements, el)

		if p.curToken.Type == TokComma {
			p.nextToken()
			continue
		}
		break
	}

	if p.curToken.Type != TokRBracket {
		return nil, fmt.Errorf("expected ']' after list elements, got %q", p.curToken.Val)
	}
	p.nextToken()
	return ListNode{Elements: elements}, nil
}

// parseMapLiteral parses a map literal: '{' (key ':' expr (',' key ':' expr)*)? '}'.
// A key is either a bare identifier or a string literal; either way it
// compiles down to a literal string key, not a variable lookup.
func (p *PrattParser) parseMapLiteral() (Expr, error) {
	p.nextToken() // consume '{'

	var keys []Expr
	var values []Expr
	if p.curToken.Type == TokRBrace {
		p.nextToken()
		return MapNode{Keys: keys, Values: values}, nil
	}

	for {
		var key Expr
		switch p.curToken.Type {
		case TokIdent:
			key = StringNode{Value: p.curToken.Val}
			p.nextToken()
		case TokString:
			key = StringNode{Value: p.curToken.Literal.(string)}
			p.nextToken()
		default:
			return nil, fmt.Errorf("expected map key (identifier or string), got %q", p.curToken.Val)
		}

		if p.curToken.Type != TokColon {
			return nil, fmt.Errorf("expected ':' after map key, got %q", p.curToken.Val)
		}
		p.nextToken()

		val, err := p.ParseExpression(LOWEST)
		if err != nil {
			return nil, err
		}

		keys = append(keys, key)
		values = append(values, val)

		if p.curToken.Type == TokComma {
			p.nextToken()
			continue
		}
		break
	}

	if p.curToken.Type != TokRBrace {
		return nil, fmt.Errorf("expected '}' after map entries, got %q", p.curToken.Val)
	}
	p.nextToken()
	return MapNode{Keys: keys, Values: values}, nil
}

// tryParseParenLambdaParams speculatively parses a parenthesized lambda
// parameter list starting at the current '(' token:
// '(' (ident (',' ident)*)? ')' '=>'
// On success it consumes through the ')' - leaving curToken as the '=>'
// arrow, not yet consumed - and returns the parameter names with ok=true.
// On anything that doesn't match that exact shape (a non-identifier where
// a param name is expected, or no '=>' after the closing ')'), it restores
// the lexer and parser to exactly the state they were in when called, so
// the caller can fall back to parsing an ordinary grouping-parenthesis
// expression or call argument list instead.
func (p *PrattParser) tryParseParenLambdaParams() ([]string, bool) {
	savedPos, savedCur, savedPeek := p.lexer.pos, p.curToken, p.peekToken
	restore := func() ([]string, bool) {
		p.lexer.pos, p.curToken, p.peekToken = savedPos, savedCur, savedPeek
		return nil, false
	}

	p.nextToken() // consume '('

	var params []string
	if p.curToken.Type != TokRParen {
		for {
			if p.curToken.Type != TokIdent {
				return restore()
			}
			params = append(params, p.curToken.Val)
			p.nextToken()

			if p.curToken.Type == TokComma {
				p.nextToken()
				continue
			}
			break
		}
		if p.curToken.Type != TokRParen {
			return restore()
		}
	}
	p.nextToken() // consume ')'

	if p.curToken.Type != TokArrow {
		return restore()
	}
	return params, true
}

// parseTernary parses the `? then : else` tail of a ternary, given the
// already-parsed condition. The else-branch recurses with rbp =
// PREC_TERNARY-1 (not PREC_TERNARY), which is what makes chained ternaries
// right-associative: `a?b:c?d:e` becomes `a?b:(c?d:e)`. Passing
// PREC_TERNARY here instead would make the inner "c?d:e" refuse to recurse
// into itself, silently returning just `c` and mis-grouping the whole
// expression as `(a?b:c)?d:e`.
func (p *PrattParser) parseTernary(cond Expr) (Expr, error) {
	p.nextToken() // consume '?'
	thenExpr, err := p.ParseExpression(LOWEST)
	if err != nil {
		return nil, err
	}
	if p.curToken.Type != TokColon {
		return nil, fmt.Errorf("expected ':' in ternary expression, got %q", p.curToken.Val)
	}
	p.nextToken() // consume ':'
	elseExpr, err := p.ParseExpression(PREC_TERNARY - 1)
	if err != nil {
		return nil, err
	}
	return TernaryNode{Cond: cond, Then: thenExpr, Else: elseExpr}, nil
}

func (p *PrattParser) parseCallArguments() ([]Expr, error) {
	var args []Expr
	if p.curToken.Type == TokRParen {
		p.nextToken()
		return args, nil
	}

	arg, err := p.ParseExpression(LOWEST)
	if err != nil {
		return nil, err
	}
	args = append(args, arg)

	for p.curToken.Type == TokComma {
		p.nextToken()
		arg, err := p.ParseExpression(LOWEST)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}

	if p.curToken.Type != TokRParen {
		return nil, fmt.Errorf("expected ')' after call arguments")
	}
	p.nextToken()
	return args, nil
}

func (p *PrattParser) parseIfExpression() (Expr, error) {
	p.nextToken() // consume 'if'

	cond, err := p.ParseExpression(LOWEST)
	if err != nil {
		return nil, err
	}

	thenExpr, err := p.parseBlockOrSingle()
	if err != nil {
		return nil, err
	}

	var elseExpr Expr
	if p.curToken.Type == TokElse {
		p.nextToken() // consume 'else'
		elseExpr, err = p.parseBlockOrSingle()
		if err != nil {
			return nil, err
		}
	}

	return IfNode{Cond: cond, Then: thenExpr, Else: elseExpr}, nil
}

// parseLetExpression parses `let name = value; body`. Body is itself a
// full expression - meaning it's free to be another `let`, which is what
// makes chains like `let a = 1; let b = a + 1; b + 1` fall out of ordinary
// recursive parsing (nested LetNodes) with no separate "let-chain" rule
// needed. ';' is only ever consumed here: nowhere else in the grammar
// expects or accepts a TokSemicolon, so writing one anywhere but right
// after a let binding's value is a parse error (an unconsumed trailing
// token) - exactly the "';' is only valid after a let" restriction,
// enforced by construction rather than as a separate check.
func (p *PrattParser) parseLetExpression() (Expr, error) {
	p.nextToken() // consume 'let'

	if p.curToken.Type != TokIdent {
		return nil, fmt.Errorf("expected identifier after 'let', got %q", p.curToken.Val)
	}
	name := p.curToken.Val
	p.nextToken()

	if p.curToken.Type != TokOp || p.curToken.Val != "=" {
		return nil, fmt.Errorf("expected '=' after 'let %s', got %q", name, p.curToken.Val)
	}
	p.nextToken() // consume '='

	value, err := p.ParseExpression(LOWEST)
	if err != nil {
		return nil, err
	}

	if p.curToken.Type != TokSemicolon {
		return nil, fmt.Errorf("expected ';' after let binding, got %q", p.curToken.Val)
	}
	p.nextToken() // consume ';'

	body, err := p.ParseExpression(LOWEST)
	if err != nil {
		return nil, err
	}

	return LetNode{Name: name, Value: value, Body: body}, nil
}

func (p *PrattParser) parseBlockOrSingle() (Expr, error) {
	if p.curToken.Type == TokLBrace {
		p.nextToken()
		expr, err := p.ParseExpression(LOWEST)
		if err != nil {
			return nil, err
		}
		if p.curToken.Type != TokRBrace {
			return nil, fmt.Errorf("expected '}' at end of block")
		}
		p.nextToken()
		return expr, nil
	}
	return p.ParseExpression(LOWEST)
}

// --- Entry Point ---

func Parse(input string) (Expr, error) {
	lexer := NewLexer(input)
	parser := NewPrattParser(lexer)
	expr, err := parser.ParseExpression(LOWEST)
	if err != nil {
		return nil, err
	}
	// A full parse must consume the entire input. Without this check,
	// trailing tokens ParseExpression's own loop simply stops on (e.g. a
	// stray operator, or the second half of a malformed "1+-2" - see the
	// lexer note on twoCharOps) would be silently discarded instead of
	// reported as the parse error they are.
	if parser.curToken.Type == TokIllegal {
		return nil, fmt.Errorf("%s", parser.curToken.Val)
	}
	if parser.curToken.Type != TokEOF {
		return nil, fmt.Errorf("unexpected trailing token %q after expression", parser.curToken.Val)
	}
	return expr, nil
}
