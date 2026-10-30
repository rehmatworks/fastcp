package pagecache

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"
)

// Status is what the panel shows about a site's page cache.
type Status struct {
	// Plugin is "WP Super Cache" or "WP Rocket" when one of them owns
	// wp-content/advanced-cache.php.
	Plugin  string `json:"plugin,omitempty"`
	Version string `json:"version,omitempty"`
	// Serving reports whether the web server sends saved pages itself
	// when the plugin's rules allow it.
	Serving bool `json:"serving"`
	// Reasons explain, in plain words, why pages go to PHP instead.
	Reasons []string `json:"reasons,omitempty"`
}

// finder is a cache plugin's serving logic for one site.
type finder interface {
	// find returns the saved page PHP would send for req, or nil when PHP
	// would build the page or the answer is not certain.
	find(fsys fs.FS, req *request, now time.Time) *hit
}

// hit is a saved page to send.
type hit struct {
	file   fs.File
	info   fs.FileInfo
	header [][2]string // sent unless the site's configuration sets them
	// lastModified sends the file's time and answers a matching
	// If-Modified-Since with 304, as WP Rocket does.
	lastModified bool
}

// site is what a site's files said when last read.
type site struct {
	checked time.Time
	deps    []dep
	status  Status
	finder  finder // nil unless serving
}

// dep is a file or directory a site was read from, and how it looked.
type dep struct {
	name string
	sig  sig
}

type sig struct {
	exists bool
	mode   fs.FileMode
	size   int64
	mtime  int64
}

func sigOf(info fs.FileInfo, err error) sig {
	if err != nil {
		return sig{}
	}
	return sig{exists: true, mode: info.Mode(), size: info.Size(), mtime: info.ModTime().UnixNano()}
}

// changed reports whether any file s was read from looks different now.
func (s *site) changed(fsys fs.FS) bool {
	for _, d := range s.deps {
		if sigOf(fs.Stat(fsys, d.name)) != d.sig {
			return true
		}
	}
	return false
}

// loader reads a site's files through Caddy's file system, noting each
// one so a later change is noticed.
type loader struct {
	fsys fs.FS
	deps []dep
}

func (l *loader) note(name string, info fs.FileInfo, err error) {
	l.deps = append(l.deps, dep{name: name, sig: sigOf(info, err)})
}

func (l *loader) stat(name string) (fs.FileInfo, error) {
	info, err := fs.Stat(l.fsys, name)
	l.note(name, info, err)
	return info, err
}

// isFile reports whether name is a regular file.
func (l *loader) isFile(name string) bool {
	info, err := l.stat(name)
	return err == nil && info.Mode().IsRegular()
}

// read returns the content of the regular file name, up to max bytes.
func (l *loader) read(name string, max int64) (string, error) {
	f, err := l.fsys.Open(name)
	if err != nil {
		l.note(name, nil, err)
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	l.note(name, info, err)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	if info.Size() > max {
		return "", errors.New("too large")
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return "", err
	}
	if int64(len(b)) > max {
		return "", errors.New("too large")
	}
	return string(b), nil
}

// head returns up to the first n bytes of the regular file name.
func (l *loader) head(name string, n int64) (string, error) {
	f, err := l.fsys.Open(name)
	if err != nil {
		l.note(name, nil, err)
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	l.note(name, info, err)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, n))
	return string(b), err
}

// readDir lists the directory name.
func (l *loader) readDir(name string) ([]fs.DirEntry, error) {
	l.stat(name)
	return fs.ReadDir(l.fsys, name)
}

// loadSite reads the page cache setup of the WordPress site in root.
func loadSite(fsys fs.FS, root string, now time.Time) *site {
	l := &loader{fsys: fsys}
	s := &site{checked: now}
	s.status, s.finder = l.load(root)
	s.deps = l.deps
	if s.finder == nil {
		s.status.Serving = false
	}
	return s
}

func (l *loader) load(root string) (Status, finder) {
	content := root + "/wp-content"
	dropin, err := l.read(content+"/advanced-cache.php", maxPHP)
	if errors.Is(err, fs.ErrNotExist) {
		return Status{Reasons: []string{"No WP Super Cache or WP Rocket page cache is installed."}}, nil
	}
	if err != nil {
		return Status{Reasons: []string{"The web server cannot read wp-content/advanced-cache.php."}}, nil
	}
	var st Status
	var f finder
	switch {
	case strings.Index(dropin, "WP SUPER CACHE 0.8.9.1") > 0 || strings.Index(dropin, "WP SUPER CACHE 1.2") > 0:
		st, f = l.loadSuperCache(root, dropin)
	case strings.Contains(dropin, "WP_ROCKET_ADVANCED_CACHE"):
		st, f = l.loadRocket(root, dropin)
	default:
		return Status{Reasons: []string{"wp-content/advanced-cache.php belongs to a cache plugin other than WP Super Cache or WP Rocket."}}, nil
	}
	if f != nil {
		if reason := l.checkFrontController(root); reason != "" {
			st.Reasons = append([]string{reason}, st.Reasons...)
			f = nil
		}
	}
	st.Serving = f != nil
	return st, f
}

// checkFrontController makes sure index.php is WordPress's own, which
// loads WordPress, and with it advanced-cache.php, from this directory.
func (l *loader) checkFrontController(root string) string {
	src, err := l.read(root+"/index.php", 64<<10)
	if err != nil {
		return "The site has no index.php the web server can read."
	}
	toks, err := tokenize(src)
	if err != nil {
		return "index.php is not WordPress's own."
	}
	var words []string
	for _, t := range toks {
		if t.kind == tEOF {
			break
		}
		if t.kind == tString {
			words = append(words, strconv.Quote(t.text))
		} else {
			words = append(words, t.text)
		}
	}
	got := strings.Join(words, " ")
	for _, want := range wordpressIndex {
		if got == want {
			return ""
		}
	}
	return "index.php is not WordPress's own, so the web server cannot tell what it does."
}

// wordpressIndex lists the tokens of the index.php WordPress has shipped.
var wordpressIndex = func() []string {
	var out []string
	define := `define ( "WP_USE_THEMES" , true ) ;`
	for _, dir := range []string{`__DIR__`, `dirname ( __FILE__ )`} {
		for _, req := range []string{"require", "require_once"} {
			arg := dir + ` . "/wp-blog-header.php"`
			out = append(out,
				define+" "+req+" "+arg+" ;",
				define+" "+req+" ( "+arg+" ) ;")
		}
	}
	return out
}()

// reachesWordPress reports whether php_fastcgi would hand a request for p
// to the WordPress index.php in root: its try_files would find neither a
// file nor a directory index for p, and would not redirect to add a slash.
// p is clean apart from a trailing slash, with no ".php" and no glob
// characters.
func reachesWordPress(fsys fs.FS, root, p string) bool {
	file := func(rel string) bool {
		info, err := fs.Stat(fsys, joinRoot(root, rel))
		return err == nil && !info.IsDir()
	}
	index := path.Join(p, "index.php")
	if !strings.HasSuffix(p, "/") && file(index) {
		return false // redirected to p + "/"
	}
	if file(path.Clean(p)) {
		return false // a file
	}
	if file(index) {
		return index == "/index.php"
	}
	return true // the index.php fallback
}

// joinRoot is caddyhttp.SanitizedPathJoin for a clean relative path.
func joinRoot(root, rel string) string {
	if rel == "/" {
		return root
	}
	return root + rel
}

// cleanRequestPath reports whether p is an absolute path with no empty,
// "." or ".." segments, allowing a trailing slash.
func cleanRequestPath(p string) bool {
	if !strings.HasPrefix(p, "/") {
		return false
	}
	trimmed := strings.TrimSuffix(p, "/")
	return trimmed == "" || path.Clean(trimmed) == trimmed
}

// within reports whether dir, an absolute path, is root or inside it.
func within(dir, root string) bool {
	dir = path.Clean(dir)
	return path.IsAbs(dir) && (dir == root || strings.HasPrefix(dir, root+"/"))
}

// compareVersions compares dotted version numbers: -1, 0 or 1.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(as), len(bs)); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}

// atLeast reports whether the plugin version v, which may carry a
// pre-release suffix such as "-beta1", is min or later.
func atLeast(v, min string) bool {
	num, pre, _ := strings.Cut(v, "-")
	c := compareVersions(num, min)
	return c > 0 || c == 0 && pre == ""
}

// validVersion reports whether v is a dotted version number.
func validVersion(v string) bool {
	if v == "" {
		return false
	}
	for _, part := range strings.Split(v, ".") {
		if part == "" || strings.Trim(part, "0123456789") != "" || len(part) > 6 {
			return false
		}
	}
	return true
}

// unreadable explains why a plugin's settings file could not be used.
func unreadable(plugin, file string, err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("%s's settings file %s is missing.", plugin, file)
	case errors.Is(err, errUnsupported):
		return fmt.Sprintf("%s's settings file %s contains PHP the web server does not read, so it cannot be sure of the settings.", plugin, file)
	default:
		return fmt.Sprintf("The web server cannot read %s's settings file %s.", plugin, file)
	}
}
