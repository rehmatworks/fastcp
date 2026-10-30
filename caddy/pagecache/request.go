package pagecache

import (
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

// request is a request as PHP sees it behind php_fastcgi: the $_SERVER
// entries the cache plugins read, and the names $_COOKIE may have.
type request struct {
	uri   string // REQUEST_URI
	https bool   // HTTPS is "on"; Caddy sets nothing otherwise
	port  string // SERVER_PORT
	// env holds the values of each HTTP_* entry. Header names that differ
	// only in '-' and '_' share an entry, and which one PHP gets is up to
	// Go's map order, so an entry with two different values is ambiguous.
	env map[string][]string
	// cookies lists every name $_COOKIE may have: the raw name and PHP's
	// spellings of it.
	cookies []string
}

// hopHeaders are the headers reverse_proxy removes before php_fastcgi
// passes the rest to PHP.
var hopHeaders = []string{
	"Alt-Svc", "Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

// newRequest builds PHP's view of r, which php_fastcgi would send with
// origURI as REQUEST_URI.
func newRequest(r *http.Request, origURI string) *request {
	h := r.Header.Clone()
	if h == nil {
		h = http.Header{}
	}
	// What reverse_proxy does to the headers before the FastCGI transport
	// turns them into HTTP_* entries.
	if _, ok := h["User-Agent"]; !ok {
		h.Set("User-Agent", "")
	}
	for _, v := range h["Connection"] {
		for _, token := range strings.Split(v, ",") {
			if token = textproto.TrimString(token); token != "" {
				h.Del(token)
			}
		}
	}
	for _, name := range hopHeaders {
		h.Del(name)
	}
	proto := "http"
	if r.TLS != nil {
		proto = "https"
	}
	h.Set("X-Forwarded-Proto", proto)

	req := &request{uri: origURI, https: r.TLS != nil, env: map[string][]string{}}
	if _, port, err := net.SplitHostPort(r.Host); err == nil && port != "" {
		req.port = port
	} else if r.TLS != nil {
		req.port = "443"
	} else {
		req.port = "80"
	}
	replacer := strings.NewReplacer(" ", "_", "-", "_")
	for name, values := range h {
		if http.CanonicalHeaderKey(name) == "Proxy" {
			continue
		}
		key := "HTTP_" + replacer.Replace(strings.ToUpper(name))
		req.env[key] = append(req.env[key], strings.Join(values, ", "))
	}
	// The transport sets HTTP_HOST to r.Host, then lets headers overwrite it.
	if len(req.env["HTTP_HOST"]) == 0 {
		req.env["HTTP_HOST"] = []string{r.Host}
	}
	if values, ok := req.server("HTTP_COOKIE"); ok {
		for _, v := range values {
			req.cookies = append(req.cookies, cookieNames(v)...)
		}
	} else {
		// Ambiguous: count every value.
		for _, v := range req.env["HTTP_COOKIE"] {
			req.cookies = append(req.cookies, cookieNames(v)...)
		}
	}
	return req
}

// server returns $_SERVER[name] for an HTTP_* entry: its value if set,
// and false when PHP may see either of two values.
func (r *request) server(name string) ([]string, bool) {
	values := r.env[name]
	for _, v := range values[min(1, len(values)):] {
		if v != values[0] {
			return values, false
		}
	}
	return values[:min(1, len(values))], true
}

// header returns the value of an HTTP_* entry, whether it is set, and
// whether PHP's value is certain.
func (r *request) header(name string) (value string, set, certain bool) {
	values, certain := r.server(name)
	if len(values) == 0 {
		return "", false, true
	}
	return values[0], true, certain
}

// hasCookie reports whether $_COOKIE may have name.
func (r *request) hasCookie(name string) bool {
	for _, c := range r.cookies {
		if c == name {
			return true
		}
	}
	return false
}

// cookieNames lists the names PHP may give the cookies in a Cookie
// header. PHP splits on ';', but repeated Cookie headers reach it joined
// with ", ", so ',' splits too: listing a name PHP does not have only
// makes a check stricter. Each name is listed raw, URL-decoded (as PHP
// before 8.0.24 did), and with the changes PHP makes to variable names.
func cookieNames(header string) []string {
	var out []string
	for _, pair := range strings.FieldsFunc(header, func(c rune) bool { return c == ';' || c == ',' }) {
		pair = strings.TrimLeft(pair, phpSpace)
		name, _, _ := strings.Cut(pair, "=")
		if name == "" {
			continue
		}
		forms := []string{name}
		if decoded, err := url.QueryUnescape(name); err == nil && decoded != name {
			forms = append(forms, decoded)
		}
		for _, f := range forms {
			out = append(out, f)
			if m := phpVarName(f); m != "" && m != f {
				out = append(out, m)
			}
		}
	}
	return out
}

// phpVarName is the key php_register_variable_ex gives a variable name:
// leading spaces dropped, spaces and dots before the first '[' turned
// into '_', and a closed [...] making the name an array under the part
// before it. An unclosed '[' becomes '_'.
func phpVarName(name string) string {
	name = strings.TrimLeft(name, " ")
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		switch c := name[i]; c {
		case ' ', '.':
			b.WriteByte('_')
		case '[':
			if strings.IndexByte(name[i+1:], ']') >= 0 {
				return b.String()
			}
			b.WriteByte('_')
			b.WriteString(name[i+1:])
			return b.String()
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
