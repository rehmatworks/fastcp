package pagecache

import (
	"errors"
	"reflect"
	"testing"
)

func mustEval(t *testing.T, src string, consts map[string]string) map[string]value {
	t.Helper()
	vars, err := evalPHP(src, consts)
	if err != nil {
		t.Fatalf("evalPHP: %v", err)
	}
	return vars
}

func wantVar(t *testing.T, vars map[string]value, name string, want value) {
	t.Helper()
	got, ok := vars[name]
	if !ok {
		t.Errorf("$%s is unset, want %+v", name, want)
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("$%s = %+v, want %+v", name, got, want)
	}
}

func intValue(n int64) value { return value{kind: integer, i: n} }

func TestEvalSuperCacheConfig(t *testing.T) {
	vars := mustEval(t, superCacheConfig, map[string]string{
		"ABSPATH": "/srv/www/", "WP_CONTENT_DIR": "/srv/www/wp-content", "WPCACHEHOME": "/srv/www/wp-content/plugins/wp-super-cache/",
	})
	wantVar(t, vars, "cache_path", strValue("/srv/www/wp-content/cache/"))
	wantVar(t, vars, "cache_enabled", value{kind: boolean, b: true})
	wantVar(t, vars, "cache_max_time", intValue(3600))
	wantVar(t, vars, "wp_cache_plugins_dir", strValue("/srv/www/wp-content/plugins/wp-super-cache/plugins"))
	wantVar(t, vars, "cache_scheduled_time", strValue("00:00"))
	wantVar(t, vars, "wpsc_version", intValue(169))
	wantVar(t, vars, "cache_rejected_uri", value{kind: array, items: []item{
		{intValue(0), strValue(`wp-.*\.php`)}, {intValue(1), strValue(`index\.php`)},
	}})
	wantVar(t, vars, "wp_cache_pages", value{}) // set key by key
	if _, ok := vars["use_flock"]; ok {
		t.Error("a commented-out line was read")
	}

	// WP Super Cache adds the slash itself when the folder lacks one.
	vars = mustEval(t, "<?php\n$cache_path = WP_CONTENT_DIR . '/cache';\nif ( '/' != substr($cache_path, -1)) {\n\t$cache_path .= '/';\n}\n",
		map[string]string{"WP_CONTENT_DIR": "/srv/www/wp-content"})
	wantVar(t, vars, "cache_path", strValue("/srv/www/wp-content/cache/"))
}

func TestEvalRocketConfig(t *testing.T) {
	vars := mustEval(t, rocketSettings, map[string]string{"ABSPATH": "/srv/www/"})
	wantVar(t, vars, "rocket_cache_ssl", intValue(1))
	wantVar(t, vars, "rocket_permalink_structure", strValue("/%postname%/"))
	wantVar(t, vars, "rocket_cache_mandatory_cookies", strValue(""))
	wantVar(t, vars, "rocket_cache_dynamic_cookies", value{kind: array})
	wantVar(t, vars, "rocket_cache_reject_ua", strValue("facebookexternalhit|WhatsApp"))
	params := vars["rocket_cache_ignored_parameters"]
	if params.kind != array || len(params.items) != 5 || !reflect.DeepEqual(params.items[0], item{strValue("utm_source"), intValue(1)}) {
		t.Errorf("$rocket_cache_ignored_parameters = %+v", params)
	}

	// Without ABSPATH the file exits before setting anything.
	vars = mustEval(t, rocketSettings, nil)
	if v := vars["rocket_cache_ssl"]; v.kind != unknown {
		t.Errorf("$rocket_cache_ssl = %+v after a possible exit", v)
	}
}

func TestEvalLiterals(t *testing.T) {
	vars := mustEval(t, `<?php
$single = 'it\'s \\ \n';
$double = "line\n\x41\101\u{e9}\q";
$template = "cost: $x";
$hex = 0x1F;
$octal = 0755;
$sep = 1_000;
$neg = -5;
$exp = 1.5e3;
$plus = +'5';
$arr = array( 'x' => 1, 2, '5' => 3, 4, );
$short = [ true, false, null ];
$joined = ABSPATH . 'wp-content' . "/" . 5;
$unknownConst = SOMETHING_ELSE;
$sum = 1 + 2;
$closed = 'yes' ?>
<p>HTML in between</p>
<?php
$after = 'tag';
`, map[string]string{"ABSPATH": "/srv/"})
	wantVar(t, vars, "single", strValue(`it's \ \n`))
	wantVar(t, vars, "double", strValue("line\nAA\u00e9\\q"))
	wantVar(t, vars, "template", value{})
	wantVar(t, vars, "hex", intValue(31))
	wantVar(t, vars, "octal", intValue(0o755))
	wantVar(t, vars, "sep", intValue(1000))
	wantVar(t, vars, "neg", intValue(-5))
	wantVar(t, vars, "exp", value{kind: float, f: 1500})
	wantVar(t, vars, "plus", value{})
	wantVar(t, vars, "arr", value{kind: array, items: []item{
		{strValue("x"), intValue(1)}, {intValue(0), intValue(2)}, {intValue(5), intValue(3)}, {intValue(6), intValue(4)},
	}})
	wantVar(t, vars, "short", value{kind: array, items: []item{
		{intValue(0), value{kind: boolean, b: true}}, {intValue(1), value{kind: boolean}}, {intValue(2), value{kind: null}},
	}})
	wantVar(t, vars, "joined", strValue("/srv/wp-content/5"))
	wantVar(t, vars, "unknownConst", value{})
	wantVar(t, vars, "sum", value{})
	wantVar(t, vars, "closed", strValue("yes"))
	wantVar(t, vars, "after", strValue("tag"))
}

// Whatever might change a variable makes it unknown.
func TestEvalUnknowns(t *testing.T) {
	vars := mustEval(t, `<?php
$plain = 'x';
$call = foo();
$cond = 1;
if ( $x ) { $cond = 2; }
$elseBranch = 1;
if ( $x ) { } elseif ( $y ) { } else $elseBranch = 2;
$loop = 1;
foreach ( $list as $loopItem ) { $loop++; }
$pushed = array( 1, 2 );
$pushed[] = 3;
$sorted = 'e';
sort( $sorted );
$ref = 'r';
$alias = &$ref;
$constClass = Foo::class;
$kept = 'g';
$obj->function();
$anon = new class { public $inner = 1; };
$captured = 'j';
$arrow = fn() => $captured;
$byRef = 'l';
$closure = function () use ( &$byRef ) {};
$listed = 'n';
list( $listed ) = [ 'o' ];
$inner = 'outer';
function helper() { $inner = 'function scope'; }
class Holder { public function set() { $kept = 'method scope'; } }
$switched = 1;
switch ( $x ) { case 1: $switched = 2; break; }
try { $tried = 1; } catch ( Exception $e ) { }
$last = 'p';
`, nil)
	for name, want := range map[string]value{
		"plain": strValue("x"), "kept": strValue("g"), "captured": strValue("j"), "inner": strValue("outer"), "last": strValue("p"),
	} {
		wantVar(t, vars, name, want)
	}
	for _, name := range []string{
		"call", "cond", "elseBranch", "loop", "loopItem", "pushed", "sorted", "ref", "alias", "constClass", "anon",
		"arrow", "byRef", "closure", "listed", "switched", "tried",
	} {
		wantVar(t, vars, name, value{})
	}
}

func TestEvalStops(t *testing.T) {
	vars := mustEval(t, "<?php\n$a = 1;\nreturn;\n$b = 2;\n", nil)
	wantVar(t, vars, "a", intValue(1))
	if _, ok := vars["b"]; ok {
		t.Error("$b set after return")
	}

	vars = mustEval(t, "<?php\n$a = 1;\nif ( $x ) { return; }\n$b = 2;\n", nil)
	wantVar(t, vars, "a", intValue(1))
	wantVar(t, vars, "b", value{})

	vars = mustEval(t, "<?php\ndefined( 'ABSPATH' ) || exit;\n$a = 1;\n", map[string]string{"ABSPATH": "/srv/"})
	wantVar(t, vars, "a", intValue(1))
	vars = mustEval(t, "<?php\ndefined( 'ABSPATH' ) or die( 'No' );\n$a = 1;\n", map[string]string{"ABSPATH": "/srv/"})
	wantVar(t, vars, "a", intValue(1))
	vars = mustEval(t, "<?php\ndefined( 'OTHER' ) || exit;\n$a = 1;\n", map[string]string{"ABSPATH": "/srv/"})
	wantVar(t, vars, "a", value{})
}

func TestEvalRefuses(t *testing.T) {
	unsupported := []string{
		"<? $a = 1;",
		"<?php $$name = 1;",
		"<?php $GLOBALS['a'] = 1;",
		"<?php extract( $settings );",
		"<?php parse_str( $q );",
		"<?php eval( '$a = 1;' );",
		"<?php goto end; end:",
		"<?php namespace Foo;",
		"<?php $a = `id`;",
		"<?php $a = <<<EOT\nx\nEOT;\n",
		"<?php if ( $x ): $a = 1; endif;",
		"<?php $a = foo( class {} );",
	}
	for _, src := range unsupported {
		if _, err := evalPHP(src, nil); !errors.Is(err, errUnsupported) {
			t.Errorf("evalPHP(%q) = %v, want errUnsupported", src, err)
		}
	}
	invalid := []string{
		"<?php $a = 'open;",
		"<?php $a = ( 1;",
		"<?php $a = 1; }",
		"<?php /* open",
		"<?php if ( $x ) { $a = 1;",
	}
	for _, src := range invalid {
		if _, err := evalPHP(src, nil); !errors.Is(err, errSyntax) {
			t.Errorf("evalPHP(%q) = %v, want errSyntax", src, err)
		}
	}
	big := make([]byte, maxPHP+1)
	if _, err := evalPHP(string(big), nil); err == nil {
		t.Error("evalPHP read an oversized file")
	}
}

func TestPluginVersion(t *testing.T) {
	for header, want := range map[string]string{
		superCacheHeader:                         "3.1.4",
		rocketHeader:                             "3.24",
		"<?php\n/*\nVersion: 3.24-beta1\n*/":     "3.24-beta1",
		"<?php\n/**\n * Version:   1.2.3  \n */": "1.2.3",
		"<?php\n// Version: 2.0\n":               "2.0",
		"<?php\n/*\n * Version: latest\n */":     "",
		"<?php\n/*\n * Version: 1.2 beta\n */":   "",
		"<?php\n/*\n * Version: 1..2\n */":       "",
		"<?php\n/*\n * No version here\n */":     "",
	} {
		if got := pluginVersion(header); got != want {
			t.Errorf("pluginVersion(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestVersions(t *testing.T) {
	for _, c := range []struct {
		v, min string
		want   bool
	}{
		{"3.1.4", "3.1.3", true}, {"3.1.3", "3.1.3", true}, {"3.1.2", "3.1.3", false}, {"3.2", "3.1.3", true},
		{"3.10", "3.9", true}, {"3.13.3-beta1", "3.13.3", false}, {"3.24-beta1", "3.13.3", true},
		{"4", "3.13.3", true}, {"3.13", "3.13.3", false},
	} {
		if got := atLeast(c.v, c.min); got != c.want {
			t.Errorf("atLeast(%q, %q) = %v", c.v, c.min, got)
		}
	}
}
