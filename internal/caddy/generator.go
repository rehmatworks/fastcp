package caddy

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/rehmatworks/fastcp/internal/config"
	"github.com/rehmatworks/fastcp/internal/models"
)

// Generator generates Caddyfile configurations
type Generator struct {
	templatesDir string
	outputDir    string
}

// NewGenerator creates a new Caddyfile generator
func NewGenerator(templatesDir, outputDir string) *Generator {
	return &Generator{
		templatesDir: templatesDir,
		outputDir:    outputDir,
	}
}

// GenerateMainProxy generates the main reverse proxy Caddyfile
func (g *Generator) GenerateMainProxy(sites []models.Site, phpVersions []models.PHPVersionConfig, httpPort, httpsPort int) (string, error) {
	var buf bytes.Buffer
	cfg := config.Get()

	logPath := filepath.Join(cfg.LogDir, "caddy-proxy.log")
	isDevMode := config.IsDevMode()

	// Global options
	buf.WriteString(`# FastCP Main Proxy Configuration
# Auto-generated - Do not edit manually

{
	admin localhost:2019
`)

	if isDevMode {
		// Disable automatic HTTPS for local development
		buf.WriteString(`	
	# Development mode - disable automatic HTTPS
	auto_https off
`)
	} else {
		// Production - enable automatic HTTPS with Let's Encrypt
		email := cfg.AdminEmail
		if email == "" {
			email = "support@fastcp.org" // Default FastCP email
		}
		buf.WriteString(fmt.Sprintf(`	
	# Production mode - automatic HTTPS via Let's Encrypt
	email %s
`, email))
	}

	buf.WriteString(fmt.Sprintf(`	
	# HTTP port configuration
	http_port %d
	https_port %d
	
	log {
		output file %s {
			roll_size 100mb
			roll_keep 5
		}
		format json
	}
}

`, httpPort, httpsPort, logPath))

	// Find port for each PHP version
	versionPorts := make(map[string]int)
	for _, pv := range phpVersions {
		if pv.Enabled {
			versionPorts[pv.Version] = pv.Port
		}
	}

	// Gateway error page (502, 503, 504)
	gatewayErrorPage := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Service Unavailable - FastCP</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: 'Inter', system-ui, sans-serif;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            background: linear-gradient(135deg, #0f172a 0%, #1e293b 50%, #0f172a 100%);
            color: #f8fafc;
            padding: 1.5rem;
        }
        .container { text-align: center; max-width: 480px; }
        .logo {
            width: 80px; height: 80px;
            margin: 0 auto 1.5rem;
            background: linear-gradient(135deg, #ef4444, #dc2626);
            border-radius: 20px;
            display: flex; align-items: center; justify-content: center;
            box-shadow: 0 20px 50px rgba(239, 68, 68, 0.3);
        }
        .logo span { font-size: 2.5rem; font-weight: 700; color: white; }
        .error-code {
            font-size: 4rem; font-weight: 700;
            background: linear-gradient(135deg, #f8fafc, #64748b);
            -webkit-background-clip: text; -webkit-text-fill-color: transparent;
            background-clip: text; margin-bottom: 0.5rem;
        }
        h1 { font-size: 1.5rem; font-weight: 600; color: #f8fafc; margin-bottom: 1.5rem; }
        .card {
            background: rgba(255,255,255,0.03);
            border: 1px solid rgba(255,255,255,0.06);
            border-radius: 16px; padding: 1.5rem; margin-bottom: 1.5rem;
            backdrop-filter: blur(10px);
        }
        .card p { color: #94a3b8; line-height: 1.7; font-size: 0.95rem; }
        .status {
            display: inline-flex; align-items: center; gap: 0.5rem;
            background: rgba(239, 68, 68, 0.1);
            border: 1px solid rgba(239, 68, 68, 0.2);
            color: #f87171; padding: 0.5rem 1rem;
            border-radius: 999px; font-size: 0.85rem; font-weight: 500;
            margin-top: 1rem;
        }
        .status::before {
            content: ''; width: 8px; height: 8px;
            background: #ef4444; border-radius: 50%;
            animation: pulse 1s infinite;
        }
        @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.3; } }
        .tips { text-align: left; margin-top: 1rem; }
        .tip {
            display: flex; align-items: flex-start; gap: 0.75rem;
            margin-bottom: 0.5rem; color: #cbd5e1; font-size: 0.85rem;
        }
        .tip-icon { color: #f59e0b; }
        .footer { margin-top: 1.5rem; color: #475569; font-size: 0.8rem; }
        .footer a { color: #10b981; text-decoration: none; }
    </style>
</head>
<body>
    <div class="container">
        <div class="logo"><span>!</span></div>
        <div class="error-code">502</div>
        <h1>PHP Not Responding</h1>
        
        <div class="card">
            <p>The PHP server for this site is not responding. This usually means FrankenPHP is not running or is overloaded.</p>
            <span class="status">Service Unavailable</span>
            <div class="tips">
                <div class="tip"><span class="tip-icon">→</span> Check if PHP instance is running in FastCP</div>
                <div class="tip"><span class="tip-icon">→</span> Try restarting the PHP instance</div>
                <div class="tip"><span class="tip-icon">→</span> Check server logs for errors</div>
            </div>
        </div>
        
        <p class="footer">Managed by <a href="https://fastcp.org" target="_blank">FastCP</a></p>
    </div>
</body>
</html>`

	// Generate site blocks for each active site
	for _, site := range sites {
		if site.Status != "active" {
			continue
		}

		// Check if this PHP version exists
		_, ok := versionPorts[site.PHPVersion]
		if !ok {
			continue
		}

		// Get Unix socket path for this PHP version
		socketPath := GetPHPSocketPath(site.PHPVersion)

		// Domain(s) for this site
		domains := []string{site.Domain}
		domains = append(domains, site.Aliases...)

		buf.WriteString(fmt.Sprintf("# Site: %s (PHP %s)\n", site.Name, site.PHPVersion))

		if isDevMode {
			// Development: Use http:// prefix to disable automatic HTTPS
			for i, d := range domains {
				domains[i] = "http://" + d
			}
		}
		// Production: Just use domain names, Caddy will auto-provision SSL

		buf.WriteString(strings.Join(domains, ", "))
		buf.WriteString(" {\n")

		// Reverse proxy to PHP instance via Unix socket with error handling
		buf.WriteString(fmt.Sprintf("\treverse_proxy unix/%s {\n", socketPath))
		buf.WriteString("\t\t@error status 502 503 504\n")
		buf.WriteString("\t\thandle_response @error {\n")
		buf.WriteString("\t\t\theader Content-Type text/html\n")
		buf.WriteString(fmt.Sprintf("\t\t\trespond %s {resp.status_code}\n", "`"+gatewayErrorPage+"`"))
		buf.WriteString("\t\t}\n")
		buf.WriteString("\t}\n")

		buf.WriteString("}\n\n")
	}

	// Default fallback for unmatched domains (catch-all on the HTTP port)
	notFoundPage := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Site Not Configured - FastCP</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: 'Inter', system-ui, sans-serif;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            background: linear-gradient(135deg, #0f172a 0%, #1e293b 50%, #0f172a 100%);
            color: #f8fafc;
            padding: 1.5rem;
        }
        .container {
            text-align: center;
            max-width: 480px;
        }
        .logo {
            width: 80px;
            height: 80px;
            margin: 0 auto 1.5rem;
            background: linear-gradient(135deg, #64748b, #475569);
            border-radius: 20px;
            display: flex;
            align-items: center;
            justify-content: center;
            box-shadow: 0 20px 50px rgba(100, 116, 139, 0.2);
        }
        .logo span {
            font-size: 2.5rem;
            font-weight: 700;
            color: white;
        }
        .error-code {
            font-size: 4rem;
            font-weight: 700;
            background: linear-gradient(135deg, #f8fafc, #64748b);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            background-clip: text;
            margin-bottom: 0.5rem;
        }
        h1 {
            font-size: 1.5rem;
            font-weight: 600;
            color: #f8fafc;
            margin-bottom: 0.5rem;
        }
        .domain {
            color: #f59e0b;
            font-size: 0.95rem;
            font-family: monospace;
            background: rgba(245, 158, 11, 0.1);
            border: 1px solid rgba(245, 158, 11, 0.2);
            padding: 0.5rem 1rem;
            border-radius: 8px;
            display: inline-block;
            margin: 1rem 0;
        }
        .card {
            background: rgba(255,255,255,0.03);
            border: 1px solid rgba(255,255,255,0.06);
            border-radius: 16px;
            padding: 1.5rem;
            margin-bottom: 1.5rem;
            backdrop-filter: blur(10px);
        }
        .card p {
            color: #94a3b8;
            line-height: 1.7;
            font-size: 0.95rem;
        }
        .steps {
            text-align: left;
            margin-top: 1rem;
        }
        .step {
            display: flex;
            align-items: flex-start;
            gap: 0.75rem;
            margin-bottom: 0.75rem;
            color: #cbd5e1;
            font-size: 0.9rem;
        }
        .step-num {
            width: 24px;
            height: 24px;
            background: rgba(16, 185, 129, 0.1);
            border: 1px solid rgba(16, 185, 129, 0.2);
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 0.75rem;
            font-weight: 600;
            color: #10b981;
            flex-shrink: 0;
        }
        .footer {
            color: #475569;
            font-size: 0.8rem;
        }
        .footer a {
            color: #10b981;
            text-decoration: none;
        }
        .footer a:hover {
            text-decoration: underline;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="logo"><span>F</span></div>
        <div class="error-code">404</div>
        <h1>Site Not Configured</h1>
        <div class="domain">{http.request.host}</div>
        
        <div class="card">
            <p>This domain is not configured on this server.</p>
            <div class="steps">
                <div class="step">
                    <span class="step-num">1</span>
                    <span>Log in to the FastCP control panel</span>
                </div>
                <div class="step">
                    <span class="step-num">2</span>
                    <span>Create a new site with this domain</span>
                </div>
                <div class="step">
                    <span class="step-num">3</span>
                    <span>Point your DNS to this server</span>
                </div>
            </div>
        </div>
        
        <p class="footer">
            Powered by <a href="https://fastcp.org" target="_blank">FastCP</a>
        </p>
    </div>
</body>
</html>`

	buf.WriteString(fmt.Sprintf(`# Default fallback for unmatched domains
:%d {
	header Content-Type text/html
	respond %s 404
}
`, httpPort, "`"+notFoundPage+"`"))

	return buf.String(), nil
}

// GetPHPSocketPath returns the Unix socket path for a PHP version
func GetPHPSocketPath(version string) string {
	return fmt.Sprintf("/var/run/fastcp/php-%s.sock", version)
}

// GeneratePHPInstance generates a Caddyfile for a specific PHP version instance
// Uses Unix socket instead of TCP port for better security and no port binding needed
func (g *Generator) GeneratePHPInstance(version string, port, adminPort int, sites []models.Site) (string, error) {
	var buf bytes.Buffer
	cfg := config.Get()

	// Filter sites for this PHP version
	var versionSites []models.Site
	for _, site := range sites {
		if site.PHPVersion == version && site.Status == "active" {
			versionSites = append(versionSites, site)
		}
	}

	logPath := filepath.Join(cfg.LogDir, fmt.Sprintf("php-%s.log", version))
	socketPath := GetPHPSocketPath(version)

	// Global options - using Unix socket instead of port
	buf.WriteString(fmt.Sprintf(`# FastCP PHP %s Instance Configuration
# Auto-generated - Do not edit manually

{
	admin unix//var/run/fastcp/php-%s-admin.sock
	
	frankenphp
	
	log {
		output file %s {
			roll_size 100mb
			roll_keep 5
		}
		format json
	}
}

`, version, version, logPath))

	// If no sites, create a minimal placeholder config
	if len(versionSites) == 0 {
		buf.WriteString(fmt.Sprintf("# No sites configured for PHP %s\n", version))
		buf.WriteString("http:// {\n")
		buf.WriteString(fmt.Sprintf("\tbind unix/%s\n", socketPath))
		buf.WriteString("\trespond \"No sites configured\" 503\n")
		buf.WriteString("}\n")
		return buf.String(), nil
	}

	// Generate a single server block for all sites, listening on Unix socket
	buf.WriteString("http:// {\n")
	buf.WriteString(fmt.Sprintf("\tbind unix/%s\n\n", socketPath))

	for _, site := range versionSites {
		domains := []string{site.Domain}
		domains = append(domains, site.Aliases...)
		matcherName := sanitizeName(site.ID)

		buf.WriteString(fmt.Sprintf("\n\t# Site: %s (%s)\n", site.Name, site.Domain))

		// Match specific domains
		buf.WriteString(fmt.Sprintf("\t@%s host %s\n", matcherName, strings.Join(domains, " ")))
		buf.WriteString(fmt.Sprintf("\thandle @%s {\n", matcherName))

		// Root directory
		rootPath := filepath.Join(site.RootPath, site.PublicPath)
		buf.WriteString(fmt.Sprintf("\t\troot * %s\n", rootPath))

		// Encoding
		buf.WriteString("\t\tencode zstd br gzip\n")

		// PHP server directive with optional worker mode
		if site.WorkerMode && site.WorkerFile != "" {
			workerNum := site.WorkerNum
			if workerNum <= 0 {
				workerNum = 2
			}
			// Worker file path must be absolute
			workerPath := site.WorkerFile
			if !filepath.IsAbs(workerPath) {
				workerPath = filepath.Join(rootPath, workerPath)
			}
			
			// Safety check: verify worker file exists to prevent breaking all sites
			if _, err := os.Stat(workerPath); err != nil {
				// Worker file doesn't exist - fall back to regular php_server
				buf.WriteString("\t\t# WARNING: Worker file not found, falling back to regular mode\n")
				buf.WriteString("\t\t# Expected: " + workerPath + "\n")
				buf.WriteString("\t\tphp_server\n")
			} else {
				buf.WriteString("\t\tphp_server {\n")
				buf.WriteString(fmt.Sprintf("\t\t\tworker %s %d\n", workerPath, workerNum))

				// Add environment variables
				for key, value := range site.Environment {
					buf.WriteString(fmt.Sprintf("\t\t\tenv %s %s\n", key, value))
				}

				buf.WriteString("\t\t}\n")
			}
		} else {
			buf.WriteString("\t\tphp_server")
			if len(site.Environment) > 0 {
				buf.WriteString(" {\n")
				for key, value := range site.Environment {
					buf.WriteString(fmt.Sprintf("\t\t\tenv %s %s\n", key, value))
				}
				buf.WriteString("\t\t}")
			}
			buf.WriteString("\n")
		}

		buf.WriteString("\t}\n")
	}

	// Default fallback for unmatched hosts
	buf.WriteString("\n\t# Default fallback\n")
	buf.WriteString("\thandle {\n")
	buf.WriteString("\t\trespond \"Site not found\" 404\n")
	buf.WriteString("\t}\n")

	buf.WriteString("}\n")

	return buf.String(), nil
}

// GenerateSiteConfig generates an individual site configuration
func (g *Generator) GenerateSiteConfig(site *models.Site) (string, error) {
	tmplContent := `# Site: {{.Name}}
# Domain: {{.Domain}}
# PHP Version: {{.PHPVersion}}
# Generated by FastCP

{{.Domain}}{{range .Aliases}}, {{.}}{{end}} {
	root * {{.RootPath}}/{{.PublicPath}}
	
	encode zstd br gzip
	
	{{if .WorkerMode}}
	php_server {
		worker {{.WorkerFile}} {{if .WorkerNum}}{{.WorkerNum}}{{else}}2{{end}}
		{{range $key, $value := .Environment}}
		env {{$key}} {{$value}}
		{{end}}
	}
	{{else}}
	php_server{{if .Environment}} {
		{{range $key, $value := .Environment}}
		env {{$key}} {{$value}}
		{{end}}
	}{{end}}
	{{end}}
	
	log {
		output file /var/log/fastcp/sites/{{.ID}}/access.log {
			roll_size 50mb
			roll_keep 3
		}
	}
}
`

	tmpl, err := template.New("site").Parse(tmplContent)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, site); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// WriteMainProxy writes the main proxy Caddyfile
func (g *Generator) WriteMainProxy(content string) error {
	path := filepath.Join(g.outputDir, "Caddyfile.proxy")
	return g.writeFile(path, content)
}

// WritePHPInstance writes a PHP instance Caddyfile
func (g *Generator) WritePHPInstance(version, content string) error {
	path := filepath.Join(g.outputDir, fmt.Sprintf("Caddyfile.php-%s", version))
	return g.writeFile(path, content)
}

// writeFile writes content to a file
func (g *Generator) writeFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// sanitizeName converts a string to a valid Caddy matcher name
func sanitizeName(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "-", "_"), ".", "_")
}

