#!/bin/bash
#
# FastCP Installer
# Usage: curl -fsSL https://get.fastcp.io | bash
#

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${GREEN}[FastCP]${NC} $1"; }
warn() { echo -e "${YELLOW}[Warning]${NC} $1"; }
error() { echo -e "${RED}[Error]${NC} $1"; exit 1; }

# Check if running as root
[[ $EUID -ne 0 ]] && error "This script must be run as root"

# Check OS
if [[ ! -f /etc/os-release ]]; then
    error "Unsupported operating system"
fi
source /etc/os-release

if [[ "$ID" != "ubuntu" && "$ID" != "debian" ]]; then
    error "FastCP only supports Ubuntu and Debian"
fi

log "Installing FastCP on $PRETTY_NAME"

# Detect architecture
ARCH=$(uname -m)
case $ARCH in
    x86_64) ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *) error "Unsupported architecture: $ARCH" ;;
esac

log "Architecture: $ARCH"

# Install dependencies
log "Installing dependencies..."
apt-get update -qq
apt-get install -y -qq curl wget acl mysql-server libpam0g openssl > /dev/null

# Get server IP early (needed for SSL cert)
SERVER_IP=$(curl -s --connect-timeout 5 ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}')

# Create directories
log "Creating directories..."
mkdir -p /opt/fastcp/{bin,data,config,ssl}
mkdir -p /opt/fastcp/config/users
mkdir -p /var/run/fastcp
mkdir -p /var/log/fastcp
chmod 755 /var/run/fastcp

# Download FrankenPHP
log "Downloading FrankenPHP..."
FRANKENPHP_URL="https://github.com/dunglas/frankenphp/releases/latest/download/frankenphp-linux-${ARCH}"
if [[ "$ARCH" == "amd64" ]]; then
    FRANKENPHP_URL="https://github.com/dunglas/frankenphp/releases/latest/download/frankenphp-linux-x86_64"
elif [[ "$ARCH" == "arm64" ]]; then
    FRANKENPHP_URL="https://github.com/dunglas/frankenphp/releases/latest/download/frankenphp-linux-aarch64"
fi

curl -fsSL "$FRANKENPHP_URL" -o /usr/local/bin/frankenphp
chmod +x /usr/local/bin/frankenphp

# Download FastCP binaries
log "Downloading FastCP..."
FASTCP_VERSION=${FASTCP_VERSION:-latest}

if [[ -f ./fastcp && -f ./fastcp-agent ]]; then
    # Local installation (if binaries exist in current dir)
    log "Using local binaries..."
    cp ./fastcp /opt/fastcp/bin/
    cp ./fastcp-agent /opt/fastcp/bin/
else
    # Download from GitHub releases
    if [[ "$FASTCP_VERSION" == "latest" ]]; then
        RELEASE_URL="https://github.com/rehmatworks/fastcp/releases/latest/download"
    else
        RELEASE_URL="https://github.com/rehmatworks/fastcp/releases/download/${FASTCP_VERSION}"
    fi
    
    curl -fsSL "${RELEASE_URL}/fastcp-linux-${ARCH}" -o /opt/fastcp/bin/fastcp || error "Failed to download fastcp"
    curl -fsSL "${RELEASE_URL}/fastcp-agent-linux-${ARCH}" -o /opt/fastcp/bin/fastcp-agent || error "Failed to download fastcp-agent"
fi

chmod +x /opt/fastcp/bin/fastcp
chmod +x /opt/fastcp/bin/fastcp-agent

# Create initial Caddyfile
log "Creating configuration..."
cat > /opt/fastcp/config/Caddyfile << 'EOF'
{
    admin localhost:2019
}

:80 {
    respond "FastCP - No sites configured" 404
}
EOF

# Generate self-signed SSL certificate for control panel
log "Generating SSL certificate..."
mkdir -p /opt/fastcp/ssl
if [[ ! -f /opt/fastcp/ssl/server.crt ]]; then
    openssl req -x509 -nodes -days 3650 -newkey rsa:2048 \
        -keyout /opt/fastcp/ssl/server.key \
        -out /opt/fastcp/ssl/server.crt \
        -subj "/C=US/ST=State/L=City/O=FastCP/CN=$(hostname -f)" \
        -addext "subjectAltName=DNS:$(hostname -f),DNS:localhost,IP:127.0.0.1,IP:${SERVER_IP:-127.0.0.1}" \
        2>/dev/null
    chmod 600 /opt/fastcp/ssl/server.key
    chmod 644 /opt/fastcp/ssl/server.crt
    log "SSL certificate generated (valid for 10 years)"
fi

# Install systemd services
log "Installing systemd services..."

cat > /etc/systemd/system/fastcp-agent.service << 'EOF'
[Unit]
Description=FastCP Agent
After=network.target mysql.service

[Service]
Type=simple
ExecStart=/opt/fastcp/bin/fastcp-agent
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

cat > /etc/systemd/system/fastcp.service << 'EOF'
[Unit]
Description=FastCP Control Panel
After=network.target fastcp-agent.service
Requires=fastcp-agent.service

[Service]
Type=simple
ExecStart=/opt/fastcp/bin/fastcp --listen :2087 --data-dir /opt/fastcp/data --agent-socket /var/run/fastcp/agent.sock --tls-cert /opt/fastcp/ssl/server.crt --tls-key /opt/fastcp/ssl/server.key
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

cat > /etc/systemd/system/fastcp-caddy.service << 'EOF'
[Unit]
Description=FastCP Caddy (Main Reverse Proxy)
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/frankenphp run --config /opt/fastcp/config/Caddyfile
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# Per-user PHP service template
cat > /etc/systemd/system/fastcp-php@.service << 'EOF'
[Unit]
Description=FastCP PHP for %i
After=network.target

[Service]
Type=simple
User=%i
Group=%i
Environment=PHP_INI_SCAN_DIR=/opt/fastcp/config/users/%i
ExecStart=/usr/local/bin/frankenphp run --config /opt/fastcp/config/users/%i/Caddyfile
Restart=always
RestartSec=5

# Resource limits (can be overridden per user)
MemoryMax=512M
CPUQuota=100%

# Security
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=/home/%i /var/run/fastcp /var/log/fastcp

[Install]
WantedBy=multi-user.target
EOF

# Enable and start services
systemctl daemon-reload
systemctl enable fastcp-agent fastcp fastcp-caddy
systemctl start fastcp-agent
sleep 2
systemctl start fastcp
systemctl start fastcp-caddy

# Configure MySQL
log "Configuring MySQL..."
systemctl enable mysql
systemctl start mysql


# Done!
echo ""
echo -e "${GREEN}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║              FastCP Installation Complete!                 ║${NC}"
echo -e "${GREEN}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "  Access your control panel at:"
echo -e "  ${GREEN}https://${SERVER_IP}:2087${NC}"
echo ""
echo -e "  Login with your system root credentials."
echo ""
echo -e "  Services:"
echo -e "    fastcp-agent    - ${GREEN}running${NC}"
echo -e "    fastcp          - ${GREEN}running${NC}"
echo -e "    fastcp-caddy    - ${GREEN}running${NC}"
echo ""
echo -e "  Useful commands:"
echo -e "    systemctl status fastcp"
echo -e "    journalctl -u fastcp -f"
echo ""
