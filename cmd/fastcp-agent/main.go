// FastCP Agent - Privileged helper daemon
// Runs as root and handles system operations via Unix socket
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rehmatworks/fastcp/internal/agent"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	socketPath := flag.String("socket", "/opt/fastcp/run/agent.sock", "Unix socket path")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("FastCP Agent %s (built %s)\n", Version, BuildTime)
		os.Exit(0)
	}

	// Must run as root
	if os.Getuid() != 0 {
		slog.Error("fastcp-agent must run as root")
		os.Exit(1)
	}

	// Setup logging
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Create and start agent
	agentServer, err := agent.New(*socketPath)
	if err != nil {
		slog.Error("failed to create agent", "error", err)
		os.Exit(1)
	}

	// Handle shutdown signals
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		slog.Info("shutting down agent...")
		cancel()
	}()

	slog.Info("starting fastcp-agent", "socket", *socketPath)
	if err := agentServer.Run(ctx); err != nil {
		slog.Error("agent error", "error", err)
		os.Exit(1)
	}
}
