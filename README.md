# FastCP

A lightweight, modern server control panel for Ubuntu servers powered by Caddy and PHP-FPM.

## Features

- **Simple & Fast** - Minimal UI, focused on essentials like ServerPilot
- **PHP-FPM Multi-Version** - Run and manage multiple PHP versions (8.x) from FastCP
- **Secure Multi-User** - System user isolation with ACLs
- **PAM Authentication** - Login with your Linux system credentials
- **WordPress Ready** - One-click WordPress installation
- **MySQL Management** - Create databases and users easily
- **SSH Key Management** - Manage SSH access from the UI
- **Automatic SSL** - Free Let's Encrypt certificates via Caddy

## Requirements

- Ubuntu 22.04 LTS or 24.04 LTS
- Minimum 1GB RAM (2GB+ recommended)
- 5GB+ free disk space
- Root access

## Quick Install

```bash
curl -fsSL https://get.fastcp.org | bash
```

Or manually:

```bash
wget -O install.sh https://get.fastcp.org
chmod +x install.sh
sudo ./install.sh
```

## Access

After installation:

- **URL**: `https://your-server-ip:2050`
- **Login**: Use your Linux system username and password

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    FastCP Control Panel                          │
│                    (Go binary, port 2050 HTTPS)                  │
│  - Web UI & REST API                                             │
│  - User authentication (PAM)                                     │
│  - phpMyAdmin reverse proxy                                      │
└────────────────────────────┬────────────────────────────────────┘
                             │ Unix Socket
┌────────────────────────────▼────────────────────────────────────┐
│                    FastCP Agent (runs as root)                   │
│  - User/database management                                      │
│  - Caddyfile generation                                          │
│  - SSH key management                                            │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                    Caddy (ports 80/443)                          │
│  - Main reverse proxy for all websites                           │
│  - Automatic HTTPS (Let's Encrypt)                               │
│  - phpMyAdmin served internally                                  │
└────────────────────────────┬────────────────────────────────────┘
                             │ Unix Sockets
┌────────────────────────────▼────────────────────────────────────┐
│         Per-Site PHP-FPM Pools (run as each Linux user)          │
│  - Isolated PHP execution for security                            │
│  - Multi-version PHP support per website                          │
│  - Resource limits (CPU/RAM) via cgroups                         │
└─────────────────────────────────────────────────────────────────┘
```

## Directory Structure

```
/opt/fastcp/
├── bin/
│   ├── caddy            # Caddy binary
│   ├── fastcp           # Control panel (when built custom)
│   └── fastcp-agent     # Privileged helper
├── config/
│   └── Caddyfile        # Main configuration
└── data/
    └── fastcp.db        # SQLite database

/home/{user}/
├── apps/
│   └── {domain}/
│       ├── public/      # Document root
│       ├── logs/        # Access/error logs
│       └── tmp/         # Temporary files
└── .ssh/
    └── authorized_keys  # SSH keys
```

## Building from Source

### Prerequisites

```bash
# Install Go 1.23+
wget https://go.dev/dl/go1.23.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.23.0.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Install build dependencies
sudo apt-get install -y build-essential libpam0g-dev
```

### Build

```bash
git clone https://github.com/rehmatworks/fastcp.git
cd fastcp

# Build FastCP binaries
make build

# Or build individually
CGO_ENABLED=1 go build -o bin/fastcp ./cmd/fastcp
CGO_ENABLED=1 go build -o bin/fastcp-agent ./cmd/fastcp-agent
```

### Install

```bash
sudo make install
```

## Configuration

### Main Caddyfile (`/opt/fastcp/config/Caddyfile`)

```caddyfile
{
    fastcp {
        data_dir /opt/fastcp/data
        agent_socket /opt/fastcp/run/agent.sock
        listen_port 2050
    }
    
    # PHP is served by per-site PHP-FPM pools and reverse proxied by Caddy
}

:2050 {
    tls internal
    fastcp_ui
}

# Sites are added here dynamically
```

## API

FastCP exposes a REST API via Caddy's admin endpoint:

```bash
# Login
curl -X POST http://localhost:2019/fastcp/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"myuser","password":"mypass"}'

# List sites
curl http://localhost:2019/fastcp/sites \
  -H "Authorization: Bearer <token>"

# Create site
curl -X POST http://localhost:2019/fastcp/sites \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"domain":"example.com","site_type":"php"}'
```

## Security

- **User Isolation**: Each site's PHP-FPM pool runs as the owning Linux user
- **Privilege Separation**: Agent runs as root, per-user PHP runs as each user
- **PAM Authentication**: Uses Linux system authentication
- **Root Login Disabled**: Only non-root users (like `fastcp`) can access the panel
- **Encrypted Credentials**: Database passwords stored with AES-256-GCM encryption

## Troubleshooting

### Check service status

```bash
systemctl status fastcp
systemctl status fastcp-agent
```

### View logs

```bash
# FastCP logs
journalctl -u fastcp -f

# Agent logs
journalctl -u fastcp-agent -f

# Site access logs
tail -f /home/{user}/apps/{domain}/logs/access.log
```

### Restart services

```bash
sudo systemctl restart fastcp-agent
sudo systemctl restart fastcp
```

## Uninstall

```bash
curl -fsSL https://get.fastcp.org/uninstall.sh | sudo bash
```

## Contributing

Contributions are welcome! Please read our contributing guidelines before submitting PRs.

## License

MIT License - see [LICENSE](LICENSE) for details.

## Credits

- [Caddy](https://caddyserver.com/) - The ultimate web server
- Inspired by [ServerPilot](https://serverpilot.io/)
