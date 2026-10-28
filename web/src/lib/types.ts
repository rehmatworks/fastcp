// Shapes returned by the FastCP API. Keep in sync with internal/api.

export interface UserView {
  username: string;
  is_admin: boolean;
  is_suspended: boolean;
  memory_mb: number;
  cpu_percent: number;
  max_sites: number;
  storage_mb: number;
}

export interface User extends UserView {
  id: number;
  site_count: number;
  created_at: string;
}

export interface Me {
  user: UserView;
  actor: UserView;
  impersonating: boolean;
  acting_until?: string;
  expires_at: string;
}

export interface LoginResponse {
  token: string;
  me: Me;
}

export interface Meta {
  version: string;
  hostname: string;
  ssh_port: number;
  php_versions: string[];
  default_php: string;
}

export interface Domain {
  id: number;
  site_id: string;
  domain: string;
  is_primary: boolean;
  redirect_to_primary: boolean;
}

export type SSLState = 'valid' | 'pending' | 'unreachable' | 'mismatch' | 'untrusted' | 'expired';

export interface SSLStatus {
  valid: boolean;
  status: SSLState;
  message: string;
  issuer?: string;
  expires_at?: string;
  checked_at: string;
}

export interface Site {
  id: string;
  username: string;
  domain: string;
  slug: string;
  site_type: 'php' | 'wordpress';
  document_root: string;
  force_https: boolean;
  compression_enabled: boolean;
  gzip_enabled: boolean;
  zstd_enabled: boolean;
  cache_control_enabled: boolean;
  cache_control_value: string;
  php_version: string;
  php_memory_limit: string;
  php_post_max_size: string;
  php_upload_max_filesize: string;
  php_max_execution_time: number;
  php_max_input_vars: number;
  created_at: string;
  domains: Domain[];
  ssl: Record<string, SSLStatus>;
}

export type SiteSettings = Partial<
  Pick<
    Site,
    | 'force_https'
    | 'compression_enabled'
    | 'gzip_enabled'
    | 'zstd_enabled'
    | 'cache_control_enabled'
    | 'cache_control_value'
    | 'php_version'
    | 'php_memory_limit'
    | 'php_post_max_size'
    | 'php_upload_max_filesize'
    | 'php_max_execution_time'
    | 'php_max_input_vars'
  >
>;

export interface Database {
  id: string;
  username: string;
  db_name: string;
  db_user: string;
  created_at: string;
}

export interface DatabaseCredentials extends Database {
  password: string;
  host: string;
  port: number;
}

export interface CronJob {
  id: string;
  username: string;
  name: string;
  expression: string;
  command: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
  description: string;
  next_run?: string;
}

export interface CronDescription {
  valid: boolean;
  description?: string;
  message?: string;
  next_runs?: string[];
}

export interface SSHKey {
  id: string;
  username: string;
  name: string;
  public_key: string;
  fingerprint: string;
  created_at: string;
}

export type BackendType = 'sftp' | 's3' | 'local' | 'rclone';

export interface BackupConfig {
  backend_type: BackendType;
  repository: string;
  local_path: string;
  sftp_username: string;
  sftp_host: string;
  sftp_port: number;
  sftp_path: string;
  s3_endpoint: string;
  s3_bucket: string;
  s3_prefix: string;
  s3_region: string;
  s3_bucket_lookup: string;
  s3_list_objects_v1: boolean;
  has_password: boolean;
  has_s3_keys: boolean;
  enabled: boolean;
  schedule_cron: string;
  schedule_description: string;
  exclude_site_ids: string[];
  exclude_database_ids: string[];
  keep_last: number;
  keep_daily: number;
  keep_weekly: number;
  keep_monthly: number;
  last_run_at?: string;
  next_run_at?: string;
  last_status: string;
  last_message: string;
  running_job_id?: string;
}

export interface BackupConfigRequest {
  backend_type: BackendType;
  local_path: string;
  rclone_repository: string;
  sftp_username: string;
  sftp_host: string;
  sftp_port: number;
  sftp_path: string;
  s3_endpoint: string;
  s3_bucket: string;
  s3_prefix: string;
  s3_region: string;
  s3_bucket_lookup: string;
  s3_list_objects_v1: boolean;
  s3_access_key: string;
  s3_secret_key: string;
  s3_session_token: string;
  repository_password: string;
  enabled: boolean;
  schedule_cron: string;
  exclude_site_ids: string[];
  exclude_database_ids: string[];
  keep_last: number;
  keep_daily: number;
  keep_weekly: number;
  keep_monthly: number;
}

export interface NamedItem {
  id: string;
  name: string;
}

export interface BackupsResponse {
  configured: boolean;
  config: BackupConfig;
  sites: NamedItem[];
  databases: NamedItem[];
  rclone_installed?: boolean;
}

export type BackupJobStatus = 'running' | 'success' | 'failed';

export interface BackupJob {
  id: string;
  username: string;
  job_type: 'manual' | 'scheduled' | 'restore_site' | 'restore_database' | 'export' | 'delete_snapshot' | string;
  status: BackupJobStatus;
  snapshot_id?: string;
  message?: string;
  artifact?: string;
  started_at: string;
  finished_at?: string;
}

export interface Snapshot {
  id: string;
  short_id: string;
  time: string;
  sites: string[];
  databases: string[];
  site_ids: string[];
  database_ids: string[];
  bytes?: number;
  files?: number;
  data_added?: number;
}

export type AgentJobStatus = 'queued' | 'running' | 'succeeded' | 'failed';

export interface AgentJob {
  id: string;
  kind: string;
  target?: string;
  status: AgentJobStatus;
  message?: string;
  log?: string;
  started_at?: string;
  finished_at?: string;
}

export interface SystemStatus {
  hostname: string;
  os: string;
  kernel: string;
  architecture: string;
  uptime: number;
  load_average: [number, number, number];
  cpu_count: number;
  memory_total: number;
  memory_used: number;
  swap_total: number;
  swap_used: number;
  disk_total: number;
  disk_used: number;
  mysql_version: string;
  caddy_version: string;
  php_versions: string[];
  agent_version: string;
}

export interface ServiceStatus {
  name: string;
  description: string;
  active: boolean;
  state: string;
  enabled: boolean;
}

export interface UpdateInfo {
  current_version: string;
  latest_version?: string;
  update_available: boolean;
  checked_at: string;
  error?: string;
  release_notes_url?: string;
}

export interface UpdatesResponse {
  info: UpdateInfo;
  agent_version?: string;
  job?: AgentJob | null;
}

export type PHPSupport = 'active' | 'security' | 'eol' | 'unknown';

export interface PHPVersionInfo {
  version: string;
  installed: boolean;
  available: boolean;
  patch_version?: string;
  candidate?: string;
  eol_date?: string;
  status: PHPSupport;
  default: boolean;
  sites: number;
  fpm_running: boolean;
}

export interface PHPSettings {
  default_version: string;
  auto_patch: boolean;
  auto_install: boolean;
  follow_latest_default: boolean;
  maintenance_hour: number;
  maintenance_minute: number;
}

export interface PHPStatus {
  versions: PHPVersionInfo[];
  settings: PHPSettings;
  repository: string;
  repository_error?: string;
  last_maintenance?: AgentJob;
  next_maintenance?: string;
  jobs: AgentJob[];
}

export interface AccountSummary {
  sites: number;
  databases: number;
  cron_jobs: number;
  ssh_keys: number;
  limits: UserView;
  backup: {
    configured: boolean;
    enabled: boolean;
    last_status: string;
    last_run_at?: string;
    next_run_at?: string;
  };
  recent_jobs: BackupJob[];
}

export interface ServerSummary {
  status?: SystemStatus;
  services: ServiceStatus[];
  users: number;
  sites: number;
  databases: number;
  update?: UpdateInfo;
  php: PHPVersionInfo[];
  errors?: string[];
}

export interface OverviewResponse {
  account: AccountSummary;
  server?: ServerSummary;
}

export type CaddyProfile = 'low_ram' | 'balanced' | 'high_throughput' | 'custom';

export interface CaddyConfig {
  profile: CaddyProfile;
  access_logs: boolean;
  read_header: string;
  read_body: string;
  write_timeout: string;
  idle_timeout: string;
  grace_period: string;
  max_header_size: number;
}

export interface MySQLConfig {
  buffer_pool_mb: number;
  max_connections: number;
  perf_schema: boolean;
  detected_ram_mb?: number;
}

export interface SSHConfig {
  port: number;
  password_auth: boolean;
  keys_installed?: boolean;
}

export interface FirewallRule {
  port: number;
  protocol: string;
  action: string;
  from: string;
  ip_version: string;
  protected: boolean;
  raw: string;
}

export interface FirewallStatus {
  installed: boolean;
  enabled: boolean;
  available: boolean;
  message?: string;
  ssh_ports: number[];
  protected_ports: number[];
  rules: FirewallRule[];
}

export interface ToolStatus {
  installed: boolean;
  version?: string;
}
