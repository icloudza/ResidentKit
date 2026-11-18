// 与后端 internal/dto 对应的类型。

export interface Admin {
  id: number
  username: string
  role: string
  totp_enabled: boolean
  last_login_at?: string
  last_login_ip?: string
}

export interface TokenResponse {
  access_token: string
  expires_at: string
  admin: Admin
}

export interface Device {
  id: number
  device_uid: string
  name: string
  alias: string
  version: string
  online: boolean
  connected_at?: string
  last_ip: string
  network: string
  rtt_ms: number
  remote_commands: Record<string, string[]>
  info?: DeviceInfo
  metrics?: DeviceMetrics
  metrics_at?: string
  first_seen_at: string
  last_seen_at: string
}

// 设备信息（hello 上报，net.link/info 刷新）。
export interface DeviceInfo {
  model: string
  os: string
  app_version: string
}

// 心跳上报的资源指标（与 daemon RKProtocol.h 的 RKMetric* 对应）。
export interface DeviceMetrics {
  svc_footprint?: number
  svc_peak?: number
  svc_limit?: number
  svc_cpu?: number
  svc_threads?: number
  jetsam_band?: number
  dev_mem_total?: number
  dev_mem_used?: number
  dev_cpu?: number
  dev_cores?: number
  mem_pressure?: number
}

export interface DeviceToken {
  id: number
  name: string
  token_prefix: string
  device_id?: number
  last_used_at?: string
  revoked_at?: string
  created_at: string
}

export interface Event {
  id: number
  device_id: number
  msg_id: string
  type: string
  level: 'info' | 'notice' | 'warning' | 'error'
  payload?: unknown
  device_ts?: string
  created_at: string
}

export type InvocationStatus = 'pending' | 'sent' | 'succeeded' | 'failed' | 'expired' | 'lost'

export interface Invocation {
  id: number
  device_id: number
  module: string
  command: string
  arguments?: unknown
  status: InvocationStatus
  result?: unknown
  error_code?: number
  error_message?: string
  operator_id?: number
  api_key_id?: number
  idempotency_key?: string
  source_ip?: string
  expires_at: string
  sent_at?: string
  finished_at?: string
  created_at: string
}

export interface AuditLog {
  id: number
  admin_id?: number
  username: string
  action: string
  target: string
  ip: string
  success: boolean
  detail?: unknown
  created_at: string
}

export interface Page<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export interface Summary {
  server_version: string
  uptime_seconds: number
  devices_online: number
  devices_total: number
  avg_rtt_ms: number
  events_24h: number
  events_prev_24h: number
  events_hourly: { hour: string; count: number }[]
  event_types: { type: string; count: number }[] | null
  alerts_24h: number
  recent_alerts: Event[] | null
  invocations_24h: number
  invocations_ok_24h: number
  invocations_done_24h: number
  invocations_pending: number
  invocations_hourly: number[]
  gateway: { connections: number; banned_ips: number; deny_hourly: number[]; rtt_hourly_ms: number[] }
}

// ---- OTA ----

export type UpdateStatus = 'queued' | 'downloading' | 'installed' | 'failed' | 'rolled_back' | 'unsupported'

export interface OTAConfig {
  enabled: boolean
  public_key?: string
  repo_enabled: boolean // 软件源已开启：上传时可选择同步到源
}

export interface Release {
  id: number
  version: string
  size: number
  sha256: string
  notes: string
  status: 'published' | 'withdrawn'
  in_repo: boolean // 已同步到软件源
  created_at: string
  counts: Partial<Record<UpdateStatus, number>>
}

export interface Rollout {
  id: number
  release_id: number
  target: 'all' | 'devices'
  device_ids: number[]
  status: 'active' | 'paused' | 'cancelled' | 'completed'
  created_at: string
  updated_at: string
}

export interface DeviceUpdate {
  id: number
  device_id: number
  release_id: number
  version: string
  rollout_id: number
  status: UpdateStatus
  error?: string
  invocation_id?: number
  created_at: string
  updated_at: string
}

export interface ReleaseDetail {
  release: Release
  rollouts: Rollout[]
  devices: DeviceUpdate[]
}

export interface DeviceUpdateInfo {
  version: string
  supported: boolean
  latest?: Release
  history: DeviceUpdate[]
}


// ---- 远程配置 ----

export interface ConfigItem {
  key: string
  type: 'bool' | 'int' | 'number' | 'string' | 'enum'
  default: boolean | number | string
  label: string
  desc?: string
  min?: number
  max?: number
  options?: string[]
  max_len?: number
}

export interface DeviceConfig {
  supported: boolean
  schema: Record<string, ConfigItem[]>
  version: number
  applied_version: number
  status: 'synced' | 'pending' | 'rejected'
  error?: string
  config: Record<string, Record<string, unknown>>
  updated_at?: string
}

// ---- 自动接入 ----

export interface Enrollment {
  id: number
  device_uid: string
  name: string
  model: string
  version: string
  ip: string
  code: string
  status: 'pending' | 'approved' | 'completed' | 'rejected' | 'expired'
  created_at: string
}

// ---- 开放 API ----

export type APIScope = 'devices:read' | 'commands:invoke' | 'events:read'

export interface APIKey {
  id: number
  name: string
  key_prefix: string
  scopes: APIScope[]
  device_ids: number[]
  allowed_ips: string[]
  expires_at?: string
  last_used_at?: string
  last_used_ip?: string
  revoked_at?: string
  created_at: string
}

export interface IssuedAPIKey extends APIKey {
  key: string
}

export interface OpenAPIInfo {
  base_url: string
  cert_pin: string
  spki_pin: string
}

export interface APIStats {
  calls: number
  calls_prev: number
  succeeded: number
  failed: number
  lost: number
  running: number
  avg_ms: number
  p95_ms: number
  active_keys: number
  used_keys: number
  hourly: { hour: string; total: number; ok: number; bad: number }[]
  top: { module: string; command: string; count: number }[]
  per_key: Record<string, { calls: number; ok: number; hourly: number[] }>
}

export interface APITryResponse {
  status: number
  duration_ms: number
  body: unknown
}

// 设备命令说明（开放 API：每个命令一个接口）。
export interface CommandParam {
  key: string
  type: string
  label?: string
  desc?: string
  required?: boolean
  default?: unknown
  min?: number
  max?: number
  options?: string[]
}

export interface CommandSpec {
  module: string
  command: string
  label: string
  icon?: string
  desc?: string
  params: CommandParam[]
  result: { key: string; type: string; desc?: string }[]
  declared: boolean
  devices?: number[] // 目录中：支持该命令的设备（近 30 天在线过的）
  online?: boolean // 目录中：当前是否有支持该命令的设备在线
}

// 设备活动概览（设备详情页的图表）。
export interface ActivityBucket {
  at: string
  invocations: number
  failed: number
  events: number
  warnings: number
}
export interface DeviceActivity {
  range: '24h' | '7d' | '30d'
  invocations: number
  failed: number
  events: number
  warnings: number
  buckets: ActivityBucket[]
}

// ---- 软件源 ----

export type RepoLinkStatus = 'active' | 'expired' | 'exhausted' | 'revoked'

export interface RepoLink {
  id: number
  name: string
  token_prefix: string
  viewable: boolean // 可再次查看地址（旧版本生成的链接不行）
  status: RepoLinkStatus
  expires_at?: string
  max_downloads: number // 0 表示不限
  downloads: number
  last_used_at?: string
  revoked_at?: string
  created_at: string
}

export interface IssuedRepoLink extends RepoLink {
  path: string // /repo/<令牌>/
}

export interface RepoOverview {
  enabled: boolean
  label: string
  base_url: string
  latest?: Release
  links: RepoLink[]
}

export interface RepoDownload {
  id: number
  version: string
  ip: string
  machine: string
  firmware: string
  user_agent: string
  created_at: string
}

// ---- 接入码 ----

export type EnrollTicketStatus = 'active' | 'expired' | 'exhausted' | 'revoked'

export interface EnrollTicket {
  id: number
  name: string
  token_prefix: string
  status: EnrollTicketStatus
  expires_at: string
  max_uses: number // 0 表示不限
  uses: number
  last_used_at?: string
  revoked_at?: string
  created_at: string
}

export interface IssuedEnrollTicket extends EnrollTicket {
  token: string
}

export interface EnrollTicketOverview {
  app_scheme: string
  server_url: string // 为空时按控制台当前地址拼
  path: string
  pin: string
  tickets: EnrollTicket[]
}

