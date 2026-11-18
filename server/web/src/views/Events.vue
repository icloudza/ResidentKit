<script setup lang="ts">
import { computed, h, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NSelect, type SelectOption } from 'naive-ui'
import { api } from '../api/client'
import { onStream } from '../api/stream'
import type { Device, Event } from '../api/types'
import PageBar from '../components/PageBar.vue'
import Pager from '../components/Pager.vue'
import NewBanner from '../components/NewBanner.vue'
import JsonView from '../components/JsonView.vue'
import ExpandMark from '../components/ExpandMark.vue'
import { deviceTitle, fmtTime, typeBadgeStyle, typeColor } from '../format'
import { eventMeta, eventSummary, levelMeta } from '../events'
import { useRevisit } from '../composables'

const route = useRoute()
const router = useRouter()

const type = ref(typeof route.query.type === 'string' ? route.query.type : '')
const deviceId = ref<number | null>(null)
const since = ref('24h')
const level = ref(typeof route.query.level === 'string' ? route.query.level : '')
const types = ref<{ type: string; count: number }[]>([])
const devices = ref<Device[]>([])
const list = ref<Event[]>([])
const total = ref(0)
const page = ref(1)
const size = ref(50)
const newCount = ref(0) // 不在第 1 页时到达的新事件数
const loading = ref(true)
const fresh = ref(new Set<number>())
const open = ref<number | null>(null)

const names = computed(() => Object.fromEntries(devices.value.map((d) => [d.id, deviceTitle(d)])))
const deviceOptions = computed(() => [{ label: '全部设备', value: 0 }, ...devices.value.map((d) => ({ label: deviceTitle(d), value: d.id }))])
const sinceOptions = [
  { label: '近 1 小时', value: '1h' }, { label: '近 24 小时', value: '24h' },
  { label: '近 7 天', value: '7d' }, { label: '近 30 天', value: '30d' }, { label: '全部时间', value: 'all' },
]

async function load() {
  loading.value = true
  try {
    const p = await api.events({
      type: type.value || undefined, device_id: deviceId.value || undefined, level: level.value || undefined,
      since: since.value === 'all' ? undefined : since.value, page: page.value, page_size: size.value,
    })
    list.value = p.items
    total.value = p.total
    newCount.value = 0
  } finally {
    loading.value = false
  }
}
function toFirst() { if (page.value === 1) void load(); else page.value = 1 }
async function loadTypes() {
  types.value = await api.eventTypes(since.value === 'all' ? '30d' : since.value)
}

function pickType(t: string | null) {
  type.value = t ?? ''
  syncQuery()
}
// 类型与级别写进地址栏（可分享、刷新保留），与下方的 watch 保持一致。
function syncQuery() {
  const q: Record<string, string> = {}
  if (type.value) q.type = type.value
  if (level.value) q.level = level.value
  if (q.type !== route.query.type || q.level !== route.query.level) void router.replace({ query: q })
}

// 类型下拉：中文名 + 类型 ID + 数量，可按任一搜索。当前选中但不在统计里的类型（如时间范围外）也保留为选项。
const typeOptions = computed<SelectOption[]>(() => {
  const opts: SelectOption[] = types.value.map((t) => ({ label: eventMeta(t.type)?.label ?? t.type, value: t.type, count: t.count }))
  if (type.value && !opts.some((o) => o.value === type.value))
    opts.unshift({ label: eventMeta(type.value)?.label ?? type.value, value: type.value, count: 0 })
  return opts
})
const renderTypeLabel = (o: SelectOption) => h('span', { class: 'topt' }, [
  h('i', { style: { background: typeColor(String(o.value)) } }),
  h('span', { class: 'tl' }, String(o.label)),
  o.label !== o.value ? h('span', { class: 'tid' }, String(o.value)) : null,
  h('span', { class: 'cnt' }, String(o.count ?? '')),
])
const filterType = (q: string, o: SelectOption) =>
  String(o.label).includes(q) || String(o.value).toLowerCase().includes(q.toLowerCase())

const levelOptions = [
  { label: '全部级别', value: '' }, { label: '仅告警', value: 'warning,error' },
  { label: '错误', value: 'error' }, { label: '警告', value: 'warning' }, { label: '提示', value: 'notice' }, { label: '信息', value: 'info' },
]

// 页面保活：从总览等处带着 ?type= / ?level= 进入时同步到筛选（只在本页可见时）。
watch(() => [route.path, route.query.type, route.query.level], () => {
  if (route.path !== '/events') return
  // 从侧边栏进入（不带筛选）：保留上次的筛选，并写回地址栏。
  if (route.query.type === undefined && route.query.level === undefined) { syncQuery(); return }
  const t = typeof route.query.type === 'string' ? route.query.type : ''
  const l = typeof route.query.level === 'string' ? route.query.level : ''
  if (t !== type.value) type.value = t
  if (l !== level.value) level.value = l
})

watch(level, syncQuery)

// 筛选变化回到第 1 页；翻页只重新加载。
watch([type, deviceId, level], () => { if (page.value !== 1) page.value = 1; else void load() })
watch(since, () => { void loadTypes(); if (page.value !== 1) page.value = 1; else void load() })
watch([page, size], () => { void load(); window.scrollTo({ top: 0 }) })

let off = () => {}
onMounted(async () => {
  void load()
  void loadTypes()
  devices.value = await api.devices()
  off = onStream((topic, data) => {
    if (topic !== 'event.created') return
    const e = data as Event
    if (type.value && e.type !== type.value) return
    if (deviceId.value && e.device_id !== deviceId.value) return
    if (level.value && !level.value.split(',').includes(e.level)) return
    if (page.value !== 1) { newCount.value++; return }
    list.value = [e, ...list.value].slice(0, size.value)
    total.value++
    fresh.value = new Set(fresh.value).add(e.id)
    const t = types.value.find((x) => x.type === e.type)
    if (t) t.count++
  })
})
onUnmounted(() => off())
// 从别的页面回来时静默刷新（页面保活，不重建）。
useRevisit(() => { void load(); void loadTypes() })
</script>

<template>
  <PageBar title="事件" />
  <div class="content">
    <div class="filters">
      <n-select :value="type || null" :options="typeOptions" :render-label="renderTypeLabel" :filter="filterType"
        filterable clearable placeholder="全部类型" size="small" style="width:260px" :consistent-menu-width="false"
        :menu-props="{ class: 'type-menu', style: 'min-width:260px;max-width:min(420px, calc(100vw - 32px))' }" @update:value="pickType" />
      <span class="spacer" />
      <n-select :value="deviceId ?? 0" :options="deviceOptions" size="small" style="width:150px"
        @update:value="(v: number) => (deviceId = v || null)" />
      <n-select v-model:value="level" :options="levelOptions" size="small" style="width:110px" />
      <n-select v-model:value="since" :options="sinceOptions" size="small" style="width:120px" />
    </div>
    <NewBanner :count="newCount" @click="toFirst" />

    <div class="card tcard">
      <div class="table-wrap">
        <table class="table" :class="{ busy: loading && list.length }">
          <thead><tr><th class="xcol" /><th style="width:150px">时间</th><th style="width:130px">设备</th><th style="width:64px">级别</th><th style="width:220px">事件</th><th>内容</th></tr></thead>
          <tbody>
            <template v-if="loading && !list.length">
              <tr v-for="i in 6" :key="i"><td class="xcol" /><td><div class="skel" style="width:80%" /></td><td><div class="skel" style="width:60%" /></td>
                <td><div class="skel" style="width:70%" /></td><td><div class="skel" style="width:70%" /></td><td><div class="skel" style="width:90%" /></td></tr>
            </template>
            <template v-else>
              <template v-for="e in list" :key="e.id">
                <tr class="clickable" :class="{ flash: fresh.has(e.id), opened: open === e.id }" @click="open = open === e.id ? null : e.id">
                  <td class="xcol"><ExpandMark can :open="open === e.id" /></td>
                  <td class="mono muted num">{{ fmtTime(e.created_at).slice(5) }}</td>
                  <td>{{ names[e.device_id] ?? `#${e.device_id}` }}</td>
                  <td><span class="pill" :class="levelMeta[e.level]?.cls">{{ levelMeta[e.level]?.text ?? e.level }}</span></td>
                  <td>
                    <div v-if="eventMeta(e.type)" class="elabel">{{ eventMeta(e.type)!.label }}</div>
                    <span class="tbadge" :style="typeBadgeStyle(e.type)">{{ e.type }}</span>
                  </td>
                  <td class="pv" :class="{ 'mono muted': !eventMeta(e.type) }">{{ eventSummary(e.type, e.payload) }}</td>
                </tr>
                <tr v-if="open === e.id" class="exp">
                  <td colspan="6">
                    <JsonView :value="e.payload ?? {}" />
                    <div class="faint mono small">消息 ID {{ e.msg_id }}<template v-if="e.device_ts"> · 设备时间 {{ fmtTime(e.device_ts) }}</template></div>
                  </td>
                </tr>
              </template>
            </template>
          </tbody>
        </table>
      </div>
      <div v-if="!loading && !list.length" class="empty">这个范围内没有事件</div>
      <Pager v-model:page="page" v-model:size="size" :total="total" :sizes="[20, 50, 100, 200]" />
    </div>
  </div>
</template>

<style scoped>
.elabel { font-weight: 600; font-size: 13px; margin-bottom: 3px; }
.filters { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
/* 下拉选项渲染在弹层里（不在本组件根节点内），用 :global 才能命中 */
:global(.type-menu .n-base-select-option__content) { flex: 1; min-width: 0; }
:global(.topt) { display: inline-flex; align-items: center; gap: 7px; width: 100%; }
:global(.topt i) { width: 7px; height: 7px; border-radius: 50%; flex: none; }
:global(.topt .tl) { flex: none; }
:global(.topt .tid) { font-family: ui-monospace, "SF Mono", Menlo, monospace; font-size: 11px; color: var(--faint);
  min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
:global(.topt .cnt) { flex: none; margin-left: auto; padding-left: 10px; font-size: 11px; color: var(--sub); font-variant-numeric: tabular-nums; }
.tcard { padding: 16px 20px; }
.pv { max-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.small { font-size: 11.5px; margin-top: 6px; }
@media (max-width: 760px) { .filters .spacer { display: none; } }
</style>
