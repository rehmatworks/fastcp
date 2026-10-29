package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/rehmatworks/fastcp/internal/validate"
)

const customReadme = `Your own Caddy configuration
============================

FastCP writes /opt/fastcp/config/Caddyfile from the control panel's settings
and overwrites manual changes to it. Put your own configuration here instead.
FastCP never changes your files; it keeps this README up to date.

  *.caddy                    Imported at the top level of the Caddyfile.
                             Add your own site blocks and snippets here.
  sites/<user>/<site>.caddy  Imported inside the site block of one site, for
                             example sites/alice/example_com.caddy. <site> is
                             the site's folder name in /home/<user>/apps.

Files and folders must be owned by root and not writable by group or others;
anyone who can change them controls the web server. Only root can open this
folder, so your files may hold credentials such as basic auth hashes.

Changes take effect when an administrator clicks Apply under Settings → Web
server in the control panel. Caddy checks the whole configuration first, and
the running configuration stays in place if yours has an error. FastCP loads
a copy of each file as it was when applied, so later edits, or a broken file
left behind, never stop the web server from starting.
`

var (
	customNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,94}\.caddy$`)
	customHashRE = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

const maxCustomFile = 1 << 20

// customFile is an administrator's file as it was when last applied. The
// Caddyfile imports FastCP's copy of it, so edits take effect only when
// applied again and a restart always loads a configuration Caddy accepted.
type customFile struct {
	Rel  string `json:"rel"`            // relative to CaddyCustomDir
	Site string `json:"site,omitempty"` // "username/slug" for site files
	Hash string `json:"hash"`
}

func (f customFile) copyPath() string { return path.Join(CaddyAppliedDir, f.Hash, f.Rel) }

// valid checks an entry read back from disk; its path ends up in the
// Caddyfile.
func (f customFile) valid() bool {
	if !customHashRE.MatchString(f.Hash) {
		return false
	}
	if f.Site == "" {
		return customNameRE.MatchString(f.Rel)
	}
	user, slug, ok := strings.Cut(f.Site, "/")
	return ok && validate.TenantUsername(user) == nil && validate.Slug(slug) == nil &&
		f.Rel == siteCustomRel(user, slug)
}

func siteCustomRel(username, slug string) string {
	return path.Join("sites", username, slug+".caddy")
}

// applyError explains why Caddy refused the configuration with the
// administrator's files. Their paths replace the copies Caddy loaded, and
// files with unbalanced braces are named, since Caddy blames the Caddyfile
// lines that follow the import for those.
func applyError(msg string, files []customSource) string {
	for _, f := range files {
		msg = strings.ReplaceAll(msg, f.copyPath(), path.Join(CaddyCustomDir, f.Rel))
	}
	for _, f := range files {
		switch d := braceDepth(string(f.Data)); {
		case d > 0:
			msg += fmt.Sprintf("\nCheck %s: a { has no matching }.", path.Join(CaddyCustomDir, f.Rel))
		case d < 0:
			msg += fmt.Sprintf("\nCheck %s: a } has no matching {.", path.Join(CaddyCustomDir, f.Rel))
		}
	}
	return msg
}

// braceDepth returns how many more blocks a Caddyfile fragment opens than
// it closes. It skips comments and quoted text but not heredocs, so it only
// serves to explain an error.
func braceDepth(data string) int {
	depth := 0
	var tok strings.Builder
	flush := func() {
		switch tok.String() {
		case "{":
			depth++
		case "}":
			depth--
		}
		tok.Reset()
	}
	for i := 0; i < len(data); i++ {
		switch c := data[i]; {
		case c == '"' || c == '`':
			tok.WriteByte(c)
			for i++; i < len(data) && data[i] != c; i++ {
				if c == '"' && data[i] == '\\' {
					i++
				}
			}
		case c == '#' && tok.Len() == 0:
			for i < len(data) && data[i] != '\n' {
				i++
			}
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			flush()
		default:
			tok.WriteByte(c)
		}
	}
	flush()
	return depth
}

// customSource is a file found in CaddyCustomDir.
type customSource struct {
	customFile
	Data    []byte
	Problem string // why the file is not used
}

// rootOnly reports whether only root can change fi.
func rootOnly(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == 0 && fi.Mode().Perm()&0o022 == 0
}

const notRootOnly = "must be owned by root and not writable by group or others"

// scanCustom reads the administrator's files in dir. sites holds the
// "username/slug" keys of existing sites. Files that anyone but root could
// change are reported with a problem and never used.
func scanCustom(dir string, sites map[string]bool, trusted func(fs.FileInfo) bool) []customSource {
	var out []customSource
	problem := func(rel, format string, a ...any) {
		out = append(out, customSource{customFile: customFile{Rel: rel}, Problem: fmt.Sprintf(format, a...)})
	}
	trustedDir := func(rel string) bool {
		fi, err := os.Lstat(filepath.Join(dir, rel))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return false
		case err != nil:
			problem(rel, "%v", err)
			return false
		case !fi.IsDir() || !trusted(fi):
			problem(rel, "the folder %s", notRootOnly)
			return false
		}
		return true
	}
	read := func(rel, site string) {
		src := customSource{customFile: customFile{Rel: rel, Site: site}}
		src.Data, src.Problem = readCustom(filepath.Join(dir, rel), trusted)
		if src.Problem == "" {
			sum := sha256.Sum256(src.Data)
			src.Hash = hex.EncodeToString(sum[:8])
		}
		out = append(out, src)
	}

	if !trustedDir(".") {
		return out
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		switch name := e.Name(); {
		case !strings.HasSuffix(name, ".caddy"):
		case !customNameRE.MatchString(name):
			problem(name, "file names may only use letters, digits, '.', '-' and '_'")
		default:
			read(name, "")
		}
	}

	if !trustedDir("sites") {
		return out
	}
	users, _ := os.ReadDir(filepath.Join(dir, "sites"))
	for _, u := range users {
		if !u.IsDir() {
			if strings.HasSuffix(u.Name(), ".caddy") {
				problem(path.Join("sites", u.Name()), "site files belong in sites/<user>/<site>.caddy")
			}
			continue
		}
		userRel := path.Join("sites", u.Name())
		if !trustedDir(userRel) {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(dir, userRel))
		for _, f := range files {
			slug, ok := strings.CutSuffix(f.Name(), ".caddy")
			if !ok {
				continue
			}
			rel, key := path.Join(userRel, f.Name()), u.Name()+"/"+slug
			if !sites[key] {
				problem(rel, "%s has no site in the folder %s", u.Name(), slug)
				continue
			}
			read(rel, key)
		}
	}
	return out
}

func readCustom(name string, trusted func(fs.FileInfo) bool) ([]byte, string) {
	fi, err := os.Lstat(name)
	if err != nil {
		return nil, err.Error()
	}
	if !fi.Mode().IsRegular() || !trusted(fi) {
		return nil, "the file " + notRootOnly + " (links are not followed)"
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err.Error()
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCustomFile+1))
	if err != nil {
		return nil, err.Error()
	}
	if len(data) > maxCustomFile {
		return nil, "the file is larger than 1 MiB"
	}
	return data, ""
}

// CustomCaddyStatus describes the administrator's configuration files.
type CustomCaddyStatus struct {
	Dir     string            `json:"dir"`
	Files   []CustomCaddyFile `json:"files"`
	Pending bool              `json:"pending"` // applying would change the configuration
}

type CustomCaddyFile struct {
	Path     string `json:"path"`
	Username string `json:"username,omitempty"`
	Slug     string `json:"slug,omitempty"`
	Domain   string `json:"domain,omitempty"` // filled in by the panel
	// State is applied, pending (new or changed since it was applied),
	// removed (applied, but the file is gone or no longer usable) or
	// ignored (see Problem).
	State   string `json:"state"`
	Problem string `json:"problem,omitempty"`
}

func customStatus(dir string, found []customSource, applied []customFile) *CustomCaddyStatus {
	st := &CustomCaddyStatus{Dir: dir, Files: []CustomCaddyFile{}}
	current := map[string]string{}
	for _, f := range applied {
		current[f.Rel] = f.Hash
	}
	seen := map[string]bool{}
	add := func(rel, site, state, problem string) {
		user, slug, _ := strings.Cut(site, "/")
		st.Files = append(st.Files, CustomCaddyFile{
			Path: path.Join(dir, rel), Username: user, Slug: slug, State: state, Problem: problem,
		})
	}
	for _, s := range found {
		seen[s.Rel] = true
		_, wasApplied := current[s.Rel]
		switch {
		case s.Problem != "":
			add(s.Rel, s.Site, "ignored", s.Problem)
			st.Pending = st.Pending || wasApplied
		case current[s.Rel] == s.Hash:
			add(s.Rel, s.Site, "applied", "")
		default:
			add(s.Rel, s.Site, "pending", "")
			st.Pending = true
		}
	}
	for _, f := range applied {
		if !seen[f.Rel] {
			add(f.Rel, f.Site, "removed", "")
			st.Pending = true
		}
	}
	sort.SliceStable(st.Files, func(i, j int) bool { return st.Files[i].Path < st.Files[j].Path })
	return st
}
