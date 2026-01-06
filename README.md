# FastCP - Modern PHP Hosting Control Panel

> **⚠️ WORK IN PROGRESS**
> 
> FastCP is currently under active development. Features may be incomplete or change without notice. Not recommended for production use yet.

> **🔴 SECURITY NOTICE: SHARED PHP ENVIRONMENT**
> 
> Currently, all websites share the same PHP process (FrankenPHP runs as a single user). This means:
> - A PHP script from one site **can access files from other sites**
> - **Do NOT use** for untrusted multi-tenant hosting
> - **Safe for:** Single user, trusted teams, agencies managing their own sites
> 
> Per-user PHP isolation is planned for a future release.

<p align="center">
  <img src="https://via.placeholder.com/200x200/10b981/ffffff?text=F" alt="FastCP Logo" width="120">
</p>

<p align="center">
  <strong>A modern, minimalist control panel for deploying PHP websites using FrankenPHP</strong>
</p>

<p align="center">
  <a href="#features">Features</a> •
  <a href="#installation">Installation</a> •
  <a href="#usage">Usage</a> •
  <a href="#api">API</a> •
  <a href="#whmcs">WHMCS</a>
</p>

---

## Features

- 🚀 **Multiple PHP Versions** - Run PHP 8.2, 8.3, 8.4 simultaneously
- ⚡ **Worker Mode** - Keep applications in memory for maximum performance
- 🔒 **Auto HTTPS** - Automatic SSL certificates via Let's Encrypt
- 🌐 **Modern UI** - Beautiful, responsive admin interface
- 🔌 **WHMCS Ready** - Built-in API for billing integration
- 📊 **Real-time Stats** - Monitor PHP instances and sites
- 🔧 **Easy Deployment** - WordPress, Laravel, Symfony ready

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    FastCP Control Panel                      │
│                   (Go + React Frontend)                      │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│   ┌──────────────────────────────────────────────────────┐  │
│   │                  Caddy Reverse Proxy                  │  │
│   │                   :80 / :443                          │  │
│   └──────────────────────────────────────────────────────┘  │
│                              │                               │
│     ┌────────────────────────┼────────────────────────┐     │
│     ▼                        ▼                        ▼     │
│  ┌────────────┐       ┌────────────┐       ┌────────────┐  │
│  │FrankenPHP  │       │FrankenPHP  │       │FrankenPHP  │  │
│  │PHP 8.2     │       │PHP 8.3     │       │PHP 8.4     │  │
│  │:9082       │       │:9083       │       │:9084       │  │
│  └────────────┘       └────────────┘       └────────────┘  │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

## Installation

### One-Line Install (Recommended)

```bash
curl -fsSL https://fastcp.org/install.sh | bash
```

This will:
1. Download the latest FastCP binary for your platform
2. Create necessary directories
3. Set up systemd service (Linux)

### Manual Download

```bash
# Linux x86_64 (Ubuntu, Debian, CentOS, etc.)
curl -L https://github.com/rehmatworks/fastcp/releases/latest/download/fastcp-linux-x86_64 -o /usr/local/bin/fastcp
chmod +x /usr/local/bin/fastcp

# Linux ARM64 (AWS Graviton, Oracle Ampere, etc.)
curl -L https://github.com/rehmatworks/fastcp/releases/latest/download/fastcp-linux-aarch64 -o /usr/local/bin/fastcp
chmod +x /usr/local/bin/fastcp
```

### Run FastCP

```bash
# Run directly
fastcp

# Or with systemd (Linux)
sudo systemctl start fastcp
sudo systemctl enable fastcp
```

**Note:** FrankenPHP will be auto-downloaded on first run (~64MB).

### Development Setup

```bash
# Requirements: Go 1.23+, Node.js 20+

# Clone the repository
git clone https://github.com/rehmatworks/fastcp.git
cd fastcp

# Install dependencies
make install-deps

# Run in development mode
make dev
```

Development mode (`FASTCP_DEV=1`) uses local directories:
- Config: `./.fastcp/config.json`
- Data: `./.fastcp/data/`
- Sites: `./.fastcp/sites/`
- Logs: `./.fastcp/logs/`
- Binary: `./.fastcp/bin/frankenphp`
- Ports: `8000` (HTTP), `8443` (HTTPS)

### Environment Variables

| Variable | Description | Default (Dev) | Default (Prod) |
|----------|-------------|---------------|----------------|
| `FASTCP_DEV` | Enable dev mode | - | - |
| `FASTCP_DATA_DIR` | Data directory | `./.fastcp/data` | `/var/lib/fastcp` |
| `FASTCP_SITES_DIR` | Sites directory | `./.fastcp/sites` | `/var/www` |
| `FASTCP_LOG_DIR` | Log directory | `./.fastcp/logs` | `/var/log/fastcp` |
| `FASTCP_CONFIG_DIR` | Config directory | `./.fastcp` | `/etc/fastcp` |
| `FASTCP_BINARY` | FrankenPHP path | `./.fastcp/bin/frankenphp` | `/usr/local/bin/frankenphp` |
| `FASTCP_PORT` | Proxy HTTP port | `8000` | `80` |
| `FASTCP_SSL_PORT` | Proxy HTTPS port | `8443` | `443` |
| `FASTCP_LISTEN` | Admin panel address | `:8080` | `:8080` |

## Usage

1. Open `https://localhost:8080` in your browser
2. Login with default credentials:
   - Username: `admin`
   - Password: `fastcp2024!`
3. Create your first site!

## Configuration

Configuration file: `/etc/fastcp/config.json`

```json
{
  "admin_user": "admin",
  "admin_password": "fastcp2024!",
  "jwt_secret": "change-this-in-production",
  "data_dir": "/var/lib/fastcp",
  "sites_dir": "/var/www",
  "log_dir": "/var/log/fastcp",
  "listen_addr": ":8080",
  "php_versions": [
    {
      "version": "8.4",
      "port": 9084,
      "admin_port": 2084,
      "binary_path": "/usr/local/bin/frankenphp-8.4",
      "enabled": true
    }
  ]
}
```

## API

### Authentication

```bash
# Login
curl -X POST https://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "fastcp2024!"}'

# Use token in subsequent requests
curl https://localhost:8080/api/v1/sites \
  -H "Authorization: Bearer YOUR_TOKEN"
```

### Sites

```bash
# List sites
GET /api/v1/sites

# Create site
POST /api/v1/sites
{
  "name": "My Site",
  "domain": "example.com",
  "php_version": "8.4",
  "worker_mode": true
}

# Get site
GET /api/v1/sites/{id}

# Update site
PUT /api/v1/sites/{id}

# Delete site
DELETE /api/v1/sites/{id}
```

### PHP Instances

```bash
# List instances
GET /api/v1/php

# Start instance
POST /api/v1/php/{version}/start

# Stop instance
POST /api/v1/php/{version}/stop

# Restart instance
POST /api/v1/php/{version}/restart

# Restart workers
POST /api/v1/php/{version}/restart-workers
```

## WHMCS Integration

FastCP includes built-in WHMCS integration for automated provisioning.

### Endpoints

```bash
# Provision (create/suspend/unsuspend/terminate)
POST /api/v1/whmcs/provision
X-API-Key: your-api-key

{
  "action": "create",
  "service_id": "12345",
  "username": "customer",
  "domain": "customer-site.com",
  "php_version": "8.4"
}

# Check status
GET /api/v1/whmcs/status/{service_id}?domain=example.com
X-API-Key: your-api-key
```

### Actions

- `create` - Create new site
- `suspend` - Suspend site
- `unsuspend` - Reactivate site
- `terminate` - Delete site

## Directory Structure

```
fastcp/
├── cmd/fastcp/          # Main entry point
├── internal/
│   ├── api/             # REST API handlers
│   ├── auth/            # Authentication
│   ├── caddy/           # Caddyfile generation
│   ├── config/          # Configuration
│   ├── middleware/      # HTTP middleware
│   ├── models/          # Data models
│   ├── php/             # PHP instance management
│   └── sites/           # Site management
├── web/                 # React frontend
│   ├── src/
│   │   ├── components/
│   │   ├── pages/
│   │   ├── hooks/
│   │   └── lib/
│   └── package.json
├── data/                # Runtime data
├── templates/           # Caddyfile templates
├── go.mod
└── Makefile
```

## Roadmap

- [ ] Unix user authentication
- [ ] File manager
- [ ] SSL certificate management
- [ ] Backup & restore
- [ ] Email integration
- [ ] Database management (MySQL/PostgreSQL)
- [ ] DNS management
- [ ] Let's Encrypt wildcard support

## License

MIT License - see [LICENSE](LICENSE) for details.

## Credits

- [FrankenPHP](https://frankenphp.dev) - The amazing PHP application server
- [Caddy](https://caddyserver.com) - The web server that powers FrankenPHP
- [Go-Chi](https://github.com/go-chi/chi) - Lightweight router for Go

