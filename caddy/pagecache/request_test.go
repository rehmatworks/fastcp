package pagecache

import (
	"net/http/httptest"
	"slices"
	"testing"
)

func TestNewRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "https://Example.com:8443/a%20b/", nil)
	r.Header.Set("Connection", "X-Custom, keep-alive")
	r.Header.Set("X-Custom", "dropped by reverse_proxy")
	r.Header.Set("Keep-Alive", "timeout=5")
	r.Header.Set("Proxy", "http://evil.example")
	r.Header.Set("X-Forwarded-Proto", "http")
	r.Header.Set("Accept-Language", "fr")
	r.Header["Accept_language"] = []string{"en"}
	r.Header.Set("X-Same", "1")
	r.Header["X_same"] = []string{"1"}
	r.Header.Add("Cookie", "a=1; b.c=2; d[e]=3; f[g=4;  %68i=5")
	r.Header.Add("Cookie", "z=6")

	req := newRequest(r, "/a%20b/")
	if req.uri != "/a%20b/" || !req.https || req.port != "8443" {
		t.Errorf("uri %q, https %v, port %q", req.uri, req.https, req.port)
	}
	for name, want := range map[string]struct {
		value        string
		set, certain bool
	}{
		"HTTP_HOST":              {"Example.com:8443", true, true},
		"HTTP_USER_AGENT":        {"", true, true}, // reverse_proxy sends an empty one
		"HTTP_X_FORWARDED_PROTO": {"https", true, true},
		"HTTP_X_CUSTOM":          {"", false, true},
		"HTTP_KEEP_ALIVE":        {"", false, true},
		"HTTP_PROXY":             {"", false, true},
		"HTTP_ACCEPT_LANGUAGE":   {"", true, false}, // two headers, one entry
		"HTTP_X_SAME":            {"1", true, true}, // two headers, one value
	} {
		value, set, certain := req.header(name)
		if set != want.set || certain != want.certain || set && certain && value != want.value {
			t.Errorf("%s = %q, %v, %v; want %+v", name, value, set, certain, want)
		}
	}
	for _, name := range []string{"a", "b.c", "b_c", "d[e]", "d", "f[g", "f_g", "%68i", "hi", "z"} {
		if !req.hasCookie(name) {
			t.Errorf("cookie %q missing from %q", name, req.cookies)
		}
	}

	plain := newRequest(httptest.NewRequest("GET", "http://example.com/", nil), "/")
	if plain.https || plain.port != "80" {
		t.Errorf("http: https %v, port %q", plain.https, plain.port)
	}
	if v, _, _ := plain.header("HTTP_X_FORWARDED_PROTO"); v != "http" {
		t.Errorf("X-Forwarded-Proto %q", v)
	}
	if secure := newRequest(httptest.NewRequest("GET", "https://example.com/", nil), "/"); secure.port != "443" {
		t.Errorf("https port %q", secure.port)
	}
}

func TestPHPVarName(t *testing.T) {
	for name, want := range map[string]string{
		"plain":    "plain",
		" a b.c":   "a_b_c",
		"a[b]":     "a",
		"a.b[c]":   "a_b",
		"a[b":      "a_b",
		"a.b[c.d":  "a_b_c.d",
		"a]b":      "a]b",
		"[x]":      "",
		"wp-x_y.z": "wp-x_y_z",
	} {
		if got := phpVarName(name); got != want {
			t.Errorf("phpVarName(%q) = %q, want %q", name, got, want)
		}
	}
	if names := cookieNames("a=1;b=2, c=3;; =4;d"); !slices.Equal(names, []string{"a", "b", "c", "d"}) {
		t.Errorf("cookieNames = %q", names)
	}
}
