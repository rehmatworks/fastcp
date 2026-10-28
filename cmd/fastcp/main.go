// Command fastcp is the FastCP control panel. It runs as the unprivileged
// fastcp-panel user and asks fastcp-agent for anything that needs root.
package main

import (
	"context"
	"crypto/tls"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/api"
	"github.com/rehmatworks/fastcp/internal/crypto"
	"github.com/rehmatworks/fastcp/internal/database"
	"github.com/rehmatworks/fastcp/internal/version"
)

//go:embed ui/dist
var uiFS embed.FS

func main() {
	listen := flag.String("listen", ":2050", "listen address")
	dataDir := flag.String("data-dir", "/opt/fastcp/data", "data directory")
	agentSocket := flag.String("agent-socket", agent.DefaultSocket, "agent socket path")
	tlsCert := flag.String("tls-cert", "/opt/fastcp/ssl/server.crt", "TLS certificate")
	tlsKey := flag.String("tls-key", "/opt/fastcp/ssl/server.key", "TLS private key")
	noTLS := flag.Bool("no-tls", false, "serve plain HTTP (development only)")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn or error")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("FastCP %s (built %s)\n", version.Version, version.BuildTime)
		return
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	dev := os.Getenv("FASTCP_DEV") == "1"
	if os.Geteuid() == 0 && !dev {
		fail(errors.New("the FastCP 3 panel does not run as root; it runs as fastcp-panel. Run `/opt/fastcp/bin/fastcp-agent setup` to install the service units"))
	}
	if runtime.GOOS != "linux" && !dev {
		fail(errors.New("FastCP runs on Linux; set FASTCP_DEV=1 to develop on this system"))
	}
	if dev && !flagSet("listen") {
		*listen = "127.0.0.1:2050"
	}

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		fail(fmt.Errorf("creating the data directory: %w", err))
	}
	box, err := crypto.Load(filepath.Join(*dataDir, ".secret"))
	if err != nil {
		fail(fmt.Errorf("loading the encryption key: %w", err))
	}
	db, err := database.Open(filepath.Join(*dataDir, "fastcp.db"))
	if err != nil {
		fail(fmt.Errorf("opening the database: %w", err))
	}
	defer db.Close()

	ui, err := fs.Sub(uiFS, "ui/dist")
	if err != nil {
		fail(err)
	}
	srv := api.New(api.Config{
		DB: db, Agent: agent.NewClient(*agentSocket), Box: box, Version: version.Version, UI: ui,
		DefaultAdminFile: filepath.Join(*dataDir, "default_admin"),
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	srv.Start(ctx)

	httpSrv := &http.Server{
		Addr:              *listen,
		Handler:           middleware.Compress(5)(srv.Handler()),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          log.New(quietTLSErrors{}, "", 0),
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("FastCP panel listening", "addr", *listen, "version", version.Version, "tls", !*noTLS)
		if *noTLS {
			errc <- httpSrv.ListenAndServe()
		} else {
			errc <- httpSrv.ListenAndServeTLS(*tlsCert, *tlsKey)
		}
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			stop()
			srv.Wait()
			fail(err)
		}
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	done := make(chan struct{})
	go func() { srv.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		slog.Warn("background jobs did not stop in time")
	}
}

func flagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// quietTLSErrors drops handshake noise from scanners hitting the port.
type quietTLSErrors struct{}

func (quietTLSErrors) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if strings.Contains(msg, "TLS handshake error") {
		slog.Debug(msg)
	} else {
		slog.Warn(msg)
	}
	return len(p), nil
}

func fail(err error) {
	slog.Error(err.Error())
	os.Exit(1)
}
