#!/bin/bash
# Installs FastCP on first start, then runs it without systemd.
set -euo pipefail

if [[ ! -x /opt/fastcp/bin/fastcp-agent ]]; then
    cd /usr/src/fastcp
    bash ./install.sh
else
    service mysql start >/dev/null 2>&1 || true
    mkdir -p /var/log/fastcp
    /opt/fastcp/bin/fastcp-agent >>/var/log/fastcp/agent.log 2>&1 &
    sleep 2
    runuser -u fastcp-panel -- /opt/fastcp/bin/fastcp --listen :2050 \
        --data-dir /opt/fastcp/data --agent-socket /opt/fastcp/run/agent.sock \
        --tls-cert /opt/fastcp/ssl/server.crt --tls-key /opt/fastcp/ssl/server.key >>/var/log/fastcp/panel.log 2>&1 &
fi

exec tail -n +1 -F /var/log/fastcp/agent.log /var/log/fastcp/panel.log
