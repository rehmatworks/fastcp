package static

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// HasEmbeddedFiles checks if the React app was embedded during build
func HasEmbeddedFiles() bool {
	// Check for index.html specifically (not just any file like .gitkeep)
	_, err := distFS.ReadFile("dist/index.html")
	return err == nil
}

// Handler returns an http.Handler for serving the embedded static files
func Handler() http.Handler {
	// Strip the "dist" prefix
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Serve static assets directly
		if strings.HasPrefix(path, "/assets/") || 
		   strings.HasSuffix(path, ".js") || 
		   strings.HasSuffix(path, ".css") || 
		   strings.HasSuffix(path, ".svg") ||
		   strings.HasSuffix(path, ".ico") ||
		   strings.HasSuffix(path, ".png") ||
		   strings.HasSuffix(path, ".jpg") ||
		   strings.HasSuffix(path, ".woff") ||
		   strings.HasSuffix(path, ".woff2") {
			fileServer.ServeHTTP(w, r)
			return
		}

		// For SPA: serve index.html for all other routes
		indexHTML, err := sub.(fs.ReadFileFS).ReadFile("index.html")
		if err != nil {
			http.Error(w, "index.html not found", http.StatusInternalServerError)
			return
		}
		
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
}

