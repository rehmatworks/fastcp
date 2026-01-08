import type { Site, PHPInstance, Stats, LoginResponse, APIKey } from '@/types'

const API_BASE = '/api/v1'

class APIClient {
  private token: string | null = null
  private impersonating: string | null = null

  constructor() {
    this.token = localStorage.getItem('fastcp_token')
    this.impersonating = sessionStorage.getItem('impersonating')
  }

  setToken(token: string | null) {
    this.token = token
    if (token) {
      localStorage.setItem('fastcp_token', token)
    } else {
      localStorage.removeItem('fastcp_token')
    }
  }

  getToken(): string | null {
    return this.token
  }

  setImpersonating(username: string | null) {
    this.impersonating = username
  }

  getImpersonating(): string | null {
    return this.impersonating
  }

  private async request<T>(
    endpoint: string,
    options: RequestInit = {}
  ): Promise<T> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      ...((options.headers as Record<string, string>) || {}),
    }

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`
    }

    // Add impersonation header if impersonating
    if (this.impersonating) {
      headers['X-Impersonate-User'] = this.impersonating
    }

    const response = await fetch(`${API_BASE}${endpoint}`, {
      ...options,
      headers,
    })

    if (response.status === 401) {
      this.setToken(null)
      window.location.href = '/login'
      throw new Error('Unauthorized')
    }

    const data = await response.json()

    if (!response.ok) {
      throw new Error(data.error || 'Request failed')
    }

    return data
  }

  // Auth
  async login(username: string, password: string): Promise<LoginResponse> {
    const data = await this.request<LoginResponse>('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    })
    this.setToken(data.token)
    return data
  }

  logout() {
    this.setToken(null)
  }

  async getCurrentUser() {
    return this.request<{ id: string; username: string; role: string }>('/me')
  }

  // Sites
  async getSites(): Promise<{ sites: Site[]; total: number }> {
    return this.request('/sites')
  }

  async getSite(id: string): Promise<Site> {
    return this.request(`/sites/${id}`)
  }

  async createSite(site: Partial<Site>): Promise<Site> {
    return this.request('/sites', {
      method: 'POST',
      body: JSON.stringify(site),
    })
  }

  async updateSite(id: string, site: Partial<Site>): Promise<Site> {
    return this.request(`/sites/${id}`, {
      method: 'PUT',
      body: JSON.stringify(site),
    })
  }

  async deleteSite(id: string): Promise<void> {
    await this.request(`/sites/${id}`, { method: 'DELETE' })
  }

  async suspendSite(id: string): Promise<void> {
    await this.request(`/sites/${id}/suspend`, { method: 'POST' })
  }

  async unsuspendSite(id: string): Promise<void> {
    await this.request(`/sites/${id}/unsuspend`, { method: 'POST' })
  }

  async restartSiteWorkers(id: string): Promise<void> {
    await this.request(`/sites/${id}/restart-workers`, { method: 'POST' })
  }

  // PHP Instances
  async getPHPInstances(): Promise<{ instances: PHPInstance[]; total: number }> {
    return this.request('/php')
  }

  async getPHPInstance(version: string): Promise<PHPInstance> {
    return this.request(`/php/${version}`)
  }

  async startPHPInstance(version: string): Promise<void> {
    await this.request(`/php/${version}/start`, { method: 'POST' })
  }

  async stopPHPInstance(version: string): Promise<void> {
    await this.request(`/php/${version}/stop`, { method: 'POST' })
  }

  async restartPHPInstance(version: string): Promise<void> {
    await this.request(`/php/${version}/restart`, { method: 'POST' })
  }

  async restartPHPWorkers(version: string): Promise<void> {
    await this.request(`/php/${version}/restart-workers`, { method: 'POST' })
  }

  // Stats
  async getStats(): Promise<Stats> {
    return this.request('/stats')
  }

  // API Keys
  async getAPIKeys(): Promise<{ api_keys: APIKey[]; total: number }> {
    return this.request('/api-keys')
  }

  async createAPIKey(name: string, permissions: string[]): Promise<APIKey> {
    return this.request('/api-keys', {
      method: 'POST',
      body: JSON.stringify({ name, permissions }),
    })
  }

  async deleteAPIKey(id: string): Promise<void> {
    await this.request(`/api-keys/${id}`, { method: 'DELETE' })
  }

  // Admin
  async reloadAll(): Promise<void> {
    await this.request('/reload', { method: 'POST' })
  }

  // Users (Admin only)
  async getUsers(): Promise<{ users: FastCPUser[]; total: number }> {
    return this.request('/users')
  }

  async getUser(username: string): Promise<FastCPUser> {
    return this.request(`/users/${username}`)
  }

  async createUser(user: CreateUserRequest): Promise<FastCPUser> {
    return this.request('/users', {
      method: 'POST',
      body: JSON.stringify(user),
    })
  }

  async updateUser(username: string, data: UpdateUserRequest): Promise<FastCPUser> {
    return this.request(`/users/${username}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    })
  }

  async deleteUser(username: string): Promise<void> {
    await this.request(`/users/${username}`, { method: 'DELETE' })
  }

  async fixUserPermissions(): Promise<{ message: string; users_fixed: number; errors: number }> {
    return this.request('/users/fix-permissions', { method: 'POST' })
  }

  // Databases
  async getDatabases(): Promise<{ databases: DatabaseItem[]; total: number }> {
    return this.request('/databases')
  }

  async getDatabase(id: string): Promise<DatabaseItem> {
    return this.request(`/databases/${id}`)
  }

  async createDatabase(data: CreateDatabaseRequest): Promise<DatabaseItem> {
    return this.request('/databases', {
      method: 'POST',
      body: JSON.stringify(data),
    })
  }

  async deleteDatabase(id: string): Promise<void> {
    await this.request(`/databases/${id}`, { method: 'DELETE' })
  }

  async resetDatabasePassword(id: string, password: string): Promise<void> {
    await this.request(`/databases/${id}/reset-password`, {
      method: 'POST',
      body: JSON.stringify({ password }),
    })
  }

  async getDatabaseStatus(): Promise<DatabaseStatus> {
    return this.request('/databases/status')
  }

  async installMySQL(): Promise<{ message: string; status: string }> {
    return this.request('/databases/install', { method: 'POST' })
  }

  async getMySQLInstallStatus(): Promise<MySQLInstallStatus> {
    return this.request('/databases/install/status')
  }

  async installPostgreSQL(): Promise<{ message: string; status: string }> {
    return this.request('/databases/install/postgresql', { method: 'POST' })
  }

  async getPostgreSQLInstallStatus(): Promise<MySQLInstallStatus> {
    return this.request('/databases/install/postgresql/status')
  }

  // Version & Upgrade
  async getVersion(): Promise<VersionCheckResult> {
    return this.request('/version')
  }

  async startUpgrade(): Promise<{ message: string; status: string }> {
    return this.request('/upgrade', { method: 'POST' })
  }

  async getUpgradeStatus(): Promise<UpgradeStatus> {
    return this.request('/upgrade/status')
  }

  // Profile / Connection info
  async getConnectionInfo(): Promise<ConnectionInfo> {
    return this.request('/me/connection')
  }

  async changePassword(currentPassword: string, newPassword: string): Promise<{ message: string }> {
    return this.request('/me/password', {
      method: 'PUT',
      body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    })
  }

  // SSH Keys
  async getSSHKeys(): Promise<{ ssh_keys: SSHKeyItem[]; total: number }> {
    return this.request('/me/ssh-keys')
  }

  async addSSHKey(name: string, publicKey: string): Promise<{ message: string; fingerprint: string }> {
    return this.request('/me/ssh-keys', {
      method: 'POST',
      body: JSON.stringify({ name, public_key: publicKey }),
    })
  }

  async deleteSSHKey(fingerprint: string): Promise<{ message: string }> {
    return this.request(`/me/ssh-keys/${encodeURIComponent(fingerprint)}`, { method: 'DELETE' })
  }

  // File Manager
  async getFiles(siteId: string, path: string = '.'): Promise<FileListResponse> {
    return this.request(`/sites/${siteId}/files?path=${encodeURIComponent(path)}`)
  }

  async getFileContent(siteId: string, path: string): Promise<FileContent> {
    return this.request(`/sites/${siteId}/files/content?path=${encodeURIComponent(path)}`)
  }

  async saveFileContent(siteId: string, path: string, content: string): Promise<{ message: string }> {
    return this.request(`/sites/${siteId}/files/content`, {
      method: 'PUT',
      body: JSON.stringify({ path, content }),
    })
  }

  async createDirectory(siteId: string, path: string, dirName: string): Promise<{ message: string }> {
    return this.request(`/sites/${siteId}/files/directory`, {
      method: 'POST',
      body: JSON.stringify({ path, dir_name: dirName }),
    })
  }

  async deleteFile(siteId: string, path: string): Promise<{ message: string }> {
    return this.request(`/sites/${siteId}/files?path=${encodeURIComponent(path)}`, { method: 'DELETE' })
  }

  // SSH Server Settings (Admin only)
  async getSSHSettings(): Promise<SSHServerSettings> {
    return this.request('/ssh-settings')
  }

  async updateSSHSettings(settings: SSHServerSettings): Promise<{ message: string }> {
    return this.request('/ssh-settings', {
      method: 'PUT',
      body: JSON.stringify(settings),
    })
  }

  // SSL Certificate methods
  async getCertificates(): Promise<{ certificates: SSLCertificate[] }> {
    return this.request('/certificates')
  }

  async getCertificate(id: string): Promise<SSLCertificate> {
    return this.request(`/certificates/${id}`)
  }

  async getSiteCertificates(siteId: string): Promise<{ certificates: SSLCertificate[] }> {
    return this.request(`/sites/${siteId}/certificates`)
  }

  async issueCertificate(request: SSLCertificateRequest): Promise<SSLCertificate> {
    return this.request('/certificates', {
      method: 'POST',
      body: JSON.stringify(request)
    })
  }

  async deleteCertificate(id: string): Promise<void> {
    await this.request(`/certificates/${id}`, {
      method: 'DELETE'
    })
  }

  async renewCertificate(id: string): Promise<SSLCertificate> {
    return this.request(`/certificates/${id}/renew`, {
      method: 'POST'
    })
  }
}

// Database types
export interface DatabaseItem {
  id: string
  user_id: string
  site_id?: string
  name: string
  username: string
  password?: string
  host: string
  port: number
  type: string
  created_at: string
}

export interface CreateDatabaseRequest {
  name: string
  username?: string
  password?: string
  type: string
  site_id?: string
}

export interface DatabaseServerInfo {
  installed: boolean
  running: boolean
  version?: string
  database_count: number
}

export interface DatabaseStatus {
  mysql: DatabaseServerInfo
  postgresql: DatabaseServerInfo
}

export interface MySQLInstallStatus {
  in_progress: boolean
  success: boolean
  error?: string
  message?: string
  started_at?: string
}

export interface VersionCheckResult {
  current_version: string
  latest_version: string
  update_available: boolean
  release_name?: string
  release_url?: string
  changelog?: string
  published_at?: string
  check_error?: string
}

export interface UpgradeStatus {
  in_progress: boolean
  success: boolean
  error?: string
  message?: string
  progress?: number
  started_at?: string
  completed_at?: string
}

// User types
export interface FastCPUser {
  username: string
  uid: number
  gid: number
  home_dir: string
  is_admin: boolean
  enabled: boolean
  is_jailed: boolean
  shell_access: boolean
  site_limit: number
  ram_limit_mb: number
  cpu_percent: number
  max_processes: number
  site_count: number
  disk_used_mb: number
  ram_used_mb: number
  process_count: number
}

export interface CreateUserRequest {
  username: string
  password: string
  is_admin: boolean
  shell_access: boolean
  site_limit: number
  ram_limit_mb: number
  cpu_percent: number
  max_processes: number
}

export interface UpdateUserRequest {
  password?: string
  enabled?: boolean
  shell_access?: boolean
  site_limit?: number
  ram_limit_mb?: number
  cpu_percent?: number
  max_processes?: number
}

export interface ConnectionInfo {
  host: string
  port: number
  username: string
  protocol: string
  ssh_enabled: boolean
  sftp_enabled: boolean
  home_dir: string
  note?: string
}

export interface SSHKeyItem {
  id: string
  name: string
  fingerprint: string
  public_key: string
  added_at?: string
}

export interface SSHServerSettings {
  password_auth_enabled: boolean
}

// File Manager types
export interface FileInfo {
  name: string
  path: string
  size: number
  is_dir: boolean
  mod_time: string
  perm: string
}

export interface FileListResponse {
  path: string
  files: FileInfo[]
  can_read: boolean
  can_write: boolean
}

export interface FileContent {
  path: string
  content: string
  size: number
  can_edit: boolean
}

// SSL Certificate types
export interface SSLCertificate {
  id: string
  site_id: string
  domain: string
  type: 'letsencrypt' | 'custom' | 'self-signed'
  status: 'active' | 'pending' | 'expired' | 'failed'
  provider?: string
  auto_renew: boolean
  cert_path: string
  key_path: string
  chain_path?: string
  issuer?: string
  subject?: string
  valid_from: string
  valid_until: string
  last_renewed?: string
  created_at: string
  updated_at: string
}

export interface SSLCertificateRequest {
  site_id: string
  domain: string
  type: 'letsencrypt' | 'custom' | 'self-signed'
  provider?: 'letsencrypt' | 'zerossl'
  auto_renew: boolean
  email?: string
  custom_cert?: string
  custom_key?: string
  custom_ca?: string
}

const apiClient = new APIClient()

export default apiClient
export { apiClient as api }

