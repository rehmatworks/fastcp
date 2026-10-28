package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/database"
)

// apiError is an error whose message is safe to show to the client.
type apiError struct {
	Status int
	Msg    string
}

func (e *apiError) Error() string { return e.Msg }

func errf(status int, format string, a ...any) error {
	return &apiError{Status: status, Msg: fmt.Sprintf(format, a...)}
}

func badRequest(format string, a ...any) error {
	return errf(http.StatusBadRequest, format, a...)
}

func conflict(format string, a ...any) error {
	return errf(http.StatusConflict, format, a...)
}

func forbidden(format string, a ...any) error {
	return errf(http.StatusForbidden, format, a...)
}

func notFound(what string) error {
	return errf(http.StatusNotFound, "%s not found", what)
}

// validation wraps an error from the validate package as a 400.
func validation(err error) error {
	if err == nil {
		return nil
	}
	return badRequest("%s", err.Error())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func ok(w http.ResponseWriter, v any) error {
	writeJSON(w, http.StatusOK, v)
	return nil
}

func created(w http.ResponseWriter, v any) error {
	writeJSON(w, http.StatusCreated, v)
	return nil
}

func accepted(w http.ResponseWriter, v any) error {
	writeJSON(w, http.StatusAccepted, v)
	return nil
}

func noContent(w http.ResponseWriter) error {
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// h adapts an error-returning handler.
func (s *Server) h(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			s.fail(w, r, err)
		}
	}
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, msg := classify(err)
	if status >= 500 {
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "status", status, "error", err)
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

func classify(err error) (int, string) {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.Status, ae.Msg
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return http.StatusRequestEntityTooLarge, "The request is too large."
	}
	switch {
	case errors.Is(err, database.ErrNotFound):
		return http.StatusNotFound, "Not found."
	case errors.Is(err, database.ErrConflict):
		return http.StatusConflict, "It already exists."
	case errors.Is(err, database.ErrBusy):
		return http.StatusConflict, "A backup is already running."
	case errors.Is(err, agent.ErrUnavailable):
		return http.StatusServiceUnavailable, "The FastCP agent is not running. Check `systemctl status fastcp-agent`."
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "The operation timed out."
	}
	var agentErr *agent.Error
	if errors.As(err, &agentErr) {
		switch agentErr.Code {
		case "invalid":
			return http.StatusBadRequest, agentErr.Message
		case "conflict":
			return http.StatusConflict, agentErr.Message
		case "not_found":
			return http.StatusNotFound, agentErr.Message
		case "denied":
			return http.StatusForbidden, agentErr.Message
		}
		return http.StatusBadGateway, agentErr.Message
	}
	return http.StatusInternalServerError, "Something went wrong. Check the panel log for details."
}

// decode reads a JSON request body into v.
func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return err
		}
		if errors.Is(err, io.EOF) {
			return badRequest("The request body is empty.")
		}
		return badRequest("The request body is not valid JSON.")
	}
	return nil
}

func queryInt(r *http.Request, name string, def, lo, hi int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	return max(lo, min(v, hi))
}
