<script setup lang="ts">
// 开放 API · 接口文档：左侧目录（每个设备命令一个接口），右侧接口说明、示例代码与在线调试。
import { computed, onMounted, ref, watch } from 'vue'
import { NButton, NInput, NSelect, useMessage } from 'naive-ui'
import { api, ApiError } from '../../api/client'
import type { APIKey, APITryResponse, CommandSpec, Device, OpenAPIInfo } from '../../api/types'
import JsonView from '../JsonView.vue'
import CodeBlock, { type CodeLang } from '../CodeBlock.vue'
import { deviceTitle } from '../../format'
import { baseEndpoints, commandEndpoint, errorCodes, langs, sample, type Endpoint, type Lang } from '../../openapi/docs'
import { scopeMeta } from '../../openapi/scopes'

const props = defineProps<{ info: OpenAPIInfo | null; devices: Device[]; keys: APIKey[] }>()
const emit = defineEmits<{ create: [] }>()
const message = useMessage()

const commands = ref<CommandSpec[]>([])
const cmdEndpoints = computed(() => commands.value.map(commandEndpoint))
const all = computed(() => [...baseEndpoints, ...cmdEndpoints.value])
const toc = computed(() => [
  { group: '设备', items: baseEndpoints.filter((e) => e.group === '设备') },
  { group: '命令', items: cmdEndpoints.value },
  { group: '指令结果', items: baseEndpoints.filter((e) => e.group === '指令结果') },
  { group: '事件', items: baseEndpoints.filter((e) => e.group === '事件') },
])
onMounted(async () => { commands.value = await api.apiCommands() })

type Section = 'intro' | 'errors' | 'concurrency'
const sections: { id: Section; label: string }[] = [
  { id: 'intro', label: '介绍与鉴权' }, { id: 'errors', label: '错误码' }, { id: 'concurrency', label: '并发与幂等' },
]
const current = ref<string>('intro')
const ep = computed<Endpoint | undefined>(() => all.value.find((e) => e.id === current.value))
const base = computed(() => props.info?.base_url || 'https://<你的域名>/open/v1')
const lang = ref<Lang>('curl')
const code = computed(() => (ep.value ? sample(ep.value, lang.value, base.value) : ''))
const codeLang = computed<CodeLang>(() => ({ curl: 'bash', python: 'python', go: 'go', node: 'js' } as const)[lang.value])

const introCall = `POST /open/v1/devices/{id}/commands/{模块}/{命令}
{ 命令参数 }

→ 200 { "code": 0, "msg": "ok", "data": { "invocation_id": 1024, "duration_ms": 142, "result": { … } } }`
const introResp = '{ "code": 0, "msg": "ok", "data": … }'

async function copy(v: string) {
  await navigator.clipboard.writeText(v)
  message.success('已复制')
}

// ---- 在线调试 ----
// 选择已创建的密钥：服务端按密钥 ID 以它的身份在进程内调用（明文只在创建时显示过一次，不需要再填）。
// 也可选「手动填写」粘贴明文，只保存在本页内存。
const MANUAL = -1
const usable = computed(() => props.keys.filter((k) => !k.revoked_at && (!k.expires_at || new Date(k.expires_at).getTime() > Date.now())))
const keyId = ref<number | null>(null)
let manualPicked = false // 用户主动选了「手动填写」才保留，否则密钥列表加载后自动选中
function pickKey(v: number) {
  keyId.value = v
  manualPicked = v === MANUAL
}
const keyOptions = computed(() => [
  ...usable.value.map((k) => ({ label: `${k.name}（${k.key_prefix}…）${ep.value?.scope && !k.scopes.includes(ep.value.scope as APIKey['scopes'][number]) ? ' · 无此权限' : ''}`, value: k.id })),
  { label: '手动填写密钥…', value: MANUAL },
])
const key = ref('') // 手动填写时使用，只在本页内存中
const deviceId = ref<number | null>(null)
const invId = ref('')
const query = ref('')
const body = ref('')
const idem = ref('')
const sending = ref(false)
const resp = ref<APITryResponse | null>(null)
// 命令接口只列出支持该命令的设备。
const tryDevices = computed(() => {
  const ids = ep.value?.command?.devices
  return ids ? props.devices.filter((d) => ids.includes(d.id)) : props.devices
})
const deviceOptions = computed(() => tryDevices.value.map((d) => ({ label: `${deviceTitle(d)}（#${d.id}${d.online ? '' : ' · 离线'}）`, value: d.id })))
let lastEp: Endpoint | undefined // 须在下面的 immediate watch 之前声明
watch([ep, usable], ([e]) => {
  // 默认选中有该接口权限的第一个密钥；已选的仍有效则保持。
  if ((keyId.value === MANUAL && manualPicked) || usable.value.some((k) => k.id === keyId.value)) return
  const fit = usable.value.find((k) => !e?.scope || k.scopes.includes(e.scope as APIKey['scopes'][number])) ?? usable.value[0]
  keyId.value = fit ? fit.id : MANUAL
}, { immediate: true })
watch([ep, tryDevices], ([e]) => {
  if (!tryDevices.value.some((d) => d.id === deviceId.value)) deviceId.value = tryDevices.value[0]?.id ?? null
  if (e !== lastEp) {
    lastEp = e
    resp.value = null
    body.value = e?.bodyExample ? JSON.stringify(e.bodyExample, null, 2) : ''
    query.value = e?.id === 'get-invocation' ? 'wait=10' : e?.id === 'list-events' ? 'page_size=5' : e?.command ? 'timeout=10' : ''
    idem.value = ''
  }
}, { immediate: true })
const tryPath = computed(() => {
  if (!ep.value) return ''
  const id = ep.value.id === 'get-invocation' ? invId.value.trim() : String(deviceId.value ?? '')
  const p = `/open/v1${ep.value.path.replace('{id}', id || '{id}')}`
  return query.value.trim() ? `${p}?${query.value.trim().replace(/^\?/, '')}` : p
})
const tryProblem = computed(() => {
  if (keyId.value === MANUAL && !key.value.trim().startsWith('rkk_')) return '填入以 rkk_ 开头的 API 密钥'
  if (tryPath.value.includes('{id}')) return ep.value?.id === 'get-invocation' ? '填入指令 ID' : '选择设备'
  if (ep.value?.method === 'POST') { try { JSON.parse(body.value || '{}') } catch { return '请求体不是有效的 JSON' } }
  return ''
})
async function send() {
  if (!ep.value || tryProblem.value) return
  sending.value = true
  try {
    resp.value = await api.tryAPI({ ...(keyId.value === MANUAL ? { key: key.value.trim() } : { key_id: keyId.value ?? undefined }), method: ep.value.method, path: tryPath.value,
      body: ep.value.method === 'POST' ? JSON.parse(body.value || '{}') : undefined, idempotency_key: idem.value.trim() || undefined })
    // 异步或超时返回了 invocation_id：方便接着查结果。
    const d = (resp.value.body as { data?: { invocation_id?: number } })?.data
    if (d?.invocation_id) invId.value = String(d.invocation_id)
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : '请求失败')
  } finally {
    sending.value = false
  }
}
</script>

<template>
  <div class="docs">
    <nav class="card toc">
      <div class="g">开始</div>
      <a v-for="s in sections" :key="s.id" :class="{ on: current === s.id }" @click="current = s.id">{{ s.label }}</a>
      <template v-for="g in toc" :key="g.group">
        <div class="g">{{ g.group }}<span v-if="g.group === '命令'" class="gn">{{ g.items.length }}</span></div>
        <div v-if="g.group === '命令' && !g.items.length" class="faint small none">暂无设备开放命令</div>
        <a v-for="e in g.items" :key="e.id" :class="{ on: current === e.id, cmd: !!e.command }" @click="current = e.id">
          <span class="m" :class="e.method.toLowerCase()">{{ e.method }}</span>
          <span class="tt"><span>{{ e.title }}</span></span>
          <span v-if="e.command && !e.command.online" class="off" title="支持该命令的设备当前都不在线">离线</span></a>
      </template>
      <a class="dl" href="/api/v1/api-keys/openapi.json" download>下载 OpenAPI 规范</a>
    </nav>

    <article class="card body">
      <template v-if="current === 'intro'">
        <h1>介绍与鉴权</h1>
        <p class="muted">外部系统用 API 密钥查询设备、调用设备命令、读取事件。<b>每个设备命令都是一个独立接口</b>（左侧「命令」），
          请求体就是命令参数，像 RPC 一样同步返回结果。</p>
        <div class="sec">接口地址</div>
        <div class="path"><code class="mono">{{ base }}</code><span class="spacer" /><a class="cp" @click="copy(base)">复制</a></div>
        <div class="sec">鉴权</div>
        <p class="muted">每个请求带 <code>Authorization: Bearer rkk_…</code>。密钥在「密钥」标签创建，按权限与设备范围授权：</p>
        <table class="table ptab"><tbody>
          <tr v-for="(m, s) in scopeMeta" :key="s"><td class="mono nowrap">{{ s }}</td><td>{{ m.label }}</td><td class="muted">{{ m.desc }}</td></tr>
        </tbody></table>
        <div class="sec">调用命令</div>
        <CodeBlock lang="json" :code="introCall" />
        <p class="muted small">设备离线时立即返回 409（带 ttl 则排队并返回 202）；设备执行失败 422（msg 为原因）；设备 timeout 秒内没返回 504（可用 invocation_id 之后查询）。命令的参数与返回字段由设备上报，不同版本以「设备可用的命令」为准。</p>
        <div class="sec">响应格式</div>
        <CodeBlock lang="json" :code="introResp" />
        <p class="muted small">code 为 0 表示成功；失败时 HTTP 状态码与 code 对应，见「错误码」。时间均为 RFC 3339（UTC）。</p>
      </template>

      <template v-else-if="current === 'errors'">
        <h1>错误码</h1>
        <p class="muted">失败响应：<code>{"code": 30005, "msg": "计数未开启", "data": {"invocation_id": 1024}}</code>，msg 为可展示的中文说明。</p>
        <table class="table ptab"><thead><tr><th>HTTP</th><th>code</th><th>含义</th></tr></thead><tbody>
          <tr v-for="e in errorCodes" :key="e[1]"><td class="mono">{{ e[0] }}</td><td class="mono">{{ e[1] }}</td><td class="muted">{{ e[2] }}</td></tr>
        </tbody></table>
        <p class="muted small">无效密钥会计入来源 IP 的失败次数，过多时该 IP 被临时封禁。</p>
      </template>

      <template v-else-if="current === 'concurrency'">
        <h1>并发与幂等</h1>
        <ul class="list">
          <li>接口不限流，可任意并发。建议复用连接（HTTP 客户端的连接池），不要每个请求都新建连接。</li>
          <li>同一设备同时最多处理 16 条命令，其余在服务器按提交顺序排队，有结果返回后依次下发，不会因突发量把设备挤掉线。</li>
          <li>带 <code>Idempotency-Key</code>：同一密钥下相同的键只执行一次；并发或重试的相同请求都返回同一条指令的结果（参数不同则 409）。网络超时后重试务必带上同一个键。</li>
          <li>默认同步等待（最多 30 秒，<code>?timeout=</code> 可调小）；长时间的命令可用 <code>?async=true</code> 立即返回，再用 <code>GET /invocations/{id}?wait=30</code> 长轮询结果。</li>
          <li>下发后超过命令的执行上限（默认 2 分钟，命令可自行声明）仍没有结果的指令记为 <code>lost</code>（结果未知）。</li>
        </ul>
        <div class="sec">指令状态（GET /invocations/{id}）</div>
        <table class="table ptab"><tbody>
          <tr><td class="mono">pending</td><td class="muted">排队中（设备离线或在途已满）</td></tr>
          <tr><td class="mono">sent</td><td class="muted">已下发，等待设备返回</td></tr>
          <tr><td class="mono">succeeded</td><td class="muted">成功，结果在 result</td></tr>
          <tr><td class="mono">failed</td><td class="muted">设备返回错误：error_code、error_message</td></tr>
          <tr><td class="mono">expired</td><td class="muted">排队超过 ttl 仍未能下发</td></tr>
          <tr><td class="mono">lost</td><td class="muted">结果未知：已下发后连接断开，或设备超时未返回</td></tr>
        </tbody></table>
      </template>

      <template v-else-if="ep">
        <h1>{{ ep.title }} <span class="pill p-run">{{ ep.scope }}</span></h1>
        <p class="muted">{{ ep.desc }}</p>
        <div class="path"><span class="m" :class="ep.method.toLowerCase()">{{ ep.method }}</span>
          <span class="mono pp">/open/v1{{ ep.path }}</span><span class="spacer" /><a class="cp" @click="copy(`${base}${ep.path}`)">复制</a></div>
        <p v-if="ep.command" class="small faint">支持的设备：{{ (ep.command.devices ?? []).map((d) => { const x = devices.find((y) => y.id === d); return x ? `${deviceTitle(x)}${x.online ? '' : '（离线）'}` : `#${d}` }).join('、') || '—' }}</p>
        <div v-if="ep.command && !ep.command.online" class="offnote">支持这个命令的设备当前都不在线：直接调用会返回 409（设备离线）；带 <code>?ttl=秒</code> 则排队，设备上线后执行。</div>

        <template v-for="[title, fields] in ([['路径参数', ep.pathParams], ['查询参数', ep.query], ['请求头', ep.headers], [ep.command ? '请求体（命令参数）' : '请求体', ep.body]] as const)" :key="title">
          <template v-if="fields?.length">
            <div class="sec">{{ title }}</div>
            <table class="table ptab"><tbody>
              <tr v-for="f in fields" :key="f.name">
                <td class="mono nowrap">{{ f.name }}<span v-if="f.required" class="req">必填</span></td>
                <td class="mono muted nowrap">{{ f.type }}</td><td class="muted">{{ f.desc }}</td></tr>
            </tbody></table>
          </template>
        </template>
        <template v-if="ep.command && !ep.body?.length">
          <div class="sec">请求体（命令参数）</div>
          <p class="muted small">{{ ep.command.declared ? '无参数，请求体可省略或传 {}' : '设备未声明参数，按模块约定传 JSON 对象' }}</p>
        </template>
        <template v-if="ep.resultFields?.length">
          <div class="sec">返回字段（data.result）</div>
          <table class="table ptab"><tbody>
            <tr v-for="f in ep.resultFields" :key="f.name"><td class="mono nowrap">{{ f.name }}</td><td class="mono muted nowrap">{{ f.type }}</td><td class="muted">{{ f.desc }}</td></tr>
          </tbody></table>
        </template>

        <div class="sec row">示例代码<span class="spacer" />
          <div class="seg"><span v-for="l in langs" :key="l.key" :class="{ on: lang === l.key }" @click="lang = l.key">{{ l.label }}</span></div>
          <a class="cp" @click="copy(code)">复制</a></div>
        <CodeBlock :code="code" :lang="codeLang" />

        <div class="sec">响应示例</div>
        <CodeBlock :code="ep.response" lang="json" />

        <div class="sec">在线调试</div>
        <div class="try">
          <div class="grid2">
            <div><label>API 密钥<a v-if="!usable.length" class="cp lbl" @click="emit('create')">创建密钥</a></label>
              <n-select :value="keyId" :options="keyOptions" size="small" @update:value="pickKey" />
              <n-input v-if="keyId === MANUAL" v-model:value="key" type="password" show-password-on="click" placeholder="rkk_…（只保存在本页内存）" size="small" class="mt6" /></div>
            <div v-if="ep.id === 'get-invocation'"><label>指令 ID</label><n-input v-model:value="invId" placeholder="异步调用后自动填入" size="small" /></div>
            <div v-else-if="ep.path.includes('{id}')"><label>设备</label>
              <n-select v-model:value="deviceId" :options="deviceOptions" size="small" placeholder="没有支持该命令的设备" /></div>
          </div>
          <div v-if="ep.query?.length" class="frow"><label>查询参数</label><n-input v-model:value="query" placeholder="如 timeout=10 或 async=true" size="small" /></div>
          <div v-if="ep.method === 'POST'" class="frow"><label>请求体</label>
            <n-input v-model:value="body" type="textarea" :autosize="{ minRows: 2, maxRows: 10 }" class="mono" size="small" placeholder="{}" /></div>
          <div v-if="ep.command" class="frow"><label>Idempotency-Key <span class="faint">（可选）</span></label>
            <n-input v-model:value="idem" placeholder="如 job-001" size="small" /></div>
          <div class="row sendrow">
            <n-button type="primary" size="small" :loading="sending" :disabled="!!tryProblem" @click="send">发送请求</n-button>
            <span class="mono faint small path2">{{ ep.method }} {{ tryPath }}</span>
            <span v-if="tryProblem" class="faint small">{{ tryProblem }}</span>
          </div>
          <div v-if="resp" class="resp">
            <div class="small"><span class="pill" :class="resp.status < 300 ? 'p-ok' : 'p-err'">{{ resp.status }}</span>
              <span class="muted num"> {{ resp.duration_ms.toFixed(0) }} ms</span></div>
            <JsonView :value="resp.body" />
          </div>
        </div>

        <div class="sec">错误</div>
        <table class="table ptab"><tbody>
          <tr v-for="e in ep.errors" :key="e[0]"><td class="mono nowrap">{{ e[0] }}</td><td class="muted">{{ e[1] }}</td></tr>
        </tbody></table>
      </template>
    </article>
  </div>
</template>

<style scoped>
.docs { display: grid; grid-template-columns: 240px minmax(0, 1fr); gap: 16px; align-items: start; }
.toc { position: sticky; top: calc(var(--bar-h, 64px) + 12px); padding: 10px; display: flex; flex-direction: column; gap: 3px;
  max-height: calc(100vh - var(--bar-h, 64px) - 32px); overflow-y: auto; }
.toc .g { font-size: 11px; color: var(--faint); padding: 10px 10px 2px; display: flex; align-items: center; gap: 6px; }
.toc .g:first-child { padding-top: 2px; }
.gn { font-size: 10.5px; background: var(--fill); border-radius: 99px; padding: 0 6px; color: var(--sub); }
.none { padding: 4px 10px; }
.toc a { display: flex; gap: 8px; align-items: center; padding: 7px 10px; border-radius: 9px; color: var(--text); font-size: 13px; cursor: pointer; text-decoration: none; }
.toc a:hover { background: var(--fill); }
.toc a.on { background: var(--svc-soft); color: var(--svc); font-weight: 600; }
.toc a.cmd { align-items: flex-start; }
.toc a.cmd .m { margin-top: 1px; }
.tt { display: flex; flex-direction: column; min-width: 0; flex: 1; }
.off { font-size: 10.5px; color: var(--faint); background: var(--fill); border-radius: 5px; padding: 1px 6px; flex: none; }
.offnote { margin: 8px 0 4px; padding: 9px 12px; border-radius: 10px; font-size: 13px; line-height: 1.6;
  background: color-mix(in srgb, var(--orange) 12%, transparent); color: var(--text); }
.toc .dl { margin-top: 10px; border-top: 1px solid var(--sep); border-radius: 0; padding-top: 12px; color: var(--svc); font-size: 12.5px; }
.m { font: 700 10.5px ui-monospace, "SF Mono", Menlo, monospace; padding: 2px 6px; border-radius: 5px; min-width: 40px; text-align: center; flex: none; }
.m.get { background: color-mix(in srgb, var(--green) 16%, transparent); color: var(--green); }
.m.post { background: var(--svc-soft); color: var(--svc); }
.body { min-width: 0; }
h1 { font-size: 20px; margin: 0 0 6px; display: flex; align-items: center; gap: 10px; }
p { margin: 6px 0; line-height: 1.7; font-size: 13.5px; }
.small { font-size: 12px; }
.nowrap { white-space: nowrap; }
.sec { font-size: 13px; font-weight: 700; margin: 20px 0 8px; gap: 10px; }
.path { display: flex; align-items: center; gap: 10px; background: var(--fill); padding: 9px 12px; border-radius: 10px; margin: 12px 0 4px; min-width: 0; }
.pp { font-size: 13.5px; font-weight: 600; overflow-wrap: anywhere; }
.cp { color: var(--svc); cursor: pointer; font-size: 12px; white-space: nowrap; }
code { font-family: ui-monospace, "SF Mono", Menlo, monospace; font-size: 12px; background: var(--fill); padding: 1px 6px; border-radius: 5px; }
.path code { background: none; padding: 0; font-size: 13px; overflow-wrap: anywhere; }
pre { margin: 0; background: var(--fill); border-radius: 10px; padding: 12px 14px; font-size: 12px; line-height: 1.6; overflow-x: auto; white-space: pre; max-width: 100%; }
.ptab { margin: 0; }
.ptab { table-layout: fixed; width: 100%; }
.ptab td { padding: 8px 10px; overflow-wrap: anywhere; }
.ptab td:first-child { width: 190px; }
.ptab td:nth-child(2):not(:last-child) { width: 120px; }
.req { color: var(--red); font-size: 11px; margin-left: 6px; font-family: inherit; }
.seg { display: inline-flex; background: var(--fill); border-radius: 9px; padding: 2px; font-weight: 400; }
.seg span { font-size: 12px; padding: 3px 10px; border-radius: 7px; cursor: pointer; color: var(--sub); }
.seg span.on { background: var(--card); color: var(--text); font-weight: 600; box-shadow: 0 1px 3px rgba(0, 0, 0, .08); }
.list { margin: 8px 0; padding-left: 20px; line-height: 1.8; font-size: 13.5px; }
.try { border: 1px solid var(--sep); border-radius: 14px; padding: 14px; display: flex; flex-direction: column; gap: 10px; }
.try label { display: block; font-size: 12px; color: var(--sub); font-weight: 600; margin-bottom: 5px; }
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.sendrow { flex-wrap: wrap; }
.path2 { overflow-wrap: anywhere; min-width: 0; }
.resp { display: flex; flex-direction: column; gap: 6px; }
.lbl { margin-left: 8px; font-weight: 400; }
.mt6 { margin-top: 6px; }
@media (max-width: 900px) { .docs, .grid2 { grid-template-columns: 1fr; } .toc { position: static; max-height: none; } }
</style>
