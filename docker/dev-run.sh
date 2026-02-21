#!/bin/bash
# Development runner script - builds and runs FastCP inside Docker

set -e

cd /app

echo "Setting up phpMyAdmin..."
# Create phpMyAdmin config if not exists
if [ ! -f /opt/fastcp/phpmyadmin/config.inc.php ]; then
    BLOWFISH_SECRET=$(openssl rand -base64 32)
    cat > /opt/fastcp/phpmyadmin/config.inc.php << 'PMACONFIG'
<?php
$cfg['blowfish_secret'] = 'BLOWFISH_PLACEHOLDER';
$cfg['Servers'][1]['auth_type'] = 'signon';
$cfg['Servers'][1]['SignonSession'] = 'SignonSession';
$cfg['Servers'][1]['SignonURL'] = '/phpmyadmin/signon.php';
$cfg['Servers'][1]['host'] = 'localhost';
$cfg['TempDir'] = '/tmp';
PMACONFIG
    sed -i "s|BLOWFISH_PLACEHOLDER|${BLOWFISH_SECRET}|" /opt/fastcp/phpmyadmin/config.inc.php
fi

# Create signon.php if not exists
if [ ! -f /opt/fastcp/phpmyadmin/signon.php ]; then
    cat > /opt/fastcp/phpmyadmin/signon.php << 'SIGNONPHP'
<?php
session_name('SignonSession');
session_start();

function decryptToken($token) {
    $secretKey = trim(file_get_contents('/opt/fastcp/data/.secret'));
    $key = hash('sha256', base64_decode($secretKey), true);
    // URL-safe base64 decode: replace - with + and _ with /
    $token = strtr($token, '-_', '+/');
    // Add padding if necessary
    $padding = strlen($token) % 4;
    if ($padding > 0) {
        $token .= str_repeat('=', 4 - $padding);
    }
    $data = base64_decode($token);
    if ($data === false || strlen($data) < 28) return null;
    $nonce = substr($data, 0, 12);
    $tag = substr($data, -16);
    $ciphertext = substr($data, 12, -16);
    $plaintext = openssl_decrypt($ciphertext, 'aes-256-gcm', $key, OPENSSL_RAW_DATA, $nonce, $tag);
    return $plaintext !== false ? $plaintext : null;
}

$token = $_GET['token'] ?? '';
if (empty($token)) {
    echo '<html><body style="font-family: -apple-system, sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #f3f4f6;">';
    echo '<div style="text-align: center; padding: 40px; background: white; border-radius: 12px; box-shadow: 0 4px 6px rgba(0,0,0,0.1);">';
    echo '<h1 style="color: #1f2937; margin-bottom: 16px;">phpMyAdmin Access</h1>';
    echo '<p style="color: #6b7280;">Please access phpMyAdmin through the FastCP control panel.</p>';
    echo '<a href="/" style="display: inline-block; margin-top: 20px; padding: 12px 24px; background: #3b82f6; color: white; text-decoration: none; border-radius: 8px;">Go to FastCP</a>';
    echo '</div></body></html>';
    exit;
}

$payload = decryptToken($token);
if (!$payload) {
    echo '<html><body style="font-family: -apple-system, sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #f3f4f6;">';
    echo '<div style="text-align: center; padding: 40px; background: white; border-radius: 12px; box-shadow: 0 4px 6px rgba(0,0,0,0.1);">';
    echo '<h1 style="color: #dc2626; margin-bottom: 16px;">Invalid Token</h1>';
    echo '<p style="color: #6b7280;">The access token is invalid or has expired.</p>';
    echo '<a href="/" style="display: inline-block; margin-top: 20px; padding: 12px 24px; background: #3b82f6; color: white; text-decoration: none; border-radius: 8px;">Go to FastCP</a>';
    echo '</div></body></html>';
    exit;
}

$parts = explode('|', $payload);
if (count($parts) !== 4) { die('Invalid token format'); }
list($dbUser, $dbPassword, $dbName, $expiry) = $parts;
if (time() > intval($expiry)) { die('Token expired'); }

$_SESSION['PMA_single_signon_user'] = $dbUser;
$_SESSION['PMA_single_signon_password'] = $dbPassword;
$_SESSION['PMA_single_signon_host'] = 'localhost';

header('Location: index.php?db=' . urlencode($dbName));
exit;
SIGNONPHP
fi

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
pkill -9 frankenphp 2>/dev/null || true
rm -f /opt/fastcp/run/agent.sock

sleep 1

echo "Starting FastCP Agent..."
/opt/fastcp/bin/fastcp-agent --socket /opt/fastcp/run/agent.sock --log-level debug &
AGENT_PID=$!

sleep 2

echo "Starting FrankenPHP web server..."
/usr/local/bin/frankenphp run --config /opt/fastcp/config/Caddyfile &
FRANKENPHP_PID=$!

sleep 2

echo "Starting FastCP..."
echo ""
echo "============================================"
echo "  FastCP is running!"
echo "============================================"
echo ""
echo "  Control Panel: https://localhost:2087"
echo "  Your Sites:    http://localhost:8080 (mapped to port 80)"
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
    kill $FRANKENPHP_PID 2>/dev/null || true
    kill $AGENT_PID 2>/dev/null || true
    exit 0
}
trap cleanup SIGINT SIGTERM

/opt/fastcp/bin/fastcp --data-dir /opt/fastcp/data --agent-socket /opt/fastcp/run/agent.sock --listen :2087 --log-level debug

# Cleanup
cleanup
