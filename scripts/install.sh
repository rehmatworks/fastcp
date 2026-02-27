#!/bin/bash
#
# FastCP Installer
# https://github.com/rehmatworks/fastcp
#
# Copyright (c) 2024-present Rehmat Alam
# Licensed under the MIT License
#

set -e
export DEBIAN_FRONTEND=${DEBIAN_FRONTEND:-noninteractive}
# Suppress noisy needrestart apt hook output during unattended install.
# This keeps terminal output clean while preserving real apt/dpkg failures.
export NEEDRESTART_SUSPEND=1

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
███████╗ █████╗ ███████╗████████╗ ██████╗██████╗ 
██╔════╝██╔══██╗██╔════╝╚══██╔══╝██╔════╝██╔══██╗
█████╗  ███████║███████╗   ██║   ██║     ██████╔╝
██╔══╝  ██╔══██║╚════██║   ██║   ██║     ██╔═══╝ 
██║     ██║  ██║███████║   ██║   ╚██████╗██║     
╚═╝     ╚═╝  ╚═╝╚══════╝   ╚═╝    ╚═════╝╚═╝                                  
EOF
    echo -e "${NC}"
    echo -e "${DIM}Lightweight Server Control Panel${NC}"
    echo ""
    echo -e "${BLUE}Author:${NC}    Rehmat Alam"
    echo -e "${BLUE}Website:${NC}   https://fastcp.org"
    echo -e "${BLUE}License:${NC}   MIT"
    echo ""
    echo -e "${DIM}────────────────────────────────────────────${NC}"
    echo ""
}

log() { echo -e "${GREEN}[FastCP]${NC} $1"; }
warn() { echo -e "${YELLOW}[Warning]${NC} $1"; }
error() { echo -e "${RED}[Error]${NC} $1"; exit 1; }

has_systemd() {
    command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]
}

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

# Docker/container test compatibility: prevent package scripts from trying to start services
IN_CONTAINER=0
if [[ -f /.dockerenv ]]; then
    IN_CONTAINER=1
    cat > /usr/sbin/policy-rc.d << 'EOF'
#!/bin/sh
exit 101
EOF
    chmod +x /usr/sbin/policy-rc.d
fi

USE_SYSTEMD=0
FASTCP_READY=0
DISPLAY_HOST="127.0.0.1"
SERVER_IPV4=""
SERVER_IPV6=""
CERT_SAN_IP="127.0.0.1"

# Wait for any existing apt locks (e.g. unattended-upgrades on fresh VPS)
wait_for_apt() {
    local max_wait=120
    local waited=0
    while fuser /var/lib/apt/lists/lock /var/lib/dpkg/lock /var/lib/dpkg/lock-frontend /var/cache/apt/archives/lock >/dev/null 2>&1; do
        if [[ $waited -eq 0 ]]; then
            log "Waiting for apt lock (another package manager is running)..."
        fi
        sleep 5
        waited=$((waited + 5))
        if [[ $waited -ge $max_wait ]]; then
            error "Timed out waiting for apt lock after ${max_wait}s"
        fi
    done
}

# Install dependencies
log "Installing dependencies..."
wait_for_apt
apt-get update -qq
wait_for_apt
BASE_DEPS="curl wget acl libpam0g openssl gnupg2 ca-certificates lsb-release apt-transport-https ufw restic rsync"
if [[ $IN_CONTAINER -eq 0 ]]; then
    BASE_DEPS="$BASE_DEPS mysql-server"
else
    warn "container mode detected: skipping mysql-server package install during local test run"
fi
apt-get install -y -qq $BASE_DEPS > /dev/null

# Detect server IPs early (needed for SSL cert and display URLs)
HOST_IPS="$(hostname -I 2>/dev/null || true)"
SERVER_IPV4="$(echo "$HOST_IPS" | tr ' ' '\n' | awk '/^([0-9]{1,3}\.){3}[0-9]{1,3}$/ {print; exit}')"
SERVER_IPV6="$(echo "$HOST_IPS" | tr ' ' '\n' | awk 'index($0, ":") > 0 {gsub(/%.*/, "", $0); print; exit}')"
if [[ $IN_CONTAINER -eq 1 ]]; then
    DISPLAY_HOST="localhost"
    CERT_SAN_IP="127.0.0.1"
else
    if [[ -n "$SERVER_IPV4" ]]; then
        DISPLAY_HOST="$SERVER_IPV4"
        CERT_SAN_IP="$SERVER_IPV4"
    elif [[ -n "$SERVER_IPV6" ]]; then
        DISPLAY_HOST="[$SERVER_IPV6]"
        CERT_SAN_IP="$SERVER_IPV6"
    else
        DISPLAY_HOST="127.0.0.1"
        CERT_SAN_IP="127.0.0.1"
    fi
fi

# Ensure swap exists on low-memory servers
TOTAL_RAM_KB=$(awk '/MemTotal/ {print $2}' /proc/meminfo)
CURRENT_SWAP_KB=$(awk '/SwapTotal/ {print $2}' /proc/meminfo)
if [[ $TOTAL_RAM_KB -le 2097152 && $CURRENT_SWAP_KB -lt 524288 ]]; then
    log "Setting up swap (low memory detected)..."
    if [[ ! -f /swapfile ]]; then
        fallocate -l 1G /swapfile 2>/dev/null || dd if=/dev/zero of=/swapfile bs=1M count=1024 status=none
        chmod 600 /swapfile
        mkswap /swapfile > /dev/null
    fi
    swapon /swapfile 2>/dev/null || true
    grep -q '/swapfile' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
    log "1GB swap enabled"
fi

# Create directories
log "Creating directories..."
mkdir -p /opt/fastcp/{bin,data,config,ssl,run}
mkdir -p /opt/fastcp/config/users
mkdir -p /var/log/fastcp
chmod 755 /opt/fastcp/run
chmod 1777 /var/log/fastcp

# Clean up old tmpfs-based runtime dir
rm -f /etc/tmpfiles.d/fastcp.conf
rm -rf /var/run/fastcp

# Generate encryption key for database passwords (if not exists)
if [[ ! -f /opt/fastcp/data/.secret ]]; then
    openssl rand -base64 32 > /opt/fastcp/data/.secret
    chmod 600 /opt/fastcp/data/.secret
    log "Generated encryption key"
fi

# Install PHP versions and common modules
log "Installing and configuring PHP..."
if [[ "$ID" == "ubuntu" ]]; then
    wait_for_apt
    apt-get install -y -qq software-properties-common > /dev/null
    add-apt-repository -y ppa:ondrej/php > /dev/null 2>&1 || true
fi

wait_for_apt
apt-get update -qq

COMMON_PHP_MODULES="bcmath bz2 cli common curl gd gmp igbinary imagick imap intl mbstring mysql opcache readline redis soap sqlite3 xml xmlrpc zip"
php_package_exists() {
    apt-cache show "$1" >/dev/null 2>&1
}

declare -A PHP_PACKAGE_SET=()
add_php_package() {
    local pkg="$1"
    [[ -n "$pkg" ]] && PHP_PACKAGE_SET["$pkg"]=1
}

PHP_TARGET_VERSIONS="8.4"
AVAILABLE_PHP_VERSIONS=""
for v in $PHP_TARGET_VERSIONS; do
    if php_package_exists "php${v}-fpm"; then
        AVAILABLE_PHP_VERSIONS="${AVAILABLE_PHP_VERSIONS} ${v}"
    fi
done
AVAILABLE_PHP_VERSIONS=$(echo "$AVAILABLE_PHP_VERSIONS" | xargs)

if [[ -z "$AVAILABLE_PHP_VERSIONS" ]]; then
    error "Default PHP ${PHP_TARGET_VERSIONS} is not available in apt repositories"
fi

log "Detected PHP versions in apt repo: ${AVAILABLE_PHP_VERSIONS}"
for v in $AVAILABLE_PHP_VERSIONS; do
    log "Preparing package list for PHP ${v}..."
    php_package_exists "php${v}" && add_php_package "php${v}"
    php_package_exists "php${v}-fpm" && add_php_package "php${v}-fpm"

    for m in $COMMON_PHP_MODULES; do
        pkg="php${v}-${m}"
        if php_package_exists "$pkg"; then
            add_php_package "$pkg"
        fi
    done
done

mapfile -t PHP_PACKAGES < <(printf "%s\n" "${!PHP_PACKAGE_SET[@]}" | sort)
if [[ ${#PHP_PACKAGES[@]} -eq 0 ]]; then
    error "No PHP packages resolved for installation"
fi

log "Downloading PHP packages (${#PHP_PACKAGES[@]})..."
wait_for_apt
apt-get install -y -qq --download-only "${PHP_PACKAGES[@]}" > /dev/null

log "Installing and configuring PHP packages..."
wait_for_apt
apt-get install -y -qq "${PHP_PACKAGES[@]}" > /dev/null

# Download plain Caddy (lightweight root reverse proxy -- no PHP)
log "Downloading Caddy..."
CADDY_ARCH="amd64"
[[ "$ARCH" == "arm64" ]] && CADDY_ARCH="arm64"
mkdir -p /usr/local/bin
TMP_CADDY="$(mktemp /tmp/fastcp-caddy.XXXXXX)"
if ! curl -fsSL "https://caddyserver.com/api/download?os=linux&arch=${CADDY_ARCH}" -o "${TMP_CADDY}"; then
    rm -f "${TMP_CADDY}"
    error "Failed to download Caddy binary"
fi
if [[ ! -s "${TMP_CADDY}" ]]; then
    rm -f "${TMP_CADDY}"
    error "Downloaded Caddy binary is empty"
fi
install -m 0755 "${TMP_CADDY}" /usr/local/bin/caddy
rm -f "${TMP_CADDY}"

# Download FastCP binaries
log "Downloading FastCP..."
FASTCP_VERSION=${FASTCP_VERSION:-latest}

if [[ -f ./go.mod && -d ./cmd/fastcp && -d ./cmd/fastcp-agent && -x "$(command -v go)" ]]; then
    # Source-tree installation for local development/testing
    log "Building local binaries from source..."
    go build -o /opt/fastcp/bin/fastcp ./cmd/fastcp || error "Failed to build fastcp from source"
    go build -o /opt/fastcp/bin/fastcp-agent ./cmd/fastcp-agent || error "Failed to build fastcp-agent from source"
elif [[ -f ./fastcp && -f ./fastcp-agent ]]; then
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

# Seed default Caddy performance profile (low RAM, error-only logs).
cat > /opt/fastcp/config/caddy-settings.json << 'EOF'
{
  "profile": "low_ram",
  "access_logs": false,
  "expert_mode": false,
  "read_header": "8s",
  "read_body": "20s",
  "write_timeout": "45s",
  "idle_timeout": "45s",
  "grace_period": "5s",
  "max_header_size": 16384
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
        -addext "subjectAltName=DNS:$(hostname -f),DNS:localhost,IP:127.0.0.1,IP:${CERT_SAN_IP}" \
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
ExecStart=/opt/fastcp/bin/fastcp --listen :2050 --data-dir /opt/fastcp/data --agent-socket /opt/fastcp/run/agent.sock --tls-cert /opt/fastcp/ssl/server.crt --tls-key /opt/fastcp/ssl/server.key
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
ExecStart=/usr/local/bin/caddy run --config /opt/fastcp/config/Caddyfile
ExecReload=/usr/local/bin/caddy reload --config /opt/fastcp/config/Caddyfile
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# Enable and start services
if has_systemd; then
    USE_SYSTEMD=1
    systemctl daemon-reload
    systemctl enable fastcp-agent fastcp fastcp-caddy
    systemctl start fastcp-agent
    sleep 2
    systemctl start fastcp
    systemctl start fastcp-caddy
    for v in 8.2 8.3 8.4 8.5; do
        systemctl enable "php${v}-fpm" 2>/dev/null || true
        systemctl start "php${v}-fpm" 2>/dev/null || true
    done
else
    warn "systemd not detected; skipping service enable/start (container/test mode)"
fi

# Configure MySQL
log "Configuring MySQL..."

if [[ $IN_CONTAINER -eq 1 ]]; then
    warn "container mode detected: skipping MySQL tuning/service steps in local test run"
else

# Use conservative defaults by default.
INNODB_BUFFER=128
MAX_CONN=30
PERF_SCHEMA="OFF"

# Configure firewall defaults (keep UFW disabled unless admin enables it)
if command -v ufw >/dev/null 2>&1; then
    log "Preparing firewall rules..."
    ufw allow 2050/tcp >/dev/null 2>&1 || true
fi

cat > /etc/mysql/conf.d/fastcp.cnf << MYSQLEOF
[mysqld]
# FastCP tuning (default low-resource profile)
innodb_buffer_pool_size = ${INNODB_BUFFER}M
innodb_log_file_size = 16M
innodb_log_buffer_size = 8M
innodb_flush_log_at_trx_commit = 2
innodb_flush_method = O_DIRECT
key_buffer_size = 4M
max_connections = ${MAX_CONN}
table_open_cache = 200
thread_cache_size = 8
performance_schema = ${PERF_SCHEMA}
skip-name-resolve
MYSQLEOF

if has_systemd; then
    systemctl enable mysql
    systemctl restart mysql
else
    warn "systemd not detected; skipping MySQL service enable/restart (container/test mode)"
fi
fi

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
$cfg['TempDir'] = '/opt/fastcp/run/phpmyadmin-tmp';
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

$cfg['ShowCreateDb'] = false;
$cfg['LoginCookieValidity'] = 3600;
$cfg['LoginCookieStore'] = 0;
$cfg['LoginCookieDeleteAll'] = true;
PMAEOF

# Replace the placeholder with actual secret
sed -i "s/FASTCP_PMA_SECRET_PLACEHOLDER/${PMA_SECRET}/" /opt/fastcp/phpmyadmin/config.inc.php

# Remove signon.php if it exists from a previous install
rm -f /opt/fastcp/phpmyadmin/signon.php

# Suppress PHP 8.4 deprecation warnings via scanned php.ini override.
# .user.ini is not relied upon here.
rm -f /opt/fastcp/phpmyadmin/.user.ini
mkdir -p /opt/fastcp/run/phpmyadmin-tmp
chown -R www-data:www-data /opt/fastcp/run/phpmyadmin-tmp
mkdir -p /opt/fastcp/config/php
cat > /opt/fastcp/config/php/99-fastcp.ini << 'INIEOF'
display_errors = Off
error_reporting = 22527
INIEOF

log "phpMyAdmin installed (served via shared PHP-FPM pool)"

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

# Bootstrap all required directories for the admin user
FASTCP_HOME=$(eval echo ~fastcp)
mkdir -p "${FASTCP_HOME}/apps"
mkdir -p "${FASTCP_HOME}/.fastcp/run"
mkdir -p "${FASTCP_HOME}/.tmp/sessions"
mkdir -p "${FASTCP_HOME}/.tmp/uploads"
mkdir -p "${FASTCP_HOME}/.tmp/cache"
mkdir -p "${FASTCP_HOME}/.tmp/phpmyadmin"
mkdir -p "${FASTCP_HOME}/.tmp/wsdl"
mkdir -p /opt/fastcp/config/users/fastcp
chown -R fastcp:fastcp "${FASTCP_HOME}/apps" "${FASTCP_HOME}/.fastcp" "${FASTCP_HOME}/.tmp"
touch /var/log/fastcp/php-fastcp-error.log
chown fastcp:fastcp /var/log/fastcp/php-fastcp-error.log
log "Bootstrapped admin user directories"

start_fastcp_without_systemd() {
    log "Starting FastCP processes (no systemd mode)..."
    mkdir -p /var/log/fastcp
    pkill -f "/opt/fastcp/bin/fastcp-agent --socket /opt/fastcp/run/agent.sock" 2>/dev/null || true
    pkill -f "/opt/fastcp/bin/fastcp --listen :2050" 2>/dev/null || true
    rm -f /opt/fastcp/run/agent.sock
    nohup /opt/fastcp/bin/fastcp-agent --socket /opt/fastcp/run/agent.sock --log-level info >/var/log/fastcp/fastcp-agent.log 2>&1 &
    sleep 2
    nohup /opt/fastcp/bin/fastcp --listen :2050 --data-dir /opt/fastcp/data --agent-socket /opt/fastcp/run/agent.sock --tls-cert /opt/fastcp/ssl/server.crt --tls-key /opt/fastcp/ssl/server.key >/var/log/fastcp/fastcp.log 2>&1 &
}

if [[ $USE_SYSTEMD -eq 0 ]]; then
    start_fastcp_without_systemd
fi

# Wait for FastCP API to be ready
log "Waiting for FastCP API..."
for i in {1..30}; do
    if curl -sk https://127.0.0.1:2050/api/version >/dev/null 2>&1; then
        FASTCP_READY=1
        break
    fi
    sleep 1
done

if [[ $FASTCP_READY -eq 0 ]]; then
    if [[ $USE_SYSTEMD -eq 0 ]]; then
        error "FastCP did not become ready in no-systemd mode. Check logs: /var/log/fastcp/fastcp.log and /var/log/fastcp/fastcp-agent.log"
    fi
    warn "FastCP did not respond yet; service may still be starting"
fi

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
echo -e "  ${GREEN}https://${DISPLAY_HOST}:2050${NC}"
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
echo -e "  ${GREEN}https://${DISPLAY_HOST}:2050/phpmyadmin/${NC}"
echo -e "  ${DIM}Log in with your database credentials${NC}"
echo ""
echo -e "  ${CYAN}${BOLD}Services Status${NC}"
if [[ $FASTCP_READY -eq 1 ]]; then
    echo -e "  ${GREEN}●${NC} fastcp-agent    ${DIM}(running)${NC}"
    echo -e "  ${GREEN}●${NC} fastcp          ${DIM}(running)${NC}"
    if [[ $USE_SYSTEMD -eq 1 ]]; then
        echo -e "  ${GREEN}●${NC} fastcp-caddy    ${DIM}(running)${NC}"
    else
        echo -e "  ${YELLOW}●${NC} fastcp-caddy    ${DIM}(managed by fastcp-agent)${NC}"
    fi
else
    echo -e "  ${YELLOW}●${NC} fastcp-agent    ${DIM}(unknown)${NC}"
    echo -e "  ${YELLOW}●${NC} fastcp          ${DIM}(unknown)${NC}"
    echo -e "  ${YELLOW}●${NC} fastcp-caddy    ${DIM}(unknown)${NC}"
fi
echo ""
echo -e "  ${CYAN}${BOLD}Useful Commands${NC}"
if [[ $USE_SYSTEMD -eq 1 ]]; then
    echo -e "  ${DIM}Check status:${NC}   systemctl status fastcp"
    echo -e "  ${DIM}View logs:${NC}      journalctl -u fastcp -f"
    echo -e "  ${DIM}Restart:${NC}        systemctl restart fastcp fastcp-agent"
else
    echo -e "  ${DIM}Check status:${NC}   pgrep -af '/opt/fastcp/bin/fastcp|/opt/fastcp/bin/fastcp-agent'"
    echo -e "  ${DIM}View logs:${NC}      tail -f /var/log/fastcp/fastcp.log /var/log/fastcp/fastcp-agent.log"
    echo -e "  ${DIM}Restart:${NC}        /app/docker/dev-run.sh"
fi
echo ""
echo -e "${DIM}────────────────────────────────────────────────────────────────${NC}"
echo -e "${DIM}  FastCP - Copyright (c) 2024-present Rehmat Alam${NC}"
echo -e "${DIM}  https://github.com/rehmatworks/fastcp${NC}"
echo -e "${DIM}────────────────────────────────────────────────────────────────${NC}"
echo ""
