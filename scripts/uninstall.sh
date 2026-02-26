#!/bin/bash
#
# FastCP Uninstaller
#

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

FASTCP_DIR="/opt/fastcp"

print_step() {
    echo -e "${GREEN}==>${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}Warning:${NC} $1"
}

# Check if running as root
if [[ $EUID -ne 0 ]]; then
    echo -e "${RED}Error:${NC} This script must be run as root"
    exit 1
fi

echo ""
echo -e "${YELLOW}╔═══════════════════════════════════════════════════════════╗${NC}"
echo -e "${YELLOW}║           FastCP Uninstaller                              ║${NC}"
echo -e "${YELLOW}╚═══════════════════════════════════════════════════════════╝${NC}"
echo ""

read -p "Are you sure you want to uninstall FastCP? [y/N] " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Uninstall cancelled."
    exit 0
fi

read -p "Do you want to remove all user data (sites, databases)? [y/N] " -n 1 -r
echo
REMOVE_DATA=false
if [[ $REPLY =~ ^[Yy]$ ]]; then
    REMOVE_DATA=true
fi

# Stop services
print_step "Stopping services..."
systemctl stop fastcp 2>/dev/null || true
systemctl stop fastcp-agent 2>/dev/null || true
systemctl stop fastcp-caddy 2>/dev/null || true
systemctl disable fastcp 2>/dev/null || true
systemctl disable fastcp-agent 2>/dev/null || true
systemctl disable fastcp-caddy 2>/dev/null || true

# Stop per-user PHP services
for user_dir in /opt/fastcp/config/users/*; do
    if [[ -d "$user_dir" ]]; then
        username=$(basename "$user_dir")
        systemctl stop "fastcp-php@${username}" 2>/dev/null || true
        systemctl disable "fastcp-php@${username}" 2>/dev/null || true
    fi
done

# Remove systemd services
print_step "Removing systemd services..."
rm -f /etc/systemd/system/fastcp.service
rm -f /etc/systemd/system/fastcp-agent.service
rm -f /etc/systemd/system/fastcp-caddy.service
rm -f /etc/systemd/system/fastcp-php@.service
systemctl daemon-reload

# Remove FastCP directory
print_step "Removing FastCP files..."
rm -rf "$FASTCP_DIR"
rm -rf /var/run/fastcp  # legacy location
rm -rf /var/log/fastcp
rm -f /etc/tmpfiles.d/fastcp.conf

# Remove firewall rules
print_step "Removing firewall rules..."
ufw delete allow 2050/tcp 2>/dev/null || true
ufw delete allow 2087/tcp 2>/dev/null || true

if $REMOVE_DATA; then
    print_warning "User data removal is not implemented for safety."
    print_warning "You may need to manually remove user data from /home/*/apps"
fi

echo ""
echo -e "${GREEN}FastCP has been uninstalled.${NC}"
echo ""
echo "Note: MySQL, Caddy, and PHP packages were not removed."
echo "To remove them, run:"
echo "  apt-get remove mysql-server"
echo "  apt-get remove caddy php8.4-fpm"
echo ""
