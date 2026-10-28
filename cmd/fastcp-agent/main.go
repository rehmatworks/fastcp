// Command fastcp-agent is FastCP's privileged helper. It runs as root and
// serves the control panel over a Unix socket.
//
//	fastcp-agent [--socket PATH]       serve requests
//	fastcp-agent setup                 create accounts, directories and units
//	fastcp-agent provision             install Caddy, phpMyAdmin and the newest PHP
//	fastcp-agent update-finish ...     internal: complete a self-update
//	fastcp-agent export-zip --out F    internal: tar on stdin to zip, run as a tenant
//	fastcp-agent --version
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
	"github.com/rehmatworks/fastcp/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "setup":
			setupLogging("info")
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			if err := agent.RunSetup(ctx); err != nil {
				fail(err)
			}
			return
		case "provision":
			setupLogging("info")
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			if err := agent.Provision(ctx, os.Stdout); err != nil {
				fail(err)
			}
			return
		case "update-finish":
			updateFinish(os.Args[2:])
			return
		case "export-zip":
			if err := exportZip(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "export-zip:", err)
				os.Exit(1)
			}
			return
		}
	}

	socketPath := flag.String("socket", agent.DefaultSocket, "Unix socket path")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn or error")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("FastCP agent %s (built %s)\n", version.Version, version.BuildTime)
		return
	}
	setupLogging(*logLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := agent.Run(ctx, *socketPath); err != nil {
		fail(err)
	}
}

func updateFinish(args []string) {
	fs := flag.NewFlagSet("update-finish", flag.ExitOnError)
	job := fs.String("job", "", "job ID")
	tag := fs.String("version", "", "release tag")
	dir := fs.String("dir", "", "staging directory")
	fs.Parse(args)
	setupLogging("info")
	if *job == "" || *tag == "" || *dir == "" {
		fail(fmt.Errorf("update-finish requires --job, --version and --dir"))
	}
	if err := agent.FinishUpdate(context.Background(), *job, *tag, *dir); err != nil {
		fail(err)
	}
}

func setupLogging(level string) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})))
}

func fail(err error) {
	slog.Error("fastcp-agent", "error", err)
	os.Exit(1)
}
