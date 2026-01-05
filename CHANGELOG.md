# Changelog

All notable changes to FastCP will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Beautiful new UI/UX with modern design system
- Outfit and JetBrains Mono fonts for better typography
- Animated gradient backgrounds and glass morphism effects
- Update notification card in sidebar with elegant styling
- Improved login page with floating gradient orbs

### Changed
- Redesigned dashboard with enhanced stat cards
- Improved sidebar navigation with active state indicators
- Better upgrade modal with version comparison display
- Refined color palette (deep navy instead of pure black)

### Fixed
- Removed duplicate header border in main layout
- Fixed MySQL installation on Ubuntu 22.04 (auth_socket support)
- Fixed WordPress plugin deletion permissions (ACL improvements)

## [0.1.3] - 2026-01-05

### Added
- Port availability checks before installation (80, 443, 8080)
- Asynchronous MySQL installation with progress tracking
- Auto-upgrade functionality from control panel
- Version check against fastcp.org API

### Fixed
- MySQL root authentication on Ubuntu 22.04+
- Installer now properly validates Ubuntu version (22.04+)
- WordPress file permissions for plugin management

## [0.1.2] - 2026-01-04

### Added
- Database management (MySQL installation, create/delete databases)
- User impersonation for admins
- PHP version management

### Changed
- Improved error handling throughout the API

## [0.1.1] - 2026-01-03

### Added
- Site management (create, delete, suspend, unsuspend)
- SSL certificate provisioning via Caddy
- Worker mode support for Laravel Octane

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

[Unreleased]: https://github.com/rehmatworks/fastcp/compare/v0.1.3...HEAD
[0.1.3]: https://github.com/rehmatworks/fastcp/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/rehmatworks/fastcp/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/rehmatworks/fastcp/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/rehmatworks/fastcp/releases/tag/v0.1.0

