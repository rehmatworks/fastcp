package api

import (
	"html"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

const (
	pmaLinkTTL    = time.Minute
	pmaSessionTTL = time.Hour
	pmaCookie     = "fastcp_pma"
	pmaPrefix     = "/phpmyadmin"
	// pmaLinkParam carries a one-time link. phpMyAdmin's scripts put its
	// own "token" in query strings, so the name must not be "token".
	pmaLinkParam = "fastcp_link"
)

type pmaGrant struct {
	SessionHash string
	DBUser      string
	DBPass      string
	DBName      string
}

// pmaStore holds one-time links handed to the browser and the phpMyAdmin
// sessions they are exchanged for.
type pmaStore struct {
	links    *tokenStore[pmaGrant]
	sessions *tokenStore[pmaGrant]
}

func newPMAStore() *pmaStore {
	return &pmaStore{links: newTokenStore[pmaGrant](), sessions: newTokenStore[pmaGrant]()}
}

func (p *pmaStore) gc(now time.Time) {
	p.links.gc(now)
	p.sessions.gc(now)
}

func pmaMessage(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>phpMyAdmin</title>` +
		`<body style="font:15px system-ui;max-width:32rem;margin:15vh auto;padding:0 1rem;color:#111">` +
		`<h1 style="font-size:18px">phpMyAdmin</h1><p>` + html.EscapeString(msg) + `</p><p><a href="/databases">Back to FastCP</a></p>`))
}

func (s *Server) phpMyAdmin(w http.ResponseWriter, r *http.Request) {
	if link := r.URL.Query().Get(pmaLinkParam); link != "" {
		g, ok := s.pma.links.take(link)
		if !ok || !s.sessionAlive(r, g.SessionHash) {
			pmaMessage(w, http.StatusForbidden, "This link has expired. Open phpMyAdmin again from the Databases page.")
			return
		}
		sid := s.pma.sessions.put(g, pmaSessionTTL)
		http.SetCookie(w, &http.Cookie{
			Name: pmaCookie, Value: sid, Path: pmaPrefix + "/", MaxAge: int(pmaSessionTTL.Seconds()),
			HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
		})
		http.Redirect(w, r, pmaPrefix+"/index.php?route=/database/structure&db="+url.QueryEscape(g.DBName), http.StatusFound)
		return
	}

	c, err := r.Cookie(pmaCookie)
	if err != nil {
		pmaMessage(w, http.StatusUnauthorized, "Open phpMyAdmin from the Databases page in FastCP.")
		return
	}
	g, ok := s.pma.sessions.peek(c.Value)
	if ok && !s.sessionAlive(r, g.SessionHash) {
		s.pma.sessions.remove(c.Value)
		ok = false
	}
	if !ok {
		pmaMessage(w, http.StatusUnauthorized, "Your phpMyAdmin session has ended. Open it again from the Databases page.")
		return
	}

	backend := s.cfg.PMABackend
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = backend
			pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, pmaPrefix)
			pr.Out.URL.RawPath = ""
			if pr.Out.URL.Path == "" {
				pr.Out.URL.Path = "/"
			}
			pr.Out.Host = backend
			pr.SetXForwarded()
			pr.Out.Header.Set("X-Forwarded-Proto", "https")
			pr.Out.Header.Del("Authorization")
			stripCookie(pr.Out, pmaCookie)
			pr.Out.SetBasicAuth(g.DBUser, g.DBPass)
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("WWW-Authenticate")
			if loc := resp.Header.Get("Location"); loc != "" {
				resp.Header.Set("Location", prefixLocation(loc))
			}
			if cookies := resp.Cookies(); len(cookies) > 0 {
				resp.Header.Del("Set-Cookie")
				for _, ck := range cookies {
					if !strings.HasPrefix(ck.Path, pmaPrefix+"/") {
						ck.Path = pmaPrefix + "/"
					}
					ck.Secure = true
					resp.Header.Add("Set-Cookie", ck.String())
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			pmaMessage(w, http.StatusBadGateway, "phpMyAdmin is not responding. Check that the web server and PHP are running.")
		},
	}
	proxy.ServeHTTP(w, r)
}

// sessionAlive reports whether the panel session that opened phpMyAdmin is
// still signed in.
func (s *Server) sessionAlive(r *http.Request, hash string) bool {
	sess, err := s.db.GetSession(r.Context(), hash)
	if err != nil {
		return false
	}
	now := time.Now()
	return now.Before(sess.ExpiresAt) && now.Sub(sess.LastSeenAt) <= sessionIdle
}

func stripCookie(r *http.Request, name string) {
	cookies := r.Cookies()
	r.Header.Del("Cookie")
	for _, c := range cookies {
		if c.Name != name {
			r.AddCookie(c)
		}
	}
}

// prefixLocation maps a redirect from the backend's root to the panel's
// /phpmyadmin/ path.
func prefixLocation(loc string) string {
	u, err := url.Parse(loc)
	if err != nil {
		return pmaPrefix + "/"
	}
	if u.Host != "" {
		// Absolute redirects keep only the path so they stay on the panel.
		u.Scheme, u.Host, u.User = "", "", nil
	}
	if !strings.HasPrefix(u.Path, "/") {
		return u.String()
	}
	if !strings.HasPrefix(u.Path, pmaPrefix+"/") {
		u.Path = pmaPrefix + u.Path
	}
	return u.String()
}
