package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/crypto"
	"github.com/rehmatworks/fastcp/internal/database"
)

var dbNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// DatabaseService handles database operations
type DatabaseService struct {
	db    *database.DB
	agent *agent.Client
}

// NewDatabaseService creates a new database service
func NewDatabaseService(db *database.DB, agent *agent.Client) *DatabaseService {
	return &DatabaseService{db: db, agent: agent}
}

// List returns all databases for a user
func (s *DatabaseService) List(ctx context.Context, username string) ([]*Database, error) {
	dbDatabases, err := s.db.ListDatabases(ctx, username)
	if err != nil {
		return nil, err
	}

	databases := make([]*Database, len(dbDatabases))
	for i, d := range dbDatabases {
		databases[i] = &Database{
			ID:        d.ID,
			Username:  d.Username,
			DBName:    d.DBName,
			DBUser:    d.DBUser,
			CreatedAt: d.CreatedAt,
		}
	}
	return databases, nil
}

// ListPaginated returns paginated databases with search
func (s *DatabaseService) ListPaginated(ctx context.Context, username string, page, limit int, search string) ([]*Database, int, error) {
	dbDatabases, total, err := s.db.ListDatabasesPaginated(ctx, username, page, limit, search)
	if err != nil {
		return nil, 0, err
	}

	databases := make([]*Database, len(dbDatabases))
	for i, d := range dbDatabases {
		databases[i] = &Database{
			ID:        d.ID,
			Username:  d.Username,
			DBName:    d.DBName,
			DBUser:    d.DBUser,
			CreatedAt: d.CreatedAt,
		}
	}
	return databases, total, nil
}

// Create creates a new MySQL database
func (s *DatabaseService) Create(ctx context.Context, req *CreateDatabaseRequest) (*Database, error) {
	// Validate name
	name := strings.ToLower(strings.TrimSpace(req.Name))
	if !dbNameRegex.MatchString(name) {
		return nil, fmt.Errorf("invalid database name: must start with letter, contain only letters, numbers, underscores")
	}

	// Generate prefixed names
	id := uuid.New().String()
	dbName := fmt.Sprintf("%s_%s", req.Username, name)
	dbUser := dbName
	password := generatePassword(16)

	// Truncate if too long (MySQL limit is 64 chars)
	if len(dbName) > 64 {
		dbName = dbName[:64]
	}
	if len(dbUser) > 32 {
		dbUser = dbUser[:32]
	}

	// Create database via agent
	if err := s.agent.CreateDatabase(ctx, &agent.CreateDatabaseRequest{
		DBName:   dbName,
		DBUser:   dbUser,
		Password: password,
	}); err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	// Encrypt password for storage
	encryptedPassword, err := crypto.Encrypt(password)
	if err != nil {
		// Log but don't fail - password just won't be available for phpMyAdmin
		encryptedPassword = ""
	}

	// Save to local database
	dbRecord := &database.Database{
		ID:         id,
		Username:   req.Username,
		DBName:     dbName,
		DBUser:     dbUser,
		DBPassword: encryptedPassword,
	}
	if err := s.db.CreateDatabase(ctx, dbRecord); err != nil {
		return nil, fmt.Errorf("failed to save database record: %w", err)
	}

	return &Database{
		ID:       id,
		Username: req.Username,
		DBName:   dbName,
		DBUser:   dbUser,
		Password: password, // Only returned on create
	}, nil
}

// Delete deletes a MySQL database
func (s *DatabaseService) Delete(ctx context.Context, id, username string) error {
	// Get database first to check ownership
	dbRecord, err := s.db.GetDatabase(ctx, id)
	if err != nil {
		return fmt.Errorf("database not found")
	}

	if dbRecord.Username != username {
		return fmt.Errorf("database not found")
	}

	// Delete via agent
	if err := s.agent.DeleteDatabase(ctx, &agent.DeleteDatabaseRequest{
		DBName: dbRecord.DBName,
		DBUser: dbRecord.DBUser,
	}); err != nil {
		return fmt.Errorf("failed to delete database: %w", err)
	}

	// Delete from local database
	if err := s.db.DeleteDatabase(ctx, id); err != nil {
		return fmt.Errorf("failed to delete database record: %w", err)
	}

	return nil
}

// ResetPassword rotates a database user's password and returns the new value once.
func (s *DatabaseService) ResetPassword(ctx context.Context, id, username string) (*Database, error) {
	dbRecord, err := s.db.GetDatabase(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("database not found")
	}
	if dbRecord.Username != username {
		return nil, fmt.Errorf("database not found")
	}

	newPassword := generatePassword(16)
	if err := s.agent.ResetDatabasePassword(ctx, &agent.ResetDatabasePasswordRequest{
		DBName:   dbRecord.DBName,
		DBUser:   dbRecord.DBUser,
		Password: newPassword,
	}); err != nil {
		return nil, fmt.Errorf("failed to reset database password: %w", err)
	}

	encryptedPassword, err := crypto.Encrypt(newPassword)
	if err == nil {
		_ = s.db.UpdateDatabasePassword(ctx, id, encryptedPassword)
	}

	return &Database{
		ID:       dbRecord.ID,
		Username: dbRecord.Username,
		DBName:   dbRecord.DBName,
		DBUser:   dbRecord.DBUser,
		Password: newPassword,
	}, nil
}

// GetCredentials returns decrypted database credentials for phpMyAdmin access
func (s *DatabaseService) GetCredentials(ctx context.Context, id, username string) (dbUser, dbPassword, dbName string, err error) {
	dbRecord, err := s.db.GetDatabase(ctx, id)
	if err != nil {
		return "", "", "", fmt.Errorf("database not found")
	}

	if dbRecord.Username != username {
		return "", "", "", fmt.Errorf("database not found")
	}

	if dbRecord.DBPassword == "" {
		return "", "", "", fmt.Errorf("password not available (database created before this feature)")
	}

	password, err := crypto.Decrypt(dbRecord.DBPassword)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to decrypt password")
	}

	return dbRecord.DBUser, password, dbRecord.DBName, nil
}

func generatePassword(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)[:length]
}
