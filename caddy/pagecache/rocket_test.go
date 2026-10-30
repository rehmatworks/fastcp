package pagecache

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

const rocketHeader = `<?php
/**
 * Plugin Name: WP Rocket
 * Plugin URI: https://wp-rocket.me
 * Description: The best WordPress performance plugin.
 * Version: 3.24
 * Requires at least: 5.8
 * Requires PHP: 7.4
 * Code Name: Iego
 * Author: WP Media
 * Author URI: https://wp-media.me
 * Licence: GPLv2 or later
 *
 * Text Domain: rocket
 * Domain Path: /languages
 *
 * Copyright 2013-2026 WP Rocket
 */

defined( 'ABSPATH' ) || exit;
`

// rocketTemplate is WP Rocket's views/cache/advanced-cache.php.
const rocketTemplate = `<?php

use WP_Rocket\Buffer\Cache;
use WP_Rocket\Buffer\Config;
use WP_Rocket\Buffer\Tests;

defined( 'ABSPATH' ) || exit;

define( 'WP_ROCKET_ADVANCED_CACHE', true );

$rocket_path        = '{{WP_ROCKET_PATH}}';
$rocket_config_path = '{{WP_ROCKET_CONFIG_PATH}}';
$rocket_cache_path  = '{{WP_ROCKET_CACHE_PATH}}';

if (
	version_compare( phpversion(), '{{WP_ROCKET_PHP_VERSION}}', '<' )
	|| ! file_exists( $rocket_path )
	|| ! file_exists( $rocket_config_path )
	|| ! file_exists( $rocket_cache_path )
) {
	define( 'WP_ROCKET_ADVANCED_CACHE_PROBLEM', true );
	return;
}

'{{MOBILE_CACHE}}';
if ( file_exists( '{{WP_ROCKET_PATH}}inc/Dependencies/Detection/MobileDetect.php' ) && ! class_exists( 'WP_Rocket\Dependencies\Detection\MobileDetect' ) ) {
	include_once '{{WP_ROCKET_PATH}}inc/Dependencies/Detection/MobileDetect.php';
}
'{{/MOBILE_CACHE}}';

spl_autoload_register(
	function ( $class ) use ( $rocket_path ) { // phpcs:ignore Universal.NamingConventions.NoReservedKeywordParameterNames.classFound
		$rocket_classes = [
			'WP_Rocket\\Buffer\\Abstract_Buffer' => [ $rocket_path . 'inc/classes/Buffer/class-abstract-buffer.php' ],
			'WP_Rocket\\Buffer\\Cache'           => [ $rocket_path . 'inc/classes/Buffer/class-cache.php' ],
			'WP_Rocket\\Buffer\\Tests'           => [ $rocket_path . 'inc/classes/Buffer/class-tests.php' ],
			'WP_Rocket\\Buffer\\Config'          => [ $rocket_path . 'inc/classes/Buffer/class-config.php' ],
			'WP_Rocket\\Logger\\HTMLFormatter'   => [
				$rocket_path . 'inc/Logger/HTMLFormatter.php',
				$rocket_path . 'inc/classes/logger/class-html-formatter.php',
			],
			'WP_Rocket\\Logger\\Logger'          => [
				$rocket_path . 'inc/Logger/Logger.php',
				$rocket_path . 'inc/classes/logger/class-logger.php',
			],
			'WP_Rocket\\Logger\\StreamHandler'   => [
				$rocket_path . 'inc/Logger/StreamHandler.php',
				$rocket_path . 'inc/classes/logger/class-stream-handler.php',
			],
			'WP_Rocket\\Traits\\Memoize'         => [ $rocket_path . 'inc/classes/traits/trait-memoize.php' ],
		];

		if ( isset( $rocket_classes[ $class ] ) ) {
			$file_options = $rocket_classes[ $class ];
			$file         = '';

			foreach ( $file_options as $file_option ) {
				if ( file_exists( $file_option ) ) {
					$file = $file_option;
					break;
				}
			}
		} elseif ( strpos( $class, 'WP_Rocket\\Dependencies\\Monolog\\' ) === 0 ) {
			$class = str_replace( 'WP_Rocket\\Dependencies\\Monolog\\', '', $class );

			$file = $rocket_path . 'inc/Dependencies/Monolog/' . str_replace( '\\', '/', $class ) . '.php';
			if ( ! file_exists( $file ) ) {
				$file = $rocket_path . 'vendor/monolog/monolog/src/' . str_replace( '\\', '/', $class ) . '.php';
			}
		} elseif ( strpos( $class, 'WP_Rocket\\Dependencies\\Psr\\Log\\' ) === 0 ) {
			$class = str_replace( 'WP_Rocket\\Dependencies\\Psr\\Log\\', '', $class );

			$file = $rocket_path . 'inc/Dependencies/Psr/Log/' . str_replace( '\\', '/', $class ) . '.php';
			if ( ! file_exists( $file ) ) {
				$file = $rocket_path . 'vendor/psr/log/' . str_replace( '\\', '/', $class ) . '.php';
			}
		} else {
			return;
		}

		if ( file_exists( $file ) ) {
			require $file;
		}
	}
);

if ( ! class_exists( '\WP_Rocket\Buffer\Cache' ) ) {
	if ( ! defined( 'DONOTROCKETOPTIMIZE' ) ) {
		define( 'DONOTROCKETOPTIMIZE', true ); // phpcs:ignore WordPress.NamingConventions.PrefixAllGlobals.NonPrefixedConstantFound
	}
	return;
}

$rocket_config_class = new Config(
	[
		'config_dir_path' => $rocket_config_path,
	]
);

( new Cache(
	new Tests(
		$rocket_config_class
	),
	$rocket_config_class,
	[
		'cache_dir_path' => $rocket_cache_path,
	]
) )->maybe_init_process();
`

// rocketSettings is a settings file as get_rocket_config_file writes it,
// with made-up hashes.
const rocketSettings = `<?php
defined( 'ABSPATH' ) || exit;

$rocket_cookie_hash = '0123456789abcdef0123456789abcdef';
$rocket_logged_in_cookie = 'wordpress_logged_in_0123456789abcdef0123456789abcdef';
$rocket_cache_mobile_files_tablet = 'desktop';
$rocket_cache_mobile = 1;
$rocket_do_caching_mobile_files = 0;
$rocket_cache_webp = 0;
$rocket_cache_ssl = 1;
$rocket_secret_cache_key = '0a1b2c3d4e5f60718293a4';
$rocket_cache_reject_uri = '/(?:.+/)?feed(?:/(?:.+/?)?)?$|/(?:.+/)?embed/|/(index.php/)?(.*)wp-json(/.*|$)|/cart/';
$rocket_cache_query_strings = array();
$rocket_cache_reject_cookies = 'wordpress_logged_in_.+|wp-postpass_|wptouch_switch_toggle|comment_author_|comment_author_email_';
$rocket_cache_reject_ua = 'facebookexternalhit|WhatsApp';
$rocket_cache_ignored_parameters = array(
  'utm_source' => 1,
  'utm_medium' => 1,
  'utm_campaign' => 1,
  'fbclid' => 1,
  'gclid' => 1,
);
$rocket_cache_mandatory_cookies = '';
$rocket_cache_dynamic_cookies = array();
$rocket_permalink_structure = '/%postname%/';
`

const rocketConfigFile = "wp-content/wp-rocket-config/example.com.php"

// rocketDropin is what AdvancedCache::get_advanced_cache_content writes.
func (w *wp) rocketDropin(phpVersion string, mobile bool) string {
	keep := ""
	if mobile {
		keep = "$1"
	}
	content := regexp.MustCompile(`(?s)'\{\{MOBILE_CACHE\}\}';(.*)'\{\{/MOBILE_CACHE\}\}';`).ReplaceAllString(rocketTemplate, keep)
	return strings.NewReplacer(
		"{{WP_ROCKET_PHP_VERSION}}", phpVersion,
		"{{WP_ROCKET_PATH}}", w.root+"/wp-content/plugins/wp-rocket/",
		"{{WP_ROCKET_CONFIG_PATH}}", w.root+"/wp-content/wp-rocket-config/",
		"{{WP_ROCKET_CACHE_PATH}}", w.root+"/wp-content/cache/wp-rocket/",
	).Replace(content)
}

// rocketSite is a site with WP Rocket's page cache on, and the home page
// and a post saved.
func rocketSite(t *testing.T) *wp {
	t.Helper()
	w := newWP(t)
	plugin := "wp-content/plugins/wp-rocket/"
	w.write(plugin+"wp-rocket.php", rocketHeader+"\n// The rest of the plugin.\n")
	w.write(plugin+"views/cache/advanced-cache.php", rocketTemplate)
	for _, f := range rocketBufferFiles {
		w.write(plugin+f, "<?php\n")
	}
	w.write(plugin+"inc/Logger/Logger.php", "<?php\n")
	w.write("wp-content/advanced-cache.php", w.rocketDropin("7.4", false))
	w.write(rocketConfigFile, rocketSettings)
	w.write("wp-content/cache/wp-rocket/example.com/index-https.html", "home")
	w.write("wp-content/cache/wp-rocket/example.com/hello-world/index-https.html", "post")
	return w
}

func (w *wp) rocketSetting(old, new string) {
	w.t.Helper()
	w.edit(rocketConfigFile, old, new)
}

func TestRocketServes(t *testing.T) {
	w := rocketSite(t)
	w.wantServing()
	if st := w.status(); st.Plugin != "WP Rocket" || st.Version != "3.24" || len(st.Reasons) != 0 {
		t.Fatalf("status = %+v", st)
	}
	w.expect("/", "home")
	w.expect("/hello-world/", "post")
	w.expect("/missing/", "")

	mod := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	w.touch("wp-content/cache/wp-rocket/example.com/hello-world/index-https.html", mod)
	rec := w.do(http.MethodGet, "/hello-world/")
	lastModified := mod.Format(http.TimeFormat)
	for name, want := range map[string]string{
		"Content-Type":   "text/html; charset=UTF-8",
		"Content-Length": "4",
		"Last-Modified":  lastModified,
		"Cache-Control":  "",
		"Vary":           "",
	} {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	rec = w.do(http.MethodGet, "/hello-world/", header("If-Modified-Since", lastModified))
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("Cache-Control") != "no-cache, must-revalidate" {
		t.Errorf("If-Modified-Since: %d %q %v", rec.Code, rec.Body, rec.Header())
	}
	earlier := mod.Add(-time.Hour).Format(http.TimeFormat)
	if rec := w.do(http.MethodGet, "/hello-world/", header("If-Modified-Since", earlier)); rec.Code != http.StatusOK {
		t.Errorf("If-Modified-Since an hour before: %d", rec.Code)
	}
	rec = w.do(http.MethodHead, "/hello-world/")
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("HEAD: %d %q", rec.Code, rec.Body)
	}
}

func TestRocketRequests(t *testing.T) {
	w := rocketSite(t)
	cache := "wp-content/cache/wp-rocket/example.com/"
	w.write(cache+"index.html", "home over http")
	w.write(cache+"caf%c3%a9/index-https.html", "cafe")
	for _, page := range []string{"cart", "CART", "feed", "wp-json", "sitemap.xml"} {
		w.write(cache+page+"/index-https.html", page)
	}

	w.expect("/hello-world", "")   // WordPress redirects to the slash
	w.expect("/?utm_source=x", "") // query strings go to PHP
	w.expect("/./", "")            // PHP would clean the path first
	w.expect("http://example.com/", "home over http")
	w.expect("http://example.com:443/", "home") // WordPress's is_ssl() goes by the port
	w.expect("https://EXAMPLE.com:8443/", "home")
	w.expect("https://example.com:0443/", "")
	w.expect("/caf%C3%A9/", "cafe") // WP Rocket lower-cases escapes
	w.expect("/caf%c3%a9/", "cafe")
	w.expect("/cart/", "")
	w.expect("/CART/", "") // $rocket_cache_reject_uri ignores case
	w.expect("/feed/", "")
	w.expect("/wp-json/", "")
	w.expect("/sitemap.xml/", "")

	for cookie, want := range map[string]string{
		"_ga=GA1.1.1; theme=dark":                                    "home",
		"wordpress_test_cookie=WP+Cookie+check":                      "home",
		"wordpress_logged_in_0123456789abcdef0123456789abcdef=admin": "",
		"comment_author_0123abcd=Ann":                                "",
		"wp-postpass_0123abcd=x":                                     "",
	} {
		w.expect("/", want, header("Cookie", cookie))
	}
	w.expect("/", "", header("User-Agent", "facebookexternalhit/1.1"))
	w.expect("/", "home", header("User-Agent", iPhoneUA))
	w.expect("/", "home", func(r *http.Request) { r.Header.Del("User-Agent") })

	w.rocketSetting("$rocket_cache_mobile = 1;", "$rocket_cache_mobile = 0;")
	w.expect("/", "", header("User-Agent", iPhoneUA))
	w.expect("/", "home")
}

func TestRocketSettings(t *testing.T) {
	t.Run("logged-in users cached", func(t *testing.T) {
		w := rocketSite(t)
		w.rocketSetting("'wordpress_logged_in_.+|wp-postpass_", "'wp-postpass_")
		w.expect("/", "home", header("Cookie", "wordpress_logged_in_ffff=x"))
		// Their pages are in a folder per user, which only PHP knows.
		w.expect("/", "", header("Cookie", "wordpress_logged_in_0123456789abcdef0123456789abcdef=admin"))
	})
	t.Run("no trailing slash", func(t *testing.T) {
		w := rocketSite(t)
		w.rocketSetting("'/%postname%/'", "'/%postname%'")
		w.expect("/hello-world", "post")
		w.expect("/hello-world/", "")
		w.expect("/", "home")
	})
	t.Run("http only", func(t *testing.T) {
		w := rocketSite(t)
		w.rocketSetting("$rocket_cache_ssl = 1;", "$rocket_cache_ssl = 0;")
		w.expect("/", "")
		w.expect("http://example.com/", "")
		w.write("wp-content/cache/wp-rocket/example.com/index.html", "home over http")
		w.expect("http://example.com/", "home over http")
	})
	t.Run("dots in folder names", func(t *testing.T) {
		w := rocketSite(t)
		w.rocketSetting("$rocket_permalink_structure", "$rocket_url_no_dots = 1;\n$rocket_permalink_structure")
		w.expect("/", "")
		w.write("wp-content/cache/wp-rocket/example_com/index-https.html", "home")
		w.expect("/", "home")
	})
}

func TestRocketWebp(t *testing.T) {
	w := rocketSite(t)
	cache := "wp-content/cache/wp-rocket/example.com/"
	w.rocketSetting("$rocket_cache_webp = 0;", "$rocket_cache_webp = 1;")
	firefox := header("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:60.0) Gecko/20100101 Firefox/60.0")

	w.expect("/", "") // a browser that takes webp, and no webp copy yet
	w.expect("/", "home", firefox)
	w.write(cache+".no-webp", "")
	w.expect("/", "home")
	w.write(cache+"index-https-webp.html", "home with webp")
	w.expect("/", "home with webp")
	w.expect("/", "home", firefox)
	w.expect("/", "home with webp", firefox, header("Accept", "image/webp,*/*"))

	// PHP sends the compressed copy first, so a page saved only that way
	// goes to PHP for browsers that take gzip.
	w.remove(cache + "index-https-webp.html")
	w.write(cache+"index-https-webp.html_gzip", "compressed")
	w.expect("/", "")
	w.expect("/", "home", header("Accept-Encoding", "identity"))
}

func TestRocketConfigFiles(t *testing.T) {
	w := rocketSite(t)
	w.write("wp-content/cache/wp-rocket/example.com/shop/product/index-https.html", "product")
	w.write("wp-content/wp-rocket-config/example.com.shop.php",
		strings.Replace(rocketSettings, "$rocket_cache_mandatory_cookies = '';", "$rocket_cache_mandatory_cookies = 'wp_lang';", 1))
	if st := w.status(); !st.Serving || len(st.Reasons) != 1 || !strings.HasPrefix(st.Reasons[0], "example.com.shop: ") {
		t.Fatalf("status = %+v", st)
	}
	w.expect("/", "home")
	w.expect("/shop/product/", "")

	r := &rocket{files: map[string]bool{"example.com.php": true, "example.com.blog.php": true, "example.com.blog.sub.php": true, "example.com.odd.php": false}}
	for uri, want := range map[string]string{
		"/":                "example.com.php",
		"/about/":          "example.com.php",
		"/blog/":           "example.com.blog.php",
		"/blog/post/":      "example.com.blog.php",
		"/blog/sub/page/":  "example.com.blog.sub.php",
		"//blog//sub/":     "example.com.blog.sub.php",
		"/0/blog/":         "example.com.blog.php",
		"/odd/":            "",
		"/blog/odd/":       "",
		"/blogging/":       "example.com.php",
		"/news/blog/":      "example.com.blog.php",
		"/news/blog/sub/x": "example.com.blog.php",
	} {
		got, ok := r.configFor("example.com", uri)
		if want == "" && ok || want != "" && got != want {
			t.Errorf("configFor(%q) = %q, %v; want %q", uri, got, ok, want)
		}
	}
}

func TestRocketReasons(t *testing.T) {
	cases := map[string]struct {
		edit func(w *wp)
		want string
	}{
		"old version": {func(w *wp) {
			w.edit("wp-content/plugins/wp-rocket/wp-rocket.php", "Version: 3.24", "Version: 3.13.2")
		}, "older than 3.13.3"},
		"beta of the minimum": {func(w *wp) {
			w.edit("wp-content/plugins/wp-rocket/wp-rocket.php", "Version: 3.24", "Version: 3.13.3-beta1")
		}, "older than 3.13.3"},
		"edited drop-in": {func(w *wp) {
			w.write("wp-content/advanced-cache.php", w.rocketDropin("7.4", false)+"\n// tweaked\n")
		}, "is not the file WP Rocket 3.24 writes"},
		"newer PHP": {func(w *wp) {
			w.write("wp-content/advanced-cache.php", w.rocketDropin("8.4", false))
		}, "needs PHP 8.4"},
		"mobile files": {func(w *wp) {
			w.write("wp-content/advanced-cache.php", w.rocketDropin("7.4", true))
			w.rocketSetting("$rocket_do_caching_mobile_files = 0;", "$rocket_do_caching_mobile_files = 1;")
		}, "Separate cache files for mobile"},
		"mandatory cookies": {func(w *wp) {
			w.rocketSetting("$rocket_cache_mandatory_cookies = '';", "$rocket_cache_mandatory_cookies = 'wp_lang';")
		}, "certain cookies"},
		"dynamic cookies": {func(w *wp) {
			w.rocketSetting("$rocket_cache_dynamic_cookies = array();", "$rocket_cache_dynamic_cookies = array( 'currency' );")
		}, "per cookie value"},
		"computed setting": {func(w *wp) {
			w.rocketSetting("$rocket_cache_ssl = 1;", "$rocket_cache_ssl = get_option( 'x' );")
		}, "$rocket_cache_ssl"},
		"unread pattern": {func(w *wp) {
			w.rocketSetting("'facebookexternalhit|WhatsApp'", "'(?<!Face)bot'")
		}, "$rocket_cache_reject_ua"},
		"no settings":     {func(w *wp) { w.remove(rocketConfigFile) }, "no settings file"},
		"no cache folder": {func(w *wp) { w.remove("wp-content/cache/wp-rocket") }, "folder"},
		"no logger":       {func(w *wp) { w.remove("wp-content/plugins/wp-rocket/inc/Logger") }, "Logger"},
		"no buffer class": {func(w *wp) {
			w.remove("wp-content/plugins/wp-rocket/inc/classes/Buffer/class-cache.php")
		}, "class-cache.php"},
		"paths elsewhere": {func(w *wp) {
			w.write("wp-content/advanced-cache.php", strings.ReplaceAll(w.rocketDropin("7.4", false), w.root+"/wp-content/cache/wp-rocket/", "/tmp/wp-rocket/"))
		}, "paths"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := rocketSite(t)
			c.edit(w)
			w.wantReason(c.want)
			w.expect("/", "")
		})
	}

	t.Run("beta", func(t *testing.T) {
		w := rocketSite(t)
		w.edit("wp-content/plugins/wp-rocket/wp-rocket.php", "Version: 3.24", "Version: 3.24-beta1")
		w.wantServing()
	})
	t.Run("mobile block without separate files", func(t *testing.T) {
		w := rocketSite(t)
		w.write("wp-content/advanced-cache.php", w.rocketDropin("7.4", true))
		w.wantServing()
	})
}

// Reasons are shown in the panel, so they must not repeat settings.
func TestRocketReasonsKeepSecrets(t *testing.T) {
	w := rocketSite(t)
	w.rocketSetting("$rocket_cache_mandatory_cookies = '';", "$rocket_cache_mandatory_cookies = '0a1b2c3d4e5f60718293a4';")
	w.rocketSetting("$rocket_cache_dynamic_cookies = array();", "$rocket_cache_dynamic_cookies = array( '0123456789abcdef0123456789abcdef' );")
	for _, r := range w.status().Reasons {
		if strings.Contains(r, "0a1b2c3d4e5f60718293a4") || strings.Contains(r, "0123456789abcdef") {
			t.Fatalf("reason %q repeats a setting", r)
		}
	}
}
