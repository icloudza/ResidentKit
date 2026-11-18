// 显示格式工具。

export function fmtTime(s?: string): string {
  if (!s) return '—'
  const d = new Date(s)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

export function fmtAgo(s?: string): string {
  if (!s) return '—'
  const sec = Math.max(0, (Date.now() - new Date(s).getTime()) / 1000)
  if (sec < 60) return '刚刚'
  if (sec < 3600) return `${Math.floor(sec / 60)} 分钟前`
  if (sec < 86400) return `${Math.floor(sec / 3600)} 小时前`
  return `${Math.floor(sec / 86400)} 天前`
}

export function fmtJSON(v: unknown): string {
  if (v === undefined || v === null) return ''
  return JSON.stringify(v, null, 2)
}

export const networkName: Record<string, string> = {
  wifi: 'Wi-Fi', cellular: '蜂窝', wired: '有线', other: '其他', none: '无',
}

// 指令状态：文字与 pill 样式类。
export const statusMeta: Record<string, { text: string; cls: string }> = {
  pending: { text: '排队中', cls: 'p-wait' },
  sent: { text: '已下发', cls: 'p-run' },
  succeeded: { text: '成功', cls: 'p-ok' },
  failed: { text: '失败', cls: 'p-err' },
  expired: { text: '已过期', cls: 'p-warn' },
  lost: { text: '结果未知', cls: 'p-warn' },
}

// 指令状态的补充说明：耗时或剩余排队时间。
export function invocationNote(i: { status: string; sent_at?: string; finished_at?: string; expires_at: string }): string {
  if (i.status === 'succeeded' || i.status === 'failed') {
    if (i.sent_at && i.finished_at) return `${Math.max(1, new Date(i.finished_at).getTime() - new Date(i.sent_at).getTime())} ms`
  }
  if (i.status === 'pending') {
    const left = (new Date(i.expires_at).getTime() - Date.now()) / 60000
    if (left > 0) return left >= 60 ? `剩 ${Math.round(left / 60)} 小时` : `剩 ${Math.ceil(left)} 分`
  }
  return ''
}

export function fmtSpan(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60)
  if (d) return `${d} 天 ${h} 小时`
  if (h) return `${h} 小时 ${m} 分`
  if (m) return `${m} 分`
  return `${s} 秒`
}

export function fmtBytes(b?: number): string {
  if (!b) return '—'
  if (b >= 1 << 30) return `${(b / (1 << 30)).toFixed(1)} GB`
  if (b >= 1 << 20) return `${(b / (1 << 20)).toFixed(1)} MB`
  return `${Math.round(b / 1024)} KB`
}

export function fmtNum(n: number): string { return n.toLocaleString('zh-CN') }

// 事件类型的稳定配色：同一类型在各页面颜色一致。
// 不用红 / 橙：它们在界面里表示失败和警告，普通事件不该看起来像出错。
const palette = ['--svc', '--teal', '--purple', '--green', '--indigo']
export function typeColor(type: string): string {
  let h = 0
  for (const c of type) h = (h * 31 + c.charCodeAt(0)) >>> 0
  return `var(${palette[h % palette.length]})`
}
export function typeBadgeStyle(type: string) {
  const c = typeColor(type)
  return { background: `color-mix(in srgb, ${c} 14%, transparent)`, color: c }
}

export const auditActionName: Record<string, string> = {
  'auth.setup': '首次初始化',
  'auth.login': '登录',
  'auth.logout': '退出',
  'auth.password': '修改密码',
  'auth.totp.enable': '启用两步验证',
  'auth.totp.disable': '关闭两步验证',
  'device.invoke': '下发指令',
  'device.alias': '修改设备别名',
  'token.issue': '签发设备令牌',
  'token.revoke': '吊销设备令牌',
  'token.delete': '删除设备令牌',
  'release.upload': '上传版本',
  'release.withdraw': '撤回版本',
  'rollout.create': '创建灰度计划',
  'rollout.change': '调整灰度计划',
  'update.retry': '重试设备更新',
  'device.config': '修改设备配置',
  'enroll.approve': '同意设备接入',
  'enroll.reject': '拒绝设备接入',
  'apikey.create': '创建 API 密钥',
  'apikey.revoke': '吊销 API 密钥',
  'repo.link.create': '生成安装链接',
  'repo.link.revoke': '吊销安装链接',
  'enroll.ticket.create': '生成接入码',
  'enroll.ticket.revoke': '吊销接入码',
  'release.repo': '同步到软件源',
  'repo.link.update': '修改安装链接',
  'repo.link.delete': '删除安装链接',
  'apikey.delete': '删除 API 密钥',
  'apikey.update': '编辑 API 密钥',
}

// 模块的中文名（配置卡片分组标题）；未登记的显示模块 ID。
export const moduleName: Record<string, string> = {
  'net.link': '服务器连接',
  'system.monitor': '资源监控',
  'system.update': '在线更新',
  'system.config': '远程配置',
  'example.heartbeat': '示例：心跳计数',
}

// OTA：设备更新状态与灰度计划状态。
export const updateStatusMeta: Record<string, { text: string; cls: string }> = {
  queued: { text: '已下发', cls: 'p-wait' },
  downloading: { text: '更新中', cls: 'p-run' },
  installed: { text: '已更新', cls: 'p-ok' },
  failed: { text: '失败', cls: 'p-err' },
  rolled_back: { text: '已回滚', cls: 'p-warn' },
  unsupported: { text: '需手动安装', cls: 'p-wait' },
}

export const rolloutStatusMeta: Record<string, { text: string; cls: string }> = {
  active: { text: '进行中', cls: 'p-run' },
  paused: { text: '已暂停', cls: 'p-warn' },
  cancelled: { text: '已取消', cls: 'p-wait' },
  completed: { text: '已完成', cls: 'p-ok' },
}

// 比较 x.y.z：a<b 为负，相等为 0。
export function compareVersions(a: string, b: string): number {
  const pa = a.split('.').map(Number)
  const pb = b.split('.').map(Number)
  for (let i = 0; i < 3; i++) {
    const d = (pa[i] || 0) - (pb[i] || 0)
    if (d) return d
  }
  return 0
}

export function deviceTitle(d: { alias?: string; name?: string; device_uid: string }): string {
  return d.alias || d.name || d.device_uid.slice(0, 8)
}

// 机型标识 → 名称（hw.machine，如 iPhone12,1）。未收录的原样显示标识。
const models: Record<string, string> = {
  'iPhone10,1': 'iPhone 8', 'iPhone10,4': 'iPhone 8', 'iPhone10,2': 'iPhone 8 Plus', 'iPhone10,5': 'iPhone 8 Plus',
  'iPhone10,3': 'iPhone X', 'iPhone10,6': 'iPhone X', 'iPhone11,2': 'iPhone XS', 'iPhone11,4': 'iPhone XS Max',
  'iPhone11,6': 'iPhone XS Max', 'iPhone11,8': 'iPhone XR', 'iPhone12,1': 'iPhone 11', 'iPhone12,3': 'iPhone 11 Pro',
  'iPhone12,5': 'iPhone 11 Pro Max', 'iPhone12,8': 'iPhone SE（第 2 代）', 'iPhone13,1': 'iPhone 12 mini',
  'iPhone13,2': 'iPhone 12', 'iPhone13,3': 'iPhone 12 Pro', 'iPhone13,4': 'iPhone 12 Pro Max',
  'iPhone14,4': 'iPhone 13 mini', 'iPhone14,5': 'iPhone 13', 'iPhone14,2': 'iPhone 13 Pro', 'iPhone14,3': 'iPhone 13 Pro Max',
  'iPhone14,6': 'iPhone SE（第 3 代）', 'iPhone14,7': 'iPhone 14', 'iPhone14,8': 'iPhone 14 Plus',
  'iPhone15,2': 'iPhone 14 Pro', 'iPhone15,3': 'iPhone 14 Pro Max', 'iPhone15,4': 'iPhone 15', 'iPhone15,5': 'iPhone 15 Plus',
  'iPhone16,1': 'iPhone 15 Pro', 'iPhone16,2': 'iPhone 15 Pro Max', 'iPhone17,3': 'iPhone 16', 'iPhone17,4': 'iPhone 16 Plus',
  'iPhone17,1': 'iPhone 16 Pro', 'iPhone17,2': 'iPhone 16 Pro Max', 'iPhone17,5': 'iPhone 16e',
}
export const modelName = (id?: string) => (id ? models[id] ?? id : '')
