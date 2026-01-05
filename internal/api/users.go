package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rehmatworks/fastcp/internal/jail"
	"github.com/rehmatworks/fastcp/internal/limits"
	"github.com/rehmatworks/fastcp/internal/middleware"
	"github.com/rehmatworks/fastcp/internal/models"
)

// FastCPUser represents a FastCP user with limits and usage
type FastCPUser struct {
	Username     string `json:"username"`
	UID          int    `json:"uid"`
	GID          int    `json:"gid"`
	HomeDir      string `json:"home_dir"`
	IsAdmin      bool   `json:"is_admin"`
	Enabled      bool   `json:"enabled"`

	// Jail/SSH settings
	IsJailed     bool   `json:"is_jailed"`     // SFTP-only, chrooted
	ShellAccess  bool   `json:"shell_access"`  // Can use SSH shell (not jailed)

	// Limits
	SiteLimit    int   `json:"site_limit"`     // 0 = unlimited
	RAMLimitMB   int64 `json:"ram_limit_mb"`   // 0 = unlimited
	CPUPercent   int   `json:"cpu_percent"`    // 0 = unlimited (100 = 1 core)
	MaxProcesses int   `json:"max_processes"`  // 0 = unlimited

	// Usage
	SiteCount    int   `json:"site_count"`
	DiskUsedMB   int64 `json:"disk_used_mb"`
	RAMUsedMB    int64 `json:"ram_used_mb"`
	ProcessCount int   `json:"process_count"`
}

// CreateUserRequest represents a request to create a user
type CreateUserRequest struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	IsAdmin      bool   `json:"is_admin"`      // Add to sudo group
	ShellAccess  bool   `json:"shell_access"`  // Allow SSH shell (false = SFTP only, jailed)

	// Resource limits
	SiteLimit    int   `json:"site_limit"`    // 0 = unlimited
	RAMLimitMB   int64 `json:"ram_limit_mb"`  // 0 = unlimited
	CPUPercent   int   `json:"cpu_percent"`   // 0 = unlimited
	MaxProcesses int   `json:"max_processes"` // 0 = unlimited
}

// UpdateUserRequest represents a request to update a user
type UpdateUserRequest struct {
	Password     string `json:"password,omitempty"`
	Enabled      bool   `json:"enabled"`
	ShellAccess  bool   `json:"shell_access"`  // Allow SSH shell (false = SFTP only, jailed)

	// Resource limits
	SiteLimit    int   `json:"site_limit"`
	RAMLimitMB   int64 `json:"ram_limit_mb"`
	CPUPercent   int   `json:"cpu_percent"`
	MaxProcesses int   `json:"max_processes"`
}

// listUsers returns all FastCP users
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.getFastCPUsers()
	if err != nil {
		s.error(w, http.StatusInternalServerError, "failed to list users")
		return
	}

	s.success(w, map[string]interface{}{
		"users": users,
		"total": len(users),
	})
}

// getUser returns a single user
func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")

	u, err := s.getFastCPUser(username)
	if err != nil {
		s.error(w, http.StatusNotFound, "user not found")
		return
	}

	s.success(w, u)
}

// createUser creates a new Unix user
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)

	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate
	if req.Username == "" {
		s.error(w, http.StatusBadRequest, "username is required")
		return
	}
	if req.Password == "" {
		s.error(w, http.StatusBadRequest, "password is required")
		return
	}
	if len(req.Password) < 8 {
		s.error(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	// Check if user already exists
	if _, err := user.Lookup(req.Username); err == nil {
		s.error(w, http.StatusConflict, "user already exists")
		return
	}

	// Create user with useradd
	cmd := exec.Command("useradd", "-m", "-s", "/bin/bash", req.Username)
	if output, err := cmd.CombinedOutput(); err != nil {
		s.logger.Error("failed to create user", "error", err, "output", string(output))
		s.error(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	// Set password
	cmd = exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s", req.Username, req.Password))
	if output, err := cmd.CombinedOutput(); err != nil {
		s.logger.Error("failed to set password", "error", err, "output", string(output))
		// Cleanup: delete the user
		_ = exec.Command("userdel", "-r", req.Username).Run()
		s.error(w, http.StatusInternalServerError, "failed to set password")
		return
	}

	// Secure home directory - prevent other users from accessing
	homeDir := fmt.Sprintf("/home/%s", req.Username)
	_ = exec.Command("chmod", "750", homeDir).Run()

	// Add to fastcp group (for FastCP panel access)
	_ = exec.Command("groupadd", "-f", "fastcp").Run()
	cmd = exec.Command("usermod", "-aG", "fastcp", req.Username)
	if output, err := cmd.CombinedOutput(); err != nil {
		s.logger.Warn("failed to add user to fastcp group", "error", err, "output", string(output))
	}

	// Add to ssh group if it exists (some systems restrict SSH to specific groups)
	_ = exec.Command("usermod", "-aG", "ssh", req.Username).Run()
	// Also try sshusers group (used by some configurations)
	_ = exec.Command("usermod", "-aG", "sshusers", req.Username).Run()

	// Add to sudo group if admin
	if req.IsAdmin {
		cmd = exec.Command("usermod", "-aG", "sudo", req.Username)
		if output, err := cmd.CombinedOutput(); err != nil {
			s.logger.Warn("failed to add user to sudo group", "error", err, "output", string(output))
		}
		// Admins are not jailed
		jail.RemoveUserFromJail(req.Username)
	} else if !req.ShellAccess {
		// Non-admin users without shell access are jailed (SFTP only)
		if err := jail.SetupUserJail(req.Username); err != nil {
			s.logger.Warn("failed to setup user jail", "error", err)
		}
		s.logger.Info("user jailed (SFTP-only)", "username", req.Username)
	}

	// Create user's web directory with proper permissions
	// For jailed users, this is in /home/username/www
	// For non-jailed users, this is in /var/www/username
	var userWebDir string
	if !req.ShellAccess && !req.IsAdmin {
		// Jailed user - www is inside home
		userWebDir = fmt.Sprintf("/home/%s/www", req.Username)
	} else {
		userWebDir = fmt.Sprintf("/var/www/%s", req.Username)
	}
	
	if err := os.MkdirAll(userWebDir, 0755); err != nil {
		s.logger.Warn("failed to create user web directory", "error", err)
	} else {
		// Set ownership to the new user
		u, _ := user.Lookup(req.Username)
		if u != nil {
			uid, _ := strconv.Atoi(u.Uid)
			gid, _ := strconv.Atoi(u.Gid)
			_ = os.Chown(userWebDir, uid, gid)
		}
	}

	// Set resource limits
	userLimits := &models.UserLimits{
		Username:      req.Username,
		MaxSites:      req.SiteLimit,
		MaxRAMMB:      req.RAMLimitMB,
		MaxCPUPercent: req.CPUPercent,
		MaxProcesses:  req.MaxProcesses,
	}

	if err := s.siteManager.SetUserLimit(userLimits); err != nil {
		s.logger.Warn("failed to save user limits", "error", err)
	}

	// Apply system-level limits (cgroups, quotas)
	limitsManager := limits.NewManager(s.logger)
	if err := limitsManager.ApplyLimits(userLimits); err != nil {
		s.logger.Warn("failed to apply system limits", "error", err)
	}

	s.logger.Info("user created", "username", req.Username, "by", claims.Username)

	// Return the created user
	u, _ := s.getFastCPUser(req.Username)
	s.json(w, http.StatusCreated, u)
}

// updateUser updates a user's settings
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	claims := middleware.GetClaims(r)

	// Verify user exists
	if _, err := user.Lookup(username); err != nil {
		s.error(w, http.StatusNotFound, "user not found")
		return
	}

	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Update password if provided
	if req.Password != "" {
		if len(req.Password) < 8 {
			s.error(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		cmd := exec.Command("chpasswd")
		cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s", username, req.Password))
		if output, err := cmd.CombinedOutput(); err != nil {
			s.logger.Error("failed to set password", "error", err, "output", string(output))
			s.error(w, http.StatusInternalServerError, "failed to update password")
			return
		}
	}

	// Update resource limits
	userLimits := &models.UserLimits{
		Username:      username,
		MaxSites:      req.SiteLimit,
		MaxRAMMB:      req.RAMLimitMB,
		MaxCPUPercent: req.CPUPercent,
		MaxProcesses:  req.MaxProcesses,
	}

	if err := s.siteManager.SetUserLimit(userLimits); err != nil {
		s.logger.Warn("failed to save user limits", "error", err)
	}

	// Apply system-level limits
	limitsManager := limits.NewManager(s.logger)
	if err := limitsManager.ApplyLimits(userLimits); err != nil {
		s.logger.Warn("failed to apply system limits", "error", err)
	}

	// Handle shell access / jail changes
	isCurrentlyJailed := jail.IsUserJailed(username)
	isAdmin := s.isUserInGroup(username, "sudo") || s.isUserInGroup(username, "wheel")

	if !isAdmin {
		if req.ShellAccess && isCurrentlyJailed {
			// Grant shell access - remove from jail
			jail.RemoveUserFromJail(username)
			s.logger.Info("user removed from jail (shell access granted)", "username", username)
		} else if !req.ShellAccess && !isCurrentlyJailed {
			// Revoke shell access - add to jail
			if err := jail.SetupUserJail(username); err != nil {
				s.logger.Warn("failed to setup user jail", "error", err)
			}
			s.logger.Info("user jailed (SFTP-only)", "username", username)
		}
	}

	// Enable/disable user and their sites
	u, err := user.Lookup(username)
	if err != nil {
		s.error(w, http.StatusNotFound, "user not found")
		return
	}

	// Get current enabled state
	currentEnabled := true
	checkCmd := exec.Command("passwd", "-S", username)
	if output, err := checkCmd.Output(); err == nil {
		fields := strings.Fields(string(output))
		if len(fields) >= 2 && fields[1] == "L" {
			currentEnabled = false
		}
	}

	// Handle state change
	if !req.Enabled && currentEnabled {
		// Disabling user - lock account and suspend all their sites
		_ = exec.Command("usermod", "-L", username).Run()
		
		// Suspend all user's sites
		userSites := s.siteManager.List(u.Uid)
		for _, site := range userSites {
			if site.Status == "active" {
				if err := s.siteManager.Suspend(site.ID); err != nil {
					s.logger.Warn("failed to suspend site", "site", site.Domain, "error", err)
				} else {
					s.logger.Info("suspended site for disabled user", "site", site.Domain, "user", username)
				}
			}
		}
		// Reload PHP to apply suspension
		s.phpManager.Reload()
		
	} else if req.Enabled && !currentEnabled {
		// Enabling user - unlock account and unsuspend all their sites
		_ = exec.Command("usermod", "-U", username).Run()
		
		// Unsuspend all user's sites
		userSites := s.siteManager.List(u.Uid)
		for _, site := range userSites {
			if site.Status == "suspended" {
				if err := s.siteManager.Unsuspend(site.ID); err != nil {
					s.logger.Warn("failed to unsuspend site", "site", site.Domain, "error", err)
				} else {
					s.logger.Info("unsuspended site for enabled user", "site", site.Domain, "user", username)
				}
			}
		}
		// Reload PHP to apply unsuspension
		s.phpManager.Reload()
	}

	s.logger.Info("user updated", "username", username, "by", claims.Username)

	fastcpUser, _ := s.getFastCPUser(username)
	s.success(w, fastcpUser)
}

// deleteUser removes a Unix user and all their data
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	claims := middleware.GetClaims(r)

	// Prevent deleting self
	if username == claims.Username {
		s.error(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}

	// Prevent deleting root
	if username == "root" {
		s.error(w, http.StatusBadRequest, "cannot delete root user")
		return
	}

	// Get user info before deletion
	u, err := user.Lookup(username)
	if err != nil {
		s.error(w, http.StatusNotFound, "user not found")
		return
	}

	// Delete all user's sites first
	userSites := s.siteManager.List(u.Uid)
	for _, site := range userSites {
		s.logger.Info("deleting site for user deletion", "site", site.Domain, "user", username)
		if err := s.siteManager.Delete(site.ID); err != nil {
			s.logger.Warn("failed to delete site", "site", site.Domain, "error", err)
		}
	}

	// Reload PHP after site deletion
	if len(userSites) > 0 {
		s.phpManager.Reload()
	}

	// Kill all user's processes
	s.logger.Info("killing user processes", "user", username)
	_ = exec.Command("pkill", "-9", "-u", username).Run()
	
	// Wait a moment for processes to die
	exec.Command("sleep", "1").Run()

	// Remove user's cgroup
	limitsManager := limits.NewManager(s.logger)
	_ = limitsManager.RemoveLimits(username)

	// Remove user limits from config
	_ = s.siteManager.SetUserLimit(&models.UserLimits{Username: username, MaxSites: 0})

	// Delete user (with home directory)
	cmd := exec.Command("userdel", "-rf", username)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Try without -r if it fails (home might already be deleted)
		cmd2 := exec.Command("userdel", "-f", username)
		if output2, err2 := cmd2.CombinedOutput(); err2 != nil {
			s.logger.Error("failed to delete user", "error", err2, "output", string(output)+string(output2))
			s.error(w, http.StatusInternalServerError, "failed to delete user: processes may still be running")
			return
		}
	}

	// Clean up user's web directory
	userWebDir := fmt.Sprintf("/var/www/%s", username)
	if err := os.RemoveAll(userWebDir); err != nil {
		s.logger.Warn("failed to remove user web directory", "path", userWebDir, "error", err)
	}

	s.logger.Info("user deleted", "username", username, "sites_deleted", len(userSites), "by", claims.Username)
	s.success(w, map[string]interface{}{
		"message":       "user deleted",
		"sites_deleted": len(userSites),
	})
}

// getFastCPUsers returns all users in the fastcp group
func (s *Server) getFastCPUsers() ([]FastCPUser, error) {
	var users []FastCPUser

	// Get users in fastcp group
	cmd := exec.Command("getent", "group", "fastcp")
	output, err := cmd.Output()
	if err != nil {
		// Group doesn't exist yet
		return users, nil
	}

	// Parse group: fastcp:x:1001:user1,user2,user3
	parts := strings.Split(strings.TrimSpace(string(output)), ":")
	if len(parts) < 4 || parts[3] == "" {
		return users, nil
	}

	usernames := strings.Split(parts[3], ",")
	for _, username := range usernames {
		if u, err := s.getFastCPUser(username); err == nil {
			users = append(users, *u)
		}
	}

	// Also add root if not already in list
	if rootUser, err := s.getFastCPUser("root"); err == nil {
		found := false
		for _, u := range users {
			if u.Username == "root" {
				found = true
				break
			}
		}
		if !found {
			users = append([]FastCPUser{*rootUser}, users...)
		}
	}

	return users, nil
}

// getFastCPUser returns info about a single user
func (s *Server) getFastCPUser(username string) (*FastCPUser, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}

	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	// Check if admin (in sudo/wheel group)
	isAdmin := s.isUserInGroup(username, "sudo") || s.isUserInGroup(username, "wheel") || username == "root"

	// Check if enabled (account not locked)
	enabled := true
	cmd := exec.Command("passwd", "-S", username)
	if output, err := cmd.Output(); err == nil {
		fields := strings.Fields(string(output))
		if len(fields) >= 2 && fields[1] == "L" {
			enabled = false
		}
	}

	// Get site count
	sites := s.siteManager.List(u.Uid)
	siteCount := len(sites)

	// Get resource limits
	userLimits := s.siteManager.GetUserLimit(username)

	// Get resource usage
	limitsManager := limits.NewManager(s.logger)
	usage, _ := limitsManager.GetUsage(username)

	// Check jail status
	isJailed := jail.IsUserJailed(username)

	fastcpUser := &FastCPUser{
		Username:     username,
		UID:          uid,
		GID:          gid,
		HomeDir:      u.HomeDir,
		IsAdmin:      isAdmin,
		Enabled:      enabled,

		// Jail status
		IsJailed:     isJailed,
		ShellAccess:  !isJailed || isAdmin,

		// Limits
		SiteLimit:    userLimits.MaxSites,
		RAMLimitMB:   userLimits.MaxRAMMB,
		CPUPercent:   userLimits.MaxCPUPercent,
		MaxProcesses: userLimits.MaxProcesses,

		// Current usage
		SiteCount:    siteCount,
	}

	// Add usage stats if available
	if usage != nil {
		fastcpUser.DiskUsedMB = usage.DiskUsedMB
		fastcpUser.RAMUsedMB = usage.RAMUsedMB
		fastcpUser.ProcessCount = usage.ProcessCount
	}

	return fastcpUser, nil
}

// isUserInGroup checks if user is in a group
func (s *Server) isUserInGroup(username, groupName string) bool {
	cmd := exec.Command("groups", username)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), groupName)
}

// fixUserPermissions fixes SSH and directory permissions for all users
func (s *Server) fixUserPermissions(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)

	results := make(map[string]interface{})
	fixed := 0
	errors := 0

	// Get all FastCP users
	users, err := s.getFastCPUsers()
	if err != nil {
		s.error(w, http.StatusInternalServerError, "failed to get users")
		return
	}

	for _, u := range users {
		if u.Username == "root" {
			continue
		}

		userResults := map[string]string{}

		// Fix home directory permissions
		homeDir := fmt.Sprintf("/home/%s", u.Username)
		if err := exec.Command("chmod", "750", homeDir).Run(); err == nil {
			userResults["home_chmod"] = "fixed"
		} else {
			userResults["home_chmod"] = "error"
			errors++
		}

		// Fix ownership of home directory
		if err := exec.Command("chown", fmt.Sprintf("%s:%s", u.Username, u.Username), homeDir).Run(); err == nil {
			userResults["home_chown"] = "fixed"
		} else {
			userResults["home_chown"] = "error"
			errors++
		}

		// Fix web directory
		webDir := fmt.Sprintf("/var/www/%s", u.Username)
		if _, err := os.Stat(webDir); err == nil {
			if err := exec.Command("chmod", "750", webDir).Run(); err == nil {
				userResults["web_chmod"] = "fixed"
			}
			if err := exec.Command("chown", "-R", fmt.Sprintf("%s:%s", u.Username, u.Username), webDir).Run(); err == nil {
				userResults["web_chown"] = "fixed"
			}
			// Apply ACL
			setUserDirACL(webDir, u.Username)
			userResults["web_acl"] = "applied"
		}

		// Ensure user is in SSH groups
		_ = exec.Command("usermod", "-aG", "ssh", u.Username).Run()
		_ = exec.Command("usermod", "-aG", "sshusers", u.Username).Run()
		userResults["ssh_groups"] = "checked"

		results[u.Username] = userResults
		fixed++
	}

	// Secure base /var/www directory
	_ = exec.Command("chown", "root:root", "/var/www").Run()
	_ = exec.Command("chmod", "751", "/var/www").Run()
	results["var_www"] = map[string]string{
		"owner":       "root:root",
		"permissions": "751",
	}

	s.logger.Info("fixed user permissions", "users", fixed, "errors", errors, "by", claims.Username)

	s.success(w, map[string]interface{}{
		"message":     "permissions fixed",
		"users_fixed": fixed,
		"errors":      errors,
		"details":     results,
	})
}

// setUserDirACL applies ACL to a user directory
// Includes read/write/execute access for fastcp user (PHP execution + WordPress file operations)
func setUserDirACL(path, username string) {
	cmds := [][]string{
		// Clear existing ACLs
		{"setfacl", "-b", path},
		// Owner has full access
		{"setfacl", "-R", "-m", fmt.Sprintf("u:%s:rwx", username), path},
		// fastcp user has full access for PHP (needed for WordPress plugin/theme management)
		{"setfacl", "-R", "-m", "u:fastcp:rwx", path},
		// Root has full access
		{"setfacl", "-R", "-m", "u:root:rwx", path},
		// No group access
		{"setfacl", "-R", "-m", "g::---", path},
		// No other users access
		{"setfacl", "-R", "-m", "o::---", path},
		// Default ACLs for new files (inherit)
		{"setfacl", "-d", "-m", fmt.Sprintf("u:%s:rwx", username), path},
		{"setfacl", "-d", "-m", "u:fastcp:rwx", path},
		{"setfacl", "-d", "-m", "u:root:rwx", path},
		{"setfacl", "-d", "-m", "g::---", path},
		{"setfacl", "-d", "-m", "o::---", path},
	}

	for _, cmdArgs := range cmds {
		cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
		_ = cmd.Run()
	}
}

