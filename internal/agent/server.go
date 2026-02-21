package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Server is the fastcp-agent server
type Server struct {
	socketPath string
	listener   net.Listener
	handlers   map[string]Handler
	mu         sync.RWMutex
	wg         sync.WaitGroup
}

// Handler is a function that handles an agent request
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// New creates a new agent server
func New(socketPath string) (*Server, error) {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Remove existing socket
	os.Remove(socketPath)

	s := &Server{
		socketPath: socketPath,
		handlers:   make(map[string]Handler),
	}

	// Register handlers
	s.registerHandlers()

	return s, nil
}

// Run starts the agent server
func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}
	s.listener = listener

	// Set socket permissions (allow fastcp control panel to connect)
	if err := os.Chmod(s.socketPath, 0660); err != nil {
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	slog.Info("agent listening", "socket", s.socketPath)

	// Accept connections
	go func() {
		var errDelay time.Duration
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					if errDelay == 0 {
						errDelay = 50 * time.Millisecond
					} else {
						errDelay *= 2
					}
					if errDelay > 5*time.Second {
						errDelay = 5 * time.Second
					}
					slog.Error("accept error, backing off", "error", err, "delay", errDelay)
					time.Sleep(errDelay)
					continue
				}
			}
			errDelay = 0

			s.wg.Add(1)
			go s.handleConnection(ctx, conn)
		}
	}()

	<-ctx.Done()

	// Cleanup
	listener.Close()
	s.wg.Wait()

	return nil
}

func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var req Request
		if err := decoder.Decode(&req); err != nil {
			return // Connection closed
		}

		slog.Debug("received request", "method", req.Method, "id", req.ID)

		// Find handler
		s.mu.RLock()
		handler, ok := s.handlers[req.Method]
		s.mu.RUnlock()

		var resp Response
		resp.ID = req.ID

		if !ok {
			resp.Error = fmt.Sprintf("unknown method: %s", req.Method)
		} else {
			// Marshal params back to JSON for handler
			paramsJSON, _ := json.Marshal(req.Params)

			result, err := handler(ctx, paramsJSON)
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.Result = result
			}
		}

		if err := encoder.Encode(resp); err != nil {
			slog.Error("failed to send response", "error", err)
			return
		}
	}
}

func (s *Server) registerHandlers() {
	// Site handlers
	s.handlers["site.createDirectory"] = s.handleCreateSiteDirectory
	s.handlers["site.deleteDirectory"] = s.handleDeleteSiteDirectory
	s.handlers["site.installWordPress"] = s.handleInstallWordPress

	// Caddy handlers
	s.handlers["caddy.reload"] = s.handleReloadCaddy

	// Database handlers
	s.handlers["database.create"] = s.handleCreateDatabase
	s.handlers["database.delete"] = s.handleDeleteDatabase

	// SSH handlers
	s.handlers["ssh.addKey"] = s.handleAddSSHKey
	s.handlers["ssh.removeKey"] = s.handleRemoveSSHKey

	// System handlers
	s.handlers["system.status"] = s.handleSystemStatus
	s.handlers["system.services"] = s.handleSystemServices
	s.handlers["system.update"] = s.handleSystemUpdate

	// User handlers
	s.handlers["user.create"] = s.handleCreateUser
	s.handlers["user.delete"] = s.handleDeleteUser

	// Cron handlers
	s.handlers["cron.sync"] = s.handleSyncCronJobs
}
