<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NInput, NButton, NPopconfirm, useDialog, useMessage } from 'naive-ui'
import { api, ApiError } from '../api/client'
import { onStream } from '../api/stream'
import type { CommandSpec, Device, DeviceActivity, DeviceInfo, DeviceMetrics, DeviceUpdateInfo, Event, Invocation } from '../api/types'
import PageBar from '../components/PageBar.vue'
import Icon from '../components/Icon.vue'
import Ring from '../components/Ring.vue'
import ActivityChart from '../components/ActivityChart.vue'
import InvokeDrawer from '../components/InvokeDrawer.vue'
import DeviceConfigCard from '../components/DeviceConfigCard.vue'
import { metaFromSpec, type CommandMeta } from '../commands'
import { compareVersions, deviceTitle, modelName, fmtAgo, fmtBytes, fmtSpan, fmtTime, networkName, updateStatusMeta } from '../format'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const dialog = useDialog()
const id = Number(route.params.id)
const device = ref<Device | null>(null)
const now = ref(Date.now())

async function load() {
  device.value = await api.device(id)
}

// ---- 别名 ----
const editing = ref(false)
const alias = ref('')
async function saveAlias() {
  await api.setAlias(id, alias.value.trim())
  editing.value = false
  message.success('已保存')
  await load()
}

// ---- 版本更新 ----
const upd = ref<DeviceUpdateInfo | null>(null)
async function loadUpdate() { upd.value = await api.deviceUpdate(id) }
const lastUpd = computed(() => upd.value?.history[0])
const canUpdate = computed(() => !!upd.value?.latest && upd.value.supported &&
  compareVersions(upd.value.version, upd.value.latest.version) < 0 &&
  !(lastUpd.value && lastUpd.value.release_id === upd.value.latest.id && ['queued', 'downloading'].includes(lastUpd.value.status)))
async function updateNow() {
  try {
    await api.createRollout(upd.value!.latest!.id, 'devices', [id])
    message.success(device.value?.online ? '已下发，约 1–2 分钟完成' : '设备上线后开始更新')
    await loadUpdate()
  } catch (e) { message.error(e instanceof ApiError ? e.message : '操作失败') }
}

// ---- 指标 ----
const m = computed(() => device.value?.metrics)
const svcMem = computed(() => (m.value?.svc_footprint && m.value.svc_limit ? m.value.svc_footprint / m.value.svc_limit : 0))
const devMem = computed(() => (m.value?.dev_mem_used && m.value.dev_mem_total ? m.value.dev_mem_used / m.value.dev_mem_total : 0))
// ---- 设备信息：下发 net.link/info 刷新，结果直接渲染到卡片上 ----
const canRefresh = computed(() => !!device.value?.online && !!device.value.remote_commands['net.link']?.includes('info'))
const refreshing = ref(false)
const infoAt = ref('')
async function refreshInfo() {
  if (!device.value || refreshing.value) return
  refreshing.value = true
  try {
    const inv = await api.invoke(id, { module: 'net.link', command: 'info', ttl_seconds: -1, wait_seconds: 10 })
    if (inv.status === 'succeeded' && device.value) {
      const r = inv.result as { info?: DeviceInfo; version?: string; network?: string; metrics?: DeviceMetrics }
      if (r.info) device.value.info = r.info
      if (r.version) device.value.version = r.version
      if (r.network) device.value.network = r.network
      if (r.metrics) { device.value.metrics = r.metrics; device.value.metrics_at = inv.finished_at }
      infoAt.value = inv.finished_at ?? new Date().toISOString()
      message.success('已刷新')
    } else if (inv.status === 'failed') message.error(inv.error_message || '设备执行失败')
    else message.warning('设备暂未返回，稍后会自动更新')
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : '刷新失败')
  } finally {
    refreshing.value = false
  }
}
async function copyUID() {
  if (!device.value) return
  await navigator.clipboard.writeText(device.value.device_uid)
  message.success('设备标识已复制')
}
const metricsFresh = computed(() => !!device.value?.metrics_at && now.value - new Date(device.value.metrics_at).getTime() < 90_000)

// ---- 远程指令 ----
// 名称、图标、说明和参数都取自这台设备上报的命令说明（模块的 +commandSchema），控制台不用登记。
const specs = ref<CommandSpec[]>([])
async function loadCommands() { specs.value = await api.deviceCommands(id) }
const commands = computed(() => specs.value
  .filter((sp) => !isSystemModule(sp.module))
  .map((sp) => ({ module: sp.module, command: sp.command, meta: metaFromSpec(sp) })))
// 系统模块（net.*、system.*）的指令不在这里列出：连接、配置、更新各有专门的页面与卡片。
const isSystemModule = (m: string) => m.startsWith('net.') || m.startsWith('system.')
const drawer = ref(false)
const target = ref<{ module: string; command: string; meta?: CommandMeta }>({ module: '', command: '' })
function open(module: string, command: string, meta: CommandMeta) {
  target.value = { module, command, meta }
  drawer.value = true
}

// ---- 活动概览：指令与事件按时间分桶的统计图；明细在单独的「活动」页分页查看 ----
const range = ref<'24h' | '7d' | '30d'>('24h')
const activity = ref<DeviceActivity | null>(null)
async function loadActivity() { activity.value = await api.deviceActivity(id, range.value) }
watch(range, () => void loadActivity())
const failRate = computed(() => (activity.value?.invocations ? (activity.value.failed / activity.value.invocations) * 100 : 0))
// 高频调用时合并刷新：有新指令或事件后 5 秒内只重新统计一次。
let actTimer: ReturnType<typeof setTimeout> | undefined
function bumpActivity() { actTimer ??= setTimeout(() => { actTimer = undefined; void loadActivity() }, 5000) }

// 删除离线设备：吊销令牌、清理它的记录。设备若再连上会被要求重新申请接入。
function confirmDelete() {
  if (!device.value) return
  const name = deviceTitle(device.value)
  dialog.warning({
    title: `删除设备「${name}」`,
    content: '会吊销它的令牌，并删除它的指令、事件、配置与更新记录，无法恢复。设备之后再连上服务器，需要重新申请接入、由你批准。',
    positiveText: '删除', negativeText: '取消', positiveButtonProps: { type: 'error' },
    onPositiveClick: async () => {
      try {
        await api.deleteDevice(id)
        message.success('设备已删除')
        void router.replace('/devices')
      } catch (e) {
        message.error(e instanceof ApiError ? e.message : '删除失败')
        return false
      }
    },
  })
}

let off = () => {}
let tick: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  void loadCommands()
  void loadActivity()
  void loadUpdate()
  tick = setInterval(() => { now.value = Date.now() }, 15000)
  off = onStream((topic, data) => {
    if (topic.startsWith('device.') && (data as { id: number }).id === id) {
      if (topic === 'device.updated' && device.value) {
        const u = data as { rtt_ms?: number; metrics?: Device['metrics']; metrics_at?: string; info?: DeviceInfo }
        if (u.rtt_ms !== undefined) device.value.rtt_ms = u.rtt_ms
        if (u.info) device.value.info = u.info
        if (u.metrics) { device.value.metrics = u.metrics; device.value.metrics_at = u.metrics_at }
      } else if (topic === 'device.deleted') {
        void router.replace('/devices')
      } else { void load(); void loadUpdate(); if (topic === 'device.online') void loadCommands() }
    } else if (topic === 'update.changed' && (data as { device_id: number }).device_id === id) {
      void loadUpdate()
    } else if ((topic === 'event.created' || topic === 'invocation.updated') && (data as Event | Invocation).device_id === id) {
      bumpActivity()
    }
  })
})
onUnmounted(() => { off(); clearInterval(tick); clearTimeout(actTimer) })
</script>

<template>
  <PageBar title="设备详情" crumb="设备" crumb-to="/devices"
    :status="device ? (device.online ? `在线 · ${device.rtt_ms || '—'} ms` : '离线') : undefined" />
  <div class="content">
    <div v-if="!device" class="card"><div class="skel" style="width:40%;height:24px" /><div class="skel" style="width:70%;margin-top:14px" /></div>
    <template v-else>
      <div class="dhero">
        <div class="dicon"><Icon name="device" :size="32" /></div>
        <div class="dmain">
          <div class="row">
            <h1 v-if="!editing">{{ deviceTitle(device) }}</h1>
            <n-input v-else v-model:value="alias" placeholder="设备别名" maxlength="64" style="max-width:260px" @keyup.enter="saveAlias" />
            <a v-if="!editing" class="edit" @click="alias = device.alias; editing = true">改名</a>
            <template v-else>
              <n-button size="small" type="primary" @click="saveAlias">保存</n-button>
              <n-button size="small" quaternary @click="editing = false">取消</n-button>
            </template>
          </div>
          <div class="chips">
            <span class="chip"><span class="dot" :class="device.online ? 'on' : 'off'" />
              {{ device.online ? `在线 ${device.connected_at ? fmtSpan((now - new Date(device.connected_at).getTime()) / 1000) : ''}` : `离线 · ${fmtAgo(device.last_seen_at)}` }}</span>
            <span v-if="device.online" class="chip"><Icon name="wifi" :size="13" />{{ networkName[device.network] || '—' }}</span>
            <span class="chip mono">{{ device.last_ip }}</span>
            <button class="chip refresh" :class="{ spin: refreshing }" :disabled="!canRefresh || refreshing"
              :title="canRefresh ? (infoAt ? `刷新于 ${fmtTime(infoAt)}` : '向设备下发 net.link/info，取最新的设备信息与资源占用') : (device.online ? '设备版本过旧，不支持刷新' : '设备离线')"
              @click="refreshInfo"><Icon name="reset" :size="13" /><span>{{ refreshing ? '刷新中' : '刷新' }}</span></button>
          </div>
        </div>
        <div class="rings" :class="{ stale: !metricsFresh }" :title="metricsFresh ? '来自设备心跳，约 25 秒更新一次' : '暂无最新指标（设备离线或版本过旧）'">
          <Ring :value="svcMem" color="var(--svc)" :text="fmtBytes(m?.svc_footprint)" caption="服务内存" />
          <Ring :value="devMem" color="var(--dev)" :text="devMem ? `${Math.round(devMem * 100)}%` : '—'" caption="整机内存" />
          <Ring :value="(m?.dev_cpu ?? 0) / 100" color="var(--dev)" :text="m?.dev_cpu !== undefined ? `${Math.round(m.dev_cpu)}%` : '—'" caption="整机 CPU" />
        </div>
        <div class="dinfo">
          <div class="kv"><span>型号</span><b :title="device.info?.model">{{ modelName(device.info?.model) || '—' }}</b></div>
          <div class="kv"><span>系统</span><b>{{ device.info?.os || '—' }}</b></div>
          <div class="kv"><span>App 版本</span><b class="num">{{ device.info?.app_version || '—' }}</b></div>
          <div class="kv"><span>服务版本</span><b class="num">{{ device.version || '—' }}</b></div>
          <div class="kv uid"><span>设备标识</span><b class="mono" title="点击复制" @click="copyUID">{{ device.device_uid }}</b></div>
        </div>
      </div>

      <div v-if="upd" class="card">
        <div class="card-title"><h3>版本更新</h3>
          <router-link v-if="upd.latest" :to="`/releases/${upd.latest.id}`" class="hint link">发布记录 ›</router-link>
        </div>
        <div class="upd">
          <b class="ver num">{{ upd.version }}</b>
          <template v-if="upd.latest && compareVersions(upd.version, upd.latest.version) < 0">
            <span class="arrow faint">→</span>
            <b class="ver num">{{ upd.latest.version }}</b>
            <span class="pill p-run">可更新</span>
          </template>
          <span v-else class="pill p-ok">已是最新</span>
          <div class="spacer" />
          <n-popconfirm v-if="canUpdate" @positive-click="updateNow">
            <template #trigger><n-button type="primary" size="small">更新到 {{ upd.latest!.version }}</n-button></template>
            下载、校验并安装 {{ upd.latest!.version }}，期间服务会重启一次；启动异常时自动回滚。确定？
          </n-popconfirm>
          <span v-else-if="!upd.supported && upd.latest && compareVersions(upd.version, upd.latest.version) < 0" class="faint small">此版本不支持在线更新，需手动安装一次</span>
        </div>
        <div v-if="lastUpd" class="ulast">
          <span class="faint small">上次更新</span>
          <span class="small num">{{ lastUpd.version }}</span>
          <span class="pill" :class="updateStatusMeta[lastUpd.status].cls">{{ updateStatusMeta[lastUpd.status].text }}</span>
          <span v-if="lastUpd.error" class="muted small">{{ lastUpd.error }}</span>
          <span class="faint small num">{{ fmtAgo(lastUpd.updated_at) }}</span>
        </div>
      </div>

      <DeviceConfigCard :device-id="id" :online="device.online" />

      <div class="card">
        <div class="card-title"><h3>远程指令</h3></div>
        <div v-if="!commands.length" class="empty">设备没有开放自定义指令</div>
        <div class="cmds">
          <button v-for="c in commands" :key="c.module + c.command" class="cmd" @click="open(c.module, c.command, c.meta)">
            <span class="ic"><Icon :name="c.meta.icon" :size="16" /></span>
            <span class="ctext"><span class="n">{{ c.meta.label }}</span></span>
          </button>
        </div>
      </div>

      <div class="card">
        <div class="card-title"><h3>活动</h3>
          <div class="seg">
            <span :class="{ on: range === '24h' }" @click="range = '24h'">24 小时</span>
            <span :class="{ on: range === '7d' }" @click="range = '7d'">7 天</span>
            <span :class="{ on: range === '30d' }" @click="range = '30d'">30 天</span>
          </div>
          <router-link :to="`/devices/${id}/activity`" class="hint link all">查看全部 ›</router-link>
        </div>
        <div class="astats">
          <router-link :to="`/devices/${id}/activity`" class="ast"><span class="k"><i class="sw inv" />指令</span><b class="num">{{ activity?.invocations ?? '—' }}</b></router-link>
          <router-link :to="`/devices/${id}/activity?tab=inv`" class="ast"><span class="k"><i class="sw bad" />失败</span>
            <b class="num">{{ activity?.failed ?? '—' }}<small v-if="activity?.failed" class="faint pct">{{ failRate.toFixed(1) }}%</small></b></router-link>
          <router-link :to="`/devices/${id}/activity?tab=ev`" class="ast"><span class="k"><i class="sw ev" />事件</span><b class="num">{{ activity?.events ?? '—' }}</b></router-link>
          <router-link :to="`/devices/${id}/activity?tab=ev`" class="ast"><span class="k"><i class="sw warn" />告警</span><b class="num">{{ activity?.warnings ?? '—' }}</b></router-link>
        </div>
        <ActivityChart v-if="activity" :buckets="activity.buckets" :daily="activity.range !== '24h'" />
        <div v-else class="skel" style="height:220px" />
      </div>
      <div class="foot faint num">首次接入 {{ fmtTime(device.first_seen_at) }}
        <template v-if="!device.online"> · <a class="del" @click="confirmDelete">删除设备</a></template></div>
    </template>
  </div>

  <InvokeDrawer v-if="device" v-model:show="drawer" :device-id="id" :module="target.module" :command="target.command" :meta="target.meta" :online="device.online" />
</template>

<style scoped>
.dhero { background: var(--header); border-radius: 22px; padding: 26px 28px 22px; display: grid; grid-template-columns: auto 1fr auto; gap: 20px; align-items: center; }
.dinfo { grid-column: 1 / -1; display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)) minmax(0, 2fr); gap: 10px; align-items: stretch; }
.kv { background: var(--card); border-radius: 14px; padding: 11px 14px; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.kv span { font-size: 11.5px; color: var(--sub); }
.kv b { font-size: 14px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.kv.uid b { font-size: 12.5px; cursor: pointer; }
.refresh { border: none; font: inherit; font-size: 12px; font-weight: 600; color: var(--svc); cursor: pointer; transition: background .15s, color .15s, opacity .15s; }
.refresh:hover:not(:disabled) { background: var(--svc); color: #fff; }
.refresh:disabled { cursor: default; opacity: .55; color: var(--sub); }
.refresh.spin { opacity: 1; color: var(--svc); }
.refresh.spin :deep(svg) { animation: rk-spin .8s linear infinite; }
@keyframes rk-spin { to { transform: rotate(-360deg); } }
.upd { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.ver { font-size: 20px; line-height: 1; }
.arrow { font-size: 16px; line-height: 1; }
.small { font-size: 12px; }
.ulast { display: flex; align-items: center; gap: 8px; margin-top: 12px; padding-top: 12px; border-top: 1px solid var(--sep); flex-wrap: wrap; }
.link { margin-left: auto; color: var(--svc); text-decoration: none; }
.dicon { width: 64px; height: 64px; border-radius: 16px; background: var(--card); display: grid; place-items: center; color: var(--svc); }
.dmain { min-width: 0; }
h1 { font-size: 26px; letter-spacing: -.3px; margin: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.edit { font-size: 12px; color: var(--svc); font-weight: 500; cursor: pointer; }
.chips { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 10px; }
.chip { display: inline-flex; align-items: center; gap: 6px; background: var(--card); border-radius: 99px; padding: 5px 11px; font-size: 12px; font-weight: 500; }
.rings { display: flex; gap: 16px; transition: opacity .3s; }
.rings.stale { opacity: .45; }
.cmds { display: grid; grid-template-columns: repeat(auto-fill, minmax(210px, 1fr)); gap: 10px; }
.cmd {
  border: 1px solid transparent; border-radius: 14px; padding: 12px 14px; background: var(--fill); cursor: pointer;
  display: flex; gap: 12px; align-items: center; text-align: left; font: inherit; color: var(--text); transition: .15s;
}
.cmd:hover { border-color: color-mix(in srgb, var(--svc) 40%, transparent); background: var(--svc-soft); }
.cmd .ic { width: 32px; height: 32px; border-radius: 9px; background: var(--card); color: var(--svc); display: grid; place-items: center; flex: none; }
.ctext { display: flex; flex-direction: column; min-width: 0; }
.n { font-weight: 600; font-size: 14px; }
.seg { margin-left: auto; display: flex; background: var(--fill); border-radius: 9px; padding: 2px; }
.seg span { font-size: 12.5px; padding: 4px 11px; border-radius: 7px; cursor: pointer; color: var(--sub); }
.seg span.on { background: var(--card); color: var(--text); font-weight: 600; box-shadow: 0 1px 3px rgba(0, 0, 0, .08); }
.all { margin-left: 12px; }
.astats { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; margin-bottom: 14px; }
.ast { background: var(--fill); border-radius: 12px; padding: 10px 14px; display: flex; flex-direction: column; gap: 4px; color: var(--text); text-decoration: none; transition: background .15s; }
.ast:hover { background: var(--svc-soft); }
.ast .k { font-size: 12px; color: var(--sub); display: flex; align-items: center; gap: 6px; }
.ast b { font-size: 20px; line-height: 1.1; }
.ast small { font-size: 12px; font-weight: 500; }
.pct { margin-left: 6px; }
.sw { width: 8px; height: 8px; border-radius: 2px; display: inline-block; }
.sw.inv { background: var(--svc); } .sw.ev { background: var(--purple); } .sw.bad { background: var(--red); } .sw.warn { background: var(--orange); }
@media (max-width: 760px) { .astats { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
.err { color: var(--red); font-size: 13px; }
.foot { text-align: center; font-size: 12px; }
.del { color: var(--red); cursor: pointer; }
.del:hover { text-decoration: underline; }
@media (max-width: 900px) {
  .dhero { grid-template-columns: auto 1fr; }
  .dinfo { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .kv.uid { grid-column: 1 / -1; }
  .rings { grid-column: 1 / -1; justify-content: space-around; }
}
</style>
