package agent

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// notWritableByOthers stands in for rootOnly, since tests do not run as root.
func notWritableByOthers(fi fs.FileInfo) bool { return fi.Mode().Perm()&0o022 == 0 }

func writeTestFile(t *testing.T, name, data string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(data), perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, perm); err != nil {
		t.Fatal(err)
	}
}

func TestScanCustom(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "extra.caddy"), "example.org {\n\trespond hi\n}\n", 0o644)
	writeTestFile(t, filepath.Join(dir, "bad name.caddy"), "x", 0o644)
	writeTestFile(t, filepath.Join(dir, "README"), "not configuration", 0o644)
	writeTestFile(t, filepath.Join(dir, "writable.caddy"), "x", 0o666)
	writeTestFile(t, filepath.Join(dir, "big.caddy"), strings.Repeat("#", maxCustomFile+1), 0o644)
	if err := os.Symlink(filepath.Join(dir, "extra.caddy"), filepath.Join(dir, "link.caddy")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "sites", "alice", "example-com.caddy"), "basic_auth {\n}\n", 0o644)
	writeTestFile(t, filepath.Join(dir, "sites", "alice", "gone.caddy"), "x", 0o644)
	writeTestFile(t, filepath.Join(dir, "sites", "stray.caddy"), "x", 0o644)
	writeTestFile(t, filepath.Join(dir, "sites", "bob", "bob-site.caddy"), "x", 0o644)
	if err := os.Chmod(filepath.Join(dir, "sites", "bob"), 0o777); err != nil {
		t.Fatal(err)
	}

	sites := map[string]bool{"alice/example-com": true, "bob/bob-site": true}
	found := scanCustom(dir, sites, notWritableByOthers)
	got := map[string]customSource{}
	for _, s := range found {
		got[s.Rel] = s
	}
	for rel, usable := range map[string]bool{
		"extra.caddy":                   true,
		"sites/alice/example-com.caddy": true,
		"bad name.caddy":                false,
		"writable.caddy":                false,
		"big.caddy":                     false,
		"link.caddy":                    false,
		"sites/alice/gone.caddy":        false,
		"sites/stray.caddy":             false,
		"sites/bob":                     false,
	} {
		s, ok := got[rel]
		switch {
		case !ok:
			t.Errorf("%s not reported", rel)
		case usable && (s.Problem != "" || s.Hash == ""):
			t.Errorf("%s should be usable: %+v", rel, s)
		case !usable && s.Problem == "":
			t.Errorf("%s should be ignored", rel)
		}
	}
	if len(got) != 9 {
		t.Errorf("unexpected entries: %v", got)
	}
	if s := got["sites/alice/example-com.caddy"]; s.Site != "alice/example-com" || !s.valid() {
		t.Errorf("site file = %+v", s)
	}

	applied := []customFile{
		{Rel: "extra.caddy", Hash: got["extra.caddy"].Hash},
		{Rel: "writable.caddy", Hash: "0000000000000000"},
		{Rel: "old.caddy", Hash: "1111111111111111"},
	}
	st := customStatus(dir, found, applied)
	states := map[string]string{}
	for _, f := range st.Files {
		states[strings.TrimPrefix(f.Path, dir+"/")] = f.State
	}
	for rel, want := range map[string]string{
		"extra.caddy":                   "applied",
		"sites/alice/example-com.caddy": "pending",
		"writable.caddy":                "ignored",
		"old.caddy":                     "removed",
	} {
		if states[rel] != want {
			t.Errorf("%s state = %q, want %q", rel, states[rel], want)
		}
	}
	if !st.Pending || len(st.Files) != 10 {
		t.Errorf("status = %+v", st)
	}

	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if found := scanCustom(dir, sites, notWritableByOthers); len(found) != 1 || found[0].Problem == "" {
		t.Errorf("a writable folder must disable every file: %+v", found)
	}
	if found := scanCustom(filepath.Join(dir, "missing"), sites, notWritableByOthers); len(found) != 0 {
		t.Errorf("a missing folder has no files: %+v", found)
	}
}

func TestApplyError(t *testing.T) {
	files := []customSource{
		{customFile: customFile{Rel: "extra.caddy", Hash: "0123456789abcdef"}, Data: []byte("http://a.test {\n\trespond ok\n}\n")},
		{customFile: customFile{Rel: "sites/alice/example-com.caddy", Site: "alice/example-com", Hash: "fedcba9876543210"}, Data: []byte("bogus\n")},
	}
	msg := "adapting config using caddyfile: " + files[1].copyPath() + ":1: unrecognized directive: bogus"
	want := "adapting config using caddyfile: " + CaddyCustomDir + "/sites/alice/example-com.caddy:1: unrecognized directive: bogus"
	if got := applyError(msg, files); got != want {
		t.Errorf("applyError = %q, want %q", got, want)
	}

	files[1].Data = []byte("reverse_proxy {\n\theader_up Host {upstream_hostport}\n")
	want = "invalid\nCheck " + CaddyCustomDir + "/sites/alice/example-com.caddy: a { has no matching }."
	if got := applyError("invalid", files); got != want {
		t.Errorf("applyError = %q, want %q", got, want)
	}
}

func TestBraceDepth(t *testing.T) {
	cases := map[string]int{
		"http://a.test {\n\trespond \"ok\"\n}\n":                    0,
		"reverse_proxy {\n\theader_up Host {upstream_hostport}\n":   1,
		"handle /x {\n\trespond x\n}\n}\n":                          -1,
		"respond \"{\" 200 # {\nheader X-A `}`\n":                   0,
		"respond \"a \\\" {\"\n":                                    0,
		"@api path /api/*\nhandle @api {\n\treverse_proxy :3001\n}": 0,
	}
	for in, want := range cases {
		if got := braceDepth(in); got != want {
			t.Errorf("braceDepth(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestCustomFileValid(t *testing.T) {
	for _, f := range []customFile{
		{Rel: "a.caddy", Hash: "0123456789abcdef"},
		{Rel: "sites/alice/example-com.caddy", Site: "alice/example-com", Hash: "0123456789abcdef"},
	} {
		if !f.valid() {
			t.Errorf("%+v should be valid", f)
		}
	}
	for _, f := range []customFile{
		{Rel: "a.caddy", Hash: "xyz"},
		{Rel: "../a.caddy", Hash: "0123456789abcdef"},
		{Rel: "a b.caddy", Hash: "0123456789abcdef"},
		{Rel: "sites/alice/other.caddy", Site: "alice/example-com", Hash: "0123456789abcdef"},
		{Rel: "sites/root/x.caddy", Site: "root/x", Hash: "0123456789abcdef"},
		{Rel: "sites/alice/../../etc.caddy", Site: "alice/../../etc", Hash: "0123456789abcdef"},
	} {
		if f.valid() {
			t.Errorf("%+v should be invalid", f)
		}
	}
}
