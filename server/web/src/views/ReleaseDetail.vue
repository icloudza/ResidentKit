<script setup lang="ts">
// 版本详情：灰度计划与每台设备的更新状态，实时刷新。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { NButton, NModal, NPopconfirm, useMessage } from 'naive-ui'
import ChoiceCard from '../components/ChoiceCard.vue'
import { api, ApiError } from '../api/client'
import { onStream } from '../api/stream'
import type { Device, ReleaseDetail, Rollout } from '../api/types'
import PageBar from '../components/PageBar.vue'
import { compareVersions, deviceTitle, fmtAgo, fmtBytes, fmtTime, rolloutStatusMeta, updateStatusMeta } from '../format'

const route = useRoute()
const message = useMessage()
const id = Number(route.params.id)
const detail = ref<ReleaseDetail | null>(null)
const devices = ref<Device[]>([])
const repoEnabled = ref(false)

async function load() {
  let cfg
  ;[detail.value, devices.value, cfg] = await Promise.all([api.release(id), api.devices(), api.otaConfig()])
  repoEnabled.value = cfg.repo_enabled
}
const rel = computed(() => detail.value?.release)
const byId = computed(() => Object.fromEntries(devices.value.map((d) => [d.id, d])))
const name = (did: number) => (byId.value[did] ? deviceTitle(byId.value[did]) : `#${did}`)
const supports = (d: Device) => d.remote_commands?.['system.update']?.includes('install') ?? false

// 版本低于本版本、尚无更新记录的设备。
const waiting = computed(() => {
  if (!rel.value) return []
  const done = new Set(detail.value!.devices.map((u) => u.device_id))
  return devices.value.filter((d) => compareVersions(d.version, rel.value!.version) < 0 && !done.has(d.id))
})
const anyActiveAll = computed(() => detail.value?.rollouts.some((r) => r.target === 'all' && r.status === 'active') ?? false)

function targetText(r: Rollout) {
  if (r.target === 'all') return '全部设备'
  const names = r.device_ids.slice(0, 3).map(name).join('、')
  return r.device_ids.length > 3 ? `${names} 等 ${r.device_ids.length} 台` : names
}

async function act(f: () => Promise<unknown>, ok: string) {
  try { await f(); message.success(ok); await load() } catch (e) { message.error(e instanceof ApiError ? e.message : '操作失败') }
}

// ---- 新建计划 ----
const show = ref(false)
const target = ref<'devices' | 'all'>('devices')
const picked = ref<number[]>([])
const candidates = computed(() => devices.value.filter((d) => rel.value && compareVersions(d.version, rel.value.version) < 0))
function openCreate() {
  target.value = 'devices'
  picked.value = []
  show.value = true
}
function togglePick(did: number, v: boolean) {
  picked.value = v ? [...picked.value, did] : picked.value.filter((x) => x !== did)
}
async function create() {
  if (target.value === 'devices' && !picked.value.length) return message.warning('请选择设备')
  await act(() => api.createRollout(id, target.value, picked.value), '已创建，在线设备会立即开始更新')
  show.value = false
}

let off = () => {}
onMounted(() => {
  void load()
  off = onStream((topic, data) => {
    if (topic === 'update.changed' && (data as { release_id: number }).release_id === id) void load()
    else if (topic === 'device.online' || topic === 'device.offline') void load()
  })
})
onUnmounted(() => off())
</script>

<template>
  <PageBar :title="rel ? `版本 ${rel.version}` : '版本'" crumb="发布" crumb-to="/releases" />
  <div class="content">
    <div v-if="!detail" class="card"><div class="skel" style="width:40%;height:24px" /></div>
    <template v-else>
      <div class="card hero">
        <div class="hmain">
          <div class="row">
            <h1>{{ rel!.version }}</h1>
            <span class="pill" :class="rel!.status === 'published' ? 'p-ok' : 'p-wait'">{{ rel!.status === 'published' ? '已发布' : '已撤回' }}</span>
            <span v-if="rel!.in_repo && rel!.status === 'published'" class="pill p-run">已同步到软件源</span>
            <template v-if="repoEnabled && rel!.status === 'published'">
              <n-button v-if="!rel!.in_repo" size="tiny" secondary @click="act(() => api.setReleaseRepo(id, true), '已同步到软件源')">同步到软件源</n-button>
              <n-popconfirm v-else @positive-click="act(() => api.setReleaseRepo(id, false), '已从软件源移除')">
                <template #trigger><n-button size="tiny" quaternary>从源里移除</n-button></template>
                移除后软件源改为提供之前同步过的版本（没有则为空），确定？
              </n-popconfirm>
            </template>
          </div>
          <div class="meta muted">
            <span class="num">{{ fmtBytes(rel!.size) }}</span>
            <span class="mono" :title="rel!.sha256">sha256 {{ rel!.sha256.slice(0, 16) }}…</span>
            <span class="num">{{ fmtTime(rel!.created_at).slice(0, 16) }}</span>
          </div>
          <p v-if="rel!.notes" class="notes">{{ rel!.notes }}</p>
        </div>
        <div class="counts">
          <div v-for="k in ['installed', 'downloading', 'failed', 'rolled_back']" :key="k">
            <b class="num">{{ (rel!.counts as any)[k] ?? 0 }}</b><span>{{ updateStatusMeta[k].text }}</span>
          </div>
        </div>
        <n-popconfirm v-if="rel!.status === 'published'" @positive-click="act(() => api.withdrawRelease(id), '已撤回')">
          <template #trigger><n-button size="small" quaternary type="error" class="wd">撤回</n-button></template>
          撤回后停止所有灰度计划，已开始的更新不受影响。确定？
        </n-popconfirm>
      </div>

      <div class="card">
        <div class="card-title"><h3>灰度计划</h3>
          <div class="spacer" />
          <n-button v-if="rel!.status === 'published'" size="small" type="primary" @click="openCreate">新建计划</n-button>
        </div>
        <div v-if="!detail.rollouts.length" class="empty">
          还没有计划。建议先选一两台设备试装，确认正常后再推送全部设备。
        </div>
        <div v-for="r in detail.rollouts" :key="r.id" class="li">
          <span class="pill" :class="rolloutStatusMeta[r.status].cls">{{ rolloutStatusMeta[r.status].text }}</span>
          <span class="tgt">{{ targetText(r) }}</span>
          <span class="faint small num">{{ fmtAgo(r.created_at) }}创建</span>
          <div class="spacer" />
          <template v-if="r.status === 'active'">
            <n-button size="tiny" quaternary @click="act(() => api.changeRollout(r.id, 'pause'), '已暂停')">暂停</n-button>
          </template>
          <n-button v-if="r.status === 'paused'" size="tiny" quaternary type="primary" @click="act(() => api.changeRollout(r.id, 'resume'), '已继续')">继续</n-button>
          <n-popconfirm v-if="r.status === 'active' || r.status === 'paused'" @positive-click="act(() => api.changeRollout(r.id, 'cancel'), '已取消')">
            <template #trigger><n-button size="tiny" quaternary type="error">取消</n-button></template>
            取消后不再下发给新设备，已开始的不受影响。
          </n-popconfirm>
        </div>
      </div>

      <div class="card tcard">
        <div class="card-title"><h3>设备</h3><span v-if="rel!.status === 'published'" class="hint">失败或回滚的可单独重试</span></div>
        <div class="table-wrap">
          <table class="table">
            <thead><tr><th>设备</th><th style="width:100px">状态</th><th>说明</th><th style="width:120px">更新于</th><th style="width:70px" /></tr></thead>
            <tbody>
              <tr v-for="u in detail.devices" :key="u.id">
                <td><router-link :to="`/devices/${u.device_id}`" class="dlink">{{ name(u.device_id) }}</router-link></td>
                <td><span class="pill" :class="updateStatusMeta[u.status].cls">{{ updateStatusMeta[u.status].text }}</span></td>
                <td class="muted small">{{ u.error || (u.status === 'downloading' ? '下载、校验与安装中，约 1–2 分钟' : u.status === 'queued' ? '等待设备确认' : '') }}</td>
                <td class="muted small num">{{ fmtAgo(u.updated_at) }}</td>
                <td class="op">
                  <n-button v-if="rel!.status === 'published' && (u.status === 'failed' || u.status === 'rolled_back')" size="tiny" quaternary type="primary"
                    @click="act(() => api.retryUpdate(u.id), '已重新下发')">重试</n-button>
                </td>
              </tr>
              <tr v-for="d in waiting" :key="`w${d.id}`" class="wait">
                <td><router-link :to="`/devices/${d.id}`" class="dlink">{{ deviceTitle(d) }}</router-link></td>
                <td><span class="pill p-wait">{{ supports(d) ? '未更新' : '需手动安装' }}</span></td>
                <td class="faint small">当前 {{ d.version }}{{ d.online ? '' : ' · 离线' }}{{ supports(d) ? (anyActiveAll ? (d.online ? ' · 即将下发' : ' · 上线后下发') : '') : ' · 版本过旧，不支持在线更新' }}</td>
                <td /><td />
              </tr>
            </tbody>
          </table>
        </div>
        <div v-if="!detail.devices.length && !waiting.length" class="empty">所有设备都已是此版本或更高</div>
      </div>
    </template>

    <n-modal v-model:show="show" preset="card" title="新建灰度计划" style="width:500px;max-width:calc(100vw - 32px)" :bordered="false">
      <div class="seg">
        <span :class="{ on: target === 'devices' }" @click="target = 'devices'">指定设备</span>
        <span :class="{ on: target === 'all' }" @click="target = 'all'">全部设备</span>
      </div>
      <p v-if="target === 'all'" class="muted small tip">所有低于 {{ rel?.version }} 的设备都会更新，包括之后新接入的设备。离线设备上线后自动开始。</p>
      <div v-else class="picks">
        <div v-if="!candidates.length" class="empty">没有低于此版本的设备</div>
        <ChoiceCard v-for="d in candidates" :key="d.id" :selected="picked.includes(d.id)" :disabled="!supports(d)"
          @toggle="togglePick(d.id, !picked.includes(d.id))">
          <span class="pn"><span class="dot" :class="d.online ? 'on' : 'off'" />{{ deviceTitle(d) }}</span>
          <span class="faint small num">{{ d.version }} · {{ d.online ? '在线' : '离线' }}{{ supports(d) ? '' : ' · 需手动安装' }}</span>
        </ChoiceCard>
      </div>
      <template #footer>
        <div class="row"><div class="spacer" />
          <n-button @click="show = false">取消</n-button>
          <n-button type="primary" @click="create">开始更新</n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.hero { display: grid; grid-template-columns: 1fr auto auto; gap: 24px; align-items: center; }
h1 { font-size: 26px; margin: 0; letter-spacing: -.3px; font-variant-numeric: tabular-nums; }
.meta { display: flex; gap: 14px; flex-wrap: wrap; margin-top: 8px; font-size: 12.5px; }
.notes { margin: 12px 0 0; font-size: 13px; white-space: pre-wrap; color: var(--sub); }
.counts { display: flex; gap: 22px; }
.counts div { display: flex; flex-direction: column; align-items: center; gap: 2px; }
.counts b { font-size: 22px; }
.counts span { font-size: 11.5px; color: var(--sub); }
.wd { align-self: start; }
.small { font-size: 12px; }
.li { gap: 10px; }
.tgt { font-weight: 600; font-size: 13px; }
.tcard { padding: 16px 20px; }
.hint { color: var(--faint); font-size: 12px; }
.dlink { color: var(--text); font-weight: 600; text-decoration: none; }
.dlink:hover { color: var(--svc); }
.op { text-align: right; }
tr.wait td { opacity: .75; }
.seg { display: inline-flex; background: var(--fill); border-radius: 9px; padding: 2px; }
.seg span { font-size: 12.5px; padding: 4px 12px; border-radius: 7px; cursor: pointer; color: var(--sub); }
.seg span.on { background: var(--card); color: var(--text); font-weight: 600; box-shadow: 0 1px 3px rgba(0, 0, 0, .08); }
.tip { margin: 14px 0 0; line-height: 1.7; }
.picks { margin-top: 12px; max-height: 320px; overflow-y: auto; display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.pn { font-weight: 600; font-size: 13px; display: inline-flex; align-items: center; gap: 6px; }
@media (max-width: 760px) { .picks { grid-template-columns: 1fr; } }
.dot.on { background: var(--green); }
.dot.off { background: var(--faint); }
@media (max-width: 760px) { .hero { grid-template-columns: 1fr; } }
</style>
