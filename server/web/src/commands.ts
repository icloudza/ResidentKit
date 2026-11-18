// 远程命令的显示信息：名称、图标、说明与参数表单。
//
// 全部取自设备上报的命令说明（模块的 +commandSchema），新增模块不用改控制台：
//   - 设备详情与下发面板用这台设备自己的说明（GET /devices/{id}/commands）；
//   - 指令记录等列表用全部设备的命令目录（loadCommandCatalog），只用来显示名称和图标。
// 这里只保留系统内部命令（配置下发、OTA 安装）的名称：它们不进目录，但会出现在指令记录里。

import { reactive } from 'vue'
import { api } from './api/client'
import type { CommandParam, CommandSpec } from './api/types'
import { paths } from './components/Icon.vue'

export type IconName = keyof typeof paths

export interface CommandMeta {
  label: string
  icon: IconName
  desc?: string
  params?: CommandParam[] // [] 表示无参数；undefined 表示设备没有说明，用 JSON 编辑参数
  internal?: boolean // 由系统自动调用（配置下发、OTA），不在设备详情里显示为按钮
  known: boolean // 是否有说明（设备声明或系统内置）
}

const system: Record<string, Omit<CommandMeta, 'known'>> = {
  'system.config/apply': { label: '应用配置', icon: 'toggle', desc: '由「配置」卡片保存时自动下发', internal: true },
  'system.update/install': { label: '安装更新', icon: 'upload', desc: '由「发布」页的灰度计划自动下发', internal: true },
}

// 设备没指定图标（或指定的控制台没有）时按命令名猜一个。
export function commandIcon(command: string, declared?: string): IconName {
  if (declared && declared in paths) return declared as IconName
  if (/^(status|get|info|query|list|show)/.test(command)) return 'info'
  if (/^(set|toggle|enable|disable|switch|on|off)/.test(command)) return 'toggle'
  if (/^(reset|clear|restart|reload|retry)/.test(command)) return 'reset'
  if (/^(install|update|upgrade|upload)/.test(command)) return 'upload'
  if (/token|key|auth|attest|cert/.test(command)) return 'key'
  return 'bolt'
}

// CommandSpec（设备上报的说明）→ 显示信息。
export function metaFromSpec(sp: CommandSpec): CommandMeta {
  return {
    label: sp.label || sp.command,
    icon: commandIcon(sp.command, sp.icon),
    desc: sp.desc,
    params: sp.declared ? sp.params : undefined,
    known: sp.declared,
  }
}

// 全部设备的命令目录，供列表显示名称；登录后加载，设备上线时刷新。
const catalog = reactive<Record<string, CommandMeta>>({})
let loading: Promise<void> | null = null
export function loadCommandCatalog(): Promise<void> {
  loading ??= api.apiCommands().then((list) => {
    for (const k of Object.keys(catalog)) delete catalog[k]
    for (const sp of list) catalog[`${sp.module}/${sp.command}`] = metaFromSpec(sp)
  }).catch(() => {}).finally(() => { loading = null })
  return loading
}

export function commandMeta(module: string, command: string): CommandMeta {
  const key = `${module}/${command}`
  const s = system[key]
  if (s) return { ...s, known: true }
  return catalog[key] ?? { label: command, icon: commandIcon(command), known: false }
}
