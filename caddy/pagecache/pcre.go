package pagecache

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// compilePCRE compiles the body of a PHP regular expression, which PHP
// wraps in delim, into a Go one. The plugins' patterns only ever decide
// that a cached page must not be sent, so a pattern is accepted when Go
// matches the same strings or more, and refused otherwise: backreferences,
// lookaround, possessive and atomic groups, and non-ASCII bytes, which PCRE
// matches byte by byte. Go's case folding covers more than ASCII and its
// \S also matches \v, both on the safe side.
func compilePCRE(body string, delim byte, caseless bool) (*regexp.Regexp, error) {
	var b strings.Builder
	if caseless {
		b.WriteString("(?i)")
	}
	quantified := false
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c >= utf8.RuneSelf {
			return nil, errors.New("non-ASCII character")
		}
		if c == delim {
			return nil, fmt.Errorf("unescaped %c", delim)
		}
		after := quantified
		quantified = false
		switch c {
		case '\\':
			esc, n, err := pcreEscape(body[i+1:], false)
			if err != nil {
				return nil, err
			}
			b.WriteString(esc)
			i += n
		case '[':
			class, n, err := pcreClass(body[i:], delim)
			if err != nil {
				return nil, err
			}
			b.WriteString(class)
			i += n - 1
		case '(':
			if strings.HasPrefix(body[i:], "(?") || strings.HasPrefix(body[i:], "(*") {
				group, n, err := pcreGroup(body[i:])
				if err != nil {
					return nil, err
				}
				b.WriteString(group)
				i += n - 1
			} else {
				b.WriteByte(c)
			}
		case '*', '+', '?':
			if after && c == '+' {
				return nil, errors.New("possessive quantifier")
			}
			b.WriteByte(c)
			quantified = !after
		case '{':
			n, ok, err := pcreRepeat(body[i:])
			if err != nil {
				return nil, err
			}
			if ok {
				b.WriteString(body[i : i+n])
				i += n - 1
				quantified = true
			} else {
				b.WriteString(`\{`)
			}
		case '}', ']':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return regexp.Compile(b.String())
}

// pcreEscape translates the escape after a backslash, returning the Go
// text and how many bytes it used.
func pcreEscape(s string, inClass bool) (string, int, error) {
	if s == "" {
		return "", 0, errors.New("trailing backslash")
	}
	c := s[0]
	switch c {
	case 'd', 'D', 'w', 'W', 'S':
		return `\` + string(c), 1, nil
	case 's':
		// PCRE's \s includes the vertical tab; Go's does not.
		if inClass {
			return `\t\n\v\f\r `, 1, nil
		}
		return `[\t\n\v\f\r ]`, 1, nil
	case 'b':
		if inClass {
			return `\x08`, 1, nil
		}
		return `\b`, 1, nil
	case 'B', 'A', 'z':
		if inClass {
			return "", 0, fmt.Errorf(`\%c in a class`, c)
		}
		return `\` + string(c), 1, nil
	case 'n':
		return `\n`, 1, nil
	case 't':
		return `\t`, 1, nil
	case 'r':
		return `\r`, 1, nil
	case 'f':
		return `\f`, 1, nil
	case 'a':
		return `\x07`, 1, nil
	case 'e':
		return `\x1B`, 1, nil
	case '0':
		n, v := 1, 0
		for n < 3 && n < len(s) && s[n] >= '0' && s[n] <= '7' {
			v = v*8 + int(s[n]-'0')
			n++
		}
		esc, err := byteClass(v)
		return esc, n, err
	case 'x':
		if strings.HasPrefix(s[1:], "{") {
			end := strings.IndexByte(s, '}')
			if end < 0 {
				return "", 0, errors.New(`unterminated \x{`)
			}
			v, err := strconv.ParseUint(s[2:end], 16, 32)
			if err != nil || v >= utf8.RuneSelf {
				return "", 0, errors.New(`\x{} outside ASCII`)
			}
			esc, err := byteClass(int(v))
			return esc, end + 1, err
		}
		n, v := 1, 0
		for n < 3 && n < len(s) && strings.IndexByte("0123456789abcdefABCDEF", s[n]) >= 0 {
			d, _ := strconv.ParseUint(s[n:n+1], 16, 8)
			v = v*16 + int(d)
			n++
		}
		esc, err := byteClass(v)
		return esc, n, err
	}
	if c >= utf8.RuneSelf || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
		return "", 0, fmt.Errorf(`\%c is not supported`, c)
	}
	// Any other punctuation, and the space, stand for themselves.
	return `\` + string(c), 1, nil
}

// byteClass is the Go escape for the byte v, which must be ASCII.
func byteClass(v int) (string, error) {
	if v >= utf8.RuneSelf {
		return "", errors.New("escape outside ASCII")
	}
	return fmt.Sprintf(`\x{%X}`, v), nil
}

// pcreClass translates the character class at the start of s, returning
// the Go text and its length in s.
func pcreClass(s string, delim byte) (string, int, error) {
	var b strings.Builder
	b.WriteByte('[')
	i := 1
	if i < len(s) && s[i] == '^' {
		b.WriteByte('^')
		i++
	}
	for first := true; i < len(s); i, first = i+1, false {
		c := s[i]
		if c >= utf8.RuneSelf {
			return "", 0, errors.New("non-ASCII character")
		}
		if c == delim {
			return "", 0, fmt.Errorf("unescaped %c", delim)
		}
		switch {
		case c == ']' && !first:
			b.WriteByte(']')
			return b.String(), i + 1, nil
		case c == ']':
			b.WriteString(`\]`)
		case c == '\\':
			esc, n, err := pcreEscape(s[i+1:], true)
			if err != nil {
				return "", 0, err
			}
			b.WriteString(esc)
			i += n
		case c == '[' && strings.HasPrefix(s[i:], "[:"):
			end := strings.Index(s[i:], ":]")
			if end < 0 || !posixClasses[strings.TrimPrefix(s[i+2:i+end], "^")] {
				return "", 0, errors.New("unknown POSIX class")
			}
			b.WriteString(s[i : i+end+2])
			i += end + 1
		case c == '[' && (strings.HasPrefix(s[i:], "[.") || strings.HasPrefix(s[i:], "[=")):
			return "", 0, errors.New("collating element")
		case c == '[':
			b.WriteString(`\[`)
		default:
			b.WriteByte(c)
		}
	}
	return "", 0, errors.New("missing ]")
}

var posixClasses = map[string]bool{
	"alnum": true, "alpha": true, "ascii": true, "blank": true, "cntrl": true, "digit": true, "graph": true,
	"lower": true, "print": true, "punct": true, "space": true, "upper": true, "word": true, "xdigit": true,
}

// pcreGroup translates the (? or (* that opens s.
func pcreGroup(s string) (string, int, error) {
	if strings.HasPrefix(s, "(?:") {
		return "(?:", 3, nil
	}
	name := ""
	switch {
	case strings.HasPrefix(s, "(?P<"):
		name = s[4:]
	case strings.HasPrefix(s, "(?<") && len(s) > 3 && s[3] != '=' && s[3] != '!':
		name = s[3:]
	}
	if name != "" {
		end := strings.IndexByte(name, '>')
		if end <= 0 || !groupName.MatchString(name[:end]) {
			return "", 0, errors.New("invalid group name")
		}
		return "(?P<" + name[:end] + ">", len(s) - len(name) + end + 1, nil
	}
	// Flags: (?i) (?-i) (?im:...) and the like.
	j := 2
	for j < len(s) && strings.IndexByte("ims-", s[j]) >= 0 {
		j++
	}
	if j > 2 && j < len(s) && (s[j] == ')' || s[j] == ':') {
		return s[:j+1], j + 1, nil
	}
	return "", 0, errors.New("unsupported group")
}

var groupName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// pcreRepeat reports whether s starts with a {n}, {n,} or {n,m}
// quantifier, and its length. Braces PCRE reads as literal text report
// false; forms that newer PCRE versions read differently are an error.
func pcreRepeat(s string) (int, bool, error) {
	end := strings.IndexByte(s, '}')
	if end < 0 {
		return 0, false, nil
	}
	inner := s[1:end]
	if inner == "" || strings.Trim(inner, "0123456789, ") != "" {
		return 0, false, nil
	}
	if inner[0] == ',' || strings.Contains(inner, " ") || strings.Count(inner, ",") > 1 {
		return 0, false, errors.New("ambiguous {} quantifier")
	}
	return end + 1, true, nil
}

// rejects reports whether re matches s, as a test that decides against
// serving. PCRE without the u flag matches bytes, which Go cannot do for
// text outside ASCII, and its $ also matches before a final newline, so
// such text counts as a match.
func rejects(re *regexp.Regexp, s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf || s[i] == '\n' {
			return true
		}
	}
	return re.MatchString(s)
}
