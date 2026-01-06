package jail

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// SSHConfigPath is the path to sshd_config
	SSHConfigPath = "/etc/ssh/sshd_config"
	// JailGroup is the group for jailed users
	JailGroup = "fastcp-jail"
	// JailMarker marks our config section
	JailMarker = "# FastCP Jail Configuration"
)

// SSHJailConfig represents the SSH jail configuration
var SSHJailConfig = `
# FastCP Jail Configuration - DO NOT EDIT MANUALLY
# This section is managed by FastCP

Match Group fastcp-jail
    ChrootDirectory %h
    ForceCommand internal-sftp -d /www
    AllowTcpForwarding no
    X11Forwarding no
    PermitTunnel no
    AllowAgentForwarding no
    PasswordAuthentication yes

# End FastCP Jail Configuration
`

// SetupJailGroup creates the jail group if it doesn't exist
func SetupJailGroup() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	// Create the jail group
	cmd := exec.Command("groupadd", "-f", JailGroup)
	_ = cmd.Run() // Ignore error if group exists

	return nil
}

// SetupSSHConfig adds the jail configuration to sshd_config
func SetupSSHConfig() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	// Read current config
	data, err := os.ReadFile(SSHConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read sshd_config: %w", err)
	}

	config := string(data)

	// Check if our config already exists
	if strings.Contains(config, JailMarker) {
		// Already configured
		return nil
	}

	// Append our config
	config = config + "\n" + SSHJailConfig

	// Write back
	if err := os.WriteFile(SSHConfigPath, []byte(config), 0644); err != nil {
		return fmt.Errorf("failed to write sshd_config: %w", err)
	}

	// Reload SSH
	_ = exec.Command("systemctl", "reload", "sshd").Run()
	_ = exec.Command("systemctl", "reload", "ssh").Run()

	return nil
}

// SetupUserJail sets up the jail environment for a user
func SetupUserJail(username string) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	// Never jail root or system users
	if username == "" || username == "root" {
		return fmt.Errorf("cannot jail root or empty username")
	}

	// Check if user is an admin (in sudo/wheel group) - never jail admins
	checkAdmin := exec.Command("groups", username)
	if output, err := checkAdmin.Output(); err == nil {
		groups := string(output)
		if strings.Contains(groups, "sudo") || strings.Contains(groups, "wheel") {
			return fmt.Errorf("cannot jail admin user %s", username)
		}
	}

	homeDir := fmt.Sprintf("/home/%s", username)

	// The home directory must be owned by root for chroot to work
	// Create subdirectories for user content
	dirs := []string{
		filepath.Join(homeDir, "www"),      // Web root for user's sites
		filepath.Join(homeDir, ".ssh"),     // For SSH keys
	}

	// Ensure home directory exists and is owned by root
	if err := os.MkdirAll(homeDir, 0755); err != nil {
		return err
	}

	// Home dir MUST be owned by root for chroot to work
	_ = exec.Command("chown", "root:root", homeDir).Run()
	_ = exec.Command("chmod", "755", homeDir).Run()

	// Create subdirectories
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	// Set up www directory - this is where user files go
	// PHP runs as the user, so no ACLs needed - simple Unix permissions
	wwwDir := filepath.Join(homeDir, "www")
	_ = exec.Command("chown", fmt.Sprintf("%s:%s", username, username), wwwDir).Run()
	_ = exec.Command("chmod", "755", wwwDir).Run()

	// Set up run/ and log/ directories for per-user PHP instances
	runDir := filepath.Join(homeDir, "run")
	logDir := filepath.Join(homeDir, "log")
	for _, dir := range []string{runDir, logDir} {
		_ = os.MkdirAll(dir, 0755)
		_ = exec.Command("chown", fmt.Sprintf("%s:%s", username, username), dir).Run()
	}

	// Set up .ssh directory
	sshDir := filepath.Join(homeDir, ".ssh")
	_ = exec.Command("chown", fmt.Sprintf("%s:%s", username, username), sshDir).Run()
	_ = exec.Command("chmod", "700", sshDir).Run()

	// Note: Sites are stored directly in /home/username/www/
	// No symlink to /var/www needed - everything is under home directory

	// Add user to jail group
	_ = exec.Command("usermod", "-aG", JailGroup, username).Run()

	return nil
}

// RemoveUserFromJail removes a user from jail group (for admin users who need shell)
func RemoveUserFromJail(username string) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	// Remove from jail group
	_ = exec.Command("gpasswd", "-d", username, JailGroup).Run()

	return nil
}

// IsUserJailed checks if a user is in the jail group
func IsUserJailed(username string) bool {
	cmd := exec.Command("groups", username)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), JailGroup)
}

// GetJailStatus returns jail status for a user
type JailStatus struct {
	IsJailed     bool   `json:"is_jailed"`
	HomeDir      string `json:"home_dir"`
	WWWDir       string `json:"www_dir"`
	CanSSH       bool   `json:"can_ssh"`
	SFTPOnly     bool   `json:"sftp_only"`
}

func GetJailStatus(username string) *JailStatus {
	isJailed := IsUserJailed(username)
	homeDir := fmt.Sprintf("/home/%s", username)
	wwwDir := filepath.Join(homeDir, "www")

	return &JailStatus{
		IsJailed: isJailed,
		HomeDir:  homeDir,
		WWWDir:   wwwDir,
		CanSSH:   !isJailed, // Non-jailed users can SSH
		SFTPOnly: isJailed,  // Jailed users are SFTP-only
	}
}

// Note: ACL functions removed - PHP now runs as the user via per-user FrankenPHP instances
// Each user's PHP process runs with their UID/GID, so no ACLs needed

// FixJailPermissions fixes permissions for all jailed users
func FixJailPermissions(username string) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	homeDir := fmt.Sprintf("/home/%s", username)

	// Home must be root-owned for chroot
	_ = exec.Command("chown", "root:root", homeDir).Run()
	_ = exec.Command("chmod", "755", homeDir).Run()

	// User directories owned by user - PHP runs as user so no ACLs needed
	for _, subdir := range []string{"www", "run", "log"} {
		dir := filepath.Join(homeDir, subdir)
		_ = os.MkdirAll(dir, 0755)
		_ = exec.Command("chown", "-R", fmt.Sprintf("%s:%s", username, username), dir).Run()
	}

	return nil
}

