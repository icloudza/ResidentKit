// 开放 API 文档内容（与 server/internal/handler/open/openapi.json、docs/open-api.md 保持一致）。
// 固定接口写在 baseEndpoints；设备命令（每个命令一个接口）由 commandEndpoint 按设备上报的说明生成。

import type { CommandSpec } from '../api/types'

export type Lang = 'curl' | 'python' | 'go' | 'node'
export const langs: { key: Lang; label: string }[] = [
  { key: 'curl', label: 'curl' }, { key: 'python', label: 'Python' }, { key: 'go', label: 'Go' }, { key: 'node', label: 'Node.js' },
]

export interface Field { name: string; type: string; required?: boolean; desc: string }

export interface Endpoint {
  id: string
  group: string
  title: string
  method: 'GET' | 'POST'
  path: string // 含 {id} 占位
  scope: string
  desc: string
  pathParams?: Field[]
  query?: Field[]
  headers?: Field[]
  body?: Field[]
  bodyExample?: Record<string, unknown>
  resultFields?: Field[]
  response: string
  errors: [string, string][]
  command?: CommandSpec
}

const authHeader: Field = { name: 'Authorization', type: 'string', required: true, desc: 'Bearer rkk_…' }
const idParam = (what: string): Field => ({ name: 'id', type: 'int', required: true, desc: `${what} ID` })

export const baseEndpoints: Endpoint[] = [
  {
    id: 'list-devices', group: '设备', title: '设备列表', method: 'GET', path: '/devices', scope: 'devices:read',
    desc: '密钥范围内的全部设备，在线的在前。',
    headers: [authHeader],
    response: `{
  "code": 0, "msg": "ok",
  "data": [{
    "id": 1, "device_uid": "F10A3F6A-…", "name": "iPhone", "alias": "iPhone .31",
    "version": "1.6.8", "online": true, "network": "wifi", "rtt_ms": 42,
    "remote_commands": { "example.heartbeat": ["status", "set", "reset"], "net.link": ["status"] },
    "last_seen_at": "2026-11-17T08:12:00Z"
  }]
}`,
    errors: [['401 · 20001', '缺少密钥，或密钥无效、已吊销、已过期'], ['403 · 20006', '密钥没有 devices:read 权限，或来源 IP 不在白名单']],
  },
  {
    id: 'get-device', group: '设备', title: '设备详情', method: 'GET', path: '/devices/{id}', scope: 'devices:read',
    desc: '单台设备的状态、版本与资源指标。',
    pathParams: [idParam('设备')], headers: [authHeader],
    response: `{
  "code": 0, "msg": "ok",
  "data": { "id": 1, "name": "iPhone", "version": "1.6.8", "online": true,
            "metrics": { "svc_footprint": 2097152, "dev_cpu": 12.5 } }
}`,
    errors: [['404 · 10002', '设备不存在或不在密钥范围内']],
  },
  {
    id: 'device-commands', group: '设备', title: '设备可用的命令', method: 'GET', path: '/devices/{id}/commands', scope: 'devices:read',
    desc: '这台设备开放的命令及其参数、返回字段。不同设备版本可能不同，以这里为准。',
    pathParams: [idParam('设备')], headers: [authHeader],
    response: `{
  "code": 0, "msg": "ok",
  "data": [{
    "module": "example.heartbeat", "command": "set", "label": "开关计数",
    "params": [{ "key": "on", "type": "bool", "required": true }],
    "result": [{ "key": "beats", "type": "int", "desc": "已计次数" }],
    "declared": true
  }]
}`,
    errors: [['404 · 10002', '设备不存在或不在密钥范围内']],
  },
  {
    id: 'get-invocation', group: '指令结果', title: '查询指令结果', method: 'GET', path: '/invocations/{id}', scope: 'commands:invoke',
    desc: '异步调用（async=true）或等待超时（504）后，用返回的 invocation_id 查询结果。只能查本密钥发起的；wait 为长轮询秒数，有结果立即返回。',
    pathParams: [idParam('指令')],
    query: [{ name: 'wait', type: 'int 0–30', desc: '长轮询秒数' }],
    headers: [authHeader],
    response: `{
  "code": 0, "msg": "ok",
  "data": { "id": 1024, "status": "succeeded", "result": { "beats": 407 },
            "created_at": "2026-11-17T08:12:03.120Z", "finished_at": "2026-11-17T08:12:03.262Z" }
}`,
    errors: [['404 · 10002', '指令不存在或不是本密钥发起的']],
  },
  {
    id: 'list-events', group: '事件', title: '设备事件', method: 'GET', path: '/devices/{id}/events', scope: 'events:read',
    desc: '设备上报的事件，新到旧分页。事件类型规范见 docs/events.md。',
    pathParams: [idParam('设备')],
    query: [
      { name: 'page', type: 'int', desc: '页码，从 1 开始' }, { name: 'page_size', type: 'int 1–200', desc: '每页条数，默认 20' },
      { name: 'type', type: 'string', desc: '事件类型，如 system.update.installed' },
      { name: 'level', type: 'string', desc: '逗号分隔：info,notice,warning,error' },
      { name: 'since', type: 'RFC 3339', desc: '只返回此后的事件' },
    ],
    headers: [authHeader],
    response: `{
  "code": 0, "msg": "ok",
  "data": {
    "items": [{ "id": 74, "type": "system.update.installed", "level": "notice",
                "payload": { "from": "1.6.7", "version": "1.6.8" }, "created_at": "2026-11-17T07:30:12Z" }],
    "total": 128, "page": 1, "page_size": 20
  }
}`,
    errors: [['400 · 10001', 'since 格式错误'], ['404 · 10002', '设备不存在或不在密钥范围内']],
  },
]

export const callQuery: Field[] = [
  { name: 'timeout', type: 'int 1–30', desc: '同步等待结果的秒数，默认 30' },
  { name: 'async', type: 'bool', desc: 'true 时立即返回 202，之后用 GET /invocations/{id} 查询' },
  { name: 'ttl', type: 'int 秒', desc: '设备离线时排队的秒数。不填时同步调用离线直接失败（409），异步调用排队 10 分钟；同步调用排队中返回 202' },
]

const callErrors: [string, string][] = [
  ['400 · 10001', '缺少必填参数、类型不符或有未知参数'],
  ['403 · 20006', '密钥没有 commands:invoke 权限，或来源 IP 不在白名单'],
  ['404 · 10002', '设备不存在、不在密钥范围内，或设备没有开放该命令'],
  ['409 · 30001', '设备离线（未指定 ttl 时）'],
  ['409 · 10004', '幂等键已用于另一条指令（设备、命令或参数不同）'],
  ['422 · 30005', '设备执行失败：msg 为设备返回的原因，data.device_error_code 为设备错误码'],
  ['502 · 30006', '结果未知：下发后连接断开或设备超时未返回'],
  ['504 · 30003', '设备未在 timeout 秒内返回：data.invocation_id 可用于之后查询'],
  ['202', '指定了 ttl 且设备离线：已排队，设备上线后执行，用 invocation_id 查询结果'],
]

function typeLabel(p: { type: string; min?: number; max?: number; options?: string[] }): string {
  if (p.type === 'enum' && p.options?.length) return p.options.join(' | ')
  if ((p.type === 'int' || p.type === 'number') && (p.min !== undefined || p.max !== undefined)) return `${p.type} ${p.min ?? ''}–${p.max ?? ''}`
  return p.type
}

function exampleValue(type: string, p?: { default?: unknown; min?: number; options?: string[] }): unknown {
  if (p?.default !== undefined) return p.default
  switch (type) {
    case 'bool': return true
    case 'int': return p?.min ?? 10
    case 'number': return p?.min ?? 1.5
    case 'enum': return p?.options?.[0] ?? ''
    case 'object': return {}
    case 'array': return []
    default: return 'string'
  }
}

// 命令接口：POST /devices/{id}/commands/{模块}/{命令}，请求体即参数，同步返回结果。
export function commandEndpoint(c: CommandSpec): Endpoint {
  const body: Record<string, unknown> = {}
  for (const p of c.params) body[p.key] = exampleValue(p.type, p)
  const result: Record<string, unknown> = {}
  for (const f of c.result) result[f.key] = exampleValue(f.type)
  const resp = JSON.stringify({ code: 0, msg: 'ok', data: { invocation_id: 1024, duration_ms: 142, result } }, null, 2)
  return {
    id: `cmd:${c.module}/${c.command}`, group: '命令', title: c.label,
    method: 'POST', path: `/devices/{id}/commands/${c.module}/${c.command}`, scope: 'commands:invoke',
    desc: (c.declared ? c.desc || '' : '设备未提供这个命令的说明，参数原样下发。') +
      (c.declared && c.desc ? '。' : '') + '默认同步等待设备返回，结果在 data.result。',
    pathParams: [idParam('设备')],
    query: callQuery,
    headers: [authHeader, { name: 'Idempotency-Key', type: 'string', desc: '幂等键（字母、数字、_.:-，≤ 64）：同一密钥下相同的键只执行一次，重试返回同一结果' }],
    body: c.params.map((p) => ({ name: p.key, type: typeLabel(p), required: p.required, desc: [p.label, p.desc].filter(Boolean).join('：') })),
    bodyExample: c.params.length || !c.declared ? body : undefined,
    resultFields: c.result.map((f) => ({ name: f.key, type: f.type, desc: f.desc ?? '' })),
    response: resp,
    errors: callErrors,
    command: c,
  }
}

export const errorCodes: [string, string, string][] = [
  ['400', '10001', '参数错误'],
  ['401', '20001', '缺少密钥，或密钥无效、已吊销、已过期'],
  ['403', '20006', '密钥没有该权限、来源 IP 不在白名单、或命令由系统内部使用'],
  ['404', '10002', '设备、命令或指令不存在（或不在密钥范围内）'],
  ['409', '30001', '设备离线'],
  ['409', '10004', 'Idempotency-Key 已用于另一条指令（设备、命令或参数不同）'],
  ['422', '30005', '设备执行失败（msg 为设备返回的原因）'],
  ['502', '30006', '结果未知：下发后连接断开或设备超时未返回'],
  ['504', '30003', '设备未在 timeout 秒内返回'],
]

// 示例代码：把 {id} 换成示例 ID。
export function sample(ep: Endpoint, lang: Lang, base: string): string {
  const path = ep.path.replace('{id}', ep.id === 'get-invocation' ? '1024' : '1')
  const query = ep.id === 'get-invocation' ? '?wait=30' : ep.id === 'list-events' ? '?page_size=20' : ''
  const url = `${base}${path}${query}`
  const body = ep.bodyExample && Object.keys(ep.bodyExample).length ? JSON.stringify(ep.bodyExample) : ''
  const post = ep.method === 'POST'
  const idem = !!ep.command
  switch (lang) {
    case 'curl':
      return [`curl${post ? ' -X POST' : ''} '${url}' \\`, `  -H 'Authorization: Bearer rkk_…'${post ? ' \\' : ''}`,
        ...(post ? [...(idem ? [`  -H 'Idempotency-Key: job-20261117-001'${body ? ' \\' : ''}`] : []),
          ...(body ? [`  -H 'Content-Type: application/json' \\`, `  -d '${body}'`] : [])] : [])].join('\n')
    case 'python':
      return `import requests

s = requests.Session()              # 复用连接，可放心并发
s.headers["Authorization"] = "Bearer rkk_…"

r = s.${ep.method.toLowerCase()}("${url}"${post ? `,\n           ${idem ? 'headers={"Idempotency-Key": "job-20261117-001"},\n           ' : ''}json=${body ? pyDict(ep.bodyExample!) : '{}'}` : ''})
res = r.json()
if res["code"] != 0:
    raise RuntimeError(res["msg"])
print(res["data"]${ep.command ? '["result"]' : ''})`
    case 'go':
      return `${post ? `body := strings.NewReader(\`${body || '{}'}\`)\n` : ''}req, _ := http.NewRequest("${ep.method}", "${url}", ${post ? 'body' : 'nil'})
req.Header.Set("Authorization", "Bearer "+apiKey)${post ? `\nreq.Header.Set("Content-Type", "application/json")` : ''}${idem ? `\nreq.Header.Set("Idempotency-Key", "job-20261117-001")` : ''}

resp, err := http.DefaultClient.Do(req) // DefaultClient 自带连接池，可并发
if err != nil {
	return err
}
defer resp.Body.Close()
var res struct {
	Code int             \`json:"code"\`
	Msg  string          \`json:"msg"\`
	Data json.RawMessage \`json:"data"\`
}
err = json.NewDecoder(resp.Body).Decode(&res)`
    case 'node':
      return `const res = await fetch('${url}', {
  method: '${ep.method}',
  headers: {
    Authorization: 'Bearer rkk_…',${post ? `\n    'Content-Type': 'application/json',` : ''}${idem ? `\n    'Idempotency-Key': 'job-20261117-001',` : ''}
  },${post ? `\n  body: JSON.stringify(${body ? jsObj(ep.bodyExample!) : '{}'}),` : ''}
})
const { code, msg, data } = await res.json()
if (code !== 0) throw new Error(msg)${ep.command ? '\nconsole.log(data.result)' : ''}`
  }
}

function lit(v: unknown, py: boolean): string {
  if (typeof v === 'string') return py ? `"${v}"` : `'${v}'`
  if (typeof v === 'boolean') return py ? (v ? 'True' : 'False') : String(v)
  return JSON.stringify(v)
}
function pyDict(o: Record<string, unknown>): string {
  return `{${Object.entries(o).map(([k, v]) => `"${k}": ${lit(v, true)}`).join(', ')}}`
}
function jsObj(o: Record<string, unknown>): string {
  return `{ ${Object.entries(o).map(([k, v]) => `${k}: ${lit(v, false)}`).join(', ')} }`
}
