package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/rehmatworks/fastcp/internal/caddy"
	"github.com/rehmatworks/fastcp/internal/database"
	"github.com/rehmatworks/fastcp/internal/middleware"
	"github.com/rehmatworks/fastcp/internal/php"
	"github.com/rehmatworks/fastcp/internal/sites"
	"github.com/rehmatworks/fastcp/internal/static"
	"github.com/rehmatworks/fastcp/internal/upgrade"
)

// Server holds all API handlers and dependencies
type Server struct {
	router         chi.Router
	siteManager    *sites.Manager
	phpManager     *php.Manager
	dbManager      *database.Manager
	caddyGen       *caddy.Generator
	upgradeManager *upgrade.Manager
	logger         *slog.Logger
}

// NewServer creates a new API server
func NewServer(
	siteManager *sites.Manager,
	phpManager *php.Manager,
	dbManager *database.Manager,
	caddyGen *caddy.Generator,
	upgradeManager *upgrade.Manager,
	logger *slog.Logger,
) *Server {
	s := &Server{
		siteManager:    siteManager,
		phpManager:     phpManager,
		dbManager:      dbManager,
		caddyGen:       caddyGen,
		upgradeManager: upgradeManager,
		logger:         logger,
	}

	s.setupRoutes()
	return s
}

// ServeHTTP implements http.Handler
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	r := chi.NewRouter()

	// Middleware
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Timeout(60 * time.Second))

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-API-Key"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check (public)
	r.Get("/health", s.healthCheck)

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		// Public routes
		r.Post("/auth/login", s.login)
		r.Post("/auth/refresh", s.refreshToken)

		// WHMCS integration routes (API key auth)
		r.Route("/whmcs", func(r chi.Router) {
			r.Use(middleware.APIKeyMiddleware)
			r.Post("/provision", s.whmcsProvision)
			r.Get("/status/{service_id}", s.whmcsStatus)
		})

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthMiddleware)

			// Current user
			r.Get("/me", s.getCurrentUser)
			r.Put("/me/password", s.changePassword)

			// Sites
			r.Route("/sites", func(r chi.Router) {
				r.Get("/", s.listSites)
				r.Post("/", s.createSite)
				r.Get("/{id}", s.getSite)
				r.Put("/{id}", s.updateSite)
				r.Delete("/{id}", s.deleteSite)
				r.Post("/{id}/suspend", s.suspendSite)
				r.Post("/{id}/unsuspend", s.unsuspendSite)
				r.Post("/{id}/restart-workers", s.restartSiteWorkers)
			})

			// PHP Instances
			r.Route("/php", func(r chi.Router) {
				r.Get("/", s.listPHPInstances)
				r.Get("/available", s.getAvailablePHPVersions)
				r.Post("/install", s.installPHPVersion)
				r.Get("/{version}", s.getPHPInstance)
				r.Post("/{version}/start", s.startPHPInstance)
				r.Post("/{version}/stop", s.stopPHPInstance)
				r.Post("/{version}/restart", s.restartPHPInstance)
				r.Post("/{version}/restart-workers", s.restartPHPWorkers)
				r.Post("/{version}/download", s.downloadPHPVersion)
				r.Get("/{version}/download/status", s.getDownloadStatus)
			})

			// Databases
			r.Route("/databases", func(r chi.Router) {
				r.Get("/", s.listDatabases)
				r.Post("/", s.createDatabase)
				r.Get("/status", s.getDatabaseStatus)
				r.Post("/install", s.installMySQL)
				r.Get("/install/status", s.getMySQLInstallStatus)
				r.Get("/{id}", s.getDatabase)
				r.Delete("/{id}", s.deleteDatabase)
				r.Post("/{id}/reset-password", s.resetDatabasePassword)
			})

			// Dashboard stats
			r.Get("/stats", s.getStats)

			// Version info (available to all authenticated users)
			r.Get("/version", s.getVersion)

			// Admin-only routes
			r.Group(func(r chi.Router) {
				r.Use(middleware.AdminOnlyMiddleware)

				// User Management
				r.Route("/users", func(r chi.Router) {
					r.Get("/", s.listUsers)
					r.Post("/", s.createUser)
					r.Post("/fix-permissions", s.fixUserPermissions)
					r.Get("/{username}", s.getUser)
					r.Put("/{username}", s.updateUser)
					r.Delete("/{username}", s.deleteUser)
				})

				// API Keys
				r.Route("/api-keys", func(r chi.Router) {
					r.Get("/", s.listAPIKeys)
					r.Post("/", s.createAPIKey)
					r.Delete("/{id}", s.deleteAPIKey)
				})

				// Configuration
				r.Get("/config", s.getConfig)
				r.Put("/config", s.updateConfig)

				// System
				r.Post("/reload", s.reloadAll)

				// Upgrade (admin only)
				r.Route("/upgrade", func(r chi.Router) {
					r.Post("/", s.startUpgrade)
					r.Get("/status", s.getUpgradeStatus)
				})
			})
		})
	})

	// Serve frontend (SPA)
	if static.HasEmbeddedFiles() {
		// Serve embedded React app
		r.Handle("/*", static.Handler())
	} else {
		// Fallback to built-in HTML when React app is not built
		r.Get("/*", s.serveFrontend)
	}

	s.router = r
}

// Response helpers
func (s *Server) json(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("failed to encode response", "error", err)
	}
}

func (s *Server) error(w http.ResponseWriter, status int, message string) {
	s.json(w, status, map[string]string{"error": message})
}

func (s *Server) success(w http.ResponseWriter, data interface{}) {
	s.json(w, http.StatusOK, data)
}

// Health check handler
func (s *Server) healthCheck(w http.ResponseWriter, r *http.Request) {
	s.json(w, http.StatusOK, map[string]string{
		"status":  "healthy",
		"version": "1.0.0",
	})
}

// Serve frontend SPA
func (s *Server) serveFrontend(w http.ResponseWriter, r *http.Request) {
	// Serve a complete login page when React app is not built
	// In production, this would serve the built React app
	html := `<!DOCTYPE html>
<html lang="en" class="dark">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>FastCP - Login</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link href="https://fonts.googleapis.com/css2?family=DM+Sans:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: 'DM Sans', system-ui, sans-serif;
            background: #0a0a0a;
            color: #fafafa;
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 1rem;
        }
        .bg-gradient {
            position: fixed;
            inset: 0;
            overflow: hidden;
            z-index: -1;
        }
        .bg-gradient::before {
            content: '';
            position: absolute;
            top: -50%;
            right: -50%;
            width: 100%;
            height: 100%;
            background: radial-gradient(circle, rgba(16,185,129,0.1) 0%, transparent 70%);
            border-radius: 50%;
        }
        .container { width: 100%; max-width: 400px; }
        .logo {
            display: flex;
            flex-direction: column;
            align-items: center;
            margin-bottom: 2rem;
        }
        .logo-icon {
            width: 64px;
            height: 64px;
            border-radius: 16px;
            background: linear-gradient(135deg, #10b981, #059669);
            display: flex;
            align-items: center;
            justify-content: center;
            margin-bottom: 1rem;
            box-shadow: 0 10px 40px rgba(16,185,129,0.25);
        }
        .logo-icon span {
            color: white;
            font-size: 28px;
            font-weight: 700;
        }
        .logo h1 { font-size: 1.5rem; font-weight: 700; }
        .logo p { color: #888; margin-top: 0.25rem; }
        .card {
            background: #111;
            border: 1px solid #222;
            border-radius: 16px;
            padding: 1.5rem;
        }
        .error {
            background: rgba(239,68,68,0.1);
            border: 1px solid rgba(239,68,68,0.2);
            color: #f87171;
            padding: 0.75rem 1rem;
            border-radius: 8px;
            font-size: 0.875rem;
            margin-bottom: 1.25rem;
            display: none;
        }
        .error.show { display: block; }
        .form-group { margin-bottom: 1.25rem; }
        label {
            display: block;
            font-size: 0.875rem;
            font-weight: 500;
            margin-bottom: 0.5rem;
        }
        input {
            width: 100%;
            padding: 0.625rem 1rem;
            background: #1a1a1a;
            border: 1px solid #333;
            border-radius: 8px;
            color: #fafafa;
            font-size: 1rem;
            font-family: inherit;
            transition: all 0.2s;
        }
        input:focus {
            outline: none;
            border-color: #10b981;
            box-shadow: 0 0 0 3px rgba(16,185,129,0.2);
        }
        input::placeholder { color: #666; }
        button {
            width: 100%;
            padding: 0.75rem 1rem;
            background: linear-gradient(135deg, #10b981, #059669);
            border: none;
            border-radius: 8px;
            color: white;
            font-size: 1rem;
            font-weight: 600;
            font-family: inherit;
            cursor: pointer;
            transition: all 0.2s;
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 0.5rem;
        }
        button:hover { filter: brightness(1.1); }
        button:disabled { opacity: 0.6; cursor: not-allowed; }
        .spinner {
            width: 16px;
            height: 16px;
            border: 2px solid rgba(255,255,255,0.3);
            border-top-color: white;
            border-radius: 50%;
            animation: spin 0.8s linear infinite;
        }
        @keyframes spin { to { transform: rotate(360deg); } }
        .footer {
            margin-top: 1.5rem;
            padding-top: 1.5rem;
            border-top: 1px solid #222;
            text-align: center;
        }
        .footer p { font-size: 0.75rem; color: #666; }
        code {
            background: #1a1a1a;
            padding: 0.125rem 0.375rem;
            border-radius: 4px;
            font-family: monospace;
        }
        .version {
            text-align: center;
            font-size: 0.75rem;
            color: #666;
            margin-top: 1.5rem;
        }
    </style>
</head>
<body>
    <div class="bg-gradient"></div>
    <div class="container">
        <div class="logo">
            <div class="logo-icon"><span>F</span></div>
            <h1>FastCP</h1>
            <p>Sign in to your control panel</p>
        </div>
        <div class="card">
            <div id="error" class="error"></div>
            <form id="loginForm">
                <div class="form-group">
                    <label for="username">Username</label>
                    <input type="text" id="username" placeholder="Enter your username" required>
                </div>
                <div class="form-group">
                    <label for="password">Password</label>
                    <input type="password" id="password" placeholder="Enter your password" required>
                </div>
                <button type="submit" id="submitBtn">
                    <span id="btnText">Sign In</span>
                </button>
            </form>
            <div class="footer">
                <p>Default: <code>admin</code> / <code>fastcp2024!</code></p>
            </div>
        </div>
        <p class="version">FastCP v1.0.0</p>
    </div>
    <script>
        const form = document.getElementById('loginForm');
        const errorDiv = document.getElementById('error');
        const submitBtn = document.getElementById('submitBtn');
        const btnText = document.getElementById('btnText');

        form.addEventListener('submit', async (e) => {
            e.preventDefault();
            errorDiv.classList.remove('show');
            submitBtn.disabled = true;
            btnText.innerHTML = '<div class="spinner"></div> Signing in...';

            try {
                const res = await fetch('/api/v1/auth/login', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        username: document.getElementById('username').value,
                        password: document.getElementById('password').value
                    })
                });
                const data = await res.json();
                if (!res.ok) throw new Error(data.error || 'Login failed');
                localStorage.setItem('fastcp_token', data.token);
                localStorage.setItem('fastcp_user', JSON.stringify(data.user));
                window.location.href = '/dashboard';
            } catch (err) {
                errorDiv.textContent = err.message;
                errorDiv.classList.add('show');
            } finally {
                submitBtn.disabled = false;
                btnText.textContent = 'Sign In';
            }
        });
    </script>
</body>
</html>`

	// Check if this is a dashboard request (user is logged in)
	if r.URL.Path == "/dashboard" || r.URL.Path == "/sites" || r.URL.Path == "/php" || r.URL.Path == "/settings" || (len(r.URL.Path) > 7 && r.URL.Path[:7] == "/sites/") {
		s.serveDashboard(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// serveDashboard serves a basic dashboard when React app is not built
func (s *Server) serveDashboard(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html lang="en" class="dark">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>FastCP - Dashboard</title>
    <link href="https://fonts.googleapis.com/css2?family=DM+Sans:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: 'DM Sans', system-ui, sans-serif;
            background: #0a0a0a;
            color: #fafafa;
            min-height: 100vh;
        }
        .sidebar {
            position: fixed;
            left: 0;
            top: 0;
            bottom: 0;
            width: 240px;
            background: #111;
            border-right: 1px solid #222;
            padding: 1rem;
        }
        .logo {
            display: flex;
            align-items: center;
            gap: 0.75rem;
            padding: 0.5rem;
            margin-bottom: 1.5rem;
        }
        .logo-icon {
            width: 36px;
            height: 36px;
            border-radius: 8px;
            background: linear-gradient(135deg, #10b981, #059669);
            display: flex;
            align-items: center;
            justify-content: center;
        }
        .logo-icon span { color: white; font-weight: 700; }
        .logo-text h1 { font-size: 1.125rem; font-weight: 600; }
        .logo-text p { font-size: 0.75rem; color: #666; }
        nav { display: flex; flex-direction: column; gap: 0.25rem; }
        nav a {
            display: flex;
            align-items: center;
            gap: 0.75rem;
            padding: 0.625rem 0.75rem;
            border-radius: 8px;
            color: #888;
            text-decoration: none;
            font-size: 0.875rem;
            transition: all 0.2s;
        }
        nav a:hover { background: #1a1a1a; color: #fafafa; }
        nav a.active { background: rgba(16,185,129,0.1); color: #10b981; }
        .main { margin-left: 240px; padding: 1.5rem; }
        .header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 1.5rem;
        }
        .header h2 { font-size: 1.5rem; font-weight: 700; }
        .btn {
            padding: 0.5rem 1rem;
            background: linear-gradient(135deg, #10b981, #059669);
            border: none;
            border-radius: 8px;
            color: white;
            font-weight: 600;
            cursor: pointer;
            text-decoration: none;
            font-size: 0.875rem;
        }
        .btn-secondary {
            background: #222;
        }
        .stats {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 1rem;
            margin-bottom: 1.5rem;
        }
        .stat-card {
            background: #111;
            border: 1px solid #222;
            border-radius: 12px;
            padding: 1.25rem;
        }
        .stat-card p { font-size: 0.875rem; color: #666; }
        .stat-card h3 { font-size: 1.75rem; font-weight: 700; margin-top: 0.25rem; }
        .stat-card span { font-size: 0.75rem; color: #666; }
        .card {
            background: #111;
            border: 1px solid #222;
            border-radius: 12px;
            overflow: hidden;
        }
        .card-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 1rem 1.25rem;
            border-bottom: 1px solid #222;
        }
        .card-header h3 { font-weight: 600; }
        .list-item {
            display: flex;
            align-items: center;
            gap: 1rem;
            padding: 1rem 1.25rem;
            border-bottom: 1px solid #222;
        }
        .list-item:last-child { border-bottom: none; }
        .icon {
            width: 36px;
            height: 36px;
            border-radius: 8px;
            background: rgba(16,185,129,0.1);
            border: 1px solid rgba(16,185,129,0.2);
            display: flex;
            align-items: center;
            justify-content: center;
            color: #10b981;
        }
        .badge {
            font-size: 0.75rem;
            padding: 0.25rem 0.5rem;
            border-radius: 999px;
            background: rgba(16,185,129,0.1);
            color: #10b981;
            border: 1px solid rgba(16,185,129,0.2);
        }
        .badge.stopped {
            background: rgba(234,179,8,0.1);
            color: #eab308;
            border-color: rgba(234,179,8,0.2);
        }
        .empty {
            padding: 3rem;
            text-align: center;
            color: #666;
        }
        #sites-list, #php-list { min-height: 100px; }
        .logout-btn {
            position: absolute;
            bottom: 1rem;
            left: 1rem;
            right: 1rem;
            padding: 0.625rem;
            background: #1a1a1a;
            border: none;
            border-radius: 8px;
            color: #888;
            cursor: pointer;
            font-family: inherit;
        }
        .logout-btn:hover { background: #222; color: #fafafa; }
    </style>
</head>
<body>
    <div class="sidebar">
        <div class="logo">
            <div class="logo-icon"><span>F</span></div>
            <div class="logo-text">
                <h1>FastCP</h1>
                <p>Control Panel</p>
            </div>
        </div>
        <nav>
            <a href="/dashboard" class="active">📊 Dashboard</a>
            <a href="/sites">🌐 Sites</a>
            <a href="/php">⚡ PHP</a>
            <a href="/settings">⚙️ Settings</a>
        </nav>
        <button class="logout-btn" onclick="logout()">🚪 Logout</button>
    </div>
    <div class="main">
        <div class="header">
            <h2>Dashboard</h2>
            <a href="/sites/new" class="btn">+ New Site</a>
        </div>
        <div class="stats" id="stats">
            <div class="stat-card">
                <p>Total Sites</p>
                <h3 id="total-sites">-</h3>
                <span id="active-sites">- active</span>
            </div>
            <div class="stat-card">
                <p>PHP Instances</p>
                <h3 id="php-count">-</h3>
                <span>configured</span>
            </div>
            <div class="stat-card">
                <p>Memory Usage</p>
                <h3 id="memory">-</h3>
                <span id="memory-total">-</span>
            </div>
            <div class="stat-card">
                <p>Uptime</p>
                <h3 id="uptime">-</h3>
                <span>system running</span>
            </div>
        </div>
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 1rem;">
            <div class="card">
                <div class="card-header">
                    <h3>Recent Sites</h3>
                    <a href="/sites" style="color: #10b981; font-size: 0.875rem;">View all →</a>
                </div>
                <div id="sites-list"><div class="empty">Loading...</div></div>
            </div>
            <div class="card">
                <div class="card-header">
                    <h3>PHP Instances</h3>
                    <a href="/php" style="color: #10b981; font-size: 0.875rem;">Manage →</a>
                </div>
                <div id="php-list"><div class="empty">Loading...</div></div>
            </div>
        </div>
    </div>
    <script>
        const token = localStorage.getItem('fastcp_token');
        if (!token) window.location.href = '/';

        async function api(endpoint) {
            const res = await fetch('/api/v1' + endpoint, {
                headers: { 'Authorization': 'Bearer ' + token }
            });
            if (res.status === 401) { logout(); return null; }
            return res.json();
        }

        function formatBytes(bytes) {
            if (bytes === 0) return '0 B';
            const k = 1024, sizes = ['B', 'KB', 'MB', 'GB'];
            const i = Math.floor(Math.log(bytes) / Math.log(k));
            return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
        }

        function formatUptime(seconds) {
            const d = Math.floor(seconds / 86400);
            const h = Math.floor((seconds % 86400) / 3600);
            const m = Math.floor((seconds % 3600) / 60);
            if (d > 0) return d + 'd ' + h + 'h';
            if (h > 0) return h + 'h ' + m + 'm';
            return m + 'm';
        }

        async function loadData() {
            const [stats, sites, php] = await Promise.all([
                api('/stats'),
                api('/sites'),
                api('/php')
            ]);

            if (stats) {
                document.getElementById('total-sites').textContent = stats.total_sites || 0;
                document.getElementById('active-sites').textContent = (stats.active_sites || 0) + ' active';
                document.getElementById('php-count').textContent = stats.php_instances || 0;
                document.getElementById('memory').textContent = formatBytes(stats.memory_usage || 0);
                document.getElementById('memory-total').textContent = 'of ' + formatBytes(stats.memory_total || 0);
                document.getElementById('uptime').textContent = formatUptime(stats.uptime || 0);
            }

            const sitesList = document.getElementById('sites-list');
            if (sites && sites.sites && sites.sites.length > 0) {
                sitesList.innerHTML = sites.sites.slice(0, 5).map(s => 
                    '<div class="list-item"><div class="icon">🌐</div><div style="flex:1"><strong>' + s.name + '</strong><br><span style="color:#666;font-size:0.875rem">' + s.domain + '</span></div><span class="badge">' + s.status + '</span></div>'
                ).join('');
            } else {
                sitesList.innerHTML = '<div class="empty">No sites yet. <a href="/sites/new" style="color:#10b981">Create one</a></div>';
            }

            const phpList = document.getElementById('php-list');
            if (php && php.instances && php.instances.length > 0) {
                phpList.innerHTML = php.instances.map(p => 
                    '<div class="list-item"><div class="icon">⚡</div><div style="flex:1"><strong>PHP ' + p.version + '</strong><br><span style="color:#666;font-size:0.875rem">' + p.site_count + ' sites • Port ' + p.port + '</span></div><span class="badge ' + (p.status !== 'running' ? 'stopped' : '') + '">' + p.status + '</span></div>'
                ).join('');
            } else {
                phpList.innerHTML = '<div class="empty">No PHP instances configured</div>';
            }
        }

        function logout() {
            localStorage.removeItem('fastcp_token');
            localStorage.removeItem('fastcp_user');
            window.location.href = '/';
        }

        loadData();
    </script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

