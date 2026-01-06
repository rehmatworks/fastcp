# Changelog

All notable changes to FastCP will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2026-01-06

### Added
- **SSH Key Management** - Add, list, and delete SSH public keys from settings
- **Password Change** - Users can change their own password from settings
- **SFTP/SSH Connection Info** - Display connection details (host, port, username, home directory)
- **Connection Status Badges** - Show SFTP/SSH enabled status
- **SSH Password Auth Toggle** - Admins can enable/disable server-wide password authentication for SSH/SFTP
- **Public IP Detection** - SSH/SFTP connection info now shows server's public IP instead of hostname

### Changed
- **Unified Home Directory Structure** - ALL sites now stored in `/home/username/www/domain/`. No more `/var/www`. Every site belongs to a user, including admin sites.

### Fixed
- **Settings Access** - Non-admin users can now access Settings page for SSH keys and password
- **Critical: RootPath preservation** - Site root path no longer changes when domain is updated
- **Critical: Jail isolation** - Disabling shell for a user no longer affects root SSH access
- **SFTP File Permissions** - Files uploaded via SFTP now accessible to PHP/WordPress (ACL fix)
- **Site Details Overflow** - Long root paths no longer overflow the info widget
- **Self-disable Prevention** - Users cannot disable their own account

### Security
- Added safeguards to prevent jailing root or admin users
- SSH key fingerprint validation prevents duplicate keys

## [0.1.9] - 2026-01-06

### Fixed
- GitHub Actions release workflow syntax errors

## [0.1.8] - 2026-01-06

### Changed
- **Domain Management UI** - Chip-based interface for managing additional domains (aliases)
- **Removed Worker Mode** - Disabled PHP worker mode feature (was causing PHP instance crashes)

### Fixed
- Duplicate domain validation - Frontend now prevents adding primary domain as an alias
- Domain normalization - Automatic lowercase, trim whitespace, strip http(s):// and paths

## [0.1.2] - 2026-01-06

### Added
- **Light/Dark Mode Support** - Full theme system with light, dark, and system preference options
- **Theme Toggle** - Theme switcher in sidebar and login page
- **Search in Databases** - Search databases by name or username
- **Custom Confirmation Modals** - Beautiful modals replace browser alerts for deletions
- **Port Availability Checks** - Installer validates ports 80, 443, 8080 before installation
- **Async MySQL Installation** - Background installation with progress tracking
- **Auto-Upgrade System** - Version check and one-click upgrade from control panel
- **Proxied Installer** - install.sh proxied from GitHub for always-latest version

### Changed
- **Complete UI Overhaul** - Modern design with Outfit and JetBrains Mono fonts
- **Databases Page Redesign** - Table layout, search bar, less prominent MySQL status
- **Dashboard Enhancement** - Gradient stat cards with shine effects
- **Sidebar Navigation** - Active state indicators, upgrade card in sidebar
- **Login Page** - Floating gradient orbs, glass morphism effects
- **Color Palette** - Deep navy backgrounds, refined for light/dark modes
- **GitHub Actions** - Strips `v` prefix from version, improved release notes

### Fixed
- MySQL root authentication on Ubuntu 22.04+ (auth_socket support)
- WordPress plugin deletion permissions (ACL improvements)
- Duplicate header border removed
- Ubuntu version validation (22.04+ only)

## [0.1.1] - 2026-01-03

### Added
- Site management (create, delete, suspend, unsuspend)
- SSL certificate provisioning via Caddy
- Worker mode support for Laravel Octane
- Database management (MySQL installation, create/delete databases)
- User impersonation for admins
- PHP version management

### Fixed
- Various bug fixes and stability improvements

## [0.1.0] - 2026-01-02

### Added
- Initial release
- Admin panel with dashboard
- User authentication with JWT
- Site creation with PHP support
- Caddy web server integration
- FrankenPHP for PHP execution
- Systemd service management

[Unreleased]: https://github.com/rehmatworks/fastcp/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/rehmatworks/fastcp/compare/v0.1.9...v0.2.0
[0.1.9]: https://github.com/rehmatworks/fastcp/compare/v0.1.8...v0.1.9
[0.1.8]: https://github.com/rehmatworks/fastcp/compare/v0.1.2...v0.1.8
[0.1.2]: https://github.com/rehmatworks/fastcp/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/rehmatworks/fastcp/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/rehmatworks/fastcp/releases/tag/v0.1.0
