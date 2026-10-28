package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/database"
	"github.com/rehmatworks/fastcp/internal/validate"
)

const maxSSHKeys = 50

func (s *Server) listSSHKeys(w http.ResponseWriter, r *http.Request) error {
	keys, err := s.db.ListSSHKeys(r.Context(), current(r).User.Username)
	if err != nil {
		return err
	}
	if keys == nil {
		keys = []*database.SSHKey{}
	}
	return ok(w, keys)
}

func (s *Server) addSSHKey(w http.ResponseWriter, r *http.Request) error {
	p := current(r)
	var req struct {
		Name      string `json:"name"`
		PublicKey string `json:"public_key"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	req.Name = strings.TrimSpace(req.Name)
	if err := validate.Label("name", req.Name, 100); err != nil {
		return validation(err)
	}
	key, err := validate.ParseSSHPublicKey(req.PublicKey)
	if err != nil {
		return validation(err)
	}
	ctx := r.Context()
	existing, err := s.db.ListSSHKeys(ctx, p.User.Username)
	if err != nil {
		return err
	}
	if len(existing) >= maxSSHKeys {
		return badRequest("An account can have at most %d SSH keys.", maxSSHKeys)
	}
	for _, k := range existing {
		if k.Fingerprint == key.Fingerprint {
			return conflict("This key is already added as %q.", k.Name)
		}
	}
	rec := &database.SSHKey{ID: uuid.NewString(), Username: p.User.Username, Name: req.Name, PublicKey: key.Line, Fingerprint: key.Fingerprint}
	if err := s.db.CreateSSHKey(ctx, rec); err != nil {
		return err
	}
	if err := s.agent.AddSSHKey(ctx, &agent.AddSSHKeyRequest{Username: rec.Username, KeyID: rec.ID, PublicKey: rec.PublicKey}); err != nil {
		_ = s.db.DeleteSSHKey(ctx, rec.ID)
		return err
	}
	return created(w, rec)
}

func (s *Server) deleteSSHKey(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	k, err := s.db.GetSSHKey(ctx, chi.URLParam(r, "id"))
	if errors.Is(err, database.ErrNotFound) || (err == nil && k.Username != current(r).User.Username) {
		return notFound("SSH key")
	}
	if err != nil {
		return err
	}
	if err := s.agent.RemoveSSHKey(ctx, &agent.RemoveSSHKeyRequest{Username: k.Username, KeyID: k.ID}); err != nil && agent.ErrorCode(err) != "not_found" {
		return err
	}
	if err := s.db.DeleteSSHKey(ctx, k.ID); err != nil {
		return err
	}
	return noContent(w)
}
