#!/bin/bash
#
# FastCP installer
# https://github.com/rehmatworks/fastcp
#
# Copyright (c) 2024-present Rehmat Alam
# Licensed under the MIT License
#
# Environment:
#   FASTCP_VERSION   release tag to install, or "latest" (default: the
#                    release this installer belongs to)
#

set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_SUSPEND=1

FASTCP_VERSION_DEFAULT="v3.1.2"
# Ed25519 public key that signs SHA256SUMS in every release. Set with
# `go run ./tools/releasekey gen -pub internal/release/signing.pub -installer scripts/install.sh`.
FASTCP_SIGNING_PUBKEY="iTMGgOsWcbaI+uKYmjR3X1XcLgvUfAeJiGgBTWfx96g="

FASTCP_VERSION="${FASTCP_VERSION:-$FASTCP_VERSION_DEFAULT}"
REPO="rehmatworks/fastcp"
ADMIN_USER="fastcp"
PANEL_PORT=2050

BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

log() { echo -e "${BOLD}==>${NC} $1"; }
warn() { echo -e "${BOLD}warning:${NC} $1" >&2; }
error() {
    echo -e "${BOLD}error:${NC} $1" >&2
    exit 1
}

has_systemd() {
    command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]
}

wait_for_apt() {
    local waited=0
    while fuser /var/lib/apt/lists/lock /var/lib/dpkg/lock /var/lib/dpkg/lock-frontend /var/cache/apt/archives/lock >/dev/null 2>&1; do
        [[ $waited -eq 0 ]] && log "Waiting for another package manager to finish..."
        sleep 5
        waited=$((waited + 5))
        [[ $waited -ge 600 ]] && error "Timed out waiting for the apt lock."
    done
    return 0
}

apt_install() {
    wait_for_apt
    apt-get install -y -qq -o DPkg::Lock::Timeout=600 "$@" >/dev/null
}

# verify_signature FILE SIG checks a base64 Ed25519 signature of FILE against
# FASTCP_SIGNING_PUBKEY. Needs OpenSSL 3.
verify_signature() {
    local file="$1" sig="$2" dir
    dir="$(dirname "$file")"
    if [[ ! "$FASTCP_SIGNING_PUBKEY" =~ ^[A-Za-z0-9+/]{43}=$ ]]; then
        error "This installer has no release signing key, so the download cannot be verified. Use the installer from https://get.fastcp.org."
    fi
    {
        echo "-----BEGIN PUBLIC KEY-----"
        # SubjectPublicKeyInfo header for an Ed25519 key, followed by the key.
        { printf '\x30\x2a\x30\x05\x06\x03\x2b\x65\x70\x03\x21\x00'; printf '%s' "$FASTCP_SIGNING_PUBKEY" | base64 -d; } | base64 -w0
        echo
        echo "-----END PUBLIC KEY-----"
    } >"$dir/signing.pem"
    base64 -d <"$sig" >"$dir/signature.bin" 2>/dev/null || error "The release signature is malformed."
    if ! openssl pkeyutl -verify -pubin -inkey "$dir/signing.pem" -rawin -in "$file" -sigfile "$dir/signature.bin" >/dev/null 2>&1; then
        error "The release signature does not match FastCP's signing key. Nothing was installed."
    fi
}

# fetch_release DIR downloads the binaries for ARCH and verifies them.
fetch_release() {
    local dir="$1" base
    if [[ "$FASTCP_VERSION" == "latest" ]]; then
        base="https://github.com/${REPO}/releases/latest/download"
    else
        base="https://github.com/${REPO}/releases/download/${FASTCP_VERSION}"
    fi
    log "Downloading FastCP ${FASTCP_VERSION}..."
    if ! curl -fsSL --retry 3 -o "$dir/SHA256SUMS" "$base/SHA256SUMS"; then
        error "FastCP ${FASTCP_VERSION} is not available. If it was published in the last few minutes, its files may still be building; try again shortly."
    fi
    curl -fsSL --retry 3 -o "$dir/SHA256SUMS.sig" "$base/SHA256SUMS.sig" || error "Could not download the release signature."
    verify_signature "$dir/SHA256SUMS" "$dir/SHA256SUMS.sig"
    local name
    for name in "fastcp-linux-${ARCH}" "fastcp-agent-linux-${ARCH}"; do
        curl -fsSL --retry 3 -o "$dir/$name" "$base/$name" || error "Could not download ${name}."
    done
    awk -v a="$ARCH" '{ n = $2; sub(/^\*/, "", n) } n == "fastcp-linux-" a || n == "fastcp-agent-linux-" a' "$dir/SHA256SUMS" >"$dir/expected"
    [[ $(wc -l <"$dir/expected") -eq 2 ]] || error "The release does not list binaries for ${ARCH}."
    (cd "$dir" && sha256sum --quiet -c expected) || error "Checksum verification failed. Nothing was installed."
    install -m 0755 -o root -g root "$dir/fastcp-linux-${ARCH}" /opt/fastcp/bin/fastcp
    install -m 0755 -o root -g root "$dir/fastcp-agent-linux-${ARCH}" /opt/fastcp/bin/fastcp-agent
}

ssh_port() {
    local port=""
    if command -v sshd >/dev/null 2>&1; then
        port="$(sshd -T 2>/dev/null | awk '$1 == "port" { print $2; exit }' || true)"
    fi
    [[ "$port" =~ ^[0-9]+$ ]] || port=22
    echo "$port"
}

port_owner() {
    ss -Hltnp "sport = :$1" 2>/dev/null | sed -n 's/.*users:(("\([^"]*\)".*/\1/p' | head -n1 || true
}

# ---------------------------------------------------------------------------

echo ""
echo -e "${BOLD}FastCP${NC} ${DIM}- lightweight server control panel${NC}"
echo -e "${DIM}https://fastcp.org${NC}"
echo ""

[[ $EUID -eq 0 ]] || error "Run the installer as root."
[[ -f /etc/os-release ]] || error "Unsupported operating system."
# shellcheck disable=SC1091
source /etc/os-release
case "${ID:-}" in
ubuntu)
    [[ "${VERSION_ID%%.*}" -ge 22 ]] || error "FastCP needs Ubuntu 22.04 or newer."
    DB_PACKAGE="mysql-server"
    DB_SERVICE="mysql"
    ;;
debian)
    [[ "${VERSION_ID:-0}" -ge 12 ]] || error "FastCP needs Debian 12 or newer."
    DB_PACKAGE="default-mysql-server"
    DB_SERVICE="mariadb"
    ;;
*)
    error "FastCP supports Ubuntu and Debian."
    ;;
esac
case "$(uname -m)" in
x86_64) ARCH="amd64" ;;
aarch64) ARCH="arm64" ;;
*) error "Unsupported architecture: $(uname -m)" ;;
esac
if [[ "$FASTCP_VERSION" != "latest" && ! "$FASTCP_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]; then
    error "FASTCP_VERSION must be a release tag such as ${FASTCP_VERSION_DEFAULT}, or \"latest\"."
fi
log "Installing on ${PRETTY_NAME} (${ARCH})"

IN_CONTAINER=0
[[ -f /.dockerenv ]] && IN_CONTAINER=1
USE_SYSTEMD=0
has_systemd && USE_SYSTEMD=1
if [[ $USE_SYSTEMD -eq 0 ]]; then
    warn "systemd is not running; services are started directly and resource limits are not enforced."
fi
if [[ $IN_CONTAINER -eq 1 && $USE_SYSTEMD -eq 0 ]]; then
    # Keep package scripts from starting services inside the container.
    printf '#!/bin/sh\nexit 101\n' >/usr/sbin/policy-rc.d
    chmod +x /usr/sbin/policy-rc.d
fi

for port in 80 443 "$PANEL_PORT"; do
    owner="$(port_owner "$port")"
    if [[ -n "$owner" && "$owner" != "caddy" && "$owner" != "fastcp" ]]; then
        warn "port ${port} is in use by ${owner}; stop it before using FastCP."
    fi
done

log "Installing packages..."
wait_for_apt
apt-get update -qq
apt_install curl ca-certificates openssl gnupg acl libpam0g ufw restic rsync cron tar gzip psmisc iproute2 "$DB_PACKAGE"

TOTAL_RAM_KB=$(awk '/MemTotal/ { print $2 }' /proc/meminfo)
SWAP_KB=$(awk '/SwapTotal/ { print $2 }' /proc/meminfo)
if [[ $IN_CONTAINER -eq 0 && $TOTAL_RAM_KB -le 2097152 && $SWAP_KB -lt 524288 && ! -e /swapfile ]]; then
    log "Adding 1 GB of swap (low memory)..."
    fallocate -l 1G /swapfile 2>/dev/null || dd if=/dev/zero of=/swapfile bs=1M count=1024 status=none
    chmod 600 /swapfile
    mkswap /swapfile >/dev/null
    swapon /swapfile 2>/dev/null || true
    grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >>/etc/fstab
fi

mkdir -p /opt/fastcp/bin /opt/fastcp/data /opt/fastcp/ssl /opt/fastcp/tmp
chmod 700 /opt/fastcp/data /opt/fastcp/ssl /opt/fastcp/tmp

# A Caddy build left in /opt/fastcp/tmp is installed by the agent instead of
# the one published with the release.
if [[ -f ./go.mod && -d ./cmd/fastcp-agent ]] && command -v go >/dev/null 2>&1; then
    log "Building FastCP from this source tree..."
    go build -trimpath -o /opt/fastcp/bin/fastcp ./cmd/fastcp
    go build -trimpath -o /opt/fastcp/bin/fastcp-agent ./cmd/fastcp-agent
    (cd caddy && CGO_ENABLED=0 go build -trimpath -o /opt/fastcp/tmp/caddy .)
elif [[ -f ./fastcp && -f ./fastcp-agent ]]; then
    warn "installing ./fastcp and ./fastcp-agent from the current directory without signature verification."
    install -m 0755 -o root -g root ./fastcp /opt/fastcp/bin/fastcp
    install -m 0755 -o root -g root ./fastcp-agent /opt/fastcp/bin/fastcp-agent
    if [[ -f ./caddy ]]; then
        install -m 0755 -o root -g root ./caddy /opt/fastcp/tmp/caddy
    fi
else
    DOWNLOAD_DIR="$(mktemp -d /tmp/fastcp-install.XXXXXX)"
    trap 'rm -rf "$DOWNLOAD_DIR"' EXIT
    fetch_release "$DOWNLOAD_DIR"
fi

if [[ ! -s /opt/fastcp/data/.secret ]]; then
    (umask 077 && openssl rand -base64 32 >/opt/fastcp/data/.secret)
fi

HOST_IPS="$(hostname -I 2>/dev/null || true)"
SERVER_IPV4="$(tr ' ' '\n' <<<"$HOST_IPS" | awk '/^([0-9]{1,3}\.){3}[0-9]{1,3}$/ { print; exit }')"
SERVER_IPV6="$(tr ' ' '\n' <<<"$HOST_IPS" | awk 'index($0, ":") > 0 && $0 !~ /^fe80/ { print; exit }')"
FQDN="$(hostname -f 2>/dev/null || hostname)"
if [[ $IN_CONTAINER -eq 1 ]]; then
    DISPLAY_HOST="localhost"
    SAN_IP="127.0.0.1"
elif [[ -n "$SERVER_IPV4" ]]; then
    DISPLAY_HOST="$SERVER_IPV4"
    SAN_IP="$SERVER_IPV4"
elif [[ -n "$SERVER_IPV6" ]]; then
    DISPLAY_HOST="[$SERVER_IPV6]"
    SAN_IP="$SERVER_IPV6"
else
    DISPLAY_HOST="127.0.0.1"
    SAN_IP="127.0.0.1"
fi
if [[ ! -s /opt/fastcp/ssl/server.crt || ! -s /opt/fastcp/ssl/server.key ]]; then
    log "Creating a self-signed certificate for the control panel..."
    openssl req -x509 -nodes -days 3650 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
        -keyout /opt/fastcp/ssl/server.key -out /opt/fastcp/ssl/server.crt \
        -subj "/O=FastCP/CN=${FQDN}" \
        -addext "subjectAltName=DNS:${FQDN},DNS:localhost,IP:127.0.0.1,IP:${SAN_IP}" 2>/dev/null
fi

ADMIN_PASSWORD=""
if ! id -u "$ADMIN_USER" >/dev/null 2>&1; then
    log "Creating the ${ADMIN_USER} administrator account..."
    useradd --create-home --shell /bin/bash "$ADMIN_USER"
    ADMIN_PASSWORD="$(openssl rand -base64 30 | tr -dc 'A-Za-z0-9' | cut -c1-20)"
    printf '%s:%s\n' "$ADMIN_USER" "$ADMIN_PASSWORD" | chpasswd
fi
echo "$ADMIN_USER" >/opt/fastcp/data/default_admin

if [[ $USE_SYSTEMD -eq 1 ]]; then
    systemctl enable --now "$DB_SERVICE" >/dev/null 2>&1 || warn "MySQL did not start; check 'systemctl status ${DB_SERVICE}'."
else
    service "$DB_SERVICE" start >/dev/null 2>&1 || warn "MySQL did not start."
fi

log "Configuring FastCP..."
/opt/fastcp/bin/fastcp-agent setup

log "Installing Caddy, phpMyAdmin and PHP (this takes a few minutes)..."
/opt/fastcp/bin/fastcp-agent provision

if command -v ufw >/dev/null 2>&1; then
    # Rules only; the firewall stays off until it is enabled in Settings.
    for rule in "$(ssh_port)/tcp" 80/tcp 443/tcp "${PANEL_PORT}/tcp"; do
        ufw allow "$rule" >/dev/null 2>&1 || true
    done
fi

log "Starting FastCP..."
if [[ $USE_SYSTEMD -eq 1 ]]; then
    systemctl daemon-reload
    systemctl enable --quiet fastcp-agent fastcp fastcp-caddy
    systemctl restart fastcp-agent
    systemctl restart fastcp
else
    mkdir -p /var/log/fastcp
    pkill -f '^/opt/fastcp/bin/fastcp-agent' 2>/dev/null || true
    pkill -f '^/opt/fastcp/bin/fastcp ' 2>/dev/null || true
    sleep 1
    nohup /opt/fastcp/bin/fastcp-agent >>/var/log/fastcp/agent.log 2>&1 &
    sleep 2
    nohup runuser -u fastcp-panel -- /opt/fastcp/bin/fastcp --listen ":${PANEL_PORT}" \
        --data-dir /opt/fastcp/data --agent-socket /opt/fastcp/run/agent.sock \
        --tls-cert /opt/fastcp/ssl/server.crt --tls-key /opt/fastcp/ssl/server.key >>/var/log/fastcp/panel.log 2>&1 &
fi

READY=0
for _ in $(seq 1 90); do
    if curl -fsk --max-time 3 "https://127.0.0.1:${PANEL_PORT}/api/health" 2>/dev/null | grep -q '"agent":"ok"'; then
        READY=1
        break
    fi
    sleep 1
done
if [[ $READY -eq 0 ]]; then
    if [[ $USE_SYSTEMD -eq 1 ]]; then
        error "FastCP did not start. See 'journalctl -u fastcp -u fastcp-agent'."
    fi
    error "FastCP did not start. See /var/log/fastcp/panel.log and /var/log/fastcp/agent.log."
fi

echo ""
echo -e "${BOLD}FastCP $(/opt/fastcp/bin/fastcp --version 2>/dev/null | awk '{ print $2 }') is installed.${NC}"
echo ""
echo -e "  Control panel   https://${DISPLAY_HOST}:${PANEL_PORT}"
echo -e "  phpMyAdmin      https://${DISPLAY_HOST}:${PANEL_PORT}/phpmyadmin/"
echo ""
if [[ -n "$ADMIN_PASSWORD" ]]; then
    echo -e "  Username        ${BOLD}${ADMIN_USER}${NC}"
    echo -e "  Password        ${BOLD}${ADMIN_PASSWORD}${NC}"
    echo ""
    echo -e "  ${DIM}Save this password now; it is not shown again. Change it with 'passwd ${ADMIN_USER}'.${NC}"
else
    echo -e "  Sign in as ${BOLD}${ADMIN_USER}${NC} with that account's existing password."
fi
echo -e "  ${DIM}Your browser will warn about the self-signed certificate on first visit.${NC}"
echo ""
if [[ $USE_SYSTEMD -eq 1 ]]; then
    echo -e "  ${DIM}Status   systemctl status fastcp fastcp-agent fastcp-caddy${NC}"
    echo -e "  ${DIM}Logs     journalctl -u fastcp -u fastcp-agent -f${NC}"
else
    echo -e "  ${DIM}Logs     tail -f /var/log/fastcp/panel.log /var/log/fastcp/agent.log${NC}"
fi
echo ""
