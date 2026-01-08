package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rehmatworks/fastcp/internal/auth"
	"github.com/rehmatworks/fastcp/internal/middleware"
	"github.com/rehmatworks/fastcp/internal/models"
)

// LoginRequest represents a login request
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse represents a login response
type LoginResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"` // seconds
	User      struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Email    string `json:"email"`
		Role     string `json:"role"`
	} `json:"user"`
}

// login handles user authentication
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Username == "" || req.Password == "" {
		s.error(w, http.StatusBadRequest, "username and password are required")
		return
	}

	user, err := auth.Authenticate(req.Username, req.Password)
	if err != nil {
		s.logger.Warn("failed login attempt", "username", req.Username, "error", err)
		s.error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := auth.GenerateToken(user)
	if err != nil {
		s.logger.Error("failed to generate token", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	resp := LoginResponse{
		Token:     token,
		ExpiresIn: 86400, // 24 hours
		User: struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Email    string `json:"email"`
			Role     string `json:"role"`
		}{
			ID:       user.ID,
			Username: user.Username,
			Email:    user.Email,
			Role:     user.Role,
		},
	}

	s.logger.Info("user logged in", "username", user.Username)
	s.success(w, resp)
}

// refreshToken refreshes an authentication token
func (s *Server) refreshToken(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		s.error(w, http.StatusUnauthorized, "invalid token")
		return
	}

	// Create a new token with refreshed expiry
	user := &models.User{
		ID:       claims.UserID,
		Username: claims.Username,
		Role:     claims.Role,
	}

	// Generate new token with refreshed expiry
	token, err := auth.GenerateToken(user)
	if err != nil {
		s.logger.Error("failed to generate refreshed token", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to refresh token")
		return
	}

	s.success(w, map[string]interface{}{
		"message": "token refreshed",
		"token":   token,
		"user": map[string]interface{}{
			"id":       user.ID,
			"username": user.Username,
			"role":     user.Role,
		},
	})
}

// getCurrentUser returns the current authenticated user
func (s *Server) getCurrentUser(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		s.error(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	s.success(w, map[string]interface{}{
		"id":       claims.UserID,
		"username": claims.Username,
		"role":     claims.Role,
	})
}

// changePassword handles password change requests
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		s.error(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		s.error(w, http.StatusBadRequest, "current_password and new_password are required")
		return
	}

	if len(req.NewPassword) < 8 {
		s.error(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}

	// Verify current password
	_, err := auth.Authenticate(claims.Username, req.CurrentPassword)
	if err != nil {
		s.error(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	// Change password using chpasswd or sudo chpasswd when configured
	if out, err := runChpasswd(claims.Username, req.NewPassword); err != nil {
		s.logger.Error("failed to change password", "error", err, "output", string(out))
		s.error(w, http.StatusInternalServerError, fmt.Sprintf("failed to change password: %v", err))
		return
	}

	s.logger.Info("password changed", "username", claims.Username)
	s.success(w, map[string]string{
		"message": "password changed successfully",
	})
}

// SSHKey represents an SSH public key
type SSHKey struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   string `json:"public_key"`
	AddedAt     string `json:"added_at"`
}

// getSSHKeys returns all SSH keys for the current user
func (s *Server) getSSHKeys(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		s.error(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	keys, err := s.readUserSSHKeys(claims.Username)
	if err != nil {
		s.logger.Error("failed to read SSH keys", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to read SSH keys")
		return
	}

	s.success(w, map[string]interface{}{
		"ssh_keys": keys,
		"total":    len(keys),
	})
}

// addSSHKey adds a new SSH public key for the current user
func (s *Server) addSSHKey(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		s.error(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	var req struct {
		Name      string `json:"name"`
		PublicKey string `json:"public_key"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.PublicKey == "" {
		s.error(w, http.StatusBadRequest, "name and public_key are required")
		return
	}

	// Validate SSH key format
	publicKey := strings.TrimSpace(req.PublicKey)
	if !isValidSSHKey(publicKey) {
		s.error(w, http.StatusBadRequest, "invalid SSH public key format")
		return
	}

	// Get fingerprint
	fingerprint, err := getSSHKeyFingerprint(publicKey)
	if err != nil {
		s.error(w, http.StatusBadRequest, "invalid SSH key: "+err.Error())
		return
	}

	// Check if key already exists
	existingKeys, _ := s.readUserSSHKeys(claims.Username)
	for _, key := range existingKeys {
		if key.Fingerprint == fingerprint {
			s.error(w, http.StatusConflict, "this SSH key is already added")
			return
		}
	}

	// Add to authorized_keys
	if err := s.addToAuthorizedKeys(claims.Username, publicKey, req.Name); err != nil {
		s.logger.Error("failed to add SSH key", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to add SSH key")
		return
	}

	s.logger.Info("SSH key added", "username", claims.Username, "name", req.Name)
	s.success(w, map[string]string{
		"message":     "SSH key added successfully",
		"fingerprint": fingerprint,
	})
}

// deleteSSHKey removes an SSH key for the current user
func (s *Server) deleteSSHKey(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		s.error(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	fingerprint := chi.URLParam(r, "fingerprint")
	if fingerprint == "" {
		s.error(w, http.StatusBadRequest, "fingerprint is required")
		return
	}

	if err := s.removeFromAuthorizedKeys(claims.Username, fingerprint); err != nil {
		s.logger.Error("failed to remove SSH key", "error", err)
		s.error(w, http.StatusInternalServerError, "failed to remove SSH key")
		return
	}

	s.logger.Info("SSH key removed", "username", claims.Username, "fingerprint", fingerprint)
	s.success(w, map[string]string{
		"message": "SSH key removed successfully",
	})
}

// getConnectionInfo returns SFTP/SSH connection details for the current user
func (s *Server) getConnectionInfo(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		s.error(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	// Get public IP address
	host := getPublicIP()
	if host == "" {
		host = "your-server-ip"
	}

	// Check if user is jailed (SFTP-only)
	isJailed := false
	cmd := exec.Command("groups", claims.Username)
	if output, err := cmd.Output(); err == nil {
		isJailed = strings.Contains(string(output), "fastcp-jail")
	}

	info := map[string]interface{}{
		"host":         host,
		"port":         22,
		"username":     claims.Username,
		"protocol":     "sftp",
		"ssh_enabled":  !isJailed,
		"sftp_enabled": true,
	}

	if isJailed {
		info["home_dir"] = fmt.Sprintf("/home/%s/www", claims.Username)
		info["note"] = "Your account is configured for SFTP only (no SSH shell access)"
	} else {
		info["home_dir"] = fmt.Sprintf("/home/%s", claims.Username)
	}

	s.success(w, info)
}

// Helper functions

func isValidSSHKey(key string) bool {
	parts := strings.Fields(key)
	if len(parts) < 2 {
		return false
	}
	keyTypes := []string{"ssh-rsa", "ssh-ed25519", "ssh-dss", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521"}
	for _, t := range keyTypes {
		if parts[0] == t {
			return true
		}
	}
	return false
}

func getSSHKeyFingerprint(publicKey string) (string, error) {
	// Use ssh-keygen to get fingerprint
	output, err := runCommandWithInput(publicKey, "ssh-keygen", "-lf", "-")
	if err != nil {
		return "", fmt.Errorf("invalid SSH key")
	}

	// Output format: "256 SHA256:xxxxx comment (TYPE)"
	parts := strings.Fields(string(output))
	if len(parts) >= 2 {
		return parts[1], nil
	}
	return "", fmt.Errorf("could not extract fingerprint")
}

func (s *Server) readUserSSHKeys(username string) ([]SSHKey, error) {
	homeDir := fmt.Sprintf("/home/%s", username)
	authKeysPath := fmt.Sprintf("%s/.ssh/authorized_keys", homeDir)

	data, err := runCommand("cat", authKeysPath)
	if err != nil {
		return []SSHKey{}, nil // File doesn't exist or can't read
	}

	var keys []SSHKey
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			fingerprint, _ := getSSHKeyFingerprint(line)
			name := ""
			if len(parts) >= 3 {
				name = parts[2]
			}
			keys = append(keys, SSHKey{
				ID:          fmt.Sprintf("%d", i),
				Name:        name,
				Fingerprint: fingerprint,
				PublicKey:   line,
			})
		}
	}

	return keys, nil
}

func (s *Server) addToAuthorizedKeys(username, publicKey, name string) error {
	homeDir := fmt.Sprintf("/home/%s", username)
	sshDir := fmt.Sprintf("%s/.ssh", homeDir)
	authKeysPath := fmt.Sprintf("%s/authorized_keys", sshDir)

	// Ensure .ssh directory exists with correct permissions
	_, _ = runCommand("mkdir", "-p", sshDir)
	_, _ = runCommand("chmod", "700", sshDir)
	_, _ = runCommand("chown", fmt.Sprintf("%s:%s", username, username), sshDir)

	// Append to authorized_keys
	keyLine := strings.TrimSpace(publicKey)
	// Add name as comment if not already present
	parts := strings.Fields(keyLine)
	if len(parts) == 2 && name != "" {
		keyLine = keyLine + " " + name
	}

	if output, err := runCommand("bash", "-c", fmt.Sprintf("echo '%s' >> %s", keyLine, authKeysPath)); err != nil {
		return fmt.Errorf("failed to write key: %s", string(output))
	}

	// Fix permissions
	_, _ = runCommand("chmod", "600", authKeysPath)
	_, _ = runCommand("chown", fmt.Sprintf("%s:%s", username, username), authKeysPath)

	return nil
}

func (s *Server) removeFromAuthorizedKeys(username, fingerprint string) error {
	keys, err := s.readUserSSHKeys(username)
	if err != nil {
		return err
	}

	// Filter out the key with matching fingerprint
	var newKeys []string
	for _, key := range keys {
		if key.Fingerprint != fingerprint {
			newKeys = append(newKeys, key.PublicKey)
		}
	}

	// Rewrite authorized_keys
	authKeysPath := fmt.Sprintf("/home/%s/.ssh/authorized_keys", username)
	content := strings.Join(newKeys, "\n")
	if content != "" {
		content += "\n"
	}

	if output, err := runCommand("bash", "-c", fmt.Sprintf("echo -n '%s' > %s", content, authKeysPath)); err != nil {
		return fmt.Errorf("failed to write keys: %s", string(output))
	}

	// Fix permissions
	_, _ = runCommand("chmod", "600", authKeysPath)
	_, _ = runCommand("chown", fmt.Sprintf("%s:%s", username, username), authKeysPath)

	return nil
}

// getPublicIP returns the server's public IP address
func getPublicIP() string {
	// Try multiple services for reliability
	services := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://icanhazip.com",
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	for _, service := range services {
		resp, err := client.Get(service)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				continue
			}
			ip := strings.TrimSpace(string(body))
			// Validate it looks like an IP
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// Fallback: try to get local IP that's likely public-facing
	return getLocalIP()
}

// getLocalIP returns the local IP address (fallback)
func getLocalIP() string {
	// Try to find a non-loopback IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				// Skip private/local IPs if possible
				ip := ipnet.IP.String()
				// Return first non-private IP, or any non-loopback as fallback
				if !isPrivateIP(ipnet.IP) {
					return ip
				}
			}
		}
	}

	// If no public IP found, return any non-loopback IPv4
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	return ""
}

// isPrivateIP checks if an IP is in private ranges
func isPrivateIP(ip net.IP) bool {
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}

	for _, cidr := range privateRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
