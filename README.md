# FastCP

A lightweight control panel for Ubuntu and Debian servers, built on Caddy, PHP-FPM and MySQL.

## Features

- **Sites** with automatic HTTPS from Let's Encrypt, extra domains and redirects, and one-click WordPress.
- **PHP 8.x**, several versions side by side and chosen per site. Patch releases are installed automatically every night, and new PHP versions are installed as soon as they are published.
- **Isolation**: each account is a Linux user, each site runs in its own PHP-FPM pool as that user, and memory and CPU limits are enforced with systemd.
- **MySQL** databases and users, with phpMyAdmin built in.
- **Backups** with restic to SFTP, S3, rclone or a local folder, on a schedule, with per-site and per-database restore and zip downloads.
- **Cron jobs**, **SSH keys and SFTP**, and a **firewall** (ufw) that keeps SSH and the panel reachable.
- **Signed updates** from the panel, with automatic rollback if the new version does not start.

## Requirements

- Ubuntu 22.04 or newer, or Debian 12 or newer, on a clean server
- 1 GB of memory (2 GB or more recommended) and 5 GB of free disk space
- Root access

## Install

```bash
curl -fsSL https://get.fastcp.org | sudo bash
```

The installer verifies the release signature, installs MySQL, Caddy, phpMyAdmin and the newest PHP, and prints the control panel address and the password of the `fastcp` administrator account. Open `https://your-server-ip:2050` and sign in. The panel uses a self-signed certificate, so your browser warns about it on the first visit.

To install a specific release, set `FASTCP_VERSION`:

```bash
curl -fsSL https://get.fastcp.org | sudo FASTCP_VERSION=v3.0.1 bash
```

Accounts sign in with their Linux user name and password. `root` cannot sign in.

## Updating

Go to **Settings → Updates**. The panel downloads the release, checks its signature, swaps the binaries and restarts. If the new version does not come up, the previous one is restored.

Updating from FastCP 2 works the same way. The first start of version 3 moves the server to the new layout: the panel and the web server stop running as root, certificates and sites are kept, and everyone has to sign in again.

## How it works

```
 Browser ──HTTPS :2050──▶ fastcp            control panel, runs as fastcp-panel
                            │ Unix socket (peer credentials checked)
                            ▼
                          fastcp-agent      the only root process; applies changes
                            │
          ┌─────────────────┼───────────────────────┐
          ▼                 ▼                       ▼
   Caddy :80/:443     PHP-FPM pools           MySQL, users, cron,
   runs as caddy      one per site, run       SSH, firewall, backups
                      as the site's owner
```

The panel keeps its data in SQLite and pushes the desired configuration to the agent, which renders the Caddyfile and PHP-FPM pools, validates them, and reloads the services without dropping connections.

| Path | Contents |
| --- | --- |
| `/opt/fastcp/bin` | `fastcp` and `fastcp-agent` |
| `/opt/fastcp/data` | panel database and keys (owned by `fastcp-panel`) |
| `/opt/fastcp/config` | generated Caddyfile and settings |
| `/var/lib/fastcp/caddy` | certificates (owned by `caddy`) |
| `/home/<user>/apps/<site>/public` | a site's document root |
| `/var/log/fastcp/sites/<user>/<site>` | a site's logs |

## Troubleshooting

```bash
systemctl status fastcp fastcp-agent fastcp-caddy
journalctl -u fastcp -u fastcp-agent -f
sudo systemctl restart fastcp-agent fastcp
```

## Uninstall

```bash
curl -fsSL https://github.com/rehmatworks/fastcp/releases/latest/download/uninstall.sh -o uninstall.sh
sudo bash uninstall.sh
```

The uninstaller asks before deleting accounts, sites, databases or packages.

## Development

You need Go 1.27, Node.js 22 or newer, and on Linux `libpam0g-dev`.

```bash
make dev                 # panel on http://127.0.0.1:2050 with an in-memory agent (macOS)
cd web && npm run dev    # UI with hot reload on http://127.0.0.1:5173
make check               # go vet, go test, svelte-check and vitest
make ui                  # rebuild cmd/fastcp/ui/dist (committed)
make docker              # install FastCP in a container with the real installer
```

The UI in `web/` is Svelte 5 with TypeScript. Its build is committed to `cmd/fastcp/ui/dist` so that `go build` works without Node; CI fails if the committed build is out of date. Agent tests that need root run with `FASTCP_INTEGRATION=1` in a Linux container:

```bash
docker run --rm -v "$PWD":/src -w /src -e FASTCP_INTEGRATION=1 golang:1.27-bookworm \
  sh -c 'apt-get update -qq && apt-get install -y -qq libpam0g-dev acl cron >/dev/null && go test ./...'
```

## Releasing

1. One time: create the signing key and add the private key as the `FASTCP_SIGNING_KEY` repository secret.
   ```bash
   go run ./tools/releasekey gen -pub internal/release/signing.pub -installer scripts/install.sh
   ```
   Commit both changed files. Never commit the private key.
2. `make bump V=x.y.z`, then `make ui`, and commit.
3. Push the commit and the tag `vx.y.z` together. The release workflow runs the checks, builds the binaries, and publishes them with `SHA256SUMS` and `SHA256SUMS.sig`.

## License

MIT. See [LICENSE](LICENSE).
