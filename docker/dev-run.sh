#!/bin/bash
# Development runner script - builds and runs FastCP inside Docker

set -e

cd /app

echo "Setting up phpMyAdmin..."
# Force latest config-auth model (no legacy signon.php flow).
BLOWFISH_SECRET=$(openssl rand -base64 32)
cat > /opt/fastcp/phpmyadmin/config.inc.php << 'PMACONFIG'
<?php
$cfg['blowfish_secret'] = 'BLOWFISH_PLACEHOLDER';
$cfg['TempDir'] = '/opt/fastcp/run/phpmyadmin-tmp';
$cfg['UploadDir'] = '';
$cfg['SaveDir'] = '';

$cfg['Servers'][1]['host'] = '127.0.0.1';
$cfg['Servers'][1]['auth_type'] = 'config';
$cfg['Servers'][1]['user'] = $_SERVER['PHP_AUTH_USER'] ?? '';
$cfg['Servers'][1]['password'] = $_SERVER['PHP_AUTH_PW'] ?? '';
$cfg['Servers'][1]['AllowNoPassword'] = false;
$cfg['Servers'][1]['hide_db'] = '^(information_schema|performance_schema|mysql|sys)$';

$cfg['ShowCreateDb'] = false;
$cfg['LoginCookieValidity'] = 3600;
$cfg['LoginCookieStore'] = 0;
$cfg['LoginCookieDeleteAll'] = true;
PMACONFIG
sed -i "s|BLOWFISH_PLACEHOLDER|${BLOWFISH_SECRET}|" /opt/fastcp/phpmyadmin/config.inc.php
rm -f /opt/fastcp/phpmyadmin/signon.php

# Ensure secret exists
if [ ! -f /opt/fastcp/data/.secret ]; then
    openssl rand -base64 32 > /opt/fastcp/data/.secret
    chmod 600 /opt/fastcp/data/.secret
fi

# Generate self-signed SSL certificate for control panel
if [ ! -f /opt/fastcp/ssl/server.crt ]; then
    echo "Generating SSL certificates..."
    mkdir -p /opt/fastcp/ssl
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout /opt/fastcp/ssl/server.key \
        -out /opt/fastcp/ssl/server.crt \
        -subj "/C=US/ST=State/L=City/O=FastCP/CN=localhost" 2>/dev/null
fi

echo "Building FastCP..."
go build -o /opt/fastcp/bin/fastcp ./cmd/fastcp
go build -o /opt/fastcp/bin/fastcp-agent ./cmd/fastcp-agent

# Kill any existing processes
pkill -9 fastcp-agent 2>/dev/null || true
pkill -9 fastcp 2>/dev/null || true
pkill -9 caddy 2>/dev/null || true
pkill -9 php-fpm 2>/dev/null || true
rm -f /opt/fastcp/run/agent.sock

sleep 1

echo "Starting FastCP Agent..."
/opt/fastcp/bin/fastcp-agent --socket /opt/fastcp/run/agent.sock --log-level debug &
AGENT_PID=$!

sleep 2

echo "Caddy is managed by fastcp-agent."
sleep 1

echo "Starting FastCP..."
echo ""
echo "============================================"
echo "  FastCP is running!"
echo "============================================"
echo ""
echo "  Control Panel: https://localhost:2050"
echo "  Your Sites:    http://localhost"
echo ""
echo "  Test credentials:"
echo "    testuser / testpass"
echo ""
echo "  Note: Root login is disabled for security."
echo ""
echo "  Press Ctrl+C to stop"
echo ""

# Trap to cleanup on exit
cleanup() {
    echo "Stopping services..."
    kill $AGENT_PID 2>/dev/null || true
    pkill -9 caddy 2>/dev/null || true
    exit 0
}
trap cleanup SIGINT SIGTERM

/opt/fastcp/bin/fastcp --data-dir /opt/fastcp/data --agent-socket /opt/fastcp/run/agent.sock --listen :2050 --log-level debug

# Cleanup
cleanup
