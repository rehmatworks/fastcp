package pagecache

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// minRocket is the first WP Rocket release whose serving code (the
// classes in inc/classes/Buffer) rocket follows.
const minRocket = "3.13.3"

// lowestPHP is the oldest PHP FastCP installs, which WP Rocket's
// advanced-cache.php compares with the PHP version it needs.
const lowestPHP = "8.1"

// rocketConfigs caps how many settings files a site may have.
const rocketConfigs = 200

// rocket sends what WP Rocket's PHP would send from its cache, for one site.
type rocket struct {
	cacheDir  string // ends with "/"
	configDir string // ends with "/"
	// files lists the entries of the settings folder: true for regular
	// files, false for anything else.
	files   map[string]bool
	configs map[string]*rocketConfig
}

// rocketConfig is one of WP Rocket's per-domain settings files.
type rocketConfig struct {
	reasons []string // why pages under it go to PHP

	cacheSSL, cacheWebp, cacheMobile bool
	urlNoDots                        bool
	loggedInCookie                   string
	permalinkSlash                   bool
	rejectURI                        *regexp.Regexp
	rejectCookies                    *regexp.Regexp
	rejectUA                         *regexp.Regexp
}

var rocketBufferFiles = []string{
	"inc/classes/Buffer/class-abstract-buffer.php", "inc/classes/Buffer/class-cache.php",
	"inc/classes/Buffer/class-config.php", "inc/classes/Buffer/class-tests.php",
	"inc/classes/traits/trait-memoize.php",
}

func (l *loader) loadRocket(root, dropin string) (Status, finder) {
	st := Status{Plugin: "WP Rocket"}
	fail := func(reason string) (Status, finder) {
		st.Reasons = append(st.Reasons, reason)
		return st, nil
	}
	content := root + "/wp-content"
	vars, err := evalPHP(dropin, map[string]string{"ABSPATH": root + "/", "WP_CONTENT_DIR": content})
	paths := map[string]string{}
	for _, name := range []string{"rocket_path", "rocket_config_path", "rocket_cache_path"} {
		v := vars[name]
		if err != nil || v.kind != str || !within(v.s, root) || strings.ContainsAny(v.s, `'\`) {
			return fail("WP Rocket's wp-content/advanced-cache.php does not have the paths WP Rocket writes into it.")
		}
		paths[name] = v.s
	}
	plugin := paths["rocket_path"]
	header, err := l.head(plugin+"wp-rocket.php", 8<<10)
	if err == nil {
		st.Version = pluginVersion(header)
	}
	switch {
	case st.Version == "":
		return fail("The installed version of WP Rocket could not be read.")
	case !atLeast(st.Version, minRocket):
		return fail(fmt.Sprintf("WP Rocket %s is older than %s, the first version whose rules the web server follows. Update the plugin.", st.Version, minRocket))
	}
	template, err := l.read(plugin+"views/cache/advanced-cache.php", maxPHP)
	if err != nil {
		return fail("WP Rocket's plugin folder is missing or unreadable.")
	}
	phpVersion, ok := matchRocketTemplate(template, dropin, plugin, paths["rocket_config_path"], paths["rocket_cache_path"])
	if !ok {
		return fail("wp-content/advanced-cache.php is not the file WP Rocket " + st.Version + " writes. Save WP Rocket's settings once to rewrite it.")
	}
	if compareVersions(phpVersion, lowestPHP) > 0 {
		return fail("WP Rocket's advanced-cache.php needs PHP " + phpVersion + " or newer, which the web server cannot check.")
	}
	for _, p := range []string{plugin, paths["rocket_config_path"], paths["rocket_cache_path"]} {
		if _, err := l.stat(strings.TrimSuffix(p, "/")); err != nil {
			return fail("A folder WP Rocket's advanced-cache.php needs is missing, so WP Rocket does not serve its cache.")
		}
	}
	for _, f := range rocketBufferFiles {
		if !l.isFile(plugin + f) {
			return fail("WP Rocket is missing " + f + ".")
		}
	}
	if !l.isFile(plugin+"inc/Logger/Logger.php") && !l.isFile(plugin+"inc/classes/logger/class-logger.php") {
		return fail("WP Rocket is missing its Logger class.")
	}

	r := &rocket{
		cacheDir:  strings.TrimRight(paths["rocket_cache_path"], "/") + "/",
		configDir: strings.TrimRight(paths["rocket_config_path"], "/") + "/",
		files:     map[string]bool{},
		configs:   map[string]*rocketConfig{},
	}
	entries, err := l.readDir(strings.TrimSuffix(r.configDir, "/"))
	if err != nil {
		return fail("The web server cannot list WP Rocket's settings folder.")
	}
	if len(entries) > rocketConfigs {
		return fail("WP Rocket's settings folder has too many files.")
	}
	consts := map[string]string{"ABSPATH": root + "/", "WP_CONTENT_DIR": content}
	var names []string
	for _, e := range entries {
		name := e.Name()
		regular := e.Type().IsRegular()
		r.files[name] = regular
		if !regular || !strings.HasSuffix(name, ".php") {
			continue
		}
		src, err := l.read(r.configDir+name, maxPHP)
		if err != nil {
			r.files[name] = false
			continue
		}
		if !strings.Contains(src, "$rocket_") {
			continue
		}
		c := &rocketConfig{}
		vars, err := evalPHP(src, consts)
		if err != nil {
			c.reasons = []string{unreadable("WP Rocket", name, err)}
		} else {
			c.read(vars)
		}
		r.configs[name] = c
		names = append(names, name)
	}
	sort.Strings(names)
	usable := false
	for _, name := range names {
		c := r.configs[name]
		for _, reason := range c.reasons {
			st.Reasons = append(st.Reasons, strings.TrimSuffix(name, ".php")+": "+reason)
		}
		usable = usable || len(c.reasons) == 0
	}
	if !usable {
		if len(names) == 0 {
			return fail("WP Rocket has no settings file for this site yet. Save WP Rocket's settings once.")
		}
		return st, nil
	}
	return st, r
}

// matchRocketTemplate reports whether dropin is what AdvancedCache::
// get_advanced_cache_content makes of template with these paths, and the
// PHP version filled in.
func matchRocketTemplate(template, dropin, plugin, config, cache string) (string, bool) {
	block := regexp.MustCompile(`(?s)'\{\{MOBILE_CACHE\}\}';(.*)'\{\{/MOBILE_CACHE\}\}';`)
	for _, keep := range []string{"$1", ""} {
		content := block.ReplaceAllString(template, keep)
		content = strings.NewReplacer(
			"{{WP_ROCKET_PATH}}", plugin,
			"{{WP_ROCKET_CONFIG_PATH}}", config,
			"{{WP_ROCKET_CACHE_PATH}}", cache,
		).Replace(content)
		parts := strings.Split(content, "{{WP_ROCKET_PHP_VERSION}}")
		if len(parts) < 2 || !strings.HasPrefix(dropin, parts[0]) {
			continue
		}
		rest := dropin[len(parts[0]):]
		end := strings.Index(rest, parts[1])
		if end < 0 && parts[1] != "" {
			continue
		}
		version := rest[:max(end, 0)]
		if validVersion(version) && strings.Join(parts, version) == dropin {
			return version, true
		}
	}
	return "", false
}

// read takes the settings Config::get_configs reads, with its defaults.
func (c *rocketConfig) read(vars map[string]value) {
	get := func(name string) (value, bool) {
		v, ok := vars["rocket_"+name]
		if ok && v.kind == unknown {
			c.reasons = append(c.reasons, fmt.Sprintf("the setting $rocket_%s is set in a way the web server does not read.", name))
		}
		return v, ok && v.kind != null && v.kind != unknown
	}
	text := func(name string) string {
		v, set := get(name)
		if !set {
			return ""
		}
		s, ok := v.text()
		if !ok {
			c.reasons = append(c.reasons, fmt.Sprintf("the setting $rocket_%s has an unexpected value.", name))
		}
		return s
	}
	truthy := func(name string) bool {
		v, _ := get(name)
		return v.truthy()
	}
	pattern := func(name, body string, caseless bool) *regexp.Regexp {
		re, err := compilePCRE(body, '#', caseless)
		if err != nil {
			c.reasons = append(c.reasons, fmt.Sprintf("the pattern in $rocket_%s uses regular expression features the web server does not reproduce exactly (%v).", name, err))
		}
		return re
	}

	c.cacheSSL = truthy("cache_ssl")
	c.cacheWebp = truthy("cache_webp")
	c.cacheMobile = truthy("cache_mobile")
	c.urlNoDots = truthy("url_no_dots")
	c.loggedInCookie = text("logged_in_cookie")
	c.permalinkSlash = strings.HasSuffix(text("permalink_structure"), "/")
	if v, set := get("permalink_structure"); set && v.kind != str {
		c.reasons = append(c.reasons, "the permalink structure has an unexpected value.")
	}
	tablet, set := get("cache_mobile_files_tablet")
	if !set {
		tablet = strValue("desktop")
	}
	if c.cacheMobile && truthy("do_caching_mobile_files") && tablet.truthy() {
		c.reasons = append(c.reasons, "Separate cache files for mobile devices is on in WP Rocket, and which file a phone gets is decided in PHP.")
	}
	if v, set := get("cache_mandatory_cookies"); set && !(v.kind == str && v.s == "") {
		c.reasons = append(c.reasons, "WP Rocket caches pages only for visitors with certain cookies, which is decided in PHP.")
	}
	if truthy("cache_dynamic_cookies") {
		c.reasons = append(c.reasons, "WP Rocket keeps separate cache files per cookie value, which is decided in PHP.")
	}
	if truthy("cache_reject_uri") {
		c.rejectURI = pattern("cache_reject_uri", "^("+text("cache_reject_uri")+")$", true)
	}
	if v, set := get("cache_reject_cookies"); set && !(v.kind == str && v.s == "") {
		c.rejectCookies = pattern("cache_reject_cookies", text("cache_reject_cookies"), false)
	}
	if truthy("cache_reject_ua") {
		c.rejectUA = pattern("cache_reject_ua", text("cache_reject_ua"), false)
	}
}

var (
	rocketMobile = []*regexp.Regexp{
		mustPCRE(`^.*(2.0\ MMP|240x320|400X240|AvantGo|BlackBerry|Blazer|Cellphone|Danger|DoCoMo|Elaine/3.0|EudoraWeb|Googlebot-Mobile|hiptop|IEMobile|KYOCERA/WX310K|LG/U990|MIDP-2.|MMEF20|MOT-V|NetFront|Newt|Nintendo\ Wii|Nitro|Nokia|Opera\ Mini|Palm|PlayStation\ Portable|portalmmm|Proxinet|ProxiNet|SHARP-TQ-GX10|SHG-i900|Small|SonyEricsson|Symbian\ OS|SymbianOS|TS21i-10|UP.Browser|UP.Link|webOS|Windows\ CE|WinWAP|YahooSeeker/M1A1-R2D2|iPhone|iPod|Android|BlackBerry9530|LG-TU915\ Obigo|LGE\ VX|webOS|Nokia5800).*`, true),
		mustPCRE(`^(w3c\ |w3c-|acs-|alav|alca|amoi|audi|avan|benq|bird|blac|blaz|brew|cell|cldc|cmd-|dang|doco|eric|hipt|htc_|inno|ipaq|ipod|jigs|kddi|keji|leno|lg-c|lg-d|lg-g|lge-|lg/u|maui|maxo|midp|mits|mmef|mobi|mot-|moto|mwbp|nec-|newt|noki|palm|pana|pant|phil|play|port|prox|qwap|sage|sams|sany|sch-|sec-|send|seri|sgh-|shar|sie-|siem|smal|smar|sony|sph-|symb|t-mo|teli|tim-|tosh|tsm-|upg1|upsi|vk-v|voda|wap-|wapa|wapi|wapp|wapr|webc|winw|winw|xda\ |xda-).*`, true),
	}
	rocketRedirectExclusion = mustPCRE(`(.*)wp\-json(/.*|$)`, false)

	firefoxVersion = regexp.MustCompile(`(?i)Firefox/([0-9]{2,})`)
	iosVersion     = regexp.MustCompile(`(?i)(?:iPad|iPhone)(.*)Version/([0-9]{2,})`)
	safariVersion  = regexp.MustCompile(`(?i)Version/([0-9]{2,})(?:.*)Safari`)
)

func mustPCRE(body string, caseless bool) *regexp.Regexp {
	re, err := compilePCRE(body, '#', caseless)
	if err != nil {
		panic(err)
	}
	return re
}

// configFor is Config::get_config_file_path: the settings file for host
// and uri, the name it is under, and whether the answer is certain.
func (r *rocket) configFor(host, uri string) (string, bool) {
	certain := true
	exists := func(name string) bool {
		regular, ok := r.files[name]
		if ok && !regular {
			certain = false // realpath() would find it, but it is not a file
		}
		return ok && regular
	}
	p := strings.ReplaceAll(uri, `\`, "/")
	p = collapseSlashes(p)
	enc := rawurlencode(p)
	for strings.HasPrefix(enc, "%2F") {
		enc = enc[3:]
	}
	for strings.HasSuffix(enc, "%2F") {
		enc = enc[:len(enc)-3]
	}
	var found []string
	dir := ""
	for _, part := range strings.Split(enc, "%2F") {
		if part == "" || part == "0" {
			continue
		}
		if name := host + "." + part + ".php"; exists(name) {
			found = append(found, name)
		}
		if name := host + "." + dir + part + ".php"; exists(name) {
			found = append(found, name)
		}
		dir += part + "."
	}
	if !certain {
		return "", false
	}
	if len(found) > 0 {
		return found[len(found)-1], true
	}
	if name := host + ".php"; exists(name) {
		return name, certain
	}
	return "", false
}

// collapseSlashes is preg_replace('|(?<=.)/+|', '/', $path).
func collapseSlashes(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] == '/' && i > 0 && p[i-1] == '/' && b.Len() > 1 {
			continue
		}
		b.WriteByte(p[i])
	}
	return b.String()
}

func (r *rocket) find(fsys fs.FS, req *request, now time.Time) *hit {
	rawHost, set, certain := req.header("HTTP_HOST")
	if !certain || !set || !simpleHost(rawHost) || !simpleURI(req.uri) {
		return nil
	}
	// Config::get_host
	host := rawHost
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	host = rawurlencode(strings.Trim(asciiLower(host), "."))
	if host == "" {
		return nil
	}

	// Tests::can_process_any_buffer
	base := "/" + strings.TrimLeft(req.uri, "/")
	for _, seg := range strings.Split(base, "/") {
		if seg == "." || seg == ".." {
			return nil // get_clean_request_uri would remove it
		}
	}
	if strings.Contains(base, "/robots.txt") || strings.Contains(base, "/.htaccess") {
		return nil
	}
	if asciiLower(base) != "/index.php" {
		switch extension(base) {
		case "php", "xml", "xsl":
			return nil
		}
	}

	// Tests::can_init_process
	name, ok := r.configFor(host, req.uri)
	if !ok {
		return nil
	}
	c := r.configs[name]
	if c == nil || len(c.reasons) > 0 {
		return nil
	}
	if !canonicalPort(req.port) {
		return nil // WordPress's is_ssl() compares the port loosely
	}
	ssl := req.https || req.port == "443"
	if ssl && !c.cacheSSL {
		return nil
	}
	if c.rejectURI != nil && rejects(c.rejectURI, base) {
		return nil
	}
	if len(req.cookies) > 0 && c.rejectCookies != nil {
		for _, cookie := range req.cookies {
			if rejects(c.rejectCookies, cookie) {
				return nil
			}
		}
	}
	ua, uaSet, certain := req.header("HTTP_USER_AGENT")
	if !certain {
		return nil
	}
	uaTruthy := uaSet && ua != "" && ua != "0"
	if uaTruthy && c.rejectUA != nil && rejects(c.rejectUA, ua) {
		return nil
	}
	if uaTruthy && !c.cacheMobile {
		for _, re := range rocketMobile {
			if rejects(re, ua) {
				return nil
			}
		}
	}

	// Cache::maybe_allow_wp_redirect
	if !rocketRedirectExclusion.MatchString(base) {
		reqSlash := strings.HasSuffix(base, "/")
		if !c.permalinkSlash {
			home := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(name, host, ""), ".php", ""), ".", "/")
			if base == "/" || strings.TrimRight(base, "/") == home {
				reqSlash = false
			}
		}
		if c.permalinkSlash != reqSlash {
			return nil
		}
	}

	// Cache::get_cache_path
	if c.loggedInCookie != "" && c.loggedInCookie != "0" && req.hasCookie(c.loggedInCookie) {
		return nil // a per-user cache
	}
	dirHost := host
	if c.urlNoDots {
		dirHost = strings.ReplaceAll(dirHost, ".", "_")
	}
	filename := "index"
	if ssl && c.cacheSSL {
		filename += "-https"
	}
	if c.cacheWebp {
		accept, set, certain := req.header("HTTP_ACCEPT")
		if !certain {
			return nil
		}
		webp := false
		if set && accept != "" && accept != "0" && strings.Contains(accept, "webp") {
			webp = true
		} else if webp, ok = webpUserAgent(ua, uaSet); !ok {
			return nil
		}
		if webp {
			filename += "-webp"
		}
	}
	encoding, set, certain := req.header("HTTP_ACCEPT_ENCODING")
	if !certain {
		return nil
	}
	gzip := set && encoding != "" && encoding != "0" && strings.Contains(encoding, "gzip")

	// Cache::maybe_init_process tries each file's _gzip copy first, which
	// holds the same page compressed. Where only that copy exists, PHP
	// sends it, so nothing else may be sent instead.
	try := func(name string) (*hit, bool) {
		if h := open(fsys, name); h != nil {
			h.lastModified = true
			return h, true
		}
		if gzip {
			if _, err := fs.Stat(fsys, name+"_gzip"); err == nil {
				return nil, true
			}
		}
		return nil, false
	}
	dir := lowerEscapes(r.cacheDir + dirHost + strings.TrimRight(base, "/"))
	dir = strings.ReplaceAll(dir, "?", "#")
	if h, decided := try(dir + "/" + filename + ".html"); decided {
		return h
	}
	if strings.Contains(filename, "-webp") {
		if _, err := fs.Stat(fsys, dir+"/.no-webp"); err == nil {
			h, _ := try(dir + "/" + strings.ReplaceAll(filename, "-webp", "") + ".html")
			return h
		}
	}
	return nil
}

// extension is pathinfo($path, PATHINFO_EXTENSION).
func extension(p string) string {
	base := strings.TrimRight(p, "/")
	base = base[strings.LastIndexByte(base, '/')+1:]
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		return base[i+1:]
	}
	return ""
}

// canonicalPort reports whether port is written as PHP would print the
// number, so string and numeric comparisons agree.
func canonicalPort(port string) bool {
	return port != "" && port[0] != '0' && strings.Trim(port, "0123456789") == ""
}

// lowerEscapes lowercases percent escapes written in upper case, as
// get_cache_path does.
func lowerEscapes(s string) string {
	b := []byte(s)
	for i := 0; i+2 < len(b); i++ {
		if b[i] == '%' && isUpperHex(b[i+1]) && isUpperHex(b[i+2]) {
			b[i+1], b[i+2] = lowerHex(b[i+1]), lowerHex(b[i+2])
			i += 2
		}
	}
	return string(b)
}

func isUpperHex(c byte) bool { return isDigit(c) || c >= 'A' && c <= 'F' }

func lowerHex(c byte) byte {
	if c >= 'A' && c <= 'F' {
		return c + 'a' - 'A'
	}
	return c
}

// webpUserAgent is Cache::is_user_agent_compatible, and whether the
// answer is certain.
func webpUserAgent(ua string, set bool) (bool, bool) {
	if !set || ua == "" || ua == "0" {
		return false, true
	}
	for i := 0; i < len(ua); i++ {
		if ua[i] >= 0x80 {
			return false, false // PCRE and Go read such text differently
		}
	}
	if m := firefoxVersion.FindStringSubmatch(ua); m != nil && phpInt(m[1]) <= 66 {
		return false, true
	}
	if m := iosVersion.FindStringSubmatch(ua); m != nil {
		return phpInt(m[2]) >= 14, true
	}
	if m := safariVersion.FindStringSubmatch(ua); m != nil && phpInt(m[1]) < 16 {
		return false, true
	}
	return true, true
}

// phpInt is (int) of a string of digits, which saturates.
func phpInt(digits string) int64 {
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 1<<63 - 1
	}
	return n
}
