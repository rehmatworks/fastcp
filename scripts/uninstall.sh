#!/bin/bash
#
# FastCP uninstaller
#

set -euo pipefail

BOLD='\033[1m'
NC='\033[0m'

FASTCP_DIR="/opt/fastcp"
FASTCP_DB="/opt/fastcp/data/fastcp.db"
LEGACY_USERS_DIR="/opt/fastcp/config/users"
SERVICE_USERS=(fastcp-panel caddy)

step() { echo -e "${BOLD}==>${NC} $1"; }
warn() { echo -e "${BOLD}warning:${NC} $1" >&2; }

has_systemd() {
    command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]
}

confirm() {
    local reply
    read -r -p "$1 [y/N] " reply
    [[ "$reply" =~ ^[Yy]$ ]]
}

# sqlite_rows QUERY prints the rows of a query against the panel database.
sqlite_rows() {
    [[ -f "$FASTCP_DB" ]] || return 0
    if ! command -v python3 >/dev/null 2>&1; then
        warn "python3 is not installed; skipping data that is listed in the FastCP database."
        return 0
    fi
    python3 - "$FASTCP_DB" "$1" <<'PY'
import sqlite3, sys
try:
    rows = sqlite3.connect(sys.argv[1]).execute(sys.argv[2]).fetchall()
except Exception:
    sys.exit(0)
for row in rows:
    print("\t".join("" if v is None else str(v) for v in row))
PY
}

collect_managed_users() {
    {
        sqlite_rows "SELECT username FROM users"
        if [[ -d "$LEGACY_USERS_DIR" ]]; then
            find "$LEGACY_USERS_DIR" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' 2>/dev/null || true
        fi
        id -u fastcp >/dev/null 2>&1 && echo fastcp
    } | awk 'NF && !seen[$0]++'
}

stop_services() {
    step "Stopping FastCP services..."
    if has_systemd; then
        systemctl disable --now fastcp fastcp-agent fastcp-caddy >/dev/null 2>&1 || true
    fi
    pkill -f '^/opt/fastcp/bin/fastcp' 2>/dev/null || true
    pkill -f '/usr/local/bin/caddy run --config /opt/fastcp/config/Caddyfile' 2>/dev/null || true
}

remove_system_files() {
    step "Removing FastCP system configuration..."
    rm -f /etc/systemd/system/fastcp.service /etc/systemd/system/fastcp-agent.service \
        /etc/systemd/system/fastcp-caddy.service /etc/systemd/system/fastcp-php@*.service
    rm -f /etc/systemd/system/user-*.slice.d/50-fastcp-limits.conf
    rmdir /etc/systemd/system/user-*.slice.d 2>/dev/null || true
    rm -f /etc/pam.d/fastcp /etc/update-motd.d/99-fastcp /etc/tmpfiles.d/fastcp.conf
    rm -f /etc/mysql/conf.d/fastcp.cnf

    local restart_fpm=()
    local conf
    for conf in /etc/php/*/fpm/pool.d/fastcp-*.conf /etc/php/*/fpm/conf.d/99-fastcp.ini; do
        [[ -e "$conf" ]] || continue
        rm -f "$conf"
        restart_fpm+=("$(echo "$conf" | cut -d/ -f4)")
    done
    if has_systemd; then
        systemctl daemon-reload || true
        local v
        for v in $(printf '%s\n' "${restart_fpm[@]}" | sort -u); do
            systemctl restart "php${v}-fpm" >/dev/null 2>&1 || true
        done
        systemctl restart mysql >/dev/null 2>&1 || systemctl restart mariadb >/dev/null 2>&1 || true
    fi
}

remove_firewall_rules() {
    command -v ufw >/dev/null 2>&1 || return 0
    step "Removing the control panel firewall rule..."
    local port
    for port in 2050 2087; do
        while ufw status 2>/dev/null | awk '{ print $1 }' | grep -qx "${port}/tcp"; do
            ufw --force delete allow "${port}/tcp" >/dev/null 2>&1 || break
        done
    done
}

drop_managed_databases() {
    command -v mysql >/dev/null 2>&1 || {
        warn "the mysql client is not installed; skipping database cleanup."
        return 0
    }
    local sql="" name user
    while IFS=$'\t' read -r name user; do
        [[ "$name" =~ ^[A-Za-z0-9_]+$ ]] && sql+="DROP DATABASE IF EXISTS \`${name}\`;"
        if [[ "$user" =~ ^[A-Za-z0-9_]+$ ]]; then
            sql+="DROP USER IF EXISTS '${user}'@'localhost';DROP USER IF EXISTS '${user}'@'127.0.0.1';"
        fi
    done < <(sqlite_rows "SELECT db_name, db_user FROM databases")
    [[ -n "$sql" ]] || return 0
    step "Dropping FastCP databases and database users..."
    mysql --protocol=socket -uroot -e "${sql}FLUSH PRIVILEGES;" >/dev/null 2>&1 ||
        warn "some databases or database users could not be dropped."
}

remove_managed_users() {
    (($# > 0)) || return 0
    step "Removing FastCP user accounts and their home directories..."
    local u
    for u in "$@"; do
        case "$u" in root | www-data | fastcp-panel | caddy) continue ;; esac
        [[ "$u" =~ ^[a-z_][a-z0-9_-]*$ ]] || continue
        if id -u "$u" >/dev/null 2>&1; then
            pkill -u "$u" 2>/dev/null || true
            userdel -r "$u" 2>/dev/null || warn "could not remove the user ${u}."
        fi
    done
}

remove_service_users() {
    local u
    for u in "${SERVICE_USERS[@]}"; do
        if id -u "$u" >/dev/null 2>&1; then
            userdel "$u" 2>/dev/null || warn "could not remove the service account ${u}."
        fi
    done
}

remove_fastcp_files() {
    step "Removing FastCP files..."
    rm -rf "$FASTCP_DIR" /var/lib/fastcp /var/log/fastcp /run/fastcp-caddy /var/run/fastcp
    if [[ -f /usr/sbin/policy-rc.d ]] && [[ "$(cat /usr/sbin/policy-rc.d)" == $'#!/bin/sh\nexit 101' ]]; then
        rm -f /usr/sbin/policy-rc.d
    fi
}

purge_packages() {
    command -v dpkg-query >/dev/null 2>&1 || return 0
    local pkgs=()
    mapfile -t pkgs < <(dpkg-query -W -f='${Package}\n' 2>/dev/null |
        grep -E '^(php([0-9.]+)?(-.*)?|libapache2-mod-php.*|mysql-(server|client|common).*|mariadb-(server|client|common).*|galera-4|restic)$' | sort -u || true)
    if ((${#pkgs[@]} == 0)); then
        warn "no related packages are installed."
    else
        echo ""
        echo "These packages will be purged:"
        printf '  %s\n' "${pkgs[@]}"
        echo ""
        if confirm "Purge ${#pkgs[@]} packages?"; then
            step "Purging packages..."
            apt-get purge -y "${pkgs[@]}" >/dev/null 2>&1 || warn "some packages could not be purged."
            apt-get autoremove -y >/dev/null 2>&1 || true
        fi
    fi
    rm -f /usr/local/bin/caddy
    rm -f /etc/apt/sources.list.d/fastcp-php.list /etc/apt/keyrings/ondrej-php.asc /etc/apt/keyrings/sury-php.gpg
}

[[ $EUID -eq 0 ]] || {
    echo "Run the uninstaller as root." >&2
    exit 1
}

echo ""
echo -e "${BOLD}FastCP uninstaller${NC}"
echo ""
echo "This removes the FastCP services, files and configuration."
echo ""
confirm "Continue?" || {
    echo "Nothing was changed."
    exit 0
}

REMOVE_DATA=false
PURGE=false
confirm "Also delete every FastCP user account, its sites and its databases?" && REMOVE_DATA=true
confirm "Also purge PHP, MySQL, restic and Caddy?" && PURGE=true

mapfile -t MANAGED_USERS < <(collect_managed_users || true)

stop_services
if $REMOVE_DATA; then
    drop_managed_databases
    remove_managed_users "${MANAGED_USERS[@]}"
fi
remove_system_files
remove_firewall_rules
remove_fastcp_files
remove_service_users
if has_systemd; then
    systemctl reset-failed >/dev/null 2>&1 || true
fi
$PURGE && purge_packages

echo ""
echo -e "${BOLD}FastCP has been removed.${NC}"
$REMOVE_DATA || echo "User accounts, sites and databases were kept."
$PURGE || echo "PHP, MySQL, restic and Caddy were kept."
if [[ -e /etc/ssh/sshd_config.d/00-fastcp.conf ]]; then
    echo "SSH settings made in FastCP are kept in /etc/ssh/sshd_config.d/00-fastcp.conf."
fi
echo ""
