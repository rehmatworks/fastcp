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

	homeDir := fmt.Sprintf("/home/%s", username)

	// The home directory must be owned by root for chroot to work
	// Create subdirectories for user content
	dirs := []string{
		filepath.Join(homeDir, "www"),      // Symlink to /var/www/username
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
	wwwDir := filepath.Join(homeDir, "www")
	_ = exec.Command("chown", fmt.Sprintf("%s:%s", username, username), wwwDir).Run()
	_ = exec.Command("chmod", "755", wwwDir).Run()

	// Set up .ssh directory
	sshDir := filepath.Join(homeDir, ".ssh")
	_ = exec.Command("chown", fmt.Sprintf("%s:%s", username, username), sshDir).Run()
	_ = exec.Command("chmod", "700", sshDir).Run()

	// Create symlink from /var/www/username to user's www directory
	// Actually, we'll use the home www as the actual storage
	varWwwDir := fmt.Sprintf("/var/www/%s", username)
	
	// Remove existing /var/www/username if it's a directory
	info, err := os.Lstat(varWwwDir)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			// Already a symlink, check if correct
			target, _ := os.Readlink(varWwwDir)
			if target == wwwDir {
				// Already correct
				goto addToGroup
			}
			os.Remove(varWwwDir)
		} else if info.IsDir() {
			// Move existing content to new location
			_ = exec.Command("cp", "-a", varWwwDir+"/.", wwwDir+"/").Run()
			_ = exec.Command("rm", "-rf", varWwwDir).Run()
		}
	}

	// Create symlink /var/www/username -> /home/username/www
	if err := os.Symlink(wwwDir, varWwwDir); err != nil {
		// If symlink fails, just ensure /var/www/username exists
		os.MkdirAll(varWwwDir, 0755)
	}

addToGroup:
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

// FixJailPermissions fixes permissions for all jailed users
func FixJailPermissions(username string) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	homeDir := fmt.Sprintf("/home/%s", username)
	wwwDir := filepath.Join(homeDir, "www")

	// Home must be root-owned for chroot
	_ = exec.Command("chown", "root:root", homeDir).Run()
	_ = exec.Command("chmod", "755", homeDir).Run()

	// www owned by user
	_ = exec.Command("chown", "-R", fmt.Sprintf("%s:%s", username, username), wwwDir).Run()
	_ = exec.Command("chmod", "755", wwwDir).Run()

	return nil
}

