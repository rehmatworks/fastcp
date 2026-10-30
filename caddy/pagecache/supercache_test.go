package pagecache

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// superCacheDropin is WP Super Cache's advanced-cache.php.
const superCacheDropin = `<?php
// WP SUPER CACHE 1.2
function wpcache_broken_message() {
	global $wp_cache_config_file;
	if ( isset( $wp_cache_config_file ) == false ) {
		return '';
	}

	$doing_ajax     = defined( 'DOING_AJAX' ) && DOING_AJAX;
	$xmlrpc_request = defined( 'XMLRPC_REQUEST' ) && XMLRPC_REQUEST;
	$rest_request   = defined( 'REST_REQUEST' ) && REST_REQUEST;
	$robots_request = strpos( $_SERVER['REQUEST_URI'], 'robots.txt' ) != false;

	$skip_output = ( $doing_ajax || $xmlrpc_request || $rest_request || $robots_request );
	if ( false == strpos( $_SERVER['REQUEST_URI'], 'wp-admin' ) && ! $skip_output ) {
		echo '<!-- WP Super Cache is installed but broken. The constant WPCACHEHOME must be set in the file wp-config.php and point at the WP Super Cache plugin directory. -->';
	}
}

if ( false == defined( 'WPCACHEHOME' ) ) {
	define( 'ADVANCEDCACHEPROBLEM', 1 );
} elseif ( ! include_once WPCACHEHOME . 'wp-cache-phase1.php' ) {
	if ( ! @is_file( WPCACHEHOME . 'wp-cache-phase1.php' ) ) {
		define( 'ADVANCEDCACHEPROBLEM', 1 );
	}
}
if ( defined( 'ADVANCEDCACHEPROBLEM' ) ) {
	register_shutdown_function( 'wpcache_broken_message' );
}
`

const superCacheHeader = `<?php
/*
 * Plugin Name: WP Super Cache
 * Plugin URI: https://wordpress.org/plugins/wp-super-cache/
 * Description: Very fast caching plugin for WordPress.
 * Version: 3.1.4
 * Author: Automattic
 * Author URI: https://automattic.com/
 * License: GPL2+
 * License URI: https://www.gnu.org/licenses/gpl-2.0.txt
 * Text Domain: wp-super-cache
 */
`

// superCacheConfig is a wp-cache-config.php as WP Super Cache 3.1.4 leaves
// it once caching is on, with made-up secrets.
const superCacheConfig = `<?php
/*
WP-Cache Config Sample File

See wp-cache.php for author details.
*/

$wpsc_served_header = false;
$wpsc_save_headers = 0;
$wp_cache_mobile_groups = '';
$wpsc_tracking_parameters = array( 'fbclid', 'ref', 'gclid', 'fb_source', 'mc_cid', 'mc_eid', 'utm_source', 'utm_medium', 'utm_campaign', 'utm_term', 'utm_content', 'utm_expid', 'mtm_source', 'mtm_medium', 'mtm_campaign', 'mtm_keyword', 'mtm_content', 'mtm_cid', 'mtm_group', 'mtm_placement' );
$wpsc_ignore_tracking_parameters = 0;
$cache_page_secret = 'made-up-secret';
$wp_cache_home_path = '/';
$wp_cache_slash_check = 1;
$cache_time_interval = 600;
if ( ! defined('WPCACHEHOME') )
	define( 'WPCACHEHOME', WP_CONTENT_DIR . '/plugins/wp-super-cache/' );

$cache_compression = 0; // Super cache compression
$cache_enabled = true;
$super_cache_enabled = true;
$cache_max_time = 3600; //in seconds
//$use_flock = true; // Set it true or false if you know what to use
$cache_path = WP_CONTENT_DIR . '/cache/';
$file_prefix = 'wp-cache-';
$ossdlcdn = 0;

// Array of files that have 'wp-' but should still be cached
$cache_acceptable_files = array( 'wp-comments-popup.php', 'wp-links-opml.php', 'wp-locations.php' );

$cache_rejected_uri = array('wp-.*\\.php', 'index\\.php');
$cache_rejected_user_agent = array();

$cache_rebuild_files = 1;

// Disable the file locking system.
// If you are experiencing problems with clearing or creating cache files
// uncommenting this may help.
$wp_cache_mutex_disabled = 1;

// Just modify it if you have conflicts with semaphores
$sem_id = 1352341263;

if ( '/' != substr($cache_path, -1)) {
	$cache_path .= '/';
}

$wp_cache_mobile = 0;
$wp_cache_mobile_whitelist = 'Stand Alone/QNws';
$wp_cache_mobile_browsers = '2.0 MMP, 240x320, 400X240, AvantGo, BlackBerry, Blazer, Cellphone, Danger, DoCoMo, Elaine/3.0, EudoraWeb, Googlebot-Mobile, hiptop, IEMobile, KYOCERA/WX310K, LG/U990, MIDP-2., MMEF20, MOT-V, NetFront, Newt, Nintendo Wii, Nitro, Nokia, Opera Mini, Palm, PlayStation Portable, portalmmm, Proxinet, ProxiNet, SHARP-TQ-GX10, SHG-i900, Small, SonyEricsson, Symbian OS, SymbianOS, TS21i-10, UP.Browser, UP.Link, webOS, Windows CE, WinWAP, YahooSeeker/M1A1-R2D2, iPhone, iPod, iPad, Android, BlackBerry9530, LG-TU915 Obigo, LGE VX, webOS, Nokia5800';

// change to relocate the supercache plugins directory
$wp_cache_plugins_dir = WPCACHEHOME . 'plugins';
// set to 1 to do garbage collection during normal process shutdown instead of wp-cron
$wp_cache_shutdown_gc = 0;
$wp_super_cache_late_init = 0;

// uncomment the next line to enable advanced debugging features
$wp_super_cache_advanced_debug = 0;
$wp_super_cache_front_page_text = '';
$wp_super_cache_front_page_clear = 0;
$wp_super_cache_front_page_check = 0;
$wp_super_cache_front_page_notification = '0';

$wp_cache_anon_only = 0;
$wp_supercache_cache_list = 0;
$wp_cache_debug_to_file = 0;
$wp_super_cache_debug = 0;
$wp_cache_debug_level = 5;
$wp_cache_debug_ip = '';
$wp_cache_debug_log = '';
$wp_cache_debug_email = '';
$wp_cache_pages[ "search" ] = 0;
$wp_cache_pages[ "feed" ] = 0;
$wp_cache_pages[ "category" ] = 0;
$wp_cache_pages[ "home" ] = 0;
$wp_cache_pages[ "frontpage" ] = 0;
$wp_cache_pages[ "tag" ] = 0;
$wp_cache_pages[ "archives" ] = 0;
$wp_cache_pages[ "pages" ] = 0;
$wp_cache_pages[ "single" ] = 0;
$wp_cache_pages[ "author" ] = 0;
$wp_cache_hide_donation = 0;
$wp_cache_not_logged_in = 2;
$wp_cache_clear_on_post_edit = 0;
$wp_cache_hello_world = 0;
$wp_cache_mobile_enabled = 1;
$wp_cache_cron_check = 0;
$wp_cache_mfunc_enabled = 0;
$wp_cache_make_known_anon = 0;
$wp_cache_refresh_single_only = 0;
$wp_cache_mod_rewrite = 0;
$wp_supercache_304 = 0;
$wp_cache_front_page_checks = 1;
$wp_cache_disable_utf8 = 0;
$wp_cache_no_cache_for_get = 0;
$cache_scheduled_time = "00:00";
$wp_cache_preload_interval = 600;
$cache_schedule_type = 'interval';
$wp_cache_preload_posts = 0;
$wp_cache_preload_on = 0;
$wp_cache_preload_taxonomies = 0;
$wp_cache_preload_email_me = 0;
$wp_cache_preload_email_volume = 'none';
$wp_cache_mobile_prefixes = 'w3c , w3c-, acs-, alav, alca, amoi, audi, avan, benq, bird, blac, blaz, brew, cell, cldc, cmd-, dang, doco, eric, hipt, htc_, inno, ipaq, ipod, jigs, kddi, keji, leno, lg-c, lg-d, lg-g, lge-, lg/u, maui, maxo, midp, mits, mmef, mobi, mot-, moto, mwbp, nec-, newt, noki, palm, pana, pant, phil, play, port, prox, qwap, sage, sams, sany, sch-, sec-, send, seri, sgh-, shar, sie-, siem, smal, smar, sony, sph-, symb, t-mo, teli, tim-, tosh, tsm-, upg1, upsi, vk-v, voda, wap-, wapa, wapi, wapp, wapr, webc, winw, winw, xda , xda-';
$cached_direct_pages = array();
$cache_gc_email_me = 0;
$cache_schedule_interval = 'daily';
$wp_super_cache_comments = 1;
$wpsc_version = 169;
?>
`

// superCacheSite is a site with WP Super Cache caching on, and the home
// page and a post saved.
func superCacheSite(t *testing.T) *wp {
	t.Helper()
	w := newWP(t)
	home := "wp-content/plugins/wp-super-cache/"
	w.write("wp-content/advanced-cache.php", superCacheDropin)
	w.write(home+"advanced-cache.php", superCacheDropin)
	w.write(home+"wp-cache.php", superCacheHeader+"\n// The rest of the plugin.\n")
	w.write(home+"wp-cache-phase1.php", "<?php\n")
	for _, addon := range []string{"awaitingmoderation.php", "domain-mapping.php", "dynamic-cache-test.php", "jetpack.php", "multisite.php", "wptouch.php"} {
		w.write(home+"plugins/"+addon, "<?php\n")
	}
	w.write("wp-content/wp-cache-config.php", superCacheConfig)
	w.write("wp-content/cache/supercache/example.com/index-https.html", "home")
	w.write("wp-content/cache/supercache/example.com/hello-world/index-https.html", "post")
	return w
}

// setting changes wp-cache-config.php.
func (w *wp) setting(old, new string) {
	w.t.Helper()
	w.edit("wp-content/wp-cache-config.php", old, new)
}

func TestSuperCacheServes(t *testing.T) {
	w := superCacheSite(t)
	w.wantServing()
	if st := w.status(); st.Plugin != "WP Super Cache" || st.Version != "3.1.4" || len(st.Reasons) != 0 {
		t.Fatalf("status = %+v", st)
	}
	w.expect("/", "home")
	w.expect("/hello-world/", "post")
	w.expect("/missing/", "")

	rec := w.do(http.MethodGet, "/hello-world/")
	for name, want := range map[string]string{
		"Content-Type":   "text/html; charset=UTF-8",
		"Vary":           "Accept-Encoding, Cookie",
		"Cache-Control":  "max-age=3, must-revalidate",
		"Content-Length": "4",
		"Last-Modified":  "",
	} {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	rec = w.do(http.MethodHead, "/hello-world/")
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != "4" {
		t.Errorf("HEAD: %d %q %v", rec.Code, rec.Body, rec.Header())
	}
	if rec := w.do(http.MethodPost, "/hello-world/"); rec.Code != http.StatusTeapot {
		t.Errorf("POST was answered from the cache")
	}
}

func TestSuperCacheRequests(t *testing.T) {
	w := superCacheSite(t)
	w.write("wp-content/cache/supercache/example.com/caf%C3%A9/index-https.html", "cafe")
	w.write("wp-content/cache/supercache/example.com/index-https-mobile.html", "home for phones")
	w.write("wp-content/cache/supercache/example.com/index.html", "home over http")

	w.expect("/hello-world", "")   // WordPress redirects to the slash
	w.expect("/?p=1", "")          // query strings go to PHP
	w.expect("/hello-world/?", "") // even an empty one
	w.expect("/caf%C3%A9/", "cafe")
	w.expect("/caf%c3%a9/", "cafe") // the plugin upper-cases escapes
	w.expect("http://example.com/", "home over http")
	w.expect("https://EXAMPLE.com:8443/", "home")
	w.expect("/", "home for phones", header("User-Agent", iPhoneUA))
	w.expect("/hello-world/", "", header("User-Agent", iPhoneUA)) // no phone copy yet
	w.expect("/", "", header("X-Wap-Profile", "http://example.org/uaprof.xml"))

	for cookie, want := range map[string]string{
		"_ga=GA1.1.1; theme=dark":                    "home",
		"wordpress_logged_in_0123abcd=admin%7C1%7Cx": "",
		"comment_author_0123abcd=Ann":                "",
		"wp-postpass_0123abcd=x":                     "",
		"wordpress_0123abcd=x":                       "", // $wp_cache_not_logged_in = 2
		"wordpress_test_cookie=WP+Cookie+check":      "home",
	} {
		w.expect("/", want, header("Cookie", cookie))
	}
	for accept, want := range map[string]string{
		"":                                 "home",
		"*/*":                              "home",
		"application/json":                 "",
		"application/activity+json":        "",
		"text/html, application/json":      "home",
		"application/json, text/html;q=.5": "",
	} {
		w.expect("/", want, header("Accept", accept))
	}

	w.age("wp-content/cache/supercache/example.com/hello-world/index-https.html", 2*time.Hour)
	w.expect("/hello-world/", "") // older than $cache_max_time
	w.setting("$cache_max_time = 3600;", "$cache_max_time = 0;")
	w.expect("/hello-world/", "post")

}

// legacyMeta is the meta file of a page WP Super Cache saved in its older
// format for a visitor whose get_wp_cache_key is key.
func legacyMeta(dir, key string) string {
	sum := md5.Sum([]byte(key))
	return "wp-content/cache/supercache/example.com/" + dir + "meta-wp-cache-" + hex.EncodeToString(sum[:]) + ".php"
}

func TestSuperCacheOlderFormat(t *testing.T) {
	w := superCacheSite(t)
	// Saved for a logged-in visitor and for a query string: PHP looks for
	// neither for a visitor without cookies.
	w.write(legacyMeta("hello-world/", "example.com443/hello-world/0123456789abcdef0123456789abcdef-text/html"), "{}")
	w.write(legacyMeta("hello-world/", "example.com443/hello-world/?utm_source=x-text/html"), "{}")
	w.expect("/hello-world/", "post")

	mine := legacyMeta("hello-world/", "example.com443/hello-world/-text/html")
	w.write(mine, "{}")
	w.expect("/hello-world/", "") // PHP sends this visitor's older page first
	w.remove(mine)

	w.setting("$cache_compression = 0;", "$cache_compression = 1;")
	w.write(mine, "{}")
	w.expect("/hello-world/", "post") // the key now ends in gzip
	w.expect("/hello-world/", "", header("Accept-Encoding", "identity"))
	w.write(legacyMeta("hello-world/", "example.com443/hello-world/gzip-text/html"), "{}")
	w.expect("/hello-world/", "")

	w = superCacheSite(t)
	w.write("wp-content/cache/supercache/example.com/index-https-mobile.html", "home for phones")
	w.write(legacyMeta("", "example.com443/-text/html-mobile"), "{}")
	w.expect("/", "home")
	w.expect("/", "", header("User-Agent", iPhoneUA))
	w.write("wp-content/cache/supercache/example.com/index.html", "home over http")
	w.write(legacyMeta("", "example.com80/-text/html"), "{}")
	w.expect("/", "home")
	w.expect("http://example.com/", "")

	w = superCacheSite(t)
	w.write("wp-content/cache/supercache/example.com/hello-world/meta-wp-cache-blog0123456789abcdef0123456789abcdef.php", "{}")
	w.expect("/hello-world/", "") // a multisite blog's key, which may be this visitor's
	w.expect("/", "home")
}

func TestSuperCacheEmptyFolders(t *testing.T) {
	w := superCacheSite(t)
	for _, f := range []string{".htaccess", "index.html", "meta/index.html", "blogs/index.html", "supercache/index.html", "blogs/blog/meta/index.html", "blogs/blog/wp_cache_mutex.lock"} {
		w.write("wp-content/cache/"+f, "")
	}
	w.wantServing()
	w.expect("/", "home")
}

func TestSuperCacheCookieSettings(t *testing.T) {
	w := superCacheSite(t)
	w.setting("$wp_cache_not_logged_in = 2;", "$wp_cache_not_logged_in = 1;")
	w.expect("/", "home")
	w.expect("/", "", header("Cookie", "theme=dark"))

	w.setting("$wp_cache_not_logged_in = 1;", "$wp_cache_not_logged_in = 0;\n$wpsc_rejected_cookies = array( 'woocommerce_items_in_cart' );")
	w.expect("/", "home", header("Cookie", "theme=dark"))
	w.expect("/", "", header("Cookie", "woocommerce_items_in_cart=1"))

	w.setting("$wpsc_rejected_cookies = array( 'woocommerce_items_in_cart' );", "$wpsc_rejected_cookies = array( 'cart(?=x)' );")
	w.wantReason("rejected-cookie pattern")
}

func TestSuperCacheSlashSetting(t *testing.T) {
	w := superCacheSite(t)
	w.write("wp-content/cache/supercache/example.com/hello-world.html/index-https.html", "html page")
	w.expect("/hello-world.html", "")
	w.setting("$wp_cache_slash_check = 1;", "$wp_cache_slash_check = 0;")
	w.expect("/hello-world.html", "html page")
	w.expect("/hello-world/", "")
	w.expect("/", "home") // the home path always matches
}

func TestSuperCacheReasons(t *testing.T) {
	cases := map[string]struct {
		edit func(w *wp)
		want string
	}{
		"off":         {func(w *wp) { w.setting("$cache_enabled = true;", "$cache_enabled = false;") }, "Caching is off"},
		"late init":   {func(w *wp) { w.setting("$wp_super_cache_late_init = 0;", "$wp_super_cache_late_init = 1;") }, "Late init"},
		"dynamic":     {func(w *wp) { w.setting("$wp_cache_mfunc_enabled = 0;", "$wp_cache_mfunc_enabled = 1;") }, "Dynamic caching"},
		"anonymous":   {func(w *wp) { w.setting("$wp_cache_make_known_anon = 0;", "$wp_cache_make_known_anon = 1;") }, "anonymous"},
		"headers":     {func(w *wp) { w.setting("$wpsc_save_headers = 0;", "$wpsc_save_headers = 1;") }, "HTTP headers"},
		"computed":    {func(w *wp) { w.setting("$cache_enabled = true;", "$cache_enabled = get_option( 'x' );") }, "$cache_enabled"},
		"conditional": {func(w *wp) { w.setting("$cache_enabled = true;", "if ( time() ) { $cache_enabled = true; }") }, "$cache_enabled"},
		"old version": {func(w *wp) {
			w.write("wp-content/plugins/wp-super-cache/wp-cache.php", strings.Replace(superCacheHeader, "3.1.4", "3.1.2", 1))
		}, "older than 3.1.3"},
		"no version": {func(w *wp) { w.write("wp-content/plugins/wp-super-cache/wp-cache.php", "<?php\n") }, "version"},
		"edited drop-in": {func(w *wp) {
			w.write("wp-content/advanced-cache.php", superCacheDropin+"\n$cache_enabled = true;\n")
		}, "not the copy"},
		"no plugin":   {func(w *wp) { w.remove("wp-content/plugins/wp-super-cache") }, "missing"},
		"extra addon": {func(w *wp) { w.write("wp-content/plugins/wp-super-cache/plugins/mine.php", "<?php\n") }, "mine.php"},
		"addon folder": {func(w *wp) {
			w.write("wp-content/plugins/wp-super-cache-plugins/mine.php", "<?php\n")
		}, "wp-super-cache-plugins"},
		"legacy pages": {func(w *wp) { w.write("wp-content/cache/wp-cache-0123abcd.php", "x") }, "older format"},
		"blog pages":   {func(w *wp) { w.write("wp-content/cache/blogs/2/wp-cache-0123abcd.php", "x") }, "older format"},
		"blogs link": {func(w *wp) {
			w.write("wp-content/cache/blogs/index.html", "")
			if err := os.Symlink(w.path("wp-content/cache/supercache"), w.path("wp-content/cache/blogs/2")); err != nil {
				w.t.Fatal(err)
			}
		}, "other than files and folders"},
		"cache folder": {func(w *wp) {
			w.setting("$cache_path = WP_CONTENT_DIR . '/cache/';", "$cache_path = '/var/cache/';")
		}, "outside"},
		"index.php":  {func(w *wp) { w.write("index.php", "<?php\nrequire __DIR__ . '/app.php';\n") }, "index.php"},
		"no drop-in": {func(w *wp) { w.remove("wp-content/advanced-cache.php") }, "No WP Super Cache or WP Rocket"},
		"other plugin": {func(w *wp) {
			w.write("wp-content/advanced-cache.php", "<?php\n// W3 Total Cache\n")
		}, "other than"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := superCacheSite(t)
			c.edit(w)
			w.wantReason(c.want)
			w.expect("/", "")
		})
	}
}

// Reasons are shown in the panel, so they must not repeat settings.
func TestSuperCacheReasonsKeepSecrets(t *testing.T) {
	w := superCacheSite(t)
	w.setting("$wpsc_save_headers = 0;", "$wpsc_save_headers = 'made-up-secret';")
	for _, r := range w.status().Reasons {
		if strings.Contains(r, "made-up-secret") {
			t.Fatalf("reason %q repeats a setting", r)
		}
	}
}
