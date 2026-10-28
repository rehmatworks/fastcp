package agent

// Filesystem layout and service identities shared by the agent, the
// installer and the migration.
const (
	BaseDir           = "/opt/fastcp"
	BinDir            = "/opt/fastcp/bin"
	DataDir           = "/opt/fastcp/data"
	ConfigDir         = "/opt/fastcp/config"
	RunDir            = "/opt/fastcp/run"
	PHPSocketRoot     = "/opt/fastcp/run/php"
	PrivateTmpDir     = "/opt/fastcp/tmp"
	SpoolRoot         = "/opt/fastcp/spool"
	ExportSpoolDir    = "/opt/fastcp/spool/exports"
	SSLDir            = "/opt/fastcp/ssl"
	PMADir            = "/opt/fastcp/phpmyadmin"
	PMATmpDir         = "/opt/fastcp/run/phpmyadmin-tmp"
	WWWDir            = "/opt/fastcp/www"
	SuspendedDir      = "/opt/fastcp/www/suspended"
	CaddyfilePath     = "/opt/fastcp/config/Caddyfile"
	StatePath         = "/opt/fastcp/config/state.json"
	CaddySettingsPath = "/opt/fastcp/config/caddy-settings.json"
	PHPSettingsPath   = "/opt/fastcp/config/php.json"
	JobsPath          = "/opt/fastcp/config/jobs.json"
	MigrationsDir     = "/opt/fastcp/config/migrations"
	CaddyStorageDir   = "/var/lib/fastcp/caddy"
	CaddyRuntimeDir   = "/run/fastcp-caddy"
	CaddyAdminSocket  = "/run/fastcp-caddy/admin.sock"
	CaddyBinary       = "/usr/local/bin/caddy"
	LogDir            = "/var/log/fastcp"
	SiteLogRoot       = "/var/log/fastcp/sites"
	DefaultSocket     = "/opt/fastcp/run/agent.sock"
	HomeRoot          = "/home"
	MOTDScript        = "/etc/update-motd.d/99-fastcp"
	MOTDVersionFile   = "/opt/fastcp/config/motd-version"

	// LegacyUserConfigDir held per-user Caddyfiles in FastCP 2.
	LegacyUserConfigDir = "/opt/fastcp/config/users"

	PanelUser = "fastcp-panel"
	CaddyUser = "caddy"
	PMAUser   = "www-data"

	PanelPort = 2050
	PMAPort   = 2088
)
