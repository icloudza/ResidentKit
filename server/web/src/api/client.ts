// API 客户端：访问令牌只存内存，过期时用 HttpOnly Cookie 中的刷新令牌自动续期。

import type {
  AuditLog, Device, DeviceToken, Event, Invocation, Page, Summary, TokenResponse, Admin, DeviceActivity,
  APIKey, APIStats, APITryResponse, CommandSpec, DeviceConfig, DeviceUpdateInfo, Enrollment, IssuedAPIKey, OpenAPIInfo, OTAConfig, Release, ReleaseDetail, Rollout,
  IssuedRepoLink, RepoDownload, RepoLink, RepoOverview, EnrollTicketOverview, IssuedEnrollTicket,
} from './types'

export class ApiError extends Error {
  constructor(public code: number, message: string, public status: number) {
    super(message)
  }
}

let accessToken = ''
let refreshing: Promise<boolean> | null = null
let onUnauthorized: () => void = () => {}

export function setAccessToken(t: string) { accessToken = t }
export function getAccessToken() { return accessToken }
export function setUnauthorizedHandler(f: () => void) { onUnauthorized = f }

async function raw<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (accessToken) headers['Authorization'] = `Bearer ${accessToken}`
  const res = await fetch(`/api/v1${path}`, {
    method, headers, credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  let json: { code: number; msg: string; data: T }
  try {
    json = await res.json()
  } catch {
    throw new ApiError(-1, `服务器返回异常（${res.status}）`, res.status)
  }
  if (json.code !== 0) throw new ApiError(json.code, json.msg, res.status)
  return json.data
}

// refresh 并发请求共用同一次刷新。
export function refresh(): Promise<boolean> {
  refreshing ??= raw<TokenResponse>('POST', '/auth/refresh')
    .then((r) => { accessToken = r.access_token; return true })
    .catch(() => { accessToken = ''; return false })
    .finally(() => { refreshing = null })
  return refreshing
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  try {
    return await raw<T>(method, path, body)
  } catch (e) {
    if (e instanceof ApiError && e.status === 401 && !path.startsWith('/auth/')) {
      if (await refresh()) return raw<T>(method, path, body)
      onUnauthorized()
    }
    throw e
  }
}

const q = (p: Record<string, string | number | undefined>) => {
  const s = new URLSearchParams()
  for (const [k, v] of Object.entries(p)) if (v !== undefined && v !== '' && v !== 0) s.set(k, String(v))
  const str = s.toString()
  return str ? `?${str}` : ''
}

export const api = {
  setupStatus: () => raw<{ needs_setup: boolean }>('GET', '/setup/status'),
  setup: (code: string, username: string, password: string) =>
    raw<TokenResponse>('POST', '/setup', { code, username, password }),

  login: (username: string, password: string, totp_code?: string) =>
    raw<TokenResponse>('POST', '/auth/login', { username, password, totp_code: totp_code || undefined }),
  logout: () => request<void>('POST', '/auth/logout'),
  me: () => request<Admin>('GET', '/auth/me'),
  changeUsername: (username: string, password: string, totp_code?: string) =>
    request<Admin>('PUT', '/auth/username', { username, password, totp_code: totp_code || undefined }),
  changePassword: (old_password: string, new_password: string) =>
    request<void>('POST', '/auth/password', { old_password, new_password }),
  totpSetup: () => request<{ secret: string; url: string }>('POST', '/auth/totp/setup'),
  totpEnable: (code: string) => request<void>('POST', '/auth/totp/enable', { code }),
  totpDisable: (code: string) => request<void>('POST', '/auth/totp/disable', { code }),

  summary: () => request<Summary>('GET', '/dashboard/summary'),
  devices: () => request<Device[]>('GET', '/devices'),
  device: (id: number) => request<Device>('GET', `/devices/${id}`),
  deviceActivity: (id: number, range: string) => request<DeviceActivity>('GET', `/devices/${id}/activity?range=${range}&tz=${-new Date().getTimezoneOffset()}`),
  setAlias: (id: number, alias: string) => request<void>('PATCH', `/devices/${id}`, { alias }),
  deleteDevice: (id: number) => request<void>('DELETE', `/devices/${id}`),
  deviceCommands: (id: number) => request<CommandSpec[]>('GET', `/devices/${id}/commands`),
  invoke: (id: number, body: { module: string; command: string; arguments?: unknown; ttl_seconds?: number; wait_seconds?: number }) =>
    request<Invocation>('POST', `/devices/${id}/invocations`, body),

  invocations: (p: { device_id?: number; status?: string; since?: string; page?: number; page_size?: number } = {}) =>
    request<Page<Invocation>>('GET', `/invocations${q(p)}`),
  events: (p: { device_id?: number; type?: string; level?: string; since?: string; page?: number; page_size?: number } = {}) =>
    request<Page<Event>>('GET', `/events${q(p)}`),
  eventTypes: (since = '24h') => request<{ type: string; count: number }[]>('GET', `/events/types?since=${since}`),
  audit: (p: { page?: number; page_size?: number } = {}) => request<Page<AuditLog>>('GET', `/audit-logs${q(p)}`),

  tokens: () => request<DeviceToken[]>('GET', '/tokens'),
  revokeToken: (id: number) => request<void>('POST', `/tokens/${id}/revoke`),
  deleteToken: (id: number) => request<void>('DELETE', `/tokens/${id}`),

  apiKeys: () => request<APIKey[]>('GET', '/api-keys'),
  apiKeyInfo: () => request<OpenAPIInfo>('GET', '/api-keys/info'),
  createAPIKey: (body: { name: string; scopes: string[]; device_ids: number[]; allowed_ips: string[]; expires_days: number; password: string; totp_code?: string }) =>
    request<IssuedAPIKey>('POST', '/api-keys', body),
  updateAPIKey: (id: number, body: { name: string; scopes: string[]; device_ids: number[]; allowed_ips: string[]; expires_days?: number; password: string; totp_code?: string }) =>
    request<APIKey>('PUT', `/api-keys/${id}`, body),
  apiCommands: () => request<CommandSpec[]>('GET', '/api-keys/commands'),
  apiStats: () => request<APIStats>('GET', '/api-keys/stats'),
  apiCalls: (p: { key_id?: number; device_id?: number; status?: string; since?: string; page?: number; page_size?: number } = {}) =>
    request<Page<Invocation>>('GET', `/api-calls${q(p)}`),
  tryAPI: (body: { key?: string; key_id?: number; method: 'GET' | 'POST'; path: string; body?: unknown; idempotency_key?: string }) =>
    request<APITryResponse>('POST', '/api-keys/try', body),
  revokeAPIKey: (id: number) => request<void>('POST', `/api-keys/${id}/revoke`),
  deleteAPIKey: (id: number) => request<void>('DELETE', `/api-keys/${id}`),

  enrollments: () => request<Enrollment[]>('GET', '/enrollments'),
  decideEnrollment: (id: number, action: 'approve' | 'reject') => request<void>('POST', `/enrollments/${id}/${action}`),
  enrollTickets: () => request<EnrollTicketOverview>('GET', '/enroll-tickets'),
  createEnrollTicket: (body: { name: string; expires_minutes: number; max_uses: number }) =>
    request<IssuedEnrollTicket>('POST', '/enroll-tickets', body),
  revokeEnrollTicket: (id: number) => request<void>('POST', `/enroll-tickets/${id}/revoke`),

  repoOverview: () => request<RepoOverview>('GET', '/repo-links'),
  createRepoLink: (body: { name: string; expires_hours: number; max_downloads: number; password: string; totp_code?: string }) =>
    request<IssuedRepoLink>('POST', '/repo-links', body),
  revokeRepoLink: (id: number) => request<void>('POST', `/repo-links/${id}/revoke`),
  repoDownloads: (id: number) => request<RepoDownload[]>('GET', `/repo-links/${id}/downloads`),
  revealRepoLink: (id: number) => request<{ path: string }>('GET', `/repo-links/${id}/reveal`),
  updateRepoLink: (id: number, body: { name: string; max_downloads: number; expires_hours?: number }) =>
    request<RepoLink>('PUT', `/repo-links/${id}`, body),
  deleteRepoLink: (id: number) => request<void>('DELETE', `/repo-links/${id}`),

  otaConfig: () => request<OTAConfig>('GET', '/releases/config'),
  releases: () => request<Release[]>('GET', '/releases'),
  release: (id: number) => request<ReleaseDetail>('GET', `/releases/${id}`),
  withdrawRelease: (id: number) => request<void>('POST', `/releases/${id}/withdraw`),
  setReleaseRepo: (id: number, inRepo: boolean) => request<void>('POST', `/releases/${id}/repo`, { in_repo: inRepo }),
  createRollout: (id: number, target: 'all' | 'devices', device_ids: number[] = []) =>
    request<Rollout>('POST', `/releases/${id}/rollouts`, { target, device_ids }),
  changeRollout: (id: number, action: 'pause' | 'resume' | 'cancel') => request<void>('POST', `/rollouts/${id}/${action}`),
  retryUpdate: (id: number) => request<void>('POST', `/device-updates/${id}/retry`),
  deviceConfig: (id: number) => request<DeviceConfig>('GET', `/devices/${id}/config`),
  saveDeviceConfig: (id: number, config: Record<string, Record<string, unknown>>) =>
    request<DeviceConfig>('PUT', `/devices/${id}/config`, { config }),
  deviceUpdate: (id: number) => request<DeviceUpdateInfo>('GET', `/devices/${id}/update`),
  uploadRelease,
}

// 上传安装包：用 XHR 以便显示进度；访问令牌过期时刷新后重试一次。
// 表单字段顺序有要求（文件放最后），由调用方按顺序 append。
function uploadRelease(form: FormData, onProgress: (ratio: number) => void): Promise<Release> {
  const send = () => new Promise<Release>((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', '/api/v1/releases')
    if (accessToken) xhr.setRequestHeader('Authorization', `Bearer ${accessToken}`)
    xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(e.loaded / e.total) }
    xhr.onerror = () => reject(new ApiError(-1, '网络错误，上传中断', 0))
    xhr.onload = () => {
      try {
        const json = JSON.parse(xhr.responseText) as { code: number; msg: string; data: Release }
        if (json.code !== 0) reject(new ApiError(json.code, json.msg, xhr.status))
        else resolve(json.data)
      } catch {
        reject(new ApiError(-1, `服务器返回异常（${xhr.status}）`, xhr.status))
      }
    }
    xhr.send(form)
  })
  return send().catch(async (e) => {
    if (e instanceof ApiError && e.status === 401 && e.code === 20001 && (await refresh())) return send()
    throw e
  })
}
