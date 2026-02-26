package api

import (
	"context"
	"fmt"
	"regexp"

	"github.com/rehmatworks/fastcp/internal/agent"
	"github.com/rehmatworks/fastcp/internal/database"
)

var usernameRegex = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// UserService handles user operations
type UserService struct {
	db    *database.DB
	agent *agent.Client
}

// NewUserService creates a new user service
func NewUserService(db *database.DB, agent *agent.Client) *UserService {
	return &UserService{db: db, agent: agent}
}

// List returns all users (admin only)
func (s *UserService) List(ctx context.Context) ([]*User, error) {
	dbUsers, err := s.db.ListUsers(ctx)
	if err != nil {
		return nil, err
	}

	users := make([]*User, len(dbUsers))
	for i, dbUser := range dbUsers {
		siteCount, _ := s.db.CountUserSites(ctx, dbUser.Username)
		users[i] = &User{
			ID:          dbUser.ID,
			Username:    dbUser.Username,
			IsAdmin:     dbUser.IsAdmin,
			IsSuspended: dbUser.IsSuspended,
			MemoryMB:    dbUser.MemoryMB,
			CPUPercent:  dbUser.CPUPercent,
			MaxSites:    dbUser.MaxSites,
			StorageMB:   dbUser.StorageMB,
			SiteCount:   siteCount,
			CreatedAt:   dbUser.CreatedAt,
		}
	}
	return users, nil
}

// Create creates a new system user (admin only)
func (s *UserService) Create(ctx context.Context, req *CreateUserRequest) (*User, error) {
	// Validate username
	if !usernameRegex.MatchString(req.Username) {
		return nil, fmt.Errorf("invalid username: must start with lowercase letter or underscore, contain only lowercase letters, digits, underscores, and hyphens, max 32 characters")
	}

	// Validate password
	if len(req.Password) < 8 {
		return nil, fmt.Errorf("password must be at least 8 characters")
	}

	// Reserved usernames
	reserved := map[string]bool{
		"root": true, "admin": true, "www-data": true, "mysql": true,
		"nobody": true, "daemon": true, "bin": true, "sys": true,
	}
	if reserved[req.Username] {
		return nil, fmt.Errorf("username '%s' is reserved", req.Username)
	}

	// Set defaults for resource limits
	memoryMB := req.MemoryMB
	if memoryMB == 0 {
		memoryMB = 512 // Default 512MB
	}
	cpuPercent := req.CPUPercent
	if cpuPercent == 0 {
		cpuPercent = 100 // Default 100% (1 core)
	}
	maxSites := req.MaxSites
	if maxSites == 0 {
		maxSites = -1 // Default unlimited
	}
	storageMB := req.StorageMB
	if storageMB == 0 {
		storageMB = -1 // Default unlimited
	}

	// Create system user via agent
	if err := s.agent.CreateUser(ctx, &agent.CreateUserRequest{
		Username:   req.Username,
		Password:   req.Password,
		MemoryMB:   memoryMB,
		CPUPercent: cpuPercent,
	}); err != nil {
		return nil, fmt.Errorf("failed to create system user: %w", err)
	}

	// Save to database
	dbUser, err := s.db.CreateUserWithLimits(ctx, req.Username, req.IsAdmin, memoryMB, cpuPercent, maxSites, storageMB)
	if err != nil {
		// Try to rollback system user creation
		s.agent.DeleteUser(ctx, &agent.DeleteUserRequest{Username: req.Username})
		return nil, fmt.Errorf("failed to save user: %w", err)
	}

	return &User{
		ID:         dbUser.ID,
		Username:   dbUser.Username,
		IsAdmin:    dbUser.IsAdmin,
		MemoryMB:   dbUser.MemoryMB,
		CPUPercent: dbUser.CPUPercent,
		MaxSites:   dbUser.MaxSites,
		StorageMB:  dbUser.StorageMB,
		SiteCount:  0,
		CreatedAt:  dbUser.CreatedAt,
	}, nil
}

// Delete deletes a system user (admin only)
func (s *UserService) Delete(ctx context.Context, username string) error {
	// Prevent deleting root
	if username == "root" {
		return fmt.Errorf("cannot delete root user")
	}

	// Delete system user via agent
	if err := s.agent.DeleteUser(ctx, &agent.DeleteUserRequest{
		Username: username,
	}); err != nil {
		return fmt.Errorf("failed to delete system user: %w", err)
	}

	// Delete from database (cascades to sites, databases, etc.)
	if err := s.db.DeleteUser(ctx, username); err != nil {
		return fmt.Errorf("failed to delete user from database: %w", err)
	}

	return nil
}

// ToggleSuspension toggles user suspension and regenerates Caddyfile
func (s *UserService) ToggleSuspension(ctx context.Context, username string) (*User, error) {
	// Prevent suspending root
	if username == "root" {
		return nil, fmt.Errorf("cannot suspend root user")
	}

	// Get current user
	dbUser, err := s.db.GetUser(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	// Toggle suspension
	newStatus := !dbUser.IsSuspended
	if err := s.db.SetUserSuspended(ctx, username, newStatus); err != nil {
		return nil, fmt.Errorf("failed to update suspension status: %w", err)
	}

	// Regenerate Caddyfile to apply changes
	if err := s.agent.RegenerateCaddyfile(ctx); err != nil {
		return nil, fmt.Errorf("failed to update web server config: %w", err)
	}

	// Return updated user
	siteCount, _ := s.db.CountUserSites(ctx, username)
	return &User{
		ID:          dbUser.ID,
		Username:    dbUser.Username,
		IsAdmin:     dbUser.IsAdmin,
		IsSuspended: newStatus,
		MemoryMB:    dbUser.MemoryMB,
		CPUPercent:  dbUser.CPUPercent,
		MaxSites:    dbUser.MaxSites,
		StorageMB:   dbUser.StorageMB,
		SiteCount:   siteCount,
		CreatedAt:   dbUser.CreatedAt,
	}, nil
}

// UpdateResources updates a user's resource limits and reapplies system constraints
func (s *UserService) UpdateResources(ctx context.Context, username string, req *UpdateUserResourcesRequest) (*User, error) {
	if username == "root" {
		return nil, fmt.Errorf("cannot update root user limits")
	}
	if req.MemoryMB != -1 && (req.MemoryMB < 128 || req.MemoryMB > 262144) {
		return nil, fmt.Errorf("memory_mb must be -1 (unlimited) or between 128 and 262144")
	}
	if req.CPUPercent != -1 && (req.CPUPercent < 10 || req.CPUPercent > 4000) {
		return nil, fmt.Errorf("cpu_percent must be -1 (unlimited) or between 10 and 4000")
	}
	if req.MaxSites < -1 {
		return nil, fmt.Errorf("max_sites must be -1 or greater")
	}
	if req.StorageMB < -1 {
		return nil, fmt.Errorf("storage_mb must be -1 or greater")
	}

	// Ensure user exists in DB first
	dbUser, err := s.db.GetUser(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	if err := s.agent.UpdateUserLimits(ctx, &agent.UpdateUserLimitsRequest{
		Username:   username,
		MemoryMB:   req.MemoryMB,
		CPUPercent: req.CPUPercent,
	}); err != nil {
		return nil, fmt.Errorf("failed to apply system user limits: %w", err)
	}

	if err := s.db.UpdateUserLimits(ctx, username, req.MemoryMB, req.CPUPercent, req.MaxSites, req.StorageMB); err != nil {
		return nil, fmt.Errorf("failed to save user limits: %w", err)
	}

	siteCount, _ := s.db.CountUserSites(ctx, username)
	return &User{
		ID:          dbUser.ID,
		Username:    dbUser.Username,
		IsAdmin:     dbUser.IsAdmin,
		IsSuspended: dbUser.IsSuspended,
		MemoryMB:    req.MemoryMB,
		CPUPercent:  req.CPUPercent,
		MaxSites:    req.MaxSites,
		StorageMB:   req.StorageMB,
		SiteCount:   siteCount,
		CreatedAt:   dbUser.CreatedAt,
	}, nil
}
