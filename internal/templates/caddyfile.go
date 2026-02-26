package templates

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// SiteConfig holds configuration for a single site
type SiteConfig struct {
	Domain       string
	Username     string
	DocumentRoot string
	PHPEnabled   bool
	SSLEnabled   bool
	Aliases      []string
}

// MainCaddyfileConfig holds the main Caddyfile configuration
type MainCaddyfileConfig struct {
	AdminEmail  string
	Sites       []SiteConfig
	DataDir     string
	AgentSocket string
	NumThreads  int
}

const mainCaddyfileTemplate = `{
    # Global options
    admin localhost:2019
    {{if .AdminEmail}}email {{.AdminEmail}}{{end}}
    
    # FastCP configuration
    fastcp {
        data_dir {{.DataDir}}
        agent_socket {{.AgentSocket}}
        listen_port 2050
    }
    
    # PHP execution is handled by PHP-FPM pools managed by FastCP
}

# FastCP Control Panel
:2050 {
    tls internal
    fastcp_ui
    
    log {
        output file /var/log/fastcp/panel.log
        format json
    }
}

{{range .Sites}}
# Site: {{.Domain}} (User: {{.Username}})
{{.Domain}}{{range .Aliases}}, {{.}}{{end}} {
    {{if .SSLEnabled}}tls {{else}}tls off{{end}}
    
    root * {{.DocumentRoot}}
    
    {{if .PHPEnabled}}
    php_server {
        resolve_root_symlink false
    }
    {{else}}
    file_server
    {{end}}
    
    log {
        output file /home/{{.Username}}/apps/{{.Domain | safeDomain}}/logs/access.log
        format json
    }
    
    # Security headers
    header {
        X-Content-Type-Options nosniff
        X-Frame-Options DENY
        X-XSS-Protection "1; mode=block"
        Referrer-Policy strict-origin-when-cross-origin
        -Server
    }
}

{{end}}
`

// GenerateMainCaddyfile generates the main Caddyfile
func GenerateMainCaddyfile(config *MainCaddyfileConfig) (string, error) {
	funcMap := template.FuncMap{
		"safeDomain": func(domain string) string {
			return strings.ReplaceAll(domain, ".", "_")
		},
	}

	tmpl, err := template.New("caddyfile").Funcs(funcMap).Parse(mainCaddyfileTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

const userCaddyfileTemplate = `{
    # User: {{.Username}} PHP configuration
    admin off
}

:0 {
    bind unix//home/{{.Username}}/.fastcp/fastcp.sock|0660
    
    {{range .Sites}}
    @{{.Domain | safeName}} host {{.Domain}}{{range .Aliases}} {{.}}{{end}}
    handle @{{.Domain | safeName}} {
        root * {{.DocumentRoot}}
        php_server
    }
    {{end}}
}
`

// UserCaddyfileConfig holds the per-user Caddyfile configuration
type UserCaddyfileConfig struct {
	Username   string
	NumThreads int
	Sites      []SiteConfig
}

// GenerateUserCaddyfile generates a per-user Caddyfile
func GenerateUserCaddyfile(config *UserCaddyfileConfig) (string, error) {
	funcMap := template.FuncMap{
		"safeName": func(domain string) string {
			safe := strings.ReplaceAll(domain, ".", "_")
			safe = strings.ReplaceAll(safe, "-", "_")
			return safe
		},
	}

	tmpl, err := template.New("user_caddyfile").Funcs(funcMap).Parse(userCaddyfileTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}
