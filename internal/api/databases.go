package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/crypto"
	"github.com/rehmatworks/fastcp/internal/database"
	"github.com/rehmatworks/fastcp/internal/validate"
)

var dbPartRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// newDatabaseNames derives a database name and a MySQL account name for a
// user. Names are prefixed with the account so tenants cannot collide, and
// the account name gets a random suffix when it would exceed MySQL's limit.
// With probe set, a numeric suffix is added until a free name is found.
func (s *Server) newDatabaseNames(ctx context.Context, username, part string, probe bool) (string, string, error) {
	prefix := strings.ReplaceAll(username, "-", "_") + "_"
	for i := 1; i <= 50; i++ {
		name := prefix + part
		if i > 1 {
			name += strconv.Itoa(i)
		}
		if len(name) > 64 {
			return "", "", badRequest("The database name is too long; use a shorter name.")
		}
		user := name
		if len(user) > 32 {
			user = user[:27] + "_" + strings.ToLower(randomPassword(4))
		}
		if validate.DBName(name) != nil || validate.DBUser(user) != nil {
			return "", "", badRequest("Database names must start with a letter and use only lowercase letters, digits and '_'.")
		}
		taken, err := s.db.DatabaseNameTaken(ctx, name, user)
		if err != nil {
			return "", "", err
		}
		if !taken {
			return name, user, nil
		}
		if !probe {
			return "", "", conflict("A database named %s already exists.", name)
		}
	}
	return "", "", conflict("Could not find a free database name.")
}

func (s *Server) ownedDatabase(r *http.Request) (*database.Database, error) {
	d, err := s.db.GetDatabase(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, database.ErrNotFound) || (err == nil && d.Username != current(r).User.Username) {
		return nil, notFound("Database")
	}
	return d, err
}

func (s *Server) listDatabases(w http.ResponseWriter, r *http.Request) error {
	dbs, err := s.db.ListDatabases(r.Context(), current(r).User.Username)
	if err != nil {
		return err
	}
	if dbs == nil {
		dbs = []*database.Database{}
	}
	return ok(w, dbs)
}

type databaseCredentials struct {
	*database.Database
	Password string `json:"password"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

func (s *Server) createDatabase(w http.ResponseWriter, r *http.Request) error {
	p := current(r)
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(r, &req); err != nil {
		return err
	}
	part := strings.ToLower(strings.TrimSpace(req.Name))
	if !dbPartRE.MatchString(part) {
		return badRequest("Database names must start with a letter, use only lowercase letters, digits and '_', and be at most 40 characters.")
	}
	ctx := r.Context()
	name, user, err := s.newDatabaseNames(ctx, p.User.Username, part, false)
	if err != nil {
		return err
	}
	password := randomPassword(24)
	enc, err := s.box.Seal(crypto.PurposeDatabasePassword, password)
	if err != nil {
		return err
	}
	rec := &database.Database{ID: uuid.NewString(), Username: p.User.Username, DBName: name, DBUser: user, PasswordEnc: enc}
	if err := s.db.CreateDatabase(ctx, rec); err != nil {
		if errors.Is(err, database.ErrConflict) {
			return conflict("A database named %s already exists.", name)
		}
		return err
	}
	if err := s.agent.CreateDatabase(ctx, &agent.CreateDatabaseRequest{DBName: name, DBUser: user, Password: password}); err != nil {
		_ = s.db.DeleteDatabase(ctx, rec.ID)
		return err
	}
	return created(w, databaseCredentials{Database: rec, Password: password, Host: "localhost", Port: 3306})
}

func (s *Server) deleteDatabase(w http.ResponseWriter, r *http.Request) error {
	d, err := s.ownedDatabase(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	if err := s.agent.DeleteDatabase(ctx, &agent.DatabaseTarget{DBName: d.DBName, DBUser: d.DBUser}); err != nil && agent.ErrorCode(err) != "not_found" {
		return err
	}
	if err := s.db.DeleteDatabase(ctx, d.ID); err != nil {
		return err
	}
	return noContent(w)
}

func (s *Server) resetDatabasePassword(w http.ResponseWriter, r *http.Request) error {
	d, err := s.ownedDatabase(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	password := randomPassword(24)
	enc, err := s.box.Seal(crypto.PurposeDatabasePassword, password)
	if err != nil {
		return err
	}
	if err := s.agent.ResetDatabasePassword(ctx, &agent.ResetDatabasePasswordRequest{DBUser: d.DBUser, Password: password}); err != nil {
		return err
	}
	if err := s.db.SetDatabasePassword(ctx, d.ID, enc); err != nil {
		return err
	}
	return ok(w, databaseCredentials{Database: d, Password: password, Host: "localhost", Port: 3306})
}

func (s *Server) phpMyAdminLink(w http.ResponseWriter, r *http.Request) error {
	d, err := s.ownedDatabase(r)
	if err != nil {
		return err
	}
	password, err := s.box.Open(crypto.PurposeDatabasePassword, d.PasswordEnc)
	if err != nil || password == "" {
		return conflict("The stored password for %s cannot be read. Reset the password, then try again.", d.DBName)
	}
	token := s.pma.links.put(pmaGrant{SessionHash: current(r).TokenHash, DBUser: d.DBUser, DBPass: password, DBName: d.DBName}, pmaLinkTTL)
	return ok(w, map[string]string{"url": pmaPrefix + "/?" + pmaLinkParam + "=" + token})
}
