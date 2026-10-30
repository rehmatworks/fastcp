// Command caddy is the web server FastCP installs: Caddy with its standard
// modules plus ownerfs, which serves a site's files only when they belong to
// the site's account, and pagecache, which sends the pages WP Super Cache
// and WP Rocket save without starting PHP, following the plugins' own rules.
package main

import (
	"github.com/caddyserver/caddy/v2"
	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	_ "github.com/caddyserver/caddy/v2/modules/standard"

	_ "github.com/rehmatworks/fastcp/caddy/ownerfs"
	_ "github.com/rehmatworks/fastcp/caddy/pagecache"
)

// Version is what `caddy version` reports. The agent compares it with
// agent.CaddyBuild to decide whether to install this build.
const Version = "v2.11.7-fastcp.2"

func main() {
	caddy.CustomVersion = Version
	caddycmd.Main()
}
