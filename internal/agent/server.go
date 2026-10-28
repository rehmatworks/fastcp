package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"runtime/debug"
	"sync"
	"time"

	"github.com/rehmatworks/fastcp/internal/safefs"
)

// HandlerFunc serves one agent method.
type HandlerFunc func(ctx context.Context, params json.RawMessage) (any, error)

// codedError carries a Response.Code to the client.
type codedError struct {
	code string
	msg  string
}

func (e *codedError) Error() string { return e.msg }

// NewError returns an error that reaches the client with the given code.
func NewError(code, msg string) error { return &codedError{code: code, msg: msg} }

func invalidf(format string, a ...any) error {
	return &codedError{code: CodeInvalid, msg: fmt.Sprintf(format, a...)}
}

func conflictf(format string, a ...any) error {
	return &codedError{code: CodeConflict, msg: fmt.Sprintf(format, a...)}
}

func notFoundf(format string, a ...any) error {
	return &codedError{code: CodeNotFound, msg: fmt.Sprintf(format, a...)}
}

func deniedf(format string, a ...any) error {
	return &codedError{code: CodeDenied, msg: fmt.Sprintf(format, a...)}
}

// invalid wraps a validation error so it is reported with CodeInvalid.
func invalid(err error) error {
	if err == nil {
		return nil
	}
	var ce *codedError
	if errors.As(err, &ce) {
		return err
	}
	return &codedError{code: CodeInvalid, msg: err.Error()}
}

func decode[T any](params json.RawMessage) (*T, error) {
	var v T
	if len(params) == 0 {
		return &v, nil
	}
	if err := json.Unmarshal(params, &v); err != nil {
		return nil, invalidf("invalid parameters: %v", err)
	}
	return &v, nil
}

// RPCServer dispatches one request per connection to registered handlers.
type RPCServer struct {
	handlers map[string]HandlerFunc
	// Authorize decides whether a peer uid may call the agent. Nil allows all.
	Authorize func(uid uint32) bool

	wg    sync.WaitGroup
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func NewRPCServer() *RPCServer {
	return &RPCServer{handlers: map[string]HandlerFunc{}, conns: map[net.Conn]struct{}{}}
}

func (s *RPCServer) Handle(method string, h HandlerFunc) { s.handlers[method] = h }

// Serve accepts connections until ctx is cancelled, then waits for
// in-flight requests to finish.
func (s *RPCServer) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	var delay time.Duration
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			if delay == 0 {
				delay = 50 * time.Millisecond
			} else if delay *= 2; delay > 5*time.Second {
				delay = 5 * time.Second
			}
			slog.Error("agent accept failed", "error", err, "retry_in", delay)
			time.Sleep(delay)
			continue
		}
		delay = 0
		s.track(conn, true)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.track(conn, false)
			defer conn.Close()
			s.serveConn(ctx, conn)
		}()
	}
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		s.mu.Lock()
		for c := range s.conns {
			c.Close()
		}
		s.mu.Unlock()
		<-done
	}
	return nil
}

func (s *RPCServer) track(c net.Conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if add {
		s.conns[c] = struct{}{}
	} else {
		delete(s.conns, c)
	}
}

func (s *RPCServer) serveConn(ctx context.Context, conn net.Conn) {
	if s.Authorize != nil {
		uid, err := peerUID(conn)
		if err != nil || !s.Authorize(uid) {
			slog.Warn("rejected agent connection", "uid", uid, "error", err)
			return
		}
	}
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	var req Request
	if err := json.NewDecoder(io.LimitReader(conn, maxMessageSize)).Decode(&req); err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	start := time.Now()
	resp := Response{ID: req.ID}
	result, err := s.dispatch(ctx, &req)
	if err == nil && result != nil {
		if b, mErr := json.Marshal(result); mErr != nil {
			err = fmt.Errorf("encode result: %w", mErr)
		} else {
			resp.Result = b
		}
	}
	if err != nil {
		resp.Error = err.Error()
		resp.Code = CodeInternal
		level := slog.LevelWarn
		var ce *codedError
		if errors.As(err, &ce) {
			resp.Code = ce.code
			if resp.Code == CodeInvalid || resp.Code == CodeConflict || resp.Code == CodeNotFound {
				level = slog.LevelInfo
			}
		} else if errors.Is(err, safefs.ErrSymlink) || errors.Is(err, safefs.ErrNotDir) || errors.Is(err, safefs.ErrNotFile) {
			// Caused by what a tenant left in their home directory; logged
			// as a warning because it may be an attack.
			resp.Code = CodeConflict
		}
		slog.Log(ctx, level, "agent call failed", "method", req.Method, "code", resp.Code, "error", resp.Error, "duration", time.Since(start).Round(time.Millisecond))
	} else {
		slog.Debug("agent call", "method", req.Method, "duration", time.Since(start).Round(time.Millisecond))
	}
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_ = json.NewEncoder(conn).Encode(&resp)
}

func (s *RPCServer) dispatch(ctx context.Context, req *Request) (result any, err error) {
	h, ok := s.handlers[req.Method]
	if !ok {
		return nil, notFoundf("unknown method %q", req.Method)
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Error("agent handler panic", "method", req.Method, "panic", r, "stack", string(debug.Stack()))
			result, err = nil, fmt.Errorf("internal error in %s", req.Method)
		}
	}()
	return h(ctx, req.Params)
}
