package pagecache

import (
	"encoding/json"
	"errors"
	"flag"
	"math"
	"os"
	"path"
	"time"

	"github.com/caddyserver/caddy/v2"
	caddycmd "github.com/caddyserver/caddy/v2/cmd"

	"github.com/rehmatworks/fastcp/caddy/ownerfs"
)

func init() {
	flags := flag.NewFlagSet("fastcp-page-cache", flag.ExitOnError)
	flags.String("root", "", "the site's document root")
	flags.Int("uid", 0, "the uid of the site's account")
	caddycmd.RegisterCommand(caddycmd.Command{
		Name:  "fastcp-page-cache",
		Usage: "--root <dir> --uid <uid>",
		Short: "Reports whether a WordPress site's cached pages are sent without PHP",
		Long: `
Reads the WordPress site in --root the way fastcp_page_cache does, through
the fastcp_owner file system of --uid, and prints as JSON which cache plugin
the site uses, whether the web server sends its saved pages, and why not.
`,
		Flags: flags,
		Func:  cmdPageCache,
	})
}

func cmdPageCache(fl caddycmd.Flags) (int, error) {
	root := fl.String("root")
	uid := fl.Int("uid")
	if !path.IsAbs(root) || path.Clean(root) != root || root == "/" {
		return caddy.ExitCodeFailedStartup, errors.New("--root must be a clean absolute path")
	}
	if uid <= 0 || uid >= math.MaxUint32 {
		return caddy.ExitCodeFailedStartup, errors.New("--uid must be a non-root user")
	}
	s := loadSite(&ownerfs.FS{UID: uid}, root, time.Now())
	if err := json.NewEncoder(os.Stdout).Encode(s.status); err != nil {
		return caddy.ExitCodeFailedStartup, err
	}
	return 0, nil
}
