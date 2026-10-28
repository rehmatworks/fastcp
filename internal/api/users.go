package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/database"
	"github.com/rehmatworks/fastcp/internal/validate"
)

type userLimits struct {
	MemoryMB   *int `json:"memory_mb"`
	CPUPercent *int `json:"cpu_percent"`
	MaxSites   *int `json:"max_sites"`
	StorageMB  *int `json:"storage_mb"`
}

func (l *userLimits) apply(u *database.User) error {
	if l.MemoryMB != nil {
		if v := *l.MemoryMB; v != -1 && (v < 128 || v > 262144) {
			return badRequest("Memory must be between 128 MB and 262144 MB, or -1 for unlimited.")
		}
		u.MemoryMB = *l.MemoryMB
	}
	if l.CPUPercent != nil {
		if v := *l.CPUPercent; v != -1 && (v < 10 || v > 4000) {
			return badRequest("CPU must be between 10%% and 4000%%, or -1 for unlimited.")
		}
		u.CPUPercent = *l.CPUPercent
	}
	if l.MaxSites != nil {
		if *l.MaxSites < -1 {
			return badRequest("The site limit must be 0 or more, or -1 for unlimited.")
		}
		u.MaxSites = *l.MaxSites
	}
	if l.StorageMB != nil {
		if *l.StorageMB < -1 {
			return badRequest("The storage allowance must be 0 or more, or -1 for unlimited.")
		}
		u.StorageMB = *l.StorageMB
	}
	return nil
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) error {
	users, err := s.db.ListUsers(r.Context())
	if err != nil {
		return err
	}
	if users == nil {
		users = []*database.User{}
	}
	return ok(w, users)
}

func (s *Server) targetUser(r *http.Request) (*database.User, error) {
	u, err := s.db.GetUser(r.Context(), chi.URLParam(r, "username"))
	if errors.Is(err, database.ErrNotFound) {
		return nil, notFound("User")
	}
	return u, err
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
		userLimits
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	req.Username = strings.TrimSpace(req.Username)
	if err := validate.NewUsername(req.Username); err != nil {
		return validation(err)
	}
	if err := validate.Password(req.Password); err != nil {
		return validation(err)
	}
	u := &database.User{Username: req.Username, IsAdmin: req.IsAdmin, MemoryMB: 512, CPUPercent: 100, MaxSites: -1, StorageMB: -1}
	if err := req.userLimits.apply(u); err != nil {
		return err
	}
	ctx := r.Context()
	if _, err := s.db.GetUser(ctx, u.Username); err == nil {
		return conflict("A user named %s already exists.", u.Username)
	}
	if err := s.agent.CreateUser(ctx, &agent.CreateUserRequest{Username: u.Username, Password: req.Password}); err != nil {
		return err
	}
	err := s.change(ctx, func() (func(), error) {
		if err := s.db.CreateUser(ctx, u); err != nil {
			if errors.Is(err, database.ErrConflict) {
				return nil, conflict("A user named %s already exists.", u.Username)
			}
			return nil, err
		}
		return func() { _ = s.db.DeleteUser(ctx, u.Username) }, nil
	})
	if err != nil {
		if derr := s.agent.DeleteUser(ctx, &agent.DeleteUserRequest{Username: u.Username}); derr != nil {
			slog.Warn("could not remove the system account after a failed create", "user", u.Username, "error", derr)
		}
		return err
	}
	created, err := s.db.GetUser(ctx, u.Username)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, created)
	return nil
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) error {
	target, err := s.targetUser(r)
	if err != nil {
		return err
	}
	var req struct {
		IsAdmin *bool `json:"is_admin"`
		userLimits
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	before := *target
	if req.IsAdmin != nil && *req.IsAdmin != target.IsAdmin {
		if !*req.IsAdmin {
			if target.Username == current(r).Actor.Username {
				return badRequest("You cannot remove your own administrator access.")
			}
			if err := s.ensureAnotherAdmin(ctx, target); err != nil {
				return err
			}
		}
		target.IsAdmin = *req.IsAdmin
	}
	if err := req.userLimits.apply(target); err != nil {
		return err
	}
	err = s.change(ctx, func() (func(), error) {
		if err := s.db.UpdateUser(ctx, target); err != nil {
			return nil, err
		}
		return func() { _ = s.db.UpdateUser(ctx, &before) }, nil
	})
	if err != nil {
		return err
	}
	u, err := s.db.GetUser(ctx, target.Username)
	if err != nil {
		return err
	}
	return ok(w, u)
}

// ensureAnotherAdmin refuses changes that would leave no active
// administrator.
func (s *Server) ensureAnotherAdmin(ctx context.Context, target *database.User) error {
	if !target.IsAdmin || target.IsSuspended {
		return nil
	}
	n, err := s.db.CountActiveAdmins(ctx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return badRequest("%s is the only active administrator.", target.Username)
	}
	return nil
}

func (s *Server) suspendUser(w http.ResponseWriter, r *http.Request) error {
	target, err := s.targetUser(r)
	if err != nil {
		return err
	}
	var req struct {
		Suspended bool `json:"suspended"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	if req.Suspended {
		if target.Username == current(r).Actor.Username {
			return badRequest("You cannot suspend your own account.")
		}
		if err := s.ensureAnotherAdmin(ctx, target); err != nil {
			return err
		}
	}
	err = s.change(ctx, func() (func(), error) {
		if err := s.db.SetUserSuspended(ctx, target.Username, req.Suspended); err != nil {
			return nil, err
		}
		return func() { _ = s.db.SetUserSuspended(ctx, target.Username, target.IsSuspended) }, nil
	})
	if err != nil {
		return err
	}
	if req.Suspended {
		if err := s.db.RevokeUserSessions(ctx, target.Username); err != nil {
			slog.Warn("could not revoke sessions", "user", target.Username, "error", err)
		}
	}
	u, err := s.db.GetUser(ctx, target.Username)
	if err != nil {
		return err
	}
	return ok(w, u)
}

func (s *Server) setUserPassword(w http.ResponseWriter, r *http.Request) error {
	target, err := s.targetUser(r)
	if err != nil {
		return err
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	if err := validate.Password(req.Password); err != nil {
		return validation(err)
	}
	ctx := r.Context()
	if err := s.agent.ChangePassword(ctx, target.Username, req.Password); err != nil {
		return err
	}
	if target.Username != current(r).Actor.Username {
		if err := s.db.RevokeUserSessions(ctx, target.Username); err != nil {
			slog.Warn("could not revoke sessions", "user", target.Username, "error", err)
		}
	}
	return noContent(w)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) error {
	target, err := s.targetUser(r)
	if err != nil {
		return err
	}
	if target.Username == current(r).Actor.Username {
		return badRequest("You cannot delete your own account.")
	}
	ctx := r.Context()
	if err := s.ensureAnotherAdmin(ctx, target); err != nil {
		return err
	}
	dbs, err := s.db.ListDatabases(ctx, target.Username)
	if err != nil {
		return err
	}
	targets := make([]agent.DatabaseTarget, 0, len(dbs))
	for _, d := range dbs {
		targets = append(targets, agent.DatabaseTarget{DBName: d.DBName, DBUser: d.DBUser})
	}

	// Take the account out of the server configuration first, so its sites,
	// pools and crontab are gone before the records are deleted.
	s.syncMu.Lock()
	st, err := s.buildState(ctx)
	if err == nil {
		st.Users = slices.DeleteFunc(st.Users, func(u agent.StateUser) bool { return u.Username == target.Username })
		st.Sites = slices.DeleteFunc(st.Sites, func(site agent.StateSite) bool { return site.Username == target.Username })
		_, err = s.agent.ApplyConfig(ctx, st)
	}
	if err == nil {
		err = s.db.DeleteUser(ctx, target.Username)
	}
	s.syncMu.Unlock()
	if err != nil {
		return err
	}

	if err := s.agent.DeleteUser(ctx, &agent.DeleteUserRequest{Username: target.Username, Databases: targets}); err != nil {
		slog.Error("user removed from FastCP but server cleanup failed", "user", target.Username, "error", err)
		return errf(http.StatusBadGateway, "%s was removed from FastCP, but cleaning up the server failed: %v", target.Username, err)
	}
	slog.Info("deleted user", "user", target.Username, "by", current(r).Actor.Username)
	return noContent(w)
}
