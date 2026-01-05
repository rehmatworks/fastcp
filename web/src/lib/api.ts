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
  created_at: string
}

export interface CreateDatabaseRequest {
  name: string
  username?: string
  password?: string
  site_id?: string
}

export interface DatabaseStatus {
  installed: boolean
  running: boolean
  version?: string
  database_count: number
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

export const api = new APIClient()

