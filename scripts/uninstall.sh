#!/bin/bash
#
# FastCP Uninstaller
#

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

FASTCP_DIR="/opt/fastcp"
FASTCP_DB="/opt/fastcp/data/fastcp.db"
FASTCP_USERS_DIR="/opt/fastcp/config/users"
FASTCP_MOTD="/etc/update-motd.d/99-fastcp"

print_step() {
    echo -e "${GREEN}==>${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}Warning:${NC} $1"
}

print_error() {
    echo -e "${RED}Error:${NC} $1"
}

print_info() {
    echo -e "${BLUE}Info:${NC} $1"
}

has_systemd() {
    command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]
}

confirm() {
    local prompt="$1"
    read -r -p "$prompt [y/N] " reply
    [[ "$reply" =~ ^[Yy]$ ]]
}

collect_managed_users() {
    local users=()
    if [[ -d "$FASTCP_USERS_DIR" ]]; then
        while IFS= read -r -d '' dir; do
            local u
            u="$(basename "$dir")"
            [[ -n "$u" ]] && users+=("$u")
        done < <(find "$FASTCP_USERS_DIR" -mindepth 1 -maxdepth 1 -type d -print0 2>/dev/null || true)
    fi
    if id -u fastcp >/dev/null 2>&1; then
        users+=("fastcp")
    fi
    if ((${#users[@]} == 0)); then
        return 0
    fi
    printf '%s\n' "${users[@]}" | awk '!seen[$0]++'
}

stop_services() {
    print_step "Stopping FastCP services..."
    if has_systemd; then
        systemctl stop fastcp 2>/dev/null || true
        systemctl stop fastcp-agent 2>/dev/null || true
        systemctl stop fastcp-caddy 2>/dev/null || true
        systemctl disable fastcp 2>/dev/null || true
        systemctl disable fastcp-agent 2>/dev/null || true
        systemctl disable fastcp-caddy 2>/dev/null || true
    fi

    # Non-systemd/dev mode processes
    pkill -f "/opt/fastcp/bin/fastcp-agent --socket /opt/fastcp/run/agent.sock" 2>/dev/null || true
    pkill -f "/opt/fastcp/bin/fastcp --listen :2050" 2>/dev/null || true
    pkill -f "/usr/local/bin/caddy run --config /opt/fastcp/config/Caddyfile" 2>/dev/null || true
}

remove_systemd_units() {
    print_step "Removing FastCP systemd units..."
    rm -f /etc/systemd/system/fastcp.service
    rm -f /etc/systemd/system/fastcp-agent.service
    rm -f /etc/systemd/system/fastcp-caddy.service
    rm -f /etc/systemd/system/fastcp-php@.service
    rm -f /etc/systemd/system/fastcp-php@*.service
    if has_systemd; then
        systemctl daemon-reload || true
    fi
}

remove_firewall_rules() {
    if ! command -v ufw >/dev/null 2>&1; then
        return 0
    fi
    print_step "Removing FastCP firewall rules..."
    while ufw status 2>/dev/null | awk '{print $1}' | grep -q '^2050/tcp$'; do
        ufw --force delete allow 2050/tcp >/dev/null 2>&1 || break
    done
    while ufw status 2>/dev/null | awk '{print $1}' | grep -q '^2087/tcp$'; do
        ufw --force delete allow 2087/tcp >/dev/null 2>&1 || break
    done
}

drop_managed_mysql_data() {
    local sql_file="/tmp/fastcp-uninstall-mysql.sql"
    rm -f "$sql_file"

    if [[ ! -f "$FASTCP_DB" ]]; then
        return 0
    fi
    if ! command -v python3 >/dev/null 2>&1; then
        print_warning "python3 not found; skipping managed MySQL database/user drop."
        return 0
    fi

    python3 - "$FASTCP_DB" "$sql_file" <<'PY'
import sqlite3, sys
db_path, sql_path = sys.argv[1], sys.argv[2]
try:
    conn = sqlite3.connect(db_path)
except Exception:
    sys.exit(0)
cur = conn.cursor()
db_rows = []
try:
    cur.execute("SELECT db_name, db_user FROM databases")
    db_rows = cur.fetchall()
except Exception:
    pass
def esc_ident(v: str) -> str:
    return v.replace("`", "``")
def esc_str(v: str) -> str:
    return v.replace("\\", "\\\\").replace("'", "\\'")
with open(sql_path, "w", encoding="utf-8") as f:
    for db_name, db_user in db_rows:
        if db_name:
            f.write(f"DROP DATABASE IF EXISTS `{esc_ident(str(db_name))}`;\n")
        if db_user:
            u = esc_str(str(db_user))
            f.write(f"DROP USER IF EXISTS '{u}'@'localhost';\n")
            f.write(f"DROP USER IF EXISTS '{u}'@'127.0.0.1';\n")
    f.write("FLUSH PRIVILEGES;\n")
PY

    if [[ ! -s "$sql_file" ]]; then
        rm -f "$sql_file"
        return 0
    fi

    if command -v mysql >/dev/null 2>&1; then
        print_step "Dropping FastCP-managed MySQL databases/users..."
        mysql --protocol=socket -uroot < "$sql_file" >/dev/null 2>&1 || \
            print_warning "Failed to drop one or more MySQL databases/users (manual cleanup may be required)."
    else
        print_warning "mysql client not found; skipping managed MySQL cleanup."
    fi
    rm -f "$sql_file"
}

remove_managed_site_data() {
    if [[ ! -f "$FASTCP_DB" ]]; then
        return 0
    fi
    if ! command -v python3 >/dev/null 2>&1; then
        print_warning "python3 not found; skipping managed site path cleanup."
        return 0
    fi
    print_step "Removing FastCP-managed site directories..."
    python3 - "$FASTCP_DB" <<'PY'
import os, sqlite3, sys
db_path = sys.argv[1]
try:
    conn = sqlite3.connect(db_path)
except Exception:
    sys.exit(0)
cur = conn.cursor()
paths = set()
try:
    cur.execute("SELECT username, document_root FROM sites")
    rows = cur.fetchall()
except Exception:
    rows = []
for username, doc_root in rows:
    if not username or not doc_root:
        continue
    site_root = os.path.dirname(str(doc_root).rstrip("/"))
    base = f"/home/{username}/apps/"
    if site_root.startswith(base):
        paths.add(site_root)
for p in sorted(paths, key=len, reverse=True):
    try:
        if os.path.exists(p):
            os.system(f"rm -rf -- {p!r}")
    except Exception:
        pass
PY
}

remove_managed_users() {
    local users=("$@")
    if ((${#users[@]} == 0)); then
        return 0
    fi
    print_step "Removing FastCP-managed Linux users..."
    for u in "${users[@]}"; do
        [[ "$u" == "root" ]] && continue
        [[ "$u" == "www-data" ]] && continue
        if id -u "$u" >/dev/null 2>&1; then
            pkill -u "$u" 2>/dev/null || true
            userdel -r "$u" 2>/dev/null || print_warning "Failed to remove user '$u' (manual cleanup may be required)."
        fi
    done
}

remove_fastcp_files() {
    print_step "Removing FastCP files and configs..."
    rm -rf "$FASTCP_DIR"
    rm -rf /var/run/fastcp
    rm -rf /var/log/fastcp
    rm -f /etc/tmpfiles.d/fastcp.conf
    rm -f "$FASTCP_MOTD"
    rm -f /etc/mysql/conf.d/fastcp.cnf
}

cleanup_policy_rcd() {
    if [[ -f /usr/sbin/policy-rc.d ]]; then
        if awk 'NR==1 && $0=="#!/bin/sh"{ok=1} NR==2 && $0=="exit 101"{ok=ok&&1} END{exit(ok?0:1)}' /usr/sbin/policy-rc.d 2>/dev/null; then
            rm -f /usr/sbin/policy-rc.d
        fi
    fi
}

purge_fastcp_related_packages() {
    if ! command -v dpkg-query >/dev/null 2>&1; then
        print_warning "dpkg-query not found; skipping package purge."
        return 0
    fi

    local -a all_installed=()
    local -a purge_list=()
    local -A seen=()
    mapfile -t all_installed < <(dpkg-query -W -f='${Package}\n' 2>/dev/null || true)

    for pkg in "${all_installed[@]}"; do
        [[ -z "$pkg" ]] && continue
        case "$pkg" in
            php|php-*|php[0-9]*|libapache2-mod-php*)
                seen["$pkg"]=1
                ;;
            mysql-server*|mysql-client*|mysql-common|mariadb-server*|mariadb-client*|mariadb-common|galera-4)
                seen["$pkg"]=1
                ;;
            caddy|restic|rsync)
                seen["$pkg"]=1
                ;;
        esac
    done

    for pkg in "${!seen[@]}"; do
        purge_list+=("$pkg")
    done
    if ((${#purge_list[@]} == 0)); then
        print_warning "No FastCP-related packages found to purge."
        return 0
    fi

    IFS=$'\n' purge_list=($(printf '%s\n' "${purge_list[@]}" | sort))
    unset IFS

    local php_count=0
    local mysql_count=0
    local other_count=0
    for pkg in "${purge_list[@]}"; do
        case "$pkg" in
            php|php-*|php[0-9]*|libapache2-mod-php*) php_count=$((php_count + 1)) ;;
            mysql-*|mariadb-*|galera-4) mysql_count=$((mysql_count + 1)) ;;
            *) other_count=$((other_count + 1)) ;;
        esac
    done

    echo ""
    print_warning "Destructive action: this will purge installed server/runtime packages."
    print_info "Packages to purge (${#purge_list[@]} total):"
    for pkg in "${purge_list[@]}"; do
        echo "  - $pkg"
    done
    echo ""
    print_info "Summary: PHP=${php_count}, MySQL/MariaDB=${mysql_count}, Other=${other_count}"
    if ! confirm "Proceed with this package purge?"; then
        print_warning "Package purge skipped by user."
        return 0
    fi

    print_step "Purging FastCP-related packages..."
    apt-get purge -y "${purge_list[@]}" >/dev/null 2>&1 || \
        print_warning "Package purge partially failed; manual package cleanup may be required."
    apt-get autoremove -y >/dev/null 2>&1 || true
    apt-get autoclean >/dev/null 2>&1 || true

    # FastCP installs Caddy as a standalone binary; purge apt package won't remove it.
    if [[ -f /usr/local/bin/caddy ]]; then
        rm -f /usr/local/bin/caddy || true
    fi
}

# Check if running as root
if [[ $EUID -ne 0 ]]; then
    print_error "This script must be run as root"
    exit 1
fi

echo ""
echo -e "${YELLOW}╔═══════════════════════════════════════════════════════════╗${NC}"
echo -e "${YELLOW}║                     FastCP Uninstaller                    ║${NC}"
echo -e "${YELLOW}╚═══════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${BLUE}This will remove FastCP services, files, and configuration.${NC}"
echo ""

if ! confirm "Proceed with uninstall?"; then
    echo "Uninstall cancelled."
    exit 0
fi

REMOVE_DATA=false
PURGE_PACKAGES=false
if confirm "Also remove all FastCP-managed user data/users/databases?"; then
    REMOVE_DATA=true
fi
if confirm "Also purge FastCP-related packages (MySQL/Caddy/PHP/restic/rsync)?"; then
    PURGE_PACKAGES=true
fi

mapfile -t MANAGED_USERS < <(collect_managed_users || true)

stop_services
remove_systemd_units

if $REMOVE_DATA; then
    drop_managed_mysql_data
    remove_managed_site_data
    remove_managed_users "${MANAGED_USERS[@]}"
fi

remove_firewall_rules
remove_fastcp_files
cleanup_policy_rcd

if has_systemd; then
    systemctl daemon-reload || true
    systemctl reset-failed 2>/dev/null || true
    systemctl restart mysql 2>/dev/null || true
fi

if $PURGE_PACKAGES; then
    purge_fastcp_related_packages
fi

echo ""
echo -e "${GREEN}FastCP uninstall completed.${NC}"
if ! $REMOVE_DATA; then
    echo -e "${YELLOW}Note:${NC} Managed sites/users/databases were preserved."
fi
if ! $PURGE_PACKAGES; then
    echo -e "${YELLOW}Note:${NC} System packages (MySQL/Caddy/PHP/etc.) were preserved."
fi
echo ""
