<script setup lang="ts">
// 安装链接：新机器在 Sileo 里添加链接即可安装（源里提供上传时选择「同步到软件源」的最新版本）。
// 每个链接可设有效期、下载次数；可随时查看地址、修改、吊销，已吊销的可删除。点一行查看下载记录。
import { computed, onMounted, ref } from 'vue'
import { NButton, NInput, NModal, NPopconfirm, NSelect, useMessage } from 'naive-ui'
import QRCode from 'qrcode'
import { api, ApiError } from '../api/client'
import type { RepoDownload, RepoLink, RepoOverview } from '../api/types'
import { useAuth } from '../stores/auth'
import Icon from './Icon.vue'
import { fmtAgo, fmtTime } from '../format'

const message = useMessage()
const auth = useAuth()
const ov = ref<RepoOverview | null>(null)
const loading = ref(true)

async function load() {
  try { ov.value = await api.repoOverview() } finally { loading.value = false }
}
defineExpose({ load })
onMounted(load)

const statusMeta: Record<RepoLink['status'], { text: string; cls: string }> = {
  active: { text: '可用', cls: 'p-ok' },
  expired: { text: '已过期', cls: 'p-wait' },
  exhausted: { text: '次数用完', cls: 'p-wait' },
  revoked: { text: '已吊销', cls: 'p-wait' },
}
const base = computed(() => (ov.value?.base_url || window.location.origin).replace(/\/+$/, ''))
function quota(l: RepoLink) { return l.max_downloads ? `${l.downloads} / ${l.max_downloads}` : `${l.downloads} / 不限` }
function fail(e: unknown, fallback: string) { message.error(e instanceof ApiError ? e.message : fallback) }

const expiresOptions = [
  { label: '24 小时', value: 24 }, { label: '7 天', value: 24 * 7 }, { label: '30 天', value: 24 * 30 }, { label: '不过期', value: 0 },
]
const maxOptions = [
  { label: '1 次', value: 1 }, { label: '3 次', value: 3 }, { label: '10 次', value: 10 }, { label: '不限', value: 0 },
]

// ---- 下载记录（弹窗）----
const dlOf = ref<RepoLink | null>(null)
const downloads = ref<RepoDownload[]>([])
const dlLoading = ref(false)
async function openDownloads(l: RepoLink) {
  dlOf.value = l
  downloads.value = []
  dlLoading.value = true
  try { downloads.value = await api.repoDownloads(l.id) } catch (e) { fail(e, '加载失败') } finally { dlLoading.value = false }
}

// ---- 查看地址（弹窗，生成后也用它）----
const shown = ref<{ name: string; url: string } | null>(null)
const qr = ref('')
async function present(name: string, path: string) {
  const url = base.value + path
  // 手机相机扫码直接打开 Sileo 并添加这个源。
  qr.value = await QRCode.toDataURL(`sileo://source/${url}`, { margin: 1, width: 200 })
  shown.value = { name, url }
}
async function view(l: RepoLink) {
  try { await present(l.name, (await api.revealRepoLink(l.id)).path) } catch (e) { fail(e, '无法查看') }
}
async function copy(v: string) {
  await navigator.clipboard.writeText(v)
  message.success('已复制')
}

// ---- 生成 ----
const show = ref(false)
const name = ref('')
const expires = ref(24)
const maxDl = ref(3)
const password = ref('')
const totp = ref('')
const busy = ref(false)
function openCreate() {
  name.value = password.value = totp.value = ''
  expires.value = 24
  maxDl.value = 3
  show.value = true
}
const ready = computed(() => !!name.value.trim() && !!password.value && (!auth.admin?.totp_enabled || totp.value.length === 6))
async function create() {
  if (!ready.value) return
  busy.value = true
  try {
    const l = await api.createRepoLink({ name: name.value.trim(), expires_hours: expires.value, max_downloads: maxDl.value,
      password: password.value, totp_code: totp.value || undefined })
    show.value = false
    await present(l.name, l.path)
    await load()
  } catch (e) {
    fail(e, '生成失败')
  } finally {
    busy.value = false
  }
}

// ---- 编辑 ----
const editing = ref<RepoLink | null>(null)
const eName = ref('')
const eExpires = ref(-1) // -1 表示有效期不变
const eMax = ref(0)
const eOptions = [{ label: '保持不变', value: -1 },
  ...expiresOptions.map((o) => ({ ...o, label: o.value ? `从现在起 ${o.label}` : '不过期' }))]
const eMaxOptions = computed(() => {
  const cur = editing.value?.max_downloads ?? 0
  return maxOptions.some((o) => o.value === cur) ? maxOptions : [...maxOptions, { label: `${cur} 次`, value: cur }]
})
function openEdit(l: RepoLink) {
  editing.value = l
  eName.value = l.name
  eExpires.value = -1
  eMax.value = l.max_downloads
}
async function save() {
  if (!editing.value || !eName.value.trim()) return
  busy.value = true
  try {
    await api.updateRepoLink(editing.value.id, { name: eName.value.trim(), max_downloads: eMax.value,
      ...(eExpires.value < 0 ? {} : { expires_hours: eExpires.value }) })
    message.success('已保存')
    editing.value = null
    await load()
  } catch (e) {
    fail(e, '保存失败')
  } finally {
    busy.value = false
  }
}

async function revoke(id: number) {
  try { await api.revokeRepoLink(id); message.success('已吊销'); await load() } catch (e) { fail(e, '吊销失败') }
}
async function remove(id: number) {
  try { await api.deleteRepoLink(id); message.success('已删除'); await load() } catch (e) { fail(e, '删除失败') }
}
</script>

<template>
  <div class="card tcard">
    <div class="card-title">
      <h3><Icon name="link" :size="16" />安装链接</h3>
      <div class="spacer" />
      <n-button v-if="ov?.enabled" size="small" @click="openCreate">生成链接</n-button>
    </div>
    <p v-if="ov && !ov.enabled" class="muted desc">
      未开启。在服务器配置里设置 <code>repo.enabled: true</code> 并重启服务后，可以在这里生成安装链接：新机器在 Sileo 里添加链接即可安装，已装的设备继续走 OTA 更新。
    </p>
    <template v-else-if="ov">
      <p class="muted desc">
        新机器在 Sileo 里添加链接即可安装。源里提供的版本：
        <b v-if="ov.latest" class="ver">{{ ov.latest.version }}</b>
        <span v-else class="warn-text">暂无（上传版本时打开「同步到软件源」），Sileo 里会是空的</span>
      </p>
      <div class="table-wrap">
        <table class="table">
          <thead><tr><th>名称</th><th style="width:90px">状态</th><th style="width:100px">已下载</th><th style="width:140px">有效期至</th><th style="width:110px">最近下载</th><th style="width:170px" /></tr></thead>
          <tbody>
            <tr v-for="l in ov.links" :key="l.id" class="clickable" :class="{ dim: l.status !== 'active' }" title="查看下载记录" @click="openDownloads(l)">
              <td><span class="label">{{ l.name }}</span> <span class="mono faint">{{ l.token_prefix }}…</span></td>
              <td><span class="pill" :class="statusMeta[l.status].cls">{{ statusMeta[l.status].text }}</span></td>
              <td class="num">{{ quota(l) }}</td>
              <td class="mono muted num">{{ l.expires_at ? fmtTime(l.expires_at).slice(0, 16) : '不过期' }}</td>
              <td class="muted">{{ l.last_used_at ? fmtAgo(l.last_used_at) : '—' }}</td>
              <td class="acts" @click.stop>
                <template v-if="l.status !== 'revoked'">
                  <n-button v-if="l.viewable" size="small" quaternary @click="view(l)">查看</n-button>
                  <n-button size="small" quaternary @click="openEdit(l)">编辑</n-button>
                  <n-popconfirm @positive-click="revoke(l.id)">
                    <template #trigger><n-button size="small" type="error" quaternary>吊销</n-button></template>
                    吊销后这个链接立即失效，已添加它的 Sileo 刷新会失败，确定？
                  </n-popconfirm>
                </template>
                <n-popconfirm v-else @positive-click="remove(l.id)">
                  <template #trigger><n-button size="small" type="error" quaternary>删除</n-button></template>
                  删除这个链接和它的下载记录？不可恢复。
                </n-popconfirm>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!ov.links.length" class="empty">还没有安装链接</div>
    </template>
    <div v-else-if="loading" class="skel" style="width:60%;margin-top:8px" />
  </div>

  <!-- 下载记录 -->
  <n-modal :show="!!dlOf" preset="card" :title="`下载记录 · ${dlOf?.name ?? ''}`" style="width:560px;max-width:calc(100vw - 32px)" :bordered="false" @close="dlOf = null" @mask-click="dlOf = null">
    <p class="muted small top">每次完整下载安装包算一次；Sileo 刷新源不算。最近 100 条。</p>
    <div v-if="dlLoading" class="skel" style="width:70%" />
    <div v-else-if="!downloads.length" class="empty">还没有通过这个链接下载过</div>
    <div v-else class="table-wrap">
      <table class="table">
        <thead><tr><th style="width:120px">时间</th><th>IP</th><th>设备</th><th style="width:80px">版本</th></tr></thead>
        <tbody>
          <tr v-for="d in downloads" :key="d.id">
            <td class="mono muted num">{{ fmtTime(d.created_at).slice(5, 16) }}</td>
            <td class="mono">{{ d.ip }}</td>
            <td>{{ d.machine || '—' }}<span v-if="d.firmware" class="muted"> · iOS {{ d.firmware }}</span></td>
            <td class="muted num">{{ d.version }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </n-modal>

  <!-- 生成 -->
  <n-modal v-model:show="show" preset="card" title="生成安装链接" style="width:500px;max-width:calc(100vw - 32px)" :bordered="false" :mask-closable="!busy">
    <div class="field"><label>名称</label><n-input v-model:value="name" maxlength="64" placeholder="比如：给张三的新手机" /></div>
    <div class="field two">
      <div><label>有效期</label><n-select v-model:value="expires" :options="expiresOptions" /></div>
      <div><label>下载次数</label><n-select v-model:value="maxDl" :options="maxOptions" /></div>
    </div>
    <p class="faint small hint">每次完整下载安装包算一次；Sileo 刷新源不算。过期或次数用完后链接自动失效，之后可在「编辑」里延长。</p>
    <div class="field two">
      <div><label>当前密码</label><n-input v-model:value="password" type="password" show-password-on="click" placeholder="需要再次验证" /></div>
      <div v-if="auth.admin?.totp_enabled"><label>两步验证码</label><n-input v-model:value="totp" maxlength="6" placeholder="6 位数字" /></div>
    </div>
    <template #footer>
      <div class="row"><div class="spacer" />
        <n-button :disabled="busy" @click="show = false">取消</n-button>
        <n-button type="primary" :loading="busy" :disabled="!ready" @click="create">生成</n-button>
      </div>
    </template>
  </n-modal>

  <!-- 编辑 -->
  <n-modal :show="!!editing" preset="card" title="编辑安装链接" style="width:460px;max-width:calc(100vw - 32px)" :bordered="false" @close="editing = null" @mask-click="editing = null">
    <div class="field"><label>名称</label><n-input v-model:value="eName" maxlength="64" /></div>
    <div class="field two">
      <div><label>有效期</label><n-select v-model:value="eExpires" :options="eOptions" /></div>
      <div><label>下载次数</label><n-select v-model:value="eMax" :options="eMaxOptions" /></div>
    </div>
    <p class="faint small hint">
      当前{{ editing?.expires_at ? `有效期至 ${fmtTime(editing.expires_at).slice(0, 16)}` : '不过期' }}，已下载 {{ editing?.downloads ?? 0 }} 次。
      过期或次数用完的链接，延长有效期或调高次数后重新生效。
    </p>
    <template #footer>
      <div class="row"><div class="spacer" />
        <n-button :disabled="busy" @click="editing = null">取消</n-button>
        <n-button type="primary" :loading="busy" :disabled="!eName.trim()" @click="save">保存</n-button>
      </div>
    </template>
  </n-modal>

  <!-- 地址与二维码 -->
  <n-modal :show="!!shown" preset="card" :title="`安装链接 · ${shown?.name ?? ''}`" style="width:500px;max-width:calc(100vw - 32px)" :bordered="false" @close="shown = null" @mask-click="shown = null">
    <div class="share">
      <img v-if="qr" :src="qr" alt="扫码在 Sileo 中添加" width="160" height="160">
      <div>
        <b>手机相机扫码</b>
        <p class="muted small">直接打开 Sileo 并添加这个源。也可以在 Sileo「源」页点「+」粘贴下面的地址。泄露时吊销即可。</p>
      </div>
    </div>
    <pre class="token mono">{{ shown?.url }}</pre>
    <template #footer>
      <div class="row"><div class="spacer" />
        <n-button @click="copy(shown!.url)">复制地址</n-button>
        <n-button type="primary" @click="shown = null">完成</n-button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.tcard { padding: 16px 20px; }
.card-title { display: flex; align-items: center; gap: 8px; }
.card-title h3 { display: flex; align-items: center; gap: 6px; margin: 0; }
.spacer { flex: 1; }
.desc { font-size: 13px; line-height: 1.7; margin: 8px 0 12px; }
.ver { font-variant-numeric: tabular-nums; color: var(--text); }
.warn-text { color: var(--orange); }
code { font-family: ui-monospace, "SF Mono", Menlo, monospace; font-size: 12px; background: var(--fill); padding: 1px 6px; border-radius: 5px; }
.label { font-weight: 600; }
tr.dim td:not(.acts) { opacity: .55; }
.acts { text-align: right; white-space: nowrap; }
.small { font-size: 12px; }
.top { margin: 0 0 10px; }
.hint { margin: 6px 0 0; line-height: 1.6; }
.field { margin-top: 14px; }
.field label { display: block; font-size: 12px; color: var(--sub); margin-bottom: 6px; font-weight: 600; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.share { display: flex; gap: 16px; align-items: center; margin: 4px 0 12px; }
.share img { border-radius: 10px; background: #fff; padding: 6px; flex: none; }
.share p { margin: 4px 0 0; }
.token { background: var(--fill); border-radius: 10px; padding: 12px; word-break: break-all; white-space: pre-wrap; font-size: 12.5px; margin: 0; }
@media (max-width: 760px) { .two { grid-template-columns: 1fr; } .share { flex-direction: column; align-items: flex-start; } }
</style>
