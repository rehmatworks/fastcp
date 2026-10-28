package agent

import "fmt"

// Systemd units and other system files owned by FastCP. They are written by
// `fastcp-agent setup` (installer, updates) and on every agent start.

const unitDir = "/etc/systemd/system"

var agentUnit = fmt.Sprintf(`# Managed by FastCP.
[Unit]
Description=FastCP agent
After=network-online.target mysql.service mariadb.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s/fastcp-agent --socket %s
Restart=always
RestartSec=3
# Package installs started by the agent must survive an agent restart.
KillMode=process
TimeoutStopSec=60

[Install]
WantedBy=multi-user.target
`, BinDir, DefaultSocket)

var panelUnit = fmt.Sprintf(`# Managed by FastCP.
[Unit]
Description=FastCP control panel
After=network-online.target fastcp-agent.service
Wants=network-online.target fastcp-agent.service

[Service]
Type=simple
User=%[1]s
Group=%[1]s
ExecStart=%[2]s/fastcp --listen :%[3]d --data-dir %[4]s --agent-socket %[5]s --tls-cert %[6]s/server.crt --tls-key %[6]s/server.key
Restart=always
RestartSec=3
UMask=0077
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
SystemCallArchitectures=native
CapabilityBoundingSet=
AmbientCapabilities=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
ReadWritePaths=%[4]s %[7]s

[Install]
WantedBy=multi-user.target
`, PanelUser, BinDir, PanelPort, DataDir, DefaultSocket, SSLDir, SpoolRoot)

var caddyUnit = fmt.Sprintf(`# Managed by FastCP.
[Unit]
Description=FastCP web server (Caddy)
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
User=%[1]s
Group=%[1]s
ExecStart=%[2]s run --config %[3]s --adapter caddyfile
ExecReload=%[2]s reload --config %[3]s --adapter caddyfile --force
Environment=HOME=%[4]s XDG_DATA_HOME=/var/lib/fastcp XDG_CONFIG_HOME=%[4]s/.config
RuntimeDirectory=fastcp-caddy
RuntimeDirectoryMode=0700
Restart=on-failure
RestartSec=3
TimeoutStopSec=10
LimitNOFILE=1048576
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectSystem=strict
ProtectHome=read-only
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
ReadWritePaths=%[4]s %[5]s

[Install]
WantedBy=multi-user.target
`, CaddyUser, CaddyBinary, CaddyfilePath, CaddyStorageDir, SiteLogRoot)

const pamConfig = `# Managed by FastCP. Used to verify control panel logins.
@include common-auth
@include common-account
`

// units maps unit file names to their content.
func units() map[string]string {
	return map[string]string{
		"fastcp-agent.service": agentUnit,
		"fastcp.service":       panelUnit,
		"fastcp-caddy.service": caddyUnit,
	}
}
