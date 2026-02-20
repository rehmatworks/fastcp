#!/bin/bash
# Development runner script - builds and runs FastCP inside Docker

set -e

cd /app

echo "Building FastCP..."
go build -o /opt/fastcp/bin/fastcp ./cmd/fastcp
go build -o /opt/fastcp/bin/fastcp-agent ./cmd/fastcp-agent

# Kill any existing processes
pkill -9 fastcp-agent 2>/dev/null || true
pkill -9 fastcp 2>/dev/null || true
pkill -9 frankenphp 2>/dev/null || true
rm -f /var/run/fastcp/agent.sock

sleep 1

echo "Starting FastCP Agent..."
/opt/fastcp/bin/fastcp-agent --socket /var/run/fastcp/agent.sock --log-level debug &
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
echo "  Control Panel: http://localhost:2087"
echo "  Your Sites:    http://localhost:8080 (mapped to port 80)"
echo ""
echo "  Test credentials:"
echo "    root / rootpass (admin)"
echo "    testuser / testpass (regular user)"
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

/opt/fastcp/bin/fastcp --data-dir /opt/fastcp/data --agent-socket /var/run/fastcp/agent.sock --listen :2087 --log-level debug

# Cleanup
cleanup
