<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { commandMeta } from '../commands'
import { useRouter } from 'vue-router'
import { useMessage } from 'naive-ui'
import { api } from '../api/client'
import { onStream } from '../api/stream'
import type { Device, Event, Invocation, Summary } from '../api/types'
import PageBar from '../components/PageBar.vue'
import StatCard from '../components/StatCard.vue'
import HourlyChart from '../components/HourlyChart.vue'
import { deviceTitle, fmtAgo, fmtNum, fmtSpan, invocationNote, networkName, statusMeta, typeColor } from '../format'
import { eventMeta, eventSummary, levelMeta } from '../events'
import { useRevisit } from '../composables'

const router = useRouter()
const message = useMessage()
const summary = ref<Summary | null>(null)
const devices = ref<Device[]>([])
const recent = ref<Invocation[]>([])

async function load() {
  const [s, d, inv] = await Promise.all([api.summary(), api.devices(), api.invocations({ page_size: 4 })])
  summary.value = s
  devices.value = d
  recent.value = inv.items
}

const names = computed(() => Object.fromEntries(devices.value.map((d) => [d.id, deviceTitle(d)])))
const s = computed(() => summary.value)

// 头图卡的整体状态。
const health = computed(() => {
  const v = s.value
  if (!v) return { text: '加载中', ok: true }
  if (!v.devices_total) return { text: '等待设备接入', ok: true }
  const off = v.devices_total - v.devices_online
  return off ? { text: `${off} 台设备离线`, ok: false } : { text: '系统运行正常', ok: true }
})

const eventsDelta = computed(() => {
  const v = s.value
  if (!v || !v.events_prev_24h) return null
  const pct = Math.round(((v.events_24h - v.events_prev_24h) / v.events_prev_24h) * 100)
  return { text: `${pct >= 0 ? '↑' : '↓'} ${Math.abs(pct)}%`, cls: pct >= 0 ? 'up' : 'down' }
})
const successRate = computed(() => {
  const v = s.value
  return v && v.invocations_done_24h ? `${((v.invocations_ok_24h / v.invocations_done_24h) * 100).toFixed(1)}%` : '—'
})
const denies = computed(() => (s.value?.gateway.deny_hourly ?? []).reduce((a, b) => a + b, 0))
const typeMax = computed(() => Math.max(1, ...(s.value?.event_types ?? []).map((t) => t.count)))
const sortedDevices = computed(() => [...devices.value].sort((a, b) => Number(b.online) - Number(a.online)))

const onlineFor = (d: Device) => (d.connected_at ? fmtSpan((Date.now() - new Date(d.connected_at).getTime()) / 1000) : '')

// 实时事件很密集（每台设备约 25 秒一次心跳），合并成最多 3 秒刷新一次。
let timer: ReturnType<typeof setTimeout> | undefined
const reload = () => { timer ??= setTimeout(() => { timer = undefined; void load() }, 3000) }

let lastToast = 0
let off = () => {}
onMounted(() => {
  void load()
  off = onStream((topic, data) => {
    if (topic === 'event.created') {
      const e = data as Event
      if (summary.value) summary.value.events_24h++
      if (Date.now() - lastToast > 3000) {
        lastToast = Date.now()
        message.info(`新事件 ${e.type} · ${names.value[e.device_id] ?? `#${e.device_id}`}`, { duration: 2200 })
      }
    } else if (topic.startsWith('device.') || topic === 'invocation.updated') {
      reload()
    }
  })
})
onUnmounted(() => { off(); clearTimeout(timer) })
// 从别的页面回来时静默刷新（页面保活，不重建）。
useRevisit(() => void load())
</script>

<template>
  <PageBar title="总览" />
  <div class="content">
    <div class="hero">
      <div class="hero-main">
        <span class="okpill"><span class="dot" :class="health.ok ? 'on' : 'off'" :style="health.ok ? '' : 'background:var(--orange)'" />{{ health.text }}</span>
        <div class="big">
          <template v-if="s">{{ s.devices_online }} 台设备在线</template>
          <div v-else class="skel" style="width:200px;height:30px" />
        </div>
        <div class="meta" v-if="s">
          服务已运行 {{ fmtSpan(s.uptime_seconds) }} · rk-server {{ s.server_version }}
          <template v-if="s.avg_rtt_ms"> · 平均延迟 {{ s.avg_rtt_ms }} ms</template>
        </div>
      </div>
      <div class="kv" v-if="s">
        <div><b class="num">{{ s.devices_total ? Math.round((s.devices_online / s.devices_total) * 100) : 0 }}%</b><span>在线率</span></div>
        <div><b class="num">{{ s.invocations_pending }}</b><span>待处理指令</span></div>
        <div><b class="num">{{ s.gateway.banned_ips }}</b><span>封禁 IP</span></div>
      </div>
    </div>

    <div class="grid grid-4">
      <StatCard label="24 小时事件" icon="pulse" color="var(--svc)" :loading="!s"
        :value="fmtNum(s?.events_24h ?? 0)" :note="eventsDelta?.text" :note-class="eventsDelta?.cls"
        :trend="s?.events_hourly.map((h) => h.count)" />
      <StatCard label="指令成功率" icon="terminal" color="var(--green)" :loading="!s"
        :value="successRate" :note="s?.invocations_done_24h ? `${s.invocations_ok_24h} / ${s.invocations_done_24h}` : '暂无'"
        :trend="s?.invocations_hourly" />
      <StatCard label="平均延迟" icon="clock" color="var(--orange)" :loading="!s"
        :value="s?.avg_rtt_ms ? String(s.avg_rtt_ms) : '—'" :note="s?.avg_rtt_ms ? 'ms' : ''"
        :trend="s?.gateway.rtt_hourly_ms" />
      <StatCard label="鉴权失败（24h）" icon="shield" color="var(--purple)" :loading="!s"
        :value="String(denies)" note="次" :trend="s?.gateway.deny_hourly" />
    </div>

    <div class="grid grid-21">
      <div class="card chart-card">
        <div class="card-title"><h3>事件量</h3><span class="hint">近 24 小时</span></div>
        <HourlyChart v-if="s" :data="s.events_hourly" fill />
        <div v-else class="skel" style="height:200px" />
      </div>
      <div class="card">
        <div class="card-title"><h3>最近告警</h3>
          <router-link :to="{ path: '/events', query: { level: 'warning,error' } }" class="more">
            24 小时 {{ s?.alerts_24h ?? 0 }} 条 ›</router-link></div>
        <div v-if="s && !s.recent_alerts?.length" class="empty ok"><span class="dot on" /> 24 小时内没有告警</div>
        <div v-for="e in s?.recent_alerts ?? []" :key="e.id" class="li">
          <span class="pill" :class="levelMeta[e.level]?.cls">{{ levelMeta[e.level]?.text }}</span>
          <div class="grow">
            <div class="t1">{{ eventMeta(e.type)?.label ?? e.type }}</div>
            <div class="t2">{{ names[e.device_id] ?? `#${e.device_id}` }} · {{ fmtAgo(e.created_at) }} · {{ eventSummary(e.type, e.payload) }}</div>
          </div>
        </div>
      </div>
    </div>

    <div class="grid grid-3">
      <div class="card">
        <div class="card-title"><h3>事件类型</h3><span class="hint">24 小时</span></div>
        <div v-if="s && !s.event_types?.length" class="empty">暂无事件</div>
        <div class="dist">
          <router-link v-for="t in s?.event_types ?? []" :key="t.type" class="drow" :to="{ path: '/events', query: { type: t.type } }">
            <span class="tname" :title="t.type">{{ eventMeta(t.type)?.label ?? t.type }}</span>
            <span class="track"><i :style="{ width: `${(t.count / typeMax) * 100}%`, background: typeColor(t.type) }" /></span>
            <span class="num muted">{{ fmtNum(t.count) }}</span>
          </router-link>
        </div>
      </div>
      <div class="card">
        <div class="card-title"><h3>设备</h3><router-link to="/devices" class="more">全部 ›</router-link></div>
        <div v-if="!devices.length" class="empty">还没有设备接入。点「设备」页右上角「添加设备」生成二维码，在手机 App 的「服务器连接」里扫码即可接入。</div>
        <div v-for="d in sortedDevices.slice(0, 5)" :key="d.id" class="li clickable"
          @click="router.push({ name: 'device', params: { id: d.id } })">
          <span class="dot" :class="d.online ? 'on' : 'off'" />
          <div class="grow">
            <div class="t1">{{ deviceTitle(d) }}</div>
            <div class="t2">v{{ d.version }} · {{ d.online ? `${networkName[d.network] || '—'} · 在线 ${onlineFor(d)}` : `${fmtAgo(d.last_seen_at)}在线` }}</div>
          </div>
          <div v-if="d.online" class="rtt num"><span class="track"><i :style="{ width: `${Math.min(100, (d.rtt_ms || 0) / 3)}%`, background: d.rtt_ms > 200 ? 'var(--orange)' : 'var(--green)' }" /></span>{{ d.rtt_ms || '—' }} ms</div>
          <span v-else class="pill p-wait">离线</span>
        </div>
      </div>
      <div class="card">
        <div class="card-title"><h3>最近指令</h3><router-link to="/invocations" class="more">全部 ›</router-link></div>
        <div v-if="!recent.length" class="empty">还没有下发过指令</div>
        <div v-for="i in recent" :key="i.id" class="li">
          <div class="grow">
            <div class="t1">{{ commandMeta(i.module, i.command).label }}</div>
            <div class="t2">{{ names[i.device_id] ?? `#${i.device_id}` }} · {{ fmtAgo(i.created_at) }}</div>
          </div>
          <span class="pill" :class="statusMeta[i.status]?.cls">{{ statusMeta[i.status]?.text }}<template v-if="invocationNote(i)"> · {{ invocationNote(i) }}</template></span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.chart-card { display: flex; flex-direction: column; }
.hero { background: var(--header); border-radius: 22px; padding: 22px 24px; display: flex; align-items: center; gap: 22px; }
.hero-main { min-width: 0; }
.okpill { display: inline-flex; align-items: center; gap: 6px; background: var(--card); border-radius: 99px; padding: 4px 10px; font-size: 12px; font-weight: 600; }
.big { font-size: 28px; font-weight: 700; letter-spacing: -.4px; margin-top: 8px; }
.meta { font-size: 13px; color: var(--sub); margin-top: 6px; }
.kv { display: flex; gap: 28px; margin-left: auto; }
.kv div { display: flex; flex-direction: column; gap: 2px; }
.kv b { font-size: 22px; }
.kv span { font-size: 12px; color: var(--sub); }
.dist { display: flex; flex-direction: column; gap: 12px; }
.drow { display: grid; grid-template-columns: minmax(0, 110px) 1fr 40px; align-items: center; gap: 10px; font-size: 13px; color: inherit; text-decoration: none; }
.drow:hover .tname { color: var(--svc); }
.tname { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
.empty.ok { display: flex; align-items: center; justify-content: center; gap: 8px; }
.drow .num { text-align: right; }
.track { display: block; height: 8px; border-radius: 4px; background: var(--fill); overflow: hidden; }
.track i { display: block; height: 100%; border-radius: 4px; }
.rtt { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--sub); }
.rtt .track { width: 56px; height: 5px; }
@media (max-width: 760px) {
  .hero { flex-direction: column; align-items: flex-start; }
  .kv { margin-left: 0; }
  .big { font-size: 24px; }
}
</style>
