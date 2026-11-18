// 事件目录：中文名称与一句话说明（规范见 docs/events.md）。
// 新增事件类型时在这里登记；未登记的类型显示类型名与原始 payload。

import { fmtBytes, fmtSpan, networkName } from './format'

type P = Record<string, any>

export interface EventMeta {
  label: string
  describe?: (p: P) => string
}

const exitName: Record<string, string> = { clean: '正常退出', watchdog: '看门狗重启', unknown: '异常退出' }
const stageName: Record<string, string> = { download: '下载', verify: '校验', install: '安装' }
const pressureName: Record<string, string> = { normal: '正常', warning: '警告', critical: '严重' }

const catalog: Record<string, EventMeta> = {
  'system.daemon.started': {
    label: '守护进程启动',
    describe: (p) => {
      const why = p.reason === 'boot' ? '越狱后首次启动'
        : p.reason === 'upgrade' ? `升级到 ${p.version}${p.prev_version ? `（原 ${p.prev_version}）` : ''}`
          : p.reason === 'downgrade' ? `回退到 ${p.version}${p.prev_version ? `（原 ${p.prev_version}）` : ''}`
            : p.reason === 'restart' ? '正常重启' : '被杀后重新拉起'
      return p.reason === 'boot' || p.reason === 'restart' || p.reason === 'downgrade' ? why : `${why}，上次${exitName[p.last_exit] ?? '退出原因未知'}`
    },
  },
  'system.service.toggled': {
    label: '常驻服务开关',
    describe: (p) => `${p.enabled ? '已开启' : '已关闭'}（${p.by === 'remote' ? '远程' : '手机 App'}）`,
  },
  'net.network.changed': {
    label: '网络切换',
    describe: (p) => `${networkName[p.from] ?? p.from} → ${networkName[p.to] ?? p.to}`,
  },
  'net.link.recovered': {
    label: '服务器连接恢复',
    describe: (p) => `断开 ${fmtSpan(p.down_s ?? 0)}，重连 ${p.reconnects ?? 0} 次${p.reason ? `（${p.reason}）` : ''}`,
  },
  'resource.memory.raised': {
    label: '内存压力升高',
    describe: (p) => `${pressureName[p.prev] ?? p.prev} → ${pressureName[p.level] ?? p.level}`
      + (p.dev_mem_pct !== undefined ? `，整机内存 ${p.dev_mem_pct}%` : ''),
  },
  'resource.memory.cleared': {
    label: '内存压力恢复',
    describe: (p) => `${pressureName[p.prev] ?? p.prev} → 正常${p.duration_s !== undefined ? `，持续 ${fmtSpan(p.duration_s)}` : ''}`,
  },
  'resource.footprint.high': {
    label: '服务内存偏高',
    describe: (p) => `${fmtBytes(p.footprint_bytes)} / 上限 ${fmtBytes(p.limit_bytes)}（${p.pct}%）`,
  },
  'system.update.started': {
    label: '开始更新',
    describe: (p) => `${p.from} → ${p.version}，下载中`,
  },
  'system.update.installed': {
    label: '更新完成',
    describe: (p) => `${p.from} → ${p.version}`,
  },
  'system.update.rolled_back': {
    label: '更新已回滚',
    describe: (p) => `${p.version} 未通过健康检查，已恢复 ${p.from}${p.error ? `（${p.error}）` : ''}`,
  },
  'system.update.failed': {
    label: '更新失败',
    describe: (p) => `${p.version}${p.stage ? ` · ${stageName[p.stage] ?? p.stage}` : ''}${p.error ? `：${p.error}` : ''}`,
  },
  'system.config.applied': {
    label: '配置已生效',
    describe: (p) => `版本 ${p.version}${p.changed?.length ? `，改动 ${p.changed.join('、')}` : '，无改动'}`,
  },
  'system.config.rejected': {
    label: '配置被拒绝',
    describe: (p) => `版本 ${p.version}：${p.error}`,
  },
}

export function eventMeta(type: string): EventMeta | undefined {
  return catalog[type]
}

// 一行可读描述：已登记的用说明，未登记的用截断后的 JSON。
export function eventSummary(type: string, payload: unknown): string {
  const m = catalog[type]
  if (m?.describe && payload && typeof payload === 'object') {
    try { return m.describe(payload as P) } catch { /* 字段不全时退回 JSON */ }
  }
  if (payload === undefined || payload === null) return ''
  const s = JSON.stringify(payload)
  return s.length > 90 ? `${s.slice(0, 90)}…` : s
}

export const levelMeta: Record<string, { text: string; cls: string }> = {
  info: { text: '信息', cls: 'lv-info' },
  notice: { text: '提示', cls: 'p-run' },
  warning: { text: '警告', cls: 'p-warn' },
  error: { text: '错误', cls: 'p-err' },
}
