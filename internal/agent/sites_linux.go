//go:build linux

package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rehmatworks/fastcp/internal/validate"
)

func (a *Agent) handleCreateSite(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[CreateSiteRequest](params)
	if err != nil {
		return nil, err
	}
	t, err := a.registeredTenant(req.Username)
	if err != nil {
		return nil, err
	}
	if err := validate.Slug(req.Slug); err != nil {
		return nil, invalid(err)
	}
	domain, err := validate.Domain(req.Domain)
	if err != nil {
		return nil, invalid(err)
	}
	site, err := ensureSiteLayout(t, req.Slug, true)
	if err != nil {
		return nil, err
	}
	defer site.Close()
	pub, err := site.OpenDir("public")
	if err != nil {
		return nil, err
	}
	defer pub.Close()
	page := welcomePage(domain, filepath.Join(t.Home, "apps", req.Slug, "public"))
	if err := pub.WriteFile("index.php", []byte(page), 0o644, t.UID, t.GID); err != nil {
		return nil, err
	}
	return map[string]string{"path": site.Path()}, nil
}

func (a *Agent) handleDeleteSite(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[DeleteSiteRequest](params)
	if err != nil {
		return nil, err
	}
	if err := validate.Slug(req.Slug); err != nil {
		return nil, invalid(err)
	}
	t, err := lookupTenant(req.Username)
	if err != nil {
		return nil, err
	}
	for _, s := range a.currentState().Sites {
		if s.Username == req.Username && s.Slug == req.Slug {
			return nil, conflictf("site %s is still configured; apply the configuration without it first", req.Slug)
		}
	}

	home, err := openHome(t)
	if err != nil {
		return nil, err
	}
	defer home.Close()
	apps, err := home.OpenDir("apps")
	if err == nil {
		err = apps.RemoveAll(req.Slug)
		apps.Close()
		if err != nil {
			return nil, err
		}
	}
	_ = os.RemoveAll(filepath.Join(SiteLogRoot, t.Name, req.Slug))
	return map[string]string{"status": "deleted"}, nil
}

// ---------------------------------------------------------------------------
// WordPress

var wpVersionRE = regexp.MustCompile(`^\d+\.\d+(\.\d+)?$`)

func httpGet(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FastCP")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("GET %s: response too large", url)
	}
	return data, nil
}

// downloadWordPress fetches the current release and verifies the checksum
// wordpress.org publishes for it.
func downloadWordPress(ctx context.Context) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	meta, err := httpGet(ctx, "https://api.wordpress.org/core/version-check/1.7/", 1<<20)
	if err != nil {
		return nil, "", fmt.Errorf("could not look up the latest WordPress release: %w", err)
	}
	var vc struct {
		Offers []struct {
			Current string `json:"current"`
		} `json:"offers"`
	}
	if err := json.Unmarshal(meta, &vc); err != nil || len(vc.Offers) == 0 || !wpVersionRE.MatchString(vc.Offers[0].Current) {
		return nil, "", fmt.Errorf("unexpected response from the WordPress version API")
	}
	ver := vc.Offers[0].Current
	base := "https://wordpress.org/wordpress-" + ver + ".tar.gz"
	sum, err := httpGet(ctx, base+".sha1", 1024)
	if err != nil {
		return nil, "", err
	}
	want := strings.TrimSpace(string(sum))
	if len(want) != 40 {
		return nil, "", fmt.Errorf("unexpected WordPress checksum format")
	}
	tarball, err := httpGet(ctx, base, 256<<20)
	if err != nil {
		return nil, "", err
	}
	got := sha1.Sum(tarball)
	if hex.EncodeToString(got[:]) != strings.ToLower(want) {
		return nil, "", fmt.Errorf("WordPress %s download failed checksum verification", ver)
	}
	return tarball, ver, nil
}

func (a *Agent) handleInstallWordPress(ctx context.Context, params json.RawMessage) (any, error) {
	req, err := decode[InstallWordPressRequest](params)
	if err != nil {
		return nil, err
	}
	t, err := a.registeredTenant(req.Username)
	if err != nil {
		return nil, err
	}
	if err := validate.Slug(req.Slug); err != nil {
		return nil, invalid(err)
	}
	if err := validateDBRequest(req.DBName, req.DBUser, req.DBPass); err != nil {
		return nil, err
	}

	tarball, ver, err := downloadWordPress(ctx)
	if err != nil {
		return nil, err
	}
	site, err := ensureSiteLayout(t, req.Slug, false)
	if err != nil {
		return nil, err
	}
	defer site.Close()

	if err := createDatabase(ctx, req.DBName, req.DBUser, req.DBPass); err != nil {
		return nil, err
	}
	public := filepath.Join(t.Home, "apps", req.Slug, "public")
	res, err := execAs(ctx, t, execOptions{
		Args:    []string{"tar", "-xzf", "-", "--strip-components=1", "--no-same-owner", "--no-same-permissions", "-C", public},
		Stdin:   bytes.NewReader(tarball),
		Timeout: 5 * time.Minute,
	})
	if err == nil && res.ExitCode != 0 {
		err = fmt.Errorf("extracting WordPress failed: %s", strings.TrimSpace(res.Output))
	}
	if err != nil {
		_ = dropDatabase(ctx, req.DBName, req.DBUser)
		return nil, err
	}

	cfg, err := wpConfig(req.DBName, req.DBUser, req.DBPass)
	if err != nil {
		return nil, err
	}
	if err := site.WriteFile("wp-config.php", []byte(cfg), 0o640, t.UID, t.GID); err != nil {
		return nil, err
	}
	grantSiteAccess(t, site, true)
	slog.Info("installed WordPress", "user", t.Name, "site", req.Slug, "version", ver)
	return map[string]string{"version": ver}, nil
}

func randomSalt() (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#%^&*()-_=+[]{}|;:,.<>?~"
	b := make([]byte, 64)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return "", err
		}
		b[i] = chars[n.Int64()]
	}
	return string(b), nil
}

// phpString renders s as a single-quoted PHP string literal.
func phpString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// wpConfig renders wp-config.php for placement one level above public/,
// where the web server cannot read it.
func wpConfig(dbName, dbUser, dbPass string) (string, error) {
	keys := []string{"AUTH_KEY", "SECURE_AUTH_KEY", "LOGGED_IN_KEY", "NONCE_KEY", "AUTH_SALT", "SECURE_AUTH_SALT", "LOGGED_IN_SALT", "NONCE_SALT"}
	var b strings.Builder
	b.WriteString("<?php\n/**\n * WordPress configuration generated by FastCP.\n * This file lives outside the public directory on purpose.\n */\n\n")
	fmt.Fprintf(&b, "define( 'DB_NAME', %s );\n", phpString(dbName))
	fmt.Fprintf(&b, "define( 'DB_USER', %s );\n", phpString(dbUser))
	fmt.Fprintf(&b, "define( 'DB_PASSWORD', %s );\n", phpString(dbPass))
	b.WriteString("define( 'DB_HOST', '127.0.0.1' );\ndefine( 'DB_CHARSET', 'utf8mb4' );\ndefine( 'DB_COLLATE', '' );\n\n")
	for _, k := range keys {
		salt, err := randomSalt()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "define( %s, %s );\n", phpString(k), phpString(salt))
	}
	b.WriteString(`
$table_prefix = 'wp_';

if ( isset( $_SERVER['HTTP_X_FORWARDED_PROTO'] ) && $_SERVER['HTTP_X_FORWARDED_PROTO'] === 'https' ) {
	$_SERVER['HTTPS'] = 'on';
}

define( 'WP_DEBUG', false );
define( 'DISALLOW_FILE_EDIT', true );

if ( ! defined( 'ABSPATH' ) ) {
	define( 'ABSPATH', __DIR__ . '/public/' );
}

require_once ABSPATH . 'wp-settings.php';
`)
	return b.String(), nil
}

// welcomePage is the placeholder index.php for new sites.
func welcomePage(domain, docRoot string) string {
	return `<?php
$fastcp_domain = ` + phpString(domain) + `;
$fastcp_root = ` + phpString(docRoot) + `;
header('Content-Type: text/html; charset=utf-8');
?><!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title><?= htmlspecialchars($fastcp_domain, ENT_QUOTES, 'UTF-8') ?></title>
<style>
:root{color-scheme:light dark;--bg:#fff;--fg:#111;--muted:#666;--line:#e5e5e5;--code:#f5f5f5}
@media (prefers-color-scheme:dark){:root{--bg:#0a0a0a;--fg:#ededed;--muted:#a1a1a1;--line:#262626;--code:#171717}}
*{box-sizing:border-box;margin:0}
body{min-height:100vh;display:grid;place-items:center;padding:2rem;background:var(--bg);color:var(--fg);font:16px/1.6 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}
main{width:100%;max-width:34rem;border:1px solid var(--line);border-radius:12px;padding:2.5rem}
.mark{display:inline-flex;align-items:center;gap:.5rem;font-weight:600;letter-spacing:-.01em;margin-bottom:2rem}
.mark span{width:.75rem;height:.75rem;border-radius:50%;background:var(--fg)}
h1{font-size:1.5rem;letter-spacing:-.02em;margin-bottom:.25rem}
.domain{color:var(--muted);margin-bottom:1.5rem;word-break:break-all}
p{color:var(--muted)}
code{display:block;margin-top:.75rem;padding:.75rem 1rem;border-radius:8px;background:var(--code);color:var(--fg);font:13px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;word-break:break-all}
footer{margin-top:2rem;font-size:.875rem;color:var(--muted)}
footer a{color:inherit}
</style>
</head>
<body>
<main>
<div class="mark"><span></span>FastCP</div>
<h1>This site is ready</h1>
<p class="domain"><?= htmlspecialchars($fastcp_domain, ENT_QUOTES, 'UTF-8') ?></p>
<p>Upload your files to replace this page:</p>
<code><?= htmlspecialchars($fastcp_root, ENT_QUOTES, 'UTF-8') ?></code>
<footer>Served by <a href="https://github.com/rehmatworks/fastcp" rel="noopener">FastCP</a></footer>
</main>
</body>
</html>
`
}

const suspendedPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Site unavailable</title>
<style>
:root{color-scheme:light dark;--bg:#fff;--fg:#111;--muted:#666;--line:#e5e5e5}
@media (prefers-color-scheme:dark){:root{--bg:#0a0a0a;--fg:#ededed;--muted:#a1a1a1;--line:#262626}}
*{box-sizing:border-box;margin:0}
body{min-height:100vh;display:grid;place-items:center;padding:2rem;background:var(--bg);color:var(--fg);font:16px/1.6 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}
main{max-width:30rem;border:1px solid var(--line);border-radius:12px;padding:2.5rem;text-align:center}
h1{font-size:1.5rem;letter-spacing:-.02em;margin-bottom:.5rem}
p{color:var(--muted)}
</style>
</head>
<body>
<main>
<h1>This site is temporarily unavailable</h1>
<p>The account hosting this site has been suspended. If you own this site, please contact your hosting provider.</p>
</main>
</body>
</html>
`
