#!/bin/bash
#
# FastCP Installer
# https://github.com/rehmatworks/fastcp
#
# Copyright (c) 2024-present Rehmat Alam
# Licensed under the MIT License
#

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

# Print banner
print_banner() {
    echo ""
    echo -e "${CYAN}${BOLD}"
    cat << 'EOF'
    ______           __  __________ 
   / ____/___ ______/ /_/ ____/ __ \
  / /_  / __ `/ ___/ __/ /   / /_/ /
 / __/ / /_/ (__  ) /_/ /___/ ____/ 
/_/    \__,_/____/\__/\____/_/      
                                    
EOF
    echo -e "${NC}"
    echo -e "${DIM}Lightweight Server Control Panel${NC}"
    echo ""
    echo -e "${BLUE}Author:${NC}    Rehmat Alam"
    echo -e "${BLUE}Website:${NC}   https://github.com/rehmatworks/fastcp"
    echo -e "${BLUE}License:${NC}   MIT"
    echo ""
    echo -e "${DIM}────────────────────────────────────────────${NC}"
    echo ""
}

log() { echo -e "${GREEN}[FastCP]${NC} $1"; }
warn() { echo -e "${YELLOW}[Warning]${NC} $1"; }
error() { echo -e "${RED}[Error]${NC} $1"; exit 1; }

# Show banner
print_banner

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
chmod 1777 /var/run/fastcp
chmod 1777 /var/log/fastcp

# Create tmpfiles.d config to ensure /var/run/fastcp persists after reboot
cat > /etc/tmpfiles.d/fastcp.conf << 'EOF'
d /var/run/fastcp 1777 root root -
EOF

# Generate encryption key for database passwords (if not exists)
if [[ ! -f /opt/fastcp/data/.secret ]]; then
    openssl rand -base64 32 > /opt/fastcp/data/.secret
    chmod 600 /opt/fastcp/data/.secret
    log "Generated encryption key"
fi

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
RuntimeDirectory=fastcp
RuntimeDirectoryMode=1777
RuntimeDirectoryPreserve=yes

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
Environment=PHP_INI_SCAN_DIR=:/opt/fastcp/config/php

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
Environment=HOME=/home/%i
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
ReadWritePaths=/home/%i /var/run/fastcp /var/log/fastcp /tmp

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

# Install phpMyAdmin
log "Installing phpMyAdmin..."
PHPMYADMIN_VERSION="5.2.2"
mkdir -p /opt/fastcp/phpmyadmin
curl -fsSL "https://files.phpmyadmin.net/phpMyAdmin/${PHPMYADMIN_VERSION}/phpMyAdmin-${PHPMYADMIN_VERSION}-all-languages.tar.gz" | tar xz --strip-components=1 -C /opt/fastcp/phpmyadmin

# Generate phpMyAdmin blowfish secret
PMA_SECRET=$(openssl rand -base64 32 | tr -dc 'a-zA-Z0-9' | head -c 32)

# Create phpMyAdmin config - auth handled by Go proxy via HTTP Basic Auth
cat > /opt/fastcp/phpmyadmin/config.inc.php << 'PMAEOF'
<?php
error_reporting(E_ALL & ~E_DEPRECATED & ~E_STRICT);

$cfg['blowfish_secret'] = 'FASTCP_PMA_SECRET_PLACEHOLDER';
$cfg['TempDir'] = '/tmp/phpmyadmin';
$cfg['UploadDir'] = '';
$cfg['SaveDir'] = '';

$i = 0;
$i++;
$cfg['Servers'][$i]['host'] = '127.0.0.1';
$cfg['Servers'][$i]['auth_type'] = 'config';
$cfg['Servers'][$i]['user'] = $_SERVER['PHP_AUTH_USER'] ?? '';
$cfg['Servers'][$i]['password'] = $_SERVER['PHP_AUTH_PW'] ?? '';
$cfg['Servers'][$i]['AllowNoPassword'] = false;
$cfg['Servers'][$i]['hide_db'] = '^(information_schema|performance_schema|mysql|sys)$';

$cfg['LoginCookieValidity'] = 3600;
$cfg['LoginCookieStore'] = 0;
$cfg['LoginCookieDeleteAll'] = true;
PMAEOF

# Replace the placeholder with actual secret
sed -i "s/FASTCP_PMA_SECRET_PLACEHOLDER/${PMA_SECRET}/" /opt/fastcp/phpmyadmin/config.inc.php

# Remove signon.php if it exists from a previous install
rm -f /opt/fastcp/phpmyadmin/signon.php

# Suppress PHP 8.4 deprecation warnings via FrankenPHP's PHP_INI_SCAN_DIR.
# .user.ini doesn't work with FrankenPHP; we use a proper php.ini instead.
rm -f /opt/fastcp/phpmyadmin/.user.ini
mkdir -p /opt/fastcp/config/php
cat > /opt/fastcp/config/php/99-fastcp.ini << 'INIEOF'
display_errors = Off
error_reporting = 22527
INIEOF

# Create phpMyAdmin temp directory
mkdir -p /tmp/phpmyadmin
chmod 777 /tmp/phpmyadmin
log "phpMyAdmin installed (served via main Caddy process)"

# Create fastcp admin user
log "Creating fastcp admin user..."
FASTCP_PASSWORD=$(openssl rand -base64 12 | tr -dc 'a-zA-Z0-9' | head -c 16)

# Create system user
if ! id -u fastcp >/dev/null 2>&1; then
    useradd -m -s /bin/bash fastcp
    echo "fastcp:${FASTCP_PASSWORD}" | chpasswd
    log "System user 'fastcp' created"
else
    # Update password if user exists
    echo "fastcp:${FASTCP_PASSWORD}" | chpasswd
    log "Updated password for existing 'fastcp' user"
fi

# Wait for FastCP API to be ready
log "Waiting for FastCP API..."
for i in {1..30}; do
    if curl -sk https://127.0.0.1:2087/api/version >/dev/null 2>&1; then
        break
    fi
    sleep 1
done

# Register fastcp user in FastCP database as admin
# The user will authenticate via PAM, so we just need to mark them as admin
# This is done by logging in once which creates the DB record, then updating it
# For now, we'll create a simple marker file that the agent can check
mkdir -p /opt/fastcp/data
echo "fastcp" > /opt/fastcp/data/default_admin

# Done!
echo ""
echo -e "${GREEN}${BOLD}"
cat << 'EOF'
  ╔═══════════════════════════════════════════════════════════════╗
  ║                                                               ║
  ║            FastCP Installation Complete!                      ║
  ║                                                               ║
  ╚═══════════════════════════════════════════════════════════════╝
EOF
echo -e "${NC}"
echo ""
echo -e "  ${CYAN}${BOLD}Control Panel URL${NC}"
echo -e "  ${GREEN}https://${SERVER_IP}:2087${NC}"
echo ""
echo -e "  ${CYAN}${BOLD}Login Credentials${NC}"
echo -e "  ${YELLOW}╔════════════════════════════════════════════╗${NC}"
printf "  ${YELLOW}║${NC}  Username: ${BOLD}%-32s${NC}${YELLOW}║${NC}\n" "fastcp"
printf "  ${YELLOW}║${NC}  Password: ${BOLD}%-32s${NC}${YELLOW}║${NC}\n" "${FASTCP_PASSWORD}"
echo -e "  ${YELLOW}╚════════════════════════════════════════════╝${NC}"
echo ""
echo -e "  ${DIM}The 'fastcp' user is the recommended admin account for${NC}"
echo -e "  ${DIM}creating and managing websites. Save these credentials!${NC}"
echo ""
echo -e "  ${DIM}To reset the password, run: ${NC}passwd fastcp"
echo ""
echo -e "  ${CYAN}${BOLD}phpMyAdmin${NC}"
echo -e "  ${GREEN}https://${SERVER_IP}:2087/phpmyadmin/${NC}"
echo -e "  ${DIM}Log in with your database credentials${NC}"
echo ""
echo -e "  ${CYAN}${BOLD}Services Status${NC}"
echo -e "  ${GREEN}●${NC} fastcp-agent    ${DIM}(running)${NC}"
echo -e "  ${GREEN}●${NC} fastcp          ${DIM}(running)${NC}"
echo -e "  ${GREEN}●${NC} fastcp-caddy    ${DIM}(running)${NC}"
echo ""
echo -e "  ${CYAN}${BOLD}Useful Commands${NC}"
echo -e "  ${DIM}Check status:${NC}   systemctl status fastcp"
echo -e "  ${DIM}View logs:${NC}      journalctl -u fastcp -f"
echo -e "  ${DIM}Restart:${NC}        systemctl restart fastcp fastcp-agent"
echo ""
echo -e "${DIM}────────────────────────────────────────────────────────────────${NC}"
echo -e "${DIM}  FastCP - Copyright (c) 2024-present Rehmat Alam${NC}"
echo -e "${DIM}  https://github.com/rehmatworks/fastcp${NC}"
echo -e "${DIM}────────────────────────────────────────────────────────────────${NC}"
echo ""
