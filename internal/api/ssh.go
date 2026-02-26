package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/database"
)

// SSHKeyService handles SSH key operations
type SSHKeyService struct {
	db    *database.DB
	agent *agent.Client
}

// NewSSHKeyService creates a new SSH key service
func NewSSHKeyService(db *database.DB, agent *agent.Client) *SSHKeyService {
	return &SSHKeyService{db: db, agent: agent}
}

// List returns all SSH keys for a user
func (s *SSHKeyService) List(ctx context.Context, username string) ([]*SSHKey, error) {
	dbKeys, err := s.db.ListSSHKeys(ctx, username)
	if err != nil {
		return nil, err
	}

	keys := make([]*SSHKey, len(dbKeys))
	for i, k := range dbKeys {
		keys[i] = &SSHKey{
			ID:          k.ID,
			Username:    k.Username,
			Name:        k.Name,
			Fingerprint: k.Fingerprint,
			CreatedAt:   k.CreatedAt,
		}
	}
	return keys, nil
}

// Add adds a new SSH key
func (s *SSHKeyService) Add(ctx context.Context, req *AddSSHKeyRequest) (*SSHKey, error) {
	// Validate and parse public key
	publicKey := strings.TrimSpace(req.PublicKey)
	if publicKey == "" {
		return nil, fmt.Errorf("public key is required")
	}

	// Parse SSH key to validate and get fingerprint
	fingerprint, err := getSSHKeyFingerprint(publicKey)
	if err != nil {
		return nil, fmt.Errorf("invalid SSH public key: %w", err)
	}

	id := uuid.New().String()
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Unnamed key"
	}

	// Add key via agent
	if err := s.agent.AddSSHKey(ctx, &agent.AddSSHKeyRequest{
		Username:  req.Username,
		KeyID:     id,
		Name:      name,
		PublicKey: publicKey,
	}); err != nil {
		return nil, fmt.Errorf("failed to add SSH key: %w", err)
	}

	// Save to database
	dbKey := &database.SSHKey{
		ID:          id,
		Username:    req.Username,
		Name:        name,
		PublicKey:   publicKey,
		Fingerprint: fingerprint,
	}
	if err := s.db.CreateSSHKey(ctx, dbKey); err != nil {
		return nil, fmt.Errorf("failed to save SSH key: %w", err)
	}

	return &SSHKey{
		ID:          id,
		Username:    req.Username,
		Name:        name,
		Fingerprint: fingerprint,
	}, nil
}

// Remove removes an SSH key
func (s *SSHKeyService) Remove(ctx context.Context, id, username string) error {
	// Get key first to check ownership
	dbKey, err := s.db.GetSSHKey(ctx, id)
	if err != nil {
		return fmt.Errorf("SSH key not found")
	}

	if dbKey.Username != username {
		return fmt.Errorf("SSH key not found")
	}

	// Remove via agent
	if err := s.agent.RemoveSSHKey(ctx, &agent.RemoveSSHKeyRequest{
		Username:  username,
		KeyID:     id,
		PublicKey: dbKey.PublicKey,
	}); err != nil {
		return fmt.Errorf("failed to remove SSH key: %w", err)
	}

	// Delete from database
	if err := s.db.DeleteSSHKey(ctx, id); err != nil {
		return fmt.Errorf("failed to delete SSH key record: %w", err)
	}

	return nil
}

// getSSHKeyFingerprint calculates the SHA256 fingerprint of an SSH public key
func getSSHKeyFingerprint(publicKey string) (string, error) {
	parts := strings.Fields(publicKey)
	if len(parts) < 2 {
		return "", fmt.Errorf("key must start with type (e.g., ssh-rsa, ssh-ed25519) followed by base64 data")
	}

	keyType := parts[0]
	validTypes := []string{"ssh-rsa", "ssh-ed25519", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521", "ssh-dss"}
	isValidType := false
	for _, t := range validTypes {
		if keyType == t {
			isValidType = true
			break
		}
	}
	if !isValidType {
		return "", fmt.Errorf("unsupported key type '%s'. Supported: ssh-rsa, ssh-ed25519, ecdsa-*", keyType)
	}

	keyData, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("key data is not valid base64. Make sure you copied the entire key")
	}

	if len(keyData) < 20 {
		return "", fmt.Errorf("key data is too short. Make sure you copied the entire public key")
	}

	hash := sha256.Sum256(keyData)
	fingerprint := base64.StdEncoding.EncodeToString(hash[:])
	fingerprint = strings.TrimRight(fingerprint, "=")

	return fmt.Sprintf("SHA256:%s", fingerprint), nil
}
