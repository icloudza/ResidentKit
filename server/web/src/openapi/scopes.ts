// API 密钥的权限说明（与服务端 model.Scope* 对应）。
import type { APIScope } from '../api/types'

export const scopeMeta: Record<APIScope, { label: string; desc: string }> = {
  'devices:read': { label: '查看设备', desc: '设备列表、在线状态、可用命令' },
  'commands:invoke': { label: '下发指令', desc: '调用设备开放的远程命令并查询结果' },
  'events:read': { label: '读取事件', desc: '设备上报的事件' },
}

export const allScopes = Object.keys(scopeMeta) as APIScope[]
