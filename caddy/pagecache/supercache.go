package pagecache

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"time"
)

// minSuperCache is the first WP Super Cache release whose serving code
// (wp-cache-phase1.php and wp_cache_serve_cache_file) superCache follows.
const minSuperCache = "3.1.3"

// superCacheAddons are the add-ons WP Super Cache ships in its plugins
// folder. None of them changes which page is served unless its setting
// is on, which loadSuperCache checks.
var superCacheAddons = map[string]bool{
	"awaitingmoderation.php": true, "domain-mapping.php": true, "dynamic-cache-test.php": true,
	"jetpack.php": true, "multisite.php": true, "wptouch.php": true,
}

// superCache sends what WP Super Cache's PHP would send from its
// supercache files, for one site.
type superCache struct {
	root       string
	cachePath  string // ends with "/"
	filePrefix string
	rejected   []*regexp.Regexp // $wpsc_rejected_cookies
	// notLoggedIn is $wp_cache_not_logged_in, compared with == 1 and == 2.
	notLoggedIn value
	extra       []string // $wpsc_cookies
	maxAge      float64  // $cache_max_time when above 0
	homePath    string
	slashCheck  value
	tracking    bool // $wpsc_ignore_tracking_parameters is on
	compression bool // $cache_compression is on

	mobile         bool
	mobileBrowsers string
	mobilePrefixes *string
	mobileGroups   [][]string

	directAll   bool // $cached_direct_pages holds true, which == any URI
	directPages []string
}

func (l *loader) loadSuperCache(root, dropin string) (Status, finder) {
	st := Status{Plugin: "WP Super Cache"}
	fail := func(reason string) (Status, finder) {
		st.Reasons = append(st.Reasons, reason)
		return st, nil
	}
	content := root + "/wp-content"
	home := content + "/plugins/wp-super-cache/"
	dist, err := l.read(home+"advanced-cache.php", maxPHP)
	if err != nil {
		return fail("The WP Super Cache plugin folder (wp-content/plugins/wp-super-cache) is missing or unreadable.")
	}
	header, err := l.head(home+"wp-cache.php", 8<<10)
	if err == nil {
		st.Version = pluginVersion(header)
	}
	switch {
	case st.Version == "":
		return fail("The installed version of WP Super Cache could not be read.")
	case !atLeast(st.Version, minSuperCache):
		return fail(fmt.Sprintf("WP Super Cache %s is older than %s, the first version whose rules the web server follows. Update the plugin.", st.Version, minSuperCache))
	}
	if dropin != dist {
		return fail("wp-content/advanced-cache.php is not the copy WP Super Cache installs. Turn caching off and on again in WP Super Cache to rewrite it.")
	}
	if !l.isFile(home + "wp-cache-phase1.php") {
		return fail("WP Super Cache is missing wp-cache-phase1.php.")
	}

	consts := map[string]string{
		"ABSPATH": root + "/", "WP_CONTENT_DIR": content, "WPCACHEHOME": home, "WPCACHECONFIGPATH": content,
	}
	src, err := l.read(content+"/wp-cache-config.php", maxPHP)
	if err != nil {
		return fail(unreadable("WP Super Cache", "wp-content/wp-cache-config.php", err))
	}
	vars, err := evalPHP(src, consts)
	if err != nil {
		return fail(unreadable("WP Super Cache", "wp-content/wp-cache-config.php", err))
	}
	c, reasons := newSuperCache(root, home, vars)
	if len(reasons) > 0 {
		st.Reasons = append(st.Reasons, reasons...)
		return st, nil
	}
	if reason := l.checkSuperCacheAddons(c, home, vars); reason != "" {
		return fail(reason)
	}
	if reason := l.checkLegacyFiles(c); reason != "" {
		return fail(reason)
	}
	return st, c
}

// newSuperCache reads the settings wp-cache-phase1.php and
// wp_cache_serve_cache_file use.
func newSuperCache(root, home string, vars map[string]value) (*superCache, []string) {
	var reasons []string
	get := func(name string) (value, bool) {
		v, ok := vars[name]
		if ok && v.kind == unknown {
			reasons = append(reasons, fmt.Sprintf("WP Super Cache's setting $%s is set in a way the web server does not read.", name))
		}
		return v, ok && v.kind != null
	}
	text := func(name, def string) string {
		v, set := get(name)
		if !set {
			return def
		}
		s, ok := v.text()
		if !ok {
			reasons = append(reasons, fmt.Sprintf("WP Super Cache's setting $%s has an unexpected value.", name))
		}
		return s
	}
	c := &superCache{root: root}

	c.cachePath = text("cache_path", "")
	switch {
	case c.cachePath == "" || c.cachePath == "/":
		reasons = append(reasons, "WP Super Cache's cache folder is not set.")
	case !strings.HasSuffix(c.cachePath, "/") || !within(c.cachePath, root):
		reasons = append(reasons, "WP Super Cache's cache folder is outside the site's folder.")
	}
	if v, _ := get("cache_enabled"); !v.truthy() && v.kind != unknown {
		reasons = append(reasons, "Caching is off in WP Super Cache.")
	}
	if v, set := get("wp_super_cache_late_init"); set && !(v.kind == integer && v.i == 0) && v.kind != unknown {
		reasons = append(reasons, "Late init is on in WP Super Cache, so cached pages are sent only after WordPress has loaded.")
	}
	if v, _ := get("wp_cache_make_known_anon"); v.truthy() {
		reasons = append(reasons, "WP Super Cache is set to make known users anonymous, which only PHP can do.")
	}
	if v, _ := get("wpsc_save_headers"); v.truthy() {
		reasons = append(reasons, "WP Super Cache is set to cache HTTP headers with page content, which only PHP can send.")
	}
	if v, set := get("wp_cache_mfunc_enabled"); set && !v.equals(0) && v.kind != unknown {
		reasons = append(reasons, "Dynamic caching is on in WP Super Cache, so cached pages are finished in PHP.")
	}
	for _, addon := range []string{"cache_jetpack", "cache_wptouch"} {
		if v, set := get(addon); set && (v.kind == str && v.s == "1") {
			reasons = append(reasons, "A WP Super Cache add-on that chooses mobile pages in PHP (Jetpack or WPTouch) is on.")
		}
	}

	if v, set := get("wpsc_rejected_cookies"); set && v.kind == array {
		exprs, ok := v.strings()
		if !ok {
			reasons = append(reasons, "WP Super Cache's rejected cookies have an unexpected value.")
		}
		for _, expr := range exprs {
			if expr == "" {
				continue
			}
			re, err := compilePCRE(expr, '~', false)
			if err != nil {
				reasons = append(reasons, fmt.Sprintf("A rejected-cookie pattern in WP Super Cache uses regular expression features the web server does not reproduce exactly (%v).", err))
				continue
			}
			c.rejected = append(c.rejected, re)
		}
	}
	c.notLoggedIn, _ = get("wp_cache_not_logged_in")
	if v, set := get("wpsc_cookies"); set && v.kind == array {
		names, ok := v.strings()
		if !ok {
			reasons = append(reasons, "WP Super Cache's extra cookies have an unexpected value.")
		}
		c.extra = names
	}
	if v, set := get("cache_max_time"); set {
		switch v.kind {
		case integer:
			c.maxAge = float64(v.i)
		case float:
			c.maxAge = v.f
		case boolean:
			if v.b {
				c.maxAge = 1
			}
		case str:
			f, ok := phpNumber(v.s)
			if !ok {
				reasons = append(reasons, "WP Super Cache's cache timeout is not a number.")
			}
			c.maxAge = f
		default:
			reasons = append(reasons, "WP Super Cache's cache timeout is not a number.")
		}
	}
	c.homePath = "/"
	if v, set := get("wp_cache_home_path"); set {
		if v.kind != str {
			reasons = append(reasons, "WP Super Cache's setting $wp_cache_home_path has an unexpected value.")
		}
		c.homePath = v.s
	}
	c.slashCheck, _ = get("wp_cache_slash_check")
	c.filePrefix = text("file_prefix", "")
	if v, _ := get("cache_compression"); v.truthy() {
		c.compression = true
	}
	if on, _ := get("wpsc_ignore_tracking_parameters"); on.truthy() {
		if params, _ := get("wpsc_tracking_parameters"); params.truthy() {
			c.tracking = true
		}
	}

	if v, _ := get("wp_cache_mobile_enabled"); v.truthy() {
		c.mobile = true
		c.mobileBrowsers = text("wp_cache_mobile_browsers", "")
		if v, set := get("wp_cache_mobile_prefixes"); set {
			s, ok := v.text()
			if !ok {
				reasons = append(reasons, "WP Super Cache's mobile prefixes have an unexpected value.")
			}
			c.mobilePrefixes = &s
		}
		if v, set := get("wp_cache_mobile_groups"); set {
			groups, ok := castGroups(v)
			if !ok {
				reasons = append(reasons, "WP Super Cache's mobile groups have an unexpected value.")
			}
			c.mobileGroups = groups
		}
	}

	if v, set := get("cached_direct_pages"); set && v.kind == array {
		for _, it := range v.items {
			switch it.val.kind {
			case str:
				c.directPages = append(c.directPages, it.val.s)
			case boolean:
				c.directAll = c.directAll || it.val.b
			case null, integer, float, array:
				// Never == a REQUEST_URI, which starts with '/'.
			default:
				reasons = append(reasons, "WP Super Cache's direct pages have an unexpected value.")
			}
		}
	}
	return c, reasons
}

// castGroups is (array) applied to $wp_cache_mobile_groups and to each
// of its elements.
func castGroups(v value) ([][]string, bool) {
	cast := func(v value) ([]string, bool) {
		switch v.kind {
		case array:
			return v.strings()
		case null:
			return nil, true
		default:
			s, ok := v.text()
			return []string{s}, ok
		}
	}
	if v.kind != array {
		g, ok := cast(v)
		return [][]string{g}, ok
	}
	var out [][]string
	for _, it := range v.items {
		g, ok := cast(it.val)
		if !ok {
			return nil, false
		}
		out = append(out, g)
	}
	return out, true
}

// checkSuperCacheAddons makes sure wp-cache-phase1.php loads no PHP
// beyond the add-ons WP Super Cache ships.
func (l *loader) checkSuperCacheAddons(c *superCache, home string, vars map[string]value) string {
	dir := home + "plugins"
	if v, ok := vars["wp_cache_plugins_dir"]; ok && v.kind != null {
		if s, ok := v.text(); !ok || strings.TrimSuffix(s, "/") != dir {
			return "WP Super Cache loads its add-ons from a custom folder."
		}
	}
	if v, ok := vars["wpsc_plugins"]; ok && (v.kind == unknown || v.kind == array && len(v.items) > 0) {
		return "WP Super Cache is set to load extra add-on files ($wpsc_plugins)."
	}
	entries, err := l.readDir(dir)
	if err != nil && !isNotExist(err) {
		return "The web server cannot list WP Super Cache's add-ons."
	}
	for _, e := range entries {
		if phpGlob(e.Name()) && !superCacheAddons[e.Name()] {
			return fmt.Sprintf("WP Super Cache has an add-on (%s) that did not come with it, so PHP may change which page is sent.", e.Name())
		}
	}
	entries, err = l.readDir(path.Clean(home + "../wp-super-cache-plugins"))
	if err != nil && !isNotExist(err) {
		return "The web server cannot list wp-content/plugins/wp-super-cache-plugins."
	}
	for _, e := range entries {
		if phpGlob(e.Name()) {
			return "WP Super Cache loads add-ons from wp-content/plugins/wp-super-cache-plugins, so PHP may change which page is sent."
		}
	}
	return ""
}

// phpGlob reports whether glob("*.php") lists name.
func phpGlob(name string) bool {
	return strings.HasSuffix(name, ".php") && !strings.HasPrefix(name, ".")
}

// superCacheBlogs caps how many folders the cache's blogs folder may have.
const superCacheBlogs = 200

// checkLegacyFiles looks for cache files of WP Super Cache's older
// "WP-Cache" format outside the supercache folders, which PHP would send
// before a supercache file: in the cache folder itself, where a single
// site keeps them, or in blogs/<blog>/, where a multisite network does.
// Those inside the supercache folders are checked per request.
func (l *loader) checkLegacyFiles(c *superCache) string {
	const unlisted = "The web server cannot list WP Super Cache's cache folder."
	const legacy = "WP Super Cache's cache folder holds pages in its older format, which PHP sends instead. Delete the cache in WP Super Cache."
	holdsLegacy := func(dir string) (bool, error) {
		entries, err := l.readDir(dir)
		if err != nil && !isNotExist(err) {
			return false, err
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), c.filePrefix) && strings.HasSuffix(e.Name(), ".php") {
				return true, nil
			}
		}
		return false, nil
	}
	dir := strings.TrimSuffix(c.cachePath, "/")
	if found, err := holdsLegacy(dir); err != nil {
		return unlisted
	} else if found {
		return legacy
	}
	blogs, err := l.readDir(dir + "/blogs")
	if err != nil && !isNotExist(err) {
		return unlisted
	}
	if len(blogs) > superCacheBlogs {
		return "WP Super Cache's cache folder has more blog folders than the web server checks."
	}
	for _, b := range blogs {
		switch {
		case b.Type().IsRegular():
		case b.IsDir():
			if found, err := holdsLegacy(dir + "/blogs/" + b.Name()); err != nil {
				return unlisted
			} else if found {
				return legacy
			}
		default:
			return "WP Super Cache's blogs folder holds something other than files and folders, which the web server does not follow."
		}
	}
	return ""
}

func (c *superCache) find(fsys fs.FS, req *request, now time.Time) *hit {
	// wp-cache-base.php
	host, _, certain := req.header("HTTP_HOST")
	if !certain || host == "" || host == "0" || !simpleHost(host) {
		return nil
	}
	host = asciiLower(host)

	// wp-cache-phase1.php
	uri := req.uri
	if c.tracking && !simpleURI(uri) {
		// wpsc_remove_tracking_params_from_uri rebuilds the URI with
		// parse_url, which only leaves simple ones as they are.
		return nil
	}
	for _, re := range c.rejected {
		for _, name := range req.cookies {
			if rejects(re, name) {
				return nil
			}
		}
	}
	if c.notLoggedIn.equals(2) && hasAuthCookie(req.cookies) {
		return nil
	}
	if c.notLoggedIn.equals(1) && len(req.cookies) > 0 {
		return nil
	}

	// wp_cache_serve_cache_file
	accept, set, certain := req.header("HTTP_ACCEPT")
	if !certain || !acceptsHTML(accept, set) {
		return nil
	}
	for _, name := range req.cookies {
		if strings.HasPrefix(name, "wp-postpass_") || strings.HasPrefix(name, "comment_author_") ||
			strings.HasPrefix(name, "wordpress_logged_in_") || contains(c.extra, name) {
			return nil
		}
	}
	if !(uri == c.homePath || c.slashCheck.truthy() && strings.HasSuffix(uri, "/") ||
		c.slashCheck.equals(0) && !strings.HasSuffix(uri, "/")) {
		return nil
	}
	dir, ok := c.dir(host, uri)
	if !ok {
		return nil
	}
	name, ok := c.filename(req)
	if !ok {
		return nil
	}
	// PHP first sends a page saved in the older format for this visitor,
	// whose meta file is next to the supercache file and named after $key.
	// On a multisite network $key starts with the blog's name, so a meta
	// file whose name is not a bare md5 may be this visitor's.
	key, ok := c.legacyKey(host, uri, req)
	if !ok {
		return nil
	}
	entries, err := fs.ReadDir(fsys, strings.TrimSuffix(dir, "/"))
	if err != nil {
		return nil
	}
	for _, e := range entries {
		rest, ok := strings.CutPrefix(e.Name(), "meta-"+c.filePrefix)
		if !ok {
			continue
		}
		if sum, ok := strings.CutSuffix(rest, ".php"); ok && (sum == key || !isMD5(sum)) {
			return nil
		}
	}
	h := open(fsys, dir+name)
	if h == nil {
		return nil
	}
	if c.maxAge > 0 && float64(h.info.ModTime().Unix())+c.maxAge < float64(now.Unix()) {
		h.file.Close()
		return nil
	}
	h.header = [][2]string{{"Vary", "Accept-Encoding, Cookie"}, {"Cache-Control", "max-age=3, must-revalidate"}}
	return h
}

// legacyKey is the $key wp_super_cache_init gives the visitor on a single
// site: the md5 of get_wp_cache_key. find has already made sure no cookie
// goes into it and that the visitor accepts HTML.
func (c *superCache) legacyKey(host, uri string, req *request) (string, bool) {
	if !canonicalPort(req.port) {
		return "", false // intval() would read it differently
	}
	key := host + req.port + strings.ReplaceAll(uri, "/index.php", "/")
	if c.compression {
		// gzip_accepted
		encoding, set, certain := req.header("HTTP_ACCEPT_ENCODING")
		if !certain {
			return "", false
		}
		if set && strings.Contains(encoding, "gzip") {
			key += "gzip"
		}
	}
	key += "-text/html"
	if c.mobile {
		var ok bool
		if key, ok = c.checkMobile(key, req); !ok {
			return "", false
		}
	}
	sum := md5.Sum([]byte(key))
	return hex.EncodeToString(sum[:]), true
}

// isMD5 reports whether s is a hash as PHP's md5() prints it.
func isMD5(s string) bool {
	return len(s) == 32 && strings.Trim(s, "0123456789abcdef") == ""
}

// direct reports whether REQUEST_URI is one of $cached_direct_pages.
func (c *superCache) direct(uri string) bool {
	return c.directAll || contains(c.directPages, uri)
}

// dir is get_current_url_supercache_dir.
func (c *superCache) dir(host, uri string) (string, bool) {
	u := normalizeURICase(uri)
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	u = strings.Map(func(r rune) rune {
		if strings.ContainsRune(" <>'\"\r\n\t()", r) {
			return -1
		}
		return r
	}, u)
	u = deepReplace(u, "..", `\`, "index.php")
	host, _, _ = strings.Cut(host, ":")
	dir := c.cachePath + "supercache/" + host + u + "/"
	if c.direct(uri) {
		dir = c.root + "/" + u + "/"
	}
	dir = strings.ReplaceAll(strings.ReplaceAll(dir, "//", "/"), "..", "")
	if len(dir)+64 > 4096 { // PHP_MAXPATHLEN
		return "", false
	}
	return dir, true
}

// filename is supercache_filename, with wp_cache_check_mobile as the
// supercache_filename_str filter.
func (c *superCache) filename(req *request) (string, bool) {
	extra := ""
	proto, set, certain := req.header("HTTP_X_FORWARDED_PROTO")
	if !certain {
		return "", false
	}
	if req.https || set && asciiLower(proto) == "https" {
		extra = "-https"
	}
	if c.mobile {
		var ok bool
		if extra, ok = c.checkMobile(extra, req); !ok {
			return "", false
		}
	}
	if c.direct(req.uri) {
		extra = ""
	}
	extra = strings.Map(func(r rune) rune {
		if r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, extra)
	return "index" + extra + ".html", true
}

// checkMobile is wp_cache_check_mobile.
func (c *superCache) checkMobile(key string, req *request) (string, bool) {
	ua, set, certain := req.header("HTTP_USER_AGENT")
	if !certain {
		return "", false
	}
	if !set {
		return key, true
	}
	ua = asciiLower(ua)
	for _, browser := range strings.Split(c.mobileBrowsers, ",") {
		if strstr(ua, phpTrim(asciiLower(browser))) {
			return key + "-" + c.mobileGroup(ua), true
		}
	}
	for _, name := range []string{"HTTP_X_WAP_PROFILE", "HTTP_PROFILE"} {
		v, set, certain := req.header(name)
		if !certain {
			return "", false
		}
		if set {
			return key + "-" + v, true
		}
	}
	if c.mobilePrefixes != nil {
		start := ua[:min(4, len(ua))]
		for _, prefix := range strings.Split(*c.mobilePrefixes, ",") {
			if looseEquals(start, prefix) {
				return key + "-" + prefix, true
			}
		}
	}
	accept, _, certain := req.header("HTTP_ACCEPT")
	if !certain {
		return "", false
	}
	if strings.Contains(asciiLower(accept), "wap") {
		return key + "-wap", true
	}
	return key, true
}

// mobileGroup is wp_cache_mobile_group.
func (c *superCache) mobileGroup(ua string) string {
	for _, group := range c.mobileGroups {
		for _, browser := range group {
			browser = phpTrim(asciiLower(browser))
			if browser != "" && strstr(ua, browser) {
				return browser
			}
		}
	}
	return "mobile"
}

// hasAuthCookie reports whether wpsc_get_auth_cookies finds a cookie,
// with WordPress's cookie constants not yet defined.
func hasAuthCookie(names []string) bool {
	for _, name := range names {
		for _, prefix := range []string{"wordpress_", "wordpress_sec_", "wordpress_logged_in_", "comment_author_", "wp-postpass_"} {
			if rest, ok := strings.CutPrefix(name, prefix); ok && rest != "" && strings.Trim(rest, "0123456789abcdef") == "" {
				return true
			}
		}
	}
	return false
}

var jsonTypes = []string{"application/json", "application/activity+json", "application/ld+json"}

// acceptsHTML reports whether wpsc_get_accept_header returns text/html.
func acceptsHTML(accept string, set bool) bool {
	if !set {
		accept = ""
	}
	raw := asciiLower(accept)
	if raw == "" || raw == "0" {
		return true
	}
	htmlQ, jsonQ := -1.0, 0.0
	for _, part := range strings.Split(raw, ",") {
		segments := strings.Split(phpTrim(part), ";")
		media := phpTrim(segments[0])
		q := 1.0
		for _, s := range segments[1:] {
			if param := strings.TrimLeft(s, phpTrimChars); strings.HasPrefix(param, "q=") {
				if f, ok := phpNumber(param[2:]); ok {
					q = max(0, min(1, f))
				}
				break
			}
		}
		if media == "text/html" {
			htmlQ = max(htmlQ, q)
		}
		if contains(jsonTypes, media) {
			jsonQ = max(jsonQ, q)
		}
	}
	return !(jsonQ > max(htmlQ, 0))
}

// normalizeURICase is wpsc_normalize_uri_case: percent escapes in upper
// case, letters in lower case.
func normalizeURICase(uri string) string {
	b := []byte(uri)
	for i := 0; i < len(b); i++ {
		switch c := b[i]; {
		case c == '%' && i+2 < len(b) && isHex(b[i+1]) && isHex(b[i+2]):
			b[i+1], b[i+2] = upper(b[i+1]), upper(b[i+2])
			i += 2
		case c >= 'A' && c <= 'Z':
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// deepReplace is wpsc_deep_replace: str_replace with every search, again
// until nothing is replaced.
func deepReplace(s string, search ...string) string {
	for {
		n := 0
		for _, t := range search {
			if c := strings.Count(s, t); c > 0 {
				s = strings.ReplaceAll(s, t, "")
				n += c
			}
		}
		if n == 0 {
			return s
		}
	}
}
