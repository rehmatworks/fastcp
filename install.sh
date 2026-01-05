#!/bin/bash
#
# FastCP Installation Script
# Usage: curl -fsSL https://fastcp.org/install.sh | bash
#
# This script will:
# - Download and install FastCP binary
# - Install required dependencies
# - Create fastcp system user for PHP isolation
# - Set up directories with proper permissions
# - Create systemd service
# - Configure initial settings
#

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Configuration
GITHUB_REPO="rehmatworks/fastcp"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/fastcp"
DATA_DIR="/var/lib/fastcp"
LOG_DIR="/var/log/fastcp"
RUN_DIR="/var/run/fastcp"
SITES_DIR="/var/www"

# Default values
ADMIN_EMAIL=""
API_PORT="8080"

# Check if running as root
check_root() {
    if [ "$EUID" -ne 0 ]; then
        echo -e "${RED}Error: This script must be run as root${NC}"
        echo "Please run: sudo bash install.sh"
        exit 1
    fi
}

# Check if a port is in use
is_port_in_use() {
    local port=$1
    if command -v ss &> /dev/null; then
        ss -tuln | grep -q ":${port} "
    elif command -v netstat &> /dev/null; then
        netstat -tuln | grep -q ":${port} "
    elif command -v lsof &> /dev/null; then
        lsof -i :${port} &> /dev/null
    else
        # If no tool available, assume port is free
        return 1
    fi
}

# Get process using a port
get_port_process() {
    local port=$1
    if command -v ss &> /dev/null; then
        ss -tulnp | grep ":${port} " | awk '{print $7}' | head -1 | sed 's/.*"\(.*\)".*/\1/'
    elif command -v lsof &> /dev/null; then
        lsof -i :${port} -t 2>/dev/null | head -1 | xargs -I{} ps -p {} -o comm= 2>/dev/null
    else
        echo "unknown"
    fi
}

# Check required ports are available
check_ports() {
    echo ""
    echo -e "${YELLOW}Checking required ports...${NC}"
    
    local ports_in_use=""
    local has_conflict=false
    
    # Check port 80 (HTTP)
    if is_port_in_use 80; then
        local proc=$(get_port_process 80)
        ports_in_use="${ports_in_use}\n  - Port 80 (HTTP) is in use${proc:+ by: $proc}"
        has_conflict=true
    fi
    
    # Check port 443 (HTTPS)
    if is_port_in_use 443; then
        local proc=$(get_port_process 443)
        ports_in_use="${ports_in_use}\n  - Port 443 (HTTPS) is in use${proc:+ by: $proc}"
        has_conflict=true
    fi
    
    # Check port 8080 (Admin Panel) - use configured port
    if is_port_in_use ${API_PORT}; then
        local proc=$(get_port_process ${API_PORT})
        ports_in_use="${ports_in_use}\n  - Port ${API_PORT} (Admin Panel) is in use${proc:+ by: $proc}"
        has_conflict=true
    fi
    
    if [ "$has_conflict" = true ]; then
        echo -e "${RED}Error: Required ports are already in use${NC}"
        echo -e "${ports_in_use}"
        echo ""
        echo -e "${YELLOW}FastCP requires the following ports to be available:${NC}"
        echo "  - Port 80   : HTTP traffic for websites"
        echo "  - Port 443  : HTTPS traffic for websites"
        echo "  - Port ${API_PORT} : FastCP admin panel"
        echo ""
        echo "Please stop the conflicting services before installing FastCP."
        echo ""
        echo "Common commands to stop web servers:"
        echo "  sudo systemctl stop nginx"
        echo "  sudo systemctl stop apache2"
        echo "  sudo systemctl stop httpd"
        echo ""
        exit 1
    fi
    
    echo -e "${GREEN}✓ All required ports are available (80, 443, ${API_PORT})${NC}"
}

# Detect OS
detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS_NAME=$ID
        OS_VERSION=$VERSION_ID
    else
        echo -e "${RED}Error: Cannot detect OS. /etc/os-release not found${NC}"
        exit 1
    fi

    # FastCP only supports Ubuntu 22.04 and newer
    if [ "$OS_NAME" != "ubuntu" ]; then
        echo -e "${RED}Error: FastCP only supports Ubuntu${NC}"
        echo -e "${YELLOW}Detected OS: ${OS_NAME} ${OS_VERSION}${NC}"
        echo ""
        echo "Supported versions:"
        echo "  - Ubuntu 22.04 LTS (Jammy Jellyfish)"
        echo "  - Ubuntu 24.04 LTS (Noble Numbat)"
        echo "  - Ubuntu 24.10 and newer"
        exit 1
    fi

    # Check Ubuntu version (must be 22.04 or higher)
    # Extract major version number
    MAJOR_VERSION=$(echo "$OS_VERSION" | cut -d. -f1)
    
    if [ "$MAJOR_VERSION" -lt 22 ]; then
        echo -e "${RED}Error: FastCP requires Ubuntu 22.04 or newer${NC}"
        echo -e "${YELLOW}Detected version: Ubuntu ${OS_VERSION}${NC}"
        echo ""
        echo "Supported versions:"
        echo "  - Ubuntu 22.04 LTS (Jammy Jellyfish)"
        echo "  - Ubuntu 24.04 LTS (Noble Numbat)"
        echo "  - Ubuntu 24.10 and newer"
        echo ""
        echo "Please upgrade your system to Ubuntu 22.04 or newer."
        exit 1
    fi

    PKG_MANAGER="apt-get"
    PKG_UPDATE="apt-get update -qq"
    PKG_INSTALL="apt-get install -y -qq"

    echo -e "${GREEN}✓ Detected OS: Ubuntu ${OS_VERSION}${NC}"
}

# Detect platform/architecture
detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    if [ "$OS" != "linux" ]; then
        echo -e "${RED}Error: FastCP only supports Linux. Detected: $OS${NC}"
        echo -e "${YELLOW}For local development on macOS, use:${NC}"
        echo "  FASTCP_DEV=1 go run ./cmd/fastcp"
        exit 1
    fi

    case "$ARCH" in
        x86_64|amd64)
            PLATFORM="linux-x86_64"
            ;;
        aarch64|arm64)
            PLATFORM="linux-aarch64"
            ;;
        *)
            echo -e "${RED}Error: Unsupported architecture: $ARCH${NC}"
            echo -e "${YELLOW}Supported architectures: x86_64 (amd64), aarch64 (arm64)${NC}"
            exit 1
            ;;
    esac

    echo -e "${BLUE}Detected architecture: ${PLATFORM}${NC}"
}

# Prompt for configuration
# Note: We read from /dev/tty to support curl | bash usage
prompt_configuration() {
    echo ""
    echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${CYAN}                      Configuration                            ${NC}"
    echo -e "${CYAN}═══════════════════════════════════════════════════════════════${NC}"
    echo ""
    
    # Admin email for SSL certificates
    echo -e "${BLUE}SSL Certificate Email${NC}"
    echo "This email will be used for Let's Encrypt SSL certificate notifications."
    echo ""
    printf "Enter admin email [support@fastcp.org]: "
    read input_email < /dev/tty
    ADMIN_EMAIL="${input_email:-support@fastcp.org}"
    
    echo ""
    
    # API port
    echo -e "${BLUE}Admin Panel Port${NC}"
    echo "Port for the FastCP admin panel (default: 8080)"
    echo ""
    printf "Enter API port [8080]: "
    read input_port < /dev/tty
    API_PORT="${input_port:-8080}"
    
    echo ""
    echo -e "${GREEN}Configuration Summary:${NC}"
    echo "  SSL Email:   $ADMIN_EMAIL"
    echo "  Admin Port:  $API_PORT"
    echo ""
    
    printf "Continue with these settings? [Y/n]: "
    read confirm < /dev/tty
    if [[ "$confirm" =~ ^[Nn] ]]; then
        echo -e "${YELLOW}Installation cancelled.${NC}"
        exit 0
    fi
}

# Install dependencies
install_dependencies() {
    echo ""
    echo -e "${YELLOW}Installing dependencies...${NC}"
    
    $PKG_UPDATE
    
    # Core dependencies
    DEPS="curl acl"
    
    $PKG_INSTALL $DEPS
    
    echo -e "${GREEN}Dependencies installed${NC}"
}

# Get latest version from GitHub
get_latest_version() {
    echo ""
    echo -e "${YELLOW}Fetching latest version...${NC}"
    
    VERSION=$(curl -s "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    
    if [ -z "$VERSION" ]; then
        echo -e "${RED}Error: Failed to get latest version from GitHub${NC}"
        echo "Please check your internet connection and try again."
        exit 1
    fi
    
    echo -e "${GREEN}Latest version: ${VERSION}${NC}"
}

# Download and install FastCP binary
install_fastcp() {
    echo ""
    echo -e "${YELLOW}Downloading FastCP ${VERSION}...${NC}"
    
    DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/fastcp-${PLATFORM}"
    
    # Download to temp file
    TMP_FILE=$(mktemp)
    
    if ! curl -fsSL "$DOWNLOAD_URL" -o "$TMP_FILE"; then
        echo -e "${RED}Error: Failed to download FastCP${NC}"
        echo "URL: $DOWNLOAD_URL"
        rm -f "$TMP_FILE"
        exit 1
    fi
    
    # Make executable and move to install dir
    chmod +x "$TMP_FILE"
    mv "$TMP_FILE" "${INSTALL_DIR}/fastcp"
    
    echo -e "${GREEN}FastCP installed to ${INSTALL_DIR}/fastcp${NC}"
}

# Create fastcp system user for PHP isolation
create_fastcp_user() {
    echo ""
    echo -e "${YELLOW}Creating fastcp system user...${NC}"
    
    # Check if user already exists
    if id "fastcp" &>/dev/null; then
        echo -e "${BLUE}User 'fastcp' already exists${NC}"
    else
        # Check if group exists
        if getent group fastcp &>/dev/null; then
            # Group exists, create user with existing group
            useradd --system --no-create-home --shell /usr/sbin/nologin -g fastcp fastcp
        else
            # Create user with new group
            useradd --system --no-create-home --shell /usr/sbin/nologin --user-group fastcp
        fi
        echo -e "${GREEN}Created system user 'fastcp'${NC}"
    fi
    
    # Add fastcp to www-data group if it exists
    if getent group www-data &>/dev/null; then
        usermod -aG www-data fastcp 2>/dev/null || true
    fi
}

# Create directories
create_directories() {
    echo ""
    echo -e "${YELLOW}Creating directories...${NC}"
    
    # Create main directories
    mkdir -p "$CONFIG_DIR"
    mkdir -p "$DATA_DIR"
    mkdir -p "$DATA_DIR/caddy"
    mkdir -p "$DATA_DIR/sites"
    mkdir -p "$LOG_DIR"
    mkdir -p "$RUN_DIR"
    mkdir -p "$SITES_DIR"
    
    # Set permissions
    chmod 755 "$CONFIG_DIR"
    chmod 755 "$DATA_DIR"
    chmod 755 "$LOG_DIR"
    chmod 755 "$RUN_DIR"
    chmod 751 "$SITES_DIR"  # Allow traversal but not listing
    
    # Set ownership for fastcp user
    chown -R fastcp:fastcp "$RUN_DIR"
    chown -R fastcp:fastcp "$LOG_DIR"
    
    echo -e "${GREEN}Directories created${NC}"
}

# Create initial configuration
create_config() {
    echo ""
    echo -e "${YELLOW}Creating configuration...${NC}"
    
    CONFIG_FILE="${DATA_DIR}/config.json"
    
    # Generate random JWT secret
    JWT_SECRET=$(head -c 32 /dev/urandom | base64 | tr -d '/+=' | head -c 32)
    
    # Create config.json
    cat > "$CONFIG_FILE" << EOF
{
    "admin_email": "${ADMIN_EMAIL}",
    "jwt_secret": "${JWT_SECRET}",
    "listen_addr": ":${API_PORT}",
    "data_dir": "${DATA_DIR}",
    "sites_dir": "${SITES_DIR}",
    "log_dir": "${LOG_DIR}",
    "proxy_port": 80,
    "proxy_ssl_port": 443,
    "php_versions": [
        {
            "version": "8.4",
            "port": 9084,
            "admin_port": 2084,
            "binary_path": "/usr/local/bin/frankenphp",
            "enabled": true,
            "num_threads": 0,
            "max_threads": 0
        }
    ]
}
EOF
    
    chmod 600 "$CONFIG_FILE"
    
    echo -e "${GREEN}Configuration created: ${CONFIG_FILE}${NC}"
}

# Create systemd service
create_systemd_service() {
    echo ""
    echo -e "${YELLOW}Creating systemd service...${NC}"
    
    cat > /etc/systemd/system/fastcp.service << EOF
[Unit]
Description=FastCP - Modern PHP Hosting Control Panel
Documentation=https://github.com/${GITHUB_REPO}
After=network.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/fastcp
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

# Security hardening
NoNewPrivileges=false
ProtectSystem=false
ProtectHome=false

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    
    echo -e "${GREEN}Systemd service created${NC}"
}

# Configure firewall
configure_firewall() {
    echo ""
    echo -e "${YELLOW}Configuring firewall...${NC}"
    
    # UFW (Ubuntu/Debian)
    if command -v ufw &> /dev/null; then
        ufw allow 80/tcp >/dev/null 2>&1 || true
        ufw allow 443/tcp >/dev/null 2>&1 || true
        ufw allow ${API_PORT}/tcp >/dev/null 2>&1 || true
        echo -e "${GREEN}UFW rules added for ports 80, 443, ${API_PORT}${NC}"
    # firewalld (CentOS/RHEL)
    elif command -v firewall-cmd &> /dev/null; then
        firewall-cmd --permanent --add-port=80/tcp >/dev/null 2>&1 || true
        firewall-cmd --permanent --add-port=443/tcp >/dev/null 2>&1 || true
        firewall-cmd --permanent --add-port=${API_PORT}/tcp >/dev/null 2>&1 || true
        firewall-cmd --reload >/dev/null 2>&1 || true
        echo -e "${GREEN}Firewalld rules added for ports 80, 443, ${API_PORT}${NC}"
    else
        echo -e "${BLUE}No firewall detected, skipping...${NC}"
    fi
}

# Start FastCP
start_fastcp() {
    echo ""
    echo -e "${YELLOW}Starting FastCP...${NC}"
    
    systemctl enable fastcp >/dev/null 2>&1
    systemctl start fastcp
    
    # Wait for startup
    sleep 3
    
    if systemctl is-active --quiet fastcp; then
        echo -e "${GREEN}FastCP is running${NC}"
    else
        echo -e "${RED}Warning: FastCP may not have started properly${NC}"
        echo "Check logs with: journalctl -u fastcp -f"
    fi
}

# Print success message
print_success() {
    # Get server IP
    SERVER_IP=$(curl -s --connect-timeout 5 ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}' || echo "YOUR_SERVER_IP")
    
    echo ""
    echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                                                               ║${NC}"
    echo -e "${GREEN}║        FastCP installed successfully! 🚀                      ║${NC}"
    echo -e "${GREEN}║                                                               ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "${CYAN}Access your control panel:${NC}"
    echo ""
    echo -e "  ${BLUE}URL:${NC}  http://${SERVER_IP}:${API_PORT}"
    echo ""
    echo -e "${CYAN}Login credentials:${NC}"
    echo ""
    echo -e "  Use your server's ${GREEN}root${NC} or ${GREEN}sudo user${NC} credentials"
    echo -e "  (Same username/password you use for SSH)"
    echo ""
    echo -e "${CYAN}Useful commands:${NC}"
    echo ""
    echo "  # Check status"
    echo "  sudo systemctl status fastcp"
    echo ""
    echo "  # View logs"
    echo "  sudo journalctl -u fastcp -f"
    echo ""
    echo "  # Restart service"
    echo "  sudo systemctl restart fastcp"
    echo ""
    echo -e "${CYAN}Configuration:${NC}"
    echo ""
    echo "  Config:  ${DATA_DIR}/config.json"
    echo "  Logs:    ${LOG_DIR}/"
    echo "  Sites:   ${SITES_DIR}/"
    echo ""
    echo -e "${YELLOW}Note: FrankenPHP (PHP runtime) will be auto-downloaded on first use.${NC}"
    echo ""
    echo -e "${BLUE}Documentation: https://github.com/${GITHUB_REPO}${NC}"
    echo ""
}

# Main installation function
main() {
    clear
    echo ""
    echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║                                                               ║${NC}"
    echo -e "${GREEN}║   ███████╗ █████╗ ███████╗████████╗ ██████╗██████╗            ║${NC}"
    echo -e "${GREEN}║   ██╔════╝██╔══██╗██╔════╝╚══██╔══╝██╔════╝██╔══██╗           ║${NC}"
    echo -e "${GREEN}║   █████╗  ███████║███████╗   ██║   ██║     ██████╔╝           ║${NC}"
    echo -e "${GREEN}║   ██╔══╝  ██╔══██║╚════██║   ██║   ██║     ██╔═══╝            ║${NC}"
    echo -e "${GREEN}║   ██║     ██║  ██║███████║   ██║   ╚██████╗██║                ║${NC}"
    echo -e "${GREEN}║   ╚═╝     ╚═╝  ╚═╝╚══════╝   ╚═╝    ╚═════╝╚═╝                ║${NC}"
    echo -e "${GREEN}║                                                               ║${NC}"
    echo -e "${GREEN}║   Modern PHP Hosting Control Panel                            ║${NC}"
    echo -e "${GREEN}║   Powered by FrankenPHP & Caddy                               ║${NC}"
    echo -e "${GREEN}║                                                               ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
    echo ""

    check_root
    detect_os
    detect_platform
    prompt_configuration
    check_ports
    install_dependencies
    get_latest_version
    install_fastcp
    create_fastcp_user
    create_directories
    create_config
    create_systemd_service
    configure_firewall
    start_fastcp
    print_success
}

# Run main function
main "$@"
