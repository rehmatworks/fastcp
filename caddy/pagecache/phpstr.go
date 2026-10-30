package pagecache

import (
	"errors"
	"io/fs"
	"regexp"
	"strings"
)

// phpTrimChars are what trim() removes by default.
const phpTrimChars = " \t\n\r\x00\x0B"

func phpTrim(s string) string { return strings.Trim(s, phpTrimChars) }

// asciiLower is strtolower, which since PHP 8.2 (and in the C locale
// before it) changes only ASCII letters.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

func isHex(c byte) bool { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func upper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 'a' + 'A'
	}
	return c
}

// strstr reports whether strstr($haystack, $needle) is truthy. Since PHP
// 8 an empty needle matches at the start.
func strstr(haystack, needle string) bool {
	i := strings.Index(haystack, needle)
	return i >= 0 && haystack[i:] != "" && haystack[i:] != "0"
}

// looseEquals is == between two strings: numerically when both are
// numeric, byte for byte otherwise.
func looseEquals(a, b string) bool {
	x, ok1 := phpNumber(a)
	y, ok2 := phpNumber(b)
	if ok1 && ok2 {
		return x == y
	}
	return a == b
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

var (
	hostPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+(:[0-9]+)?$`)
	uriPattern  = regexp.MustCompile(`^/[A-Za-z0-9/._~%-]*$`)
)

// simpleHost reports whether every transformation the plugins apply to
// HTTP_HOST (htmlentities, rawurlencode, the port patterns) is either the
// identity or plain lowercasing.
func simpleHost(host string) bool { return hostPattern.MatchString(host) }

// simpleURI reports whether parse_url gives back uri as its path.
func simpleURI(uri string) bool { return uriPattern.MatchString(uri) && !strings.HasPrefix(uri, "//") }

// rawurlencode encodes all but RFC 3986's unreserved characters, in
// upper case.
func rawurlencode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || isDigit(c) || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

// pluginVersion reads a "Version:" header the way WordPress's
// get_file_data does.
var versionHeader = regexp.MustCompile(`(?mi)^(?:[ \t]*<\?php)?[ \t/*#@]*Version:(.*)$`)

func pluginVersion(header string) string {
	m := versionHeader.FindStringSubmatch(strings.ReplaceAll(header, "\r", "\n"))
	if m == nil {
		return ""
	}
	v := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m[1]), "*/"))
	num, pre, _ := strings.Cut(v, "-")
	if !validVersion(num) || pre != "" && !preRelease.MatchString(pre) {
		return ""
	}
	return v
}

var preRelease = regexp.MustCompile(`^[0-9A-Za-z.]{1,20}$`)

// open opens name for sending if it is a regular file.
func open(fsys fs.FS, name string) *hit {
	f, err := fsys.Open(name)
	if err != nil {
		return nil
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil
	}
	return &hit{file: f, info: info}
}
