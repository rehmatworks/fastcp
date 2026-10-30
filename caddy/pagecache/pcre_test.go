package pagecache

import "testing"

func TestCompilePCRE(t *testing.T) {
	cases := []struct {
		body     string
		caseless bool
		match    []string
		noMatch  []string
	}{
		{`wordpress_logged_in_.+|wp-postpass_|wptouch_switch_toggle|comment_author_|comment_author_email_`, false,
			[]string{"wordpress_logged_in_abc", "wp-postpass_1", "xcomment_author_y"},
			[]string{"wordpress_logged_in_", "WP-POSTPASS_1", "wordpress_test_cookie"}},
		{`^(/(?:.+/)?feed(?:/(?:.+/?)?)?$|/(?:.+/)?embed/|/(index.php/)?(.*)wp-json(/.*|$))$`, true,
			[]string{"/feed/", "/blog/FEED", "/wp-json/wp/v2/posts", "/index.php/wp-json", "/a/embed/"},
			[]string{"/feeds/x/", "/hello-world/", "/embed"}},
		{`facebookexternalhit|WhatsApp`, false, []string{"WhatsApp/2.23"}, []string{"whatsapp/2"}},
		{`a\#b`, false, []string{"a#b"}, nil},
		{`\s`, false, []string{"\v", " "}, []string{"x"}},
		{`[\s]x`, false, []string{"\vx"}, nil},
		{`x{2}`, false, []string{"xx"}, []string{"x"}},
		{`a{b}`, false, []string{"a{b}"}, []string{"ab"}},
		{`[]a]`, false, []string{"]", "a"}, []string{"b"}},
		{`[[:digit:]]+$`, false, []string{"123"}, []string{"12a"}},
		{`(?i)abc`, false, []string{"ABC"}, nil},
		{`(?P<name>x)(?<other>y)`, false, []string{"xy"}, nil},
		{`\x41\x{42}\0`, false, []string{"AB\x00"}, nil},
		{`a*?b`, false, []string{"aab"}, nil},
		{`\bword\b`, false, []string{"a word here"}, []string{"swordfish"}},
	}
	for _, c := range cases {
		re, err := compilePCRE(c.body, '#', c.caseless)
		if err != nil {
			t.Errorf("compilePCRE(%q): %v", c.body, err)
			continue
		}
		for _, s := range c.match {
			if !re.MatchString(s) {
				t.Errorf("%q does not match %q", c.body, s)
			}
		}
		for _, s := range c.noMatch {
			if re.MatchString(s) {
				t.Errorf("%q matches %q", c.body, s)
			}
		}
	}
}

// Patterns Go would read differently from PCRE are refused.
func TestCompilePCRERefuses(t *testing.T) {
	for _, body := range []string{
		`(a)\1`, `a(?=b)`, `a(?!b)`, `(?<=a)b`, `(?<!a)b`, `(?>a)`, `a++`, `a*+`, `a?+`, `x{2}+`,
		`(*UTF)a`, `(?x)a b`, `(?U)a+`, `\p{L}`, `\Z`, `[\B]`, `[[:foo:]]`, `[[.a.]]`, `x{,2}`,
		`\x{e9}`, `\xe9`, "é", `a#b`, `\`, `[a`, `\101`,
	} {
		if re, err := compilePCRE(body, '#', false); err == nil {
			t.Errorf("compilePCRE(%q) = %v, want an error", body, re)
		}
	}
}

func TestRejects(t *testing.T) {
	re := mustPCRE(`^abc$`, false)
	for s, want := range map[string]bool{
		"abc":   true,
		"abd":   false,
		"abc\n": true, // PCRE's $ matches before a final newline
		"café":  true, // PCRE matches bytes; refuse rather than guess
	} {
		if got := rejects(re, s); got != want {
			t.Errorf("rejects(%q) = %v", s, got)
		}
	}
}
