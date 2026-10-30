package pagecache

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The settings of WP Super Cache and WP Rocket are PHP files the plugins
// write. evalPHP runs the part of such a file that assigns literals at the
// top level, in order. A variable set any other way (by a function, under a
// condition, after a possible return) becomes unknown, so the caller never
// acts on a value PHP might not have.

type kind uint8

const (
	unknown kind = iota
	null
	boolean
	integer
	float
	str
	array
)

type value struct {
	kind  kind
	b     bool
	i     int64
	f     float64
	s     string
	items []item
}

type item struct {
	key value // integer or str
	val value
}

func strValue(s string) value { return value{kind: str, s: s} }

// truthy converts v to a boolean as PHP does.
func (v value) truthy() bool {
	switch v.kind {
	case boolean:
		return v.b
	case integer:
		return v.i != 0
	case float:
		return v.f != 0
	case str:
		return v.s != "" && v.s != "0"
	case array:
		return len(v.items) > 0
	}
	return false
}

// text converts a scalar to a string as PHP does. Floats are left out:
// PHP prints them its own way.
func (v value) text() (string, bool) {
	switch v.kind {
	case null:
		return "", true
	case boolean:
		if v.b {
			return "1", true
		}
		return "", true
	case integer:
		return strconv.FormatInt(v.i, 10), true
	case str:
		return v.s, true
	}
	return "", false
}

// equals reports whether v == n holds in PHP 8.
func (v value) equals(n int64) bool {
	switch v.kind {
	case null:
		return n == 0
	case boolean:
		return v.b == (n != 0)
	case integer:
		return v.i == n
	case float:
		return v.f == float64(n)
	case str:
		if f, ok := phpNumber(v.s); ok {
			return f == float64(n)
		}
		return v.s == strconv.FormatInt(n, 10)
	}
	return false
}

// strings lists the elements of an array of scalars as PHP converts them to
// strings.
func (v value) strings() ([]string, bool) {
	if v.kind != array {
		return nil, false
	}
	out := make([]string, 0, len(v.items))
	for _, it := range v.items {
		s, ok := it.val.text()
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

const phpSpace = " \t\n\r\v\f"

// phpNumber parses a numeric string as PHP 8 defines it: a decimal number,
// optionally signed, with an exponent, and surrounded by whitespace.
func phpNumber(s string) (float64, bool) {
	t := strings.Trim(s, phpSpace)
	if t == "" {
		return 0, false
	}
	body := strings.TrimLeft(t, "+-")
	if len(t)-len(body) > 1 || body == "" || !(isDigit(body[0]) || body[0] == '.') {
		return 0, false
	}
	for i := 0; i < len(body); i++ {
		if c := body[i]; !isDigit(c) && c != '.' && c != 'e' && c != 'E' && c != '+' && c != '-' {
			return 0, false
		}
	}
	f, err := strconv.ParseFloat(t, 64)
	return f, err == nil || errors.Is(err, strconv.ErrRange)
}

var (
	errSyntax      = errors.New("not valid PHP")
	errUnsupported = errors.New("uses PHP that FastCP does not read")
	errStop        = errors.New("stop")
)

// maxPHP caps the size of a settings file; the plugins write a few KiB.
const maxPHP = 1 << 20

// evalPHP returns the variables src leaves set. consts are the constants
// the file may use (WP_CONTENT_DIR and the like). A variable missing from
// the result is unset; one of kind unknown was set in a way not evaluated.
func evalPHP(src string, consts map[string]string) (map[string]value, error) {
	if len(src) > maxPHP {
		return nil, errors.New("too large")
	}
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	pair, err := pairs(toks)
	if err != nil {
		return nil, err
	}
	in := &interp{toks: toks, pair: pair, consts: consts, vars: map[string]value{}}
	if err := in.statements(false, false); err != nil && !errors.Is(err, errStop) {
		return nil, err
	}
	return in.vars, nil
}

// ---------------------------------------------------------------------------
// Tokens

type tokKind uint8

const (
	tEOF      tokKind = iota
	tVar              // $name; text is the name
	tName             // identifier or keyword
	tString           // string literal; text is its value
	tTemplate         // double-quoted string with variables in it
	tNumber
	tOp
)

type token struct {
	kind tokKind
	text string
}

func (t token) is(op string) bool { return t.kind == tOp && t.text == op }

func (t token) isName(name string) bool { return t.kind == tName && strings.EqualFold(t.text, name) }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isNameStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= utf8.RuneSelf
}

func isNameByte(c byte) bool { return isNameStart(c) || isDigit(c) }

func isSpace(c byte) bool { return strings.IndexByte(phpSpace, c) >= 0 }

// ops lists PHP's operators, longest first.
var ops = []string{
	"<<=", ">>=", "**=", "...", "<=>", "===", "!==", "??=", "?->",
	"<<", ">>", "**", "==", "!=", "<>", "<=", ">=", "&&", "||", "++", "--",
	"+=", "-=", "*=", "/=", ".=", "%=", "&=", "|=", "^=", "->", "=>", "::", "??",
}

func tokenize(src string) ([]token, error) {
	var toks []token
	html := true
	for i := 0; i < len(src); {
		if html {
			j := strings.Index(src[i:], "<?")
			if j < 0 {
				break
			}
			i += j + 2
			switch {
			case len(src)-i >= 3 && strings.EqualFold(src[i:i+3], "php") && (len(src) == i+3 || isSpace(src[i+3])):
				i += 3
			case strings.HasPrefix(src[i:], "="):
				i++
				toks = append(toks, token{tName, "echo"})
			default:
				return nil, fmt.Errorf("%w: short open tag", errUnsupported)
			}
			html = false
			continue
		}
		c := src[i]
		switch {
		case isSpace(c):
			i++
		case c == '#' || strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' && !strings.HasPrefix(src[i:], "?>") {
				i++
			}
		case strings.HasPrefix(src[i:], "/*"):
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				return nil, errSyntax
			}
			i += j + 4
		case strings.HasPrefix(src[i:], "?>"):
			toks = append(toks, token{tOp, ";"})
			i += 2
			if strings.HasPrefix(src[i:], "\r\n") {
				i += 2
			} else if i < len(src) && src[i] == '\n' {
				i++
			}
			html = true
		case c == '$':
			j := i + 1
			if j < len(src) && isNameStart(src[j]) {
				for j < len(src) && isNameByte(src[j]) {
					j++
				}
				toks = append(toks, token{tVar, src[i+1 : j]})
			} else {
				toks = append(toks, token{tOp, "$"})
			}
			i = j
		case isNameStart(c):
			j := i + 1
			for j < len(src) && isNameByte(src[j]) {
				j++
			}
			toks = append(toks, token{tName, src[i:j]})
			i = j
		case isDigit(c) || c == '.' && i+1 < len(src) && isDigit(src[i+1]):
			j := i + 1
			hex := strings.HasPrefix(strings.ToLower(src[i:]), "0x")
			for j < len(src) {
				d := src[j]
				if isNameByte(d) || d == '.' || !hex && (d == '+' || d == '-') && (src[j-1] == 'e' || src[j-1] == 'E') {
					j++
					continue
				}
				break
			}
			toks = append(toks, token{tNumber, src[i:j]})
			i = j
		case c == '\'':
			s, n, err := singleQuoted(src[i:])
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{tString, s})
			i += n
		case c == '"':
			s, dynamic, n, err := doubleQuoted(src[i:])
			if err != nil {
				return nil, err
			}
			if dynamic {
				toks = append(toks, token{tTemplate, s})
			} else {
				toks = append(toks, token{tString, s})
			}
			i += n
		case c == '`':
			return nil, fmt.Errorf("%w: backticks", errUnsupported)
		case strings.HasPrefix(src[i:], "<<<"):
			return nil, fmt.Errorf("%w: heredoc", errUnsupported)
		default:
			op := src[i : i+1]
			for _, o := range ops {
				if strings.HasPrefix(src[i:], o) {
					op = o
					break
				}
			}
			toks = append(toks, token{tOp, op})
			i += len(op)
		}
	}
	return append(toks, token{kind: tEOF}), nil
}

func singleQuoted(s string) (string, int, error) {
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'':
			return b.String(), i + 1, nil
		case '\\':
			if i+1 < len(s) && (s[i+1] == '\\' || s[i+1] == '\'') {
				i++
				b.WriteByte(s[i])
			} else {
				b.WriteByte(c)
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", 0, errSyntax
}

func doubleQuoted(s string) (string, bool, int, error) {
	var b strings.Builder
	dynamic := false
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			return b.String(), dynamic, i + 1, nil
		case '$':
			if i+1 < len(s) && (isNameStart(s[i+1]) || s[i+1] == '{') {
				dynamic = true
			}
			b.WriteByte(c)
		case '{':
			if i+1 < len(s) && s[i+1] == '$' {
				dynamic = true
			}
			b.WriteByte(c)
		case '\\':
			if i+1 == len(s) {
				return "", false, 0, errSyntax
			}
			i++
			switch e := s[i]; e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'v':
				b.WriteByte('\v')
			case 'e':
				b.WriteByte(0x1b)
			case 'f':
				b.WriteByte('\f')
			case '\\', '$', '"':
				b.WriteByte(e)
			case '0', '1', '2', '3', '4', '5', '6', '7':
				n, j := 0, i
				for j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '7' {
					n = n*8 + int(s[j]-'0')
					j++
				}
				b.WriteByte(byte(n))
				i = j - 1
			case 'x':
				n, j := 0, i+1
				for j < len(s) && j < i+3 && strings.IndexByte("0123456789abcdefABCDEF", s[j]) >= 0 {
					d, _ := strconv.ParseUint(s[j:j+1], 16, 8)
					n = n*16 + int(d)
					j++
				}
				if j == i+1 {
					b.WriteString(`\x`)
				} else {
					b.WriteByte(byte(n))
					i = j - 1
				}
			case 'u':
				end := strings.IndexByte(s[i:], '}')
				if i+1 >= len(s) || s[i+1] != '{' || end < 0 {
					b.WriteString(`\u`)
					break
				}
				r, err := strconv.ParseUint(s[i+2:i+end], 16, 32)
				if err != nil || r > utf8.MaxRune {
					return "", false, 0, errSyntax
				}
				b.WriteRune(rune(r))
				i += end
			default:
				b.WriteByte('\\')
				b.WriteByte(e)
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", false, 0, errSyntax
}

// pairs maps each bracket to the one that closes or opens it.
func pairs(toks []token) ([]int, error) {
	pair := make([]int, len(toks))
	var stack []int
	closer := map[string]string{"(": ")", "[": "]", "{": "}"}
	for i, t := range toks {
		pair[i] = -1
		if t.kind != tOp {
			continue
		}
		switch t.text {
		case "(", "[", "{":
			stack = append(stack, i)
		case ")", "]", "}":
			if len(stack) == 0 || closer[toks[stack[len(stack)-1]].text] != t.text {
				return nil, errSyntax
			}
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			pair[open], pair[i] = i, open
		}
	}
	if len(stack) > 0 {
		return nil, errSyntax
	}
	return pair, nil
}

// ---------------------------------------------------------------------------
// Statements

type interp struct {
	toks   []token
	pair   []int
	pos    int
	consts map[string]string
	vars   map[string]value
	// stopped is set once a return, exit or throw may have ended the file
	// early, after which assignments may not run.
	stopped bool
}

func (in *interp) peek() token { return in.toks[in.pos] }

func (in *interp) peekAt(n int) token {
	if in.pos+n < len(in.toks) {
		return in.toks[in.pos+n]
	}
	return token{kind: tEOF}
}

func (in *interp) next() token {
	t := in.toks[in.pos]
	if t.kind != tEOF {
		in.pos++
	}
	return t
}

func (in *interp) get(name string) value {
	if v, ok := in.vars[name]; ok {
		return v
	}
	return value{kind: null}
}

func (in *interp) statements(cond, braced bool) error {
	for {
		switch t := in.peek(); {
		case t.kind == tEOF:
			if braced {
				return errSyntax
			}
			return nil
		case braced && t.is("}"):
			in.pos++
			return nil
		}
		if err := in.statement(cond); err != nil {
			return err
		}
	}
}

// statement runs one statement. cond is set inside if, loop and try
// bodies, where PHP may or may not run it.
func (in *interp) statement(cond bool) error {
	t := in.peek()
	switch t.kind {
	case tEOF:
		return errSyntax
	case tOp:
		switch t.text {
		case ";":
			in.pos++
			return nil
		case "{":
			in.pos++
			return in.statements(cond, true)
		case "}":
			return errSyntax
		}
	case tVar:
		if in.assign(cond) {
			return nil
		}
	case tName:
		switch strings.ToLower(t.text) {
		case "if":
			return in.ifStatement()
		case "else", "elseif":
			return errSyntax
		case "while", "for", "foreach", "declare":
			in.pos++
			if err := in.parens(); err != nil {
				return err
			}
			return in.body()
		case "switch":
			in.pos++
			if err := in.parens(); err != nil {
				return err
			}
			if !in.peek().is("{") {
				return fmt.Errorf("%w: alternative syntax", errUnsupported)
			}
			end := in.pair[in.pos]
			if err := in.scan(in.pos+1, end); err != nil {
				return err
			}
			in.pos = end + 1
			return nil
		case "do":
			in.pos++
			if err := in.statement(true); err != nil {
				return err
			}
			if !in.next().isName("while") {
				return errSyntax
			}
			if err := in.parens(); err != nil {
				return err
			}
			if !in.next().is(";") {
				return errSyntax
			}
			return nil
		case "try":
			in.pos++
			if err := in.statement(true); err != nil {
				return err
			}
			for in.peek().isName("catch") {
				in.pos++
				if err := in.parens(); err != nil {
					return err
				}
				if err := in.statement(true); err != nil {
					return err
				}
			}
			if in.peek().isName("finally") {
				in.pos++
				return in.statement(true)
			}
			return nil
		case "function":
			if n := in.peekAt(1); n.kind == tName || n.is("&") {
				return in.skipDeclaration()
			}
		case "abstract", "final", "class", "interface", "trait", "enum", "readonly":
			return in.skipDeclaration()
		case "namespace", "goto":
			return fmt.Errorf("%w: %s", errUnsupported, t.text)
		case "return", "exit", "die", "throw":
			if !cond && !in.stopped {
				return errStop
			}
		}
	}
	return in.expression()
}

func (in *interp) ifStatement() error {
	in.pos++
	if err := in.parens(); err != nil {
		return err
	}
	if err := in.body(); err != nil {
		return err
	}
	for {
		switch t := in.peek(); {
		case t.isName("elseif"):
			in.pos++
			if err := in.parens(); err != nil {
				return err
			}
			if err := in.body(); err != nil {
				return err
			}
		case t.isName("else"):
			in.pos++
			return in.body()
		default:
			return nil
		}
	}
}

// body runs the statement an if, loop or declare controls.
func (in *interp) body() error {
	if in.peek().is(":") {
		return fmt.Errorf("%w: alternative syntax", errUnsupported)
	}
	return in.statement(true)
}

// parens steps over a parenthesized condition, noting what it assigns.
func (in *interp) parens() error {
	if !in.peek().is("(") {
		return errSyntax
	}
	end := in.pair[in.pos]
	if err := in.scan(in.pos+1, end); err != nil {
		return err
	}
	in.pos = end + 1
	return nil
}

// skipDeclaration steps over a function or class declaration, whose body
// runs in its own scope.
func (in *interp) skipDeclaration() error {
	for i := in.pos; i < len(in.toks); i++ {
		switch t := in.toks[i]; {
		case t.is("(") || t.is("["):
			i = in.pair[i]
		case t.is("{"):
			in.pos = in.pair[i] + 1
			return nil
		case t.is(";") || t.kind == tEOF:
			return errSyntax
		}
	}
	return errSyntax
}

// assign runs `$name = literal;` or `$name .= literal;`. It reports false,
// moving nowhere, for anything else.
func (in *interp) assign(cond bool) bool {
	start := in.pos
	name := in.toks[start].text
	op := in.peekAt(1)
	if !op.is("=") && !op.is(".=") || name == "GLOBALS" || name == "this" {
		return false
	}
	in.pos += 2
	v, ok := in.expr()
	if !ok || !in.peek().is(";") {
		in.pos = start
		return false
	}
	in.pos++
	cur := in.get(name)
	switch {
	case cond || in.stopped:
		// The only conditional assignment in the plugins' files:
		// if ( '/' != substr( $cache_path, -1 ) ) { $cache_path .= '/'; }
		if op.text == ".=" && v.kind == str && v.s == "/" && cur.kind == str {
			if !strings.HasSuffix(cur.s, "/") {
				cur.s += "/"
			}
			in.vars[name] = cur
		} else {
			in.vars[name] = value{}
		}
	case op.text == "=":
		in.vars[name] = v
	default:
		if s, ok := concat(cur, v); ok {
			in.vars[name] = s
		} else {
			in.vars[name] = value{}
		}
	}
	return true
}

// expression steps over any other statement, noting the variables it may
// change.
func (in *interp) expression() error {
	end := in.pos
	for ; end < len(in.toks); end++ {
		t := in.toks[end]
		if t.kind == tEOF || t.is(";") || t.is("}") {
			break
		}
		if t.is("(") || t.is("[") || t.is("{") {
			end = in.pair[end]
		}
	}
	if !in.guard(in.pos, end) {
		if err := in.scan(in.pos, end); err != nil {
			return err
		}
	}
	in.pos = end
	if in.peek().is(";") {
		in.pos++
	}
	return nil
}

// guard reports whether toks[i:end] is `defined( 'NAME' ) || exit...` for a
// constant WordPress defines before the file runs.
func (in *interp) guard(i, end int) bool {
	t := in.toks
	if end-i < 6 || !t[i].isName("defined") || !t[i+1].is("(") || t[i+2].kind != tString || !t[i+3].is(")") {
		return false
	}
	if _, ok := in.consts[t[i+2].text]; !ok || !(t[i+4].is("||") || t[i+4].isName("or")) {
		return false
	}
	if !t[i+5].isName("exit") && !t[i+5].isName("die") {
		return false
	}
	switch rest := end - (i + 6); {
	case rest == 0:
		return true
	case t[i+6].is("("):
		return in.pair[i+6] == end-1
	}
	return false
}

// assignOps are the operators that change the variable before them.
var assignOps = map[string]bool{
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, ".=": true, "%=": true, "**=": true,
	"??=": true, "&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true, "++": true, "--": true,
}

// pure lists functions that cannot change the variables passed to them.
var pure = map[string]bool{
	"array": true, "isset": true, "empty": true, "defined": true, "define": true, "constant": true,
	"substr": true, "strlen": true, "strpos": true, "stripos": true, "strrpos": true, "strtolower": true,
	"strtoupper": true, "trim": true, "rtrim": true, "ltrim": true, "str_contains": true,
	"str_starts_with": true, "str_ends_with": true, "implode": true, "explode": true, "sprintf": true,
	"file_exists": true, "is_file": true, "is_dir": true, "is_readable": true, "dirname": true,
	"basename": true, "realpath": true, "version_compare": true, "phpversion": true,
	"class_exists": true, "function_exists": true, "is_array": true, "is_string": true,
	"is_numeric": true, "in_array": true, "count": true, "intval": true, "strval": true,
	"spl_autoload_register": true, "if": true, "elseif": true, "while": true, "for": true,
	"foreach": true, "switch": true, "catch": true, "match": true, "echo": true, "print": true,
	"include": true, "include_once": true, "require": true, "require_once": true, "return": true,
	"exit": true, "die": true, "fn": true, "function": true, "use": true,
}

// scan notes the variables toks[i:end] may change, stepping over the
// bodies of functions defined there.
func (in *interp) scan(i, end int) error {
	for k := i; k < end; k++ {
		t := in.toks[k]
		switch t.kind {
		case tVar:
			if in.written(k) {
				if t.text == "GLOBALS" {
					return fmt.Errorf("%w: $GLOBALS", errUnsupported)
				}
				in.vars[t.text] = value{}
			}
		case tOp:
			switch t.text {
			case "$":
				return fmt.Errorf("%w: variable variables", errUnsupported)
			case "(":
				if k > i {
					if p := in.toks[k-1]; p.kind == tVar || p.is(")") || p.is("]") {
						in.byReference(k)
					}
				}
			case "[":
				if c := in.pair[k]; c+1 < len(in.toks) && in.toks[c+1].is("=") && (k == i || !in.indexes(k-1)) {
					in.unknownIn(k, c)
				}
			}
		case tName:
			w := strings.ToLower(t.text)
			member := k > 0 && (in.toks[k-1].is("->") || in.toks[k-1].is("?->") || in.toks[k-1].is("::"))
			switch {
			case member:
			case w == "function" || w == "fn":
				next, err := in.skipClosure(k)
				if err != nil {
					return err
				}
				k = next - 1
				continue
			case w == "class":
				if k == 0 || !in.toks[k-1].isName("new") {
					return fmt.Errorf("%w: class declaration", errUnsupported)
				}
				for k < end && !in.toks[k].is("{") {
					k++
				}
				if k < end {
					k = in.pair[k]
				}
				continue
			}
			switch w {
			case "extract", "parse_str", "mb_parse_str", "eval", "settype", "get_defined_vars":
				return fmt.Errorf("%w: %s", errUnsupported, t.text)
			case "exit", "die", "return", "throw":
				in.stopped = true
			case "goto":
				return fmt.Errorf("%w: goto", errUnsupported)
			case "global", "static", "unset", "list", "as":
				in.unknownIn(k, end)
			}
			if k+1 < end && in.toks[k+1].is("(") && !pure[w] {
				in.byReference(k + 1)
			}
		}
	}
	return nil
}

// indexes reports whether the token at k ends an expression that a
// following [ indexes, rather than starting an array.
func (in *interp) indexes(k int) bool {
	t := in.toks[k]
	return t.kind == tVar || t.kind == tName || t.kind == tString || t.is(")") || t.is("]") || t.is("}")
}

// written reports whether the variable at k is assigned, incremented or
// taken by reference.
func (in *interp) written(k int) bool {
	if k > 0 {
		if p := in.toks[k-1]; p.is("&") || p.is("++") || p.is("--") {
			return true
		}
	}
	for j := k + 1; j < len(in.toks); {
		switch t := in.toks[j]; {
		case t.is("[") || t.is("{"):
			j = in.pair[j] + 1
		case t.is("->") || t.is("?->") || t.is("::"):
			j += 2
		default:
			return t.kind == tOp && assignOps[t.text]
		}
	}
	return false
}

// byReference makes unknown the variables passed whole to the call whose
// arguments open at k, since the function may take them by reference.
func (in *interp) byReference(k int) {
	end := in.pair[k]
	start := k + 1
	for j := start; j <= end; j++ {
		t := in.toks[j]
		if t.is("(") || t.is("[") || t.is("{") {
			j = in.pair[j]
			continue
		}
		if !t.is(",") && j != end {
			continue
		}
		if start < j && in.toks[start].kind == tVar {
			in.vars[in.toks[start].text] = value{}
		}
		start = j + 1
	}
}

func (in *interp) unknownIn(i, end int) {
	for k := i; k < end; k++ {
		if in.toks[k].kind == tVar {
			in.vars[in.toks[k].text] = value{}
		}
	}
}

// skipClosure steps over the parameters and body of the anonymous function
// at k, returning where scanning continues. Variables it captures by
// reference become unknown; an arrow function's expression is scanned on.
func (in *interp) skipClosure(k int) (int, error) {
	arrow := in.toks[k].isName("fn")
	j := k + 1
	if in.toks[j].is("&") {
		j++
	}
	if !in.toks[j].is("(") {
		return 0, errSyntax
	}
	j = in.pair[j] + 1
	if in.toks[j].isName("use") {
		if !in.toks[j+1].is("(") {
			return 0, errSyntax
		}
		end := in.pair[j+1]
		for v := j + 2; v < end; v++ {
			if in.toks[v].kind == tVar && in.toks[v-1].is("&") {
				in.vars[in.toks[v].text] = value{}
			}
		}
		j = end + 1
	}
	for ; j < len(in.toks); j++ {
		switch t := in.toks[j]; {
		case arrow && t.is("=>"):
			return j + 1, nil
		case !arrow && t.is("{"):
			return in.pair[j] + 1, nil
		case t.is(";") || t.kind == tEOF:
			return 0, errSyntax
		}
	}
	return 0, errSyntax
}

// ---------------------------------------------------------------------------
// Literal expressions

// expr evaluates a literal expression: scalars, arrays, known constants and
// their concatenation.
func (in *interp) expr() (value, bool) {
	v, ok := in.operand()
	for ok && in.peek().is(".") {
		in.pos++
		var r value
		if r, ok = in.operand(); ok {
			v, ok = concat(v, r)
		}
	}
	return v, ok
}

func (in *interp) operand() (value, bool) {
	t := in.next()
	switch t.kind {
	case tString:
		return strValue(t.text), true
	case tNumber:
		return number(t.text)
	case tName:
		switch strings.ToLower(t.text) {
		case "true":
			return value{kind: boolean, b: true}, true
		case "false":
			return value{kind: boolean}, true
		case "null":
			return value{kind: null}, true
		case "array":
			if in.next().is("(") {
				return in.items(")")
			}
			return value{}, false
		}
		if s, ok := in.consts[t.text]; ok && !in.peek().is("(") && !in.peek().is("::") && !in.peek().is(`\`) {
			return strValue(s), true
		}
	case tOp:
		switch t.text {
		case "[":
			return in.items("]")
		case "(":
			v, ok := in.expr()
			return v, ok && in.next().is(")")
		case "-", "+":
			v, ok := in.operand()
			if !ok || t.text == "+" {
				return v, ok && (v.kind == integer || v.kind == float)
			}
			switch v.kind {
			case integer:
				v.i = -v.i
				return v, true
			case float:
				v.f = -v.f
				return v, true
			}
		}
	}
	return value{}, false
}

func (in *interp) items(close string) (value, bool) {
	arr := value{kind: array}
	var next int64
	for {
		if in.peek().is(close) {
			in.pos++
			return arr, true
		}
		v, ok := in.expr()
		if !ok {
			return value{}, false
		}
		key := value{kind: integer, i: next}
		if in.peek().is("=>") {
			in.pos++
			if key, ok = arrayKey(v); !ok {
				return value{}, false
			}
			if v, ok = in.expr(); !ok {
				return value{}, false
			}
		}
		arr.set(key, v)
		if key.kind == integer && key.i >= next {
			next = key.i + 1
		}
		switch t := in.next(); {
		case t.is(","):
		case t.is(close):
			return arr, true
		default:
			return value{}, false
		}
	}
}

func (v *value) set(key, val value) {
	for i := range v.items {
		if k := v.items[i].key; k.kind == key.kind && k.i == key.i && k.s == key.s {
			v.items[i].val = val
			return
		}
	}
	v.items = append(v.items, item{key, val})
}

// arrayKey converts v to an array key as PHP does.
func arrayKey(v value) (value, bool) {
	switch v.kind {
	case integer:
		return v, true
	case boolean:
		if v.b {
			return value{kind: integer, i: 1}, true
		}
		return value{kind: integer}, true
	case null:
		return strValue(""), true
	case str:
		if n, err := strconv.ParseInt(v.s, 10, 64); err == nil && strconv.FormatInt(n, 10) == v.s {
			return value{kind: integer, i: n}, true
		}
		return v, true
	}
	return value{}, false
}

func concat(a, b value) (value, bool) {
	as, ok1 := a.text()
	bs, ok2 := b.text()
	if !ok1 || !ok2 {
		return value{}, false
	}
	return strValue(as + bs), true
}

func number(text string) (value, bool) {
	t := strings.ReplaceAll(text, "_", "")
	lower := strings.ToLower(t)
	if !strings.HasPrefix(lower, "0x") && strings.ContainsAny(lower, ".e") {
		f, err := strconv.ParseFloat(t, 64)
		return value{kind: float, f: f}, err == nil
	}
	if len(lower) > 1 && lower[0] == '0' && isDigit(lower[1]) {
		t = "0o" + t[1:]
	}
	n, err := strconv.ParseInt(t, 0, 64)
	return value{kind: integer, i: n}, err == nil
}
