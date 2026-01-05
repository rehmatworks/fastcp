export interface User {
  id: string
  username: string
  email: string
  role: string
}

export interface Site {
  id: string
  user_id: string
  name: string
  domain: string
  aliases: string[]
  php_version: string
  root_path: string
  public_path: string
  app_type: 'blank' | 'wordpress'
  database_id?: string
  worker_mode: boolean
  worker_file: string
  worker_num: number
  ssl: boolean
  status: 'active' | 'suspended' | 'pending'
  environment: Record<string, string>
  created_at: string
  updated_at: string
}

export interface PHPInstance {
  version: string
  port: number
  admin_port: number
  binary_path: string
  config_path: string
  status: 'running' | 'stopped' | 'error'
  site_count: number
  thread_count: number
  max_threads: number
  started_at: string
}

export interface Stats {
  total_sites: number
  active_sites: number
  total_users: number
  php_instances: number
  disk_usage: number
  disk_total: number
  memory_usage: number
  memory_total: number
  cpu_usage: number
  uptime: number
}

export interface APIKey {
  id: string
  name: string
  key: string
  permissions: string[]
  user_id: string
  last_used_at: string
  created_at: string
  expires_at: string
}

export interface LoginResponse {
  token: string
  expires_in: number
  user: User
}

export interface APIResponse<T> {
  data?: T
  error?: string
}

