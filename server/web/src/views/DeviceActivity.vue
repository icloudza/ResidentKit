<script setup lang="ts">
// 设备活动：指令与事件分开分页查看，可按状态 / 级别 / 时间筛选（数据量大时比滚动时间线好查）。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NSelect } from 'naive-ui'
import { api } from '../api/client'
import { onStream } from '../api/stream'
import type { Device, Event, Invocation } from '../api/types'
import PageBar from '../components/PageBar.vue'
import Pager from '../components/Pager.vue'
import NewBanner from '../components/NewBanner.vue'
import JsonView from '../components/JsonView.vue'
import ExpandMark from '../components/ExpandMark.vue'
import { commandMeta } from '../commands'
import { eventMeta, eventSummary, levelMeta } from '../events'
import { deviceTitle, fmtTime, invocationNote, statusMeta, typeBadgeStyle } from '../format'

const route = useRoute()
const router = useRouter()
const id = Number(route.params.id)

type Tab = 'inv' | 'ev'
const tab = computed<Tab>(() => (route.query.tab === 'ev' ? 'ev' : 'inv'))
function setTab(t: Tab) { void router.replace({ query: { ...route.query, tab: t === 'inv' ? undefined : t } }) }

const device = ref<Device | null>(null)
const since = ref('7d')
const status = ref('')
const level = ref('')
const page = ref(1)
const size = ref(50)
const total = ref(0)
const loading = ref(true)
const invs = ref<Invocation[]>([])
const evs = ref<Event[]>([])
const open = ref<number | null>(null)
const newCount = ref(0)

const sinceOptions = [
  { label: '近 1 小时', value: '1h' }, { label: '近 24 小时', value: '24h' }, { label: '近 7 天', value: '7d' },
  { label: '近 30 天', value: '30d' }, { label: '全部时间', value: 'all' },
]
const statusOptions = [
  { label: '全部状态', value: '' }, { label: '成功', value: 'succeeded' }, { label: '失败', value: 'failed' },
  { label: '结果未知', value: 'lost' }, { label: '已过期', value: 'expired' }, { label: '进行中', value: 'pending,sent' },
]
const levelOptions = [
  { label: '全部级别', value: '' }, { label: '仅告警', value: 'warning,error' }, { label: '错误', value: 'error' },
  { label: '警告', value: 'warning' }, { label: '提示', value: 'notice' }, { label: '信息', value: 'info' },
]

async function load() {
  loading.value = true
  try {
    const p = { device_id: id, since: since.value, page: page.value, page_size: size.value }
    if (tab.value === 'inv') {
      const r = await api.invocations({ ...p, status: status.value || undefined })
      invs.value = r.items
      total.value = r.total
    } else {
      const r = await api.events({ ...p, level: level.value || undefined, since: since.value === 'all' ? undefined : since.value })
      evs.value = r.items
      total.value = r.total
    }
    newCount.value = 0
  } finally {
    loading.value = false
  }
}
const list = computed(() => (tab.value === 'inv' ? invs.value : evs.value))
function toFirst() { if (page.value === 1) void load(); else page.value = 1 }

watch(tab, () => { open.value = null; total.value = 0; toFirst() })
watch([since, status, level], toFirst)
watch([page, size], () => { void load(); window.scrollTo({ top: 0 }) })

let off = () => {}
onMounted(async () => {
  void load()
  off = onStream((topic, data) => {
    if (topic === 'invocation.updated' && tab.value === 'inv') {
      const inv = data as Invocation
      if (inv.device_id !== id) return
      const i = invs.value.findIndex((x) => x.id === inv.id)
      if (i >= 0) { invs.value[i] = inv; return }
      if (page.value !== 1 || (status.value && !status.value.split(',').includes(inv.status))) { newCount.value++; return }
      invs.value = [inv, ...invs.value].slice(0, size.value)
      total.value++
    } else if (topic === 'event.created' && tab.value === 'ev') {
      const e = data as Event
      if (e.device_id !== id || (level.value && !level.value.split(',').includes(e.level))) return
      if (page.value !== 1) { newCount.value++; return }
      evs.value = [e, ...evs.value].slice(0, size.value)
      total.value++
    }
  })
  device.value = await api.device(id)
})
onUnmounted(() => off())
</script>

<template>
  <PageBar title="活动" :crumb="device ? deviceTitle(device) : '设备'" :crumb-to="`/devices/${id}`" />
  <div class="content">
    <div class="filters">
      <div class="seg">
        <span :class="{ on: tab === 'inv' }" @click="setTab('inv')">指令</span>
        <span :class="{ on: tab === 'ev' }" @click="setTab('ev')">事件</span>
      </div>
      <span class="spacer" />
      <n-select v-if="tab === 'inv'" v-model:value="status" :options="statusOptions" size="small" style="width:120px" />
      <n-select v-else v-model:value="level" :options="levelOptions" size="small" style="width:120px" />
      <n-select v-model:value="since" :options="sinceOptions" size="small" style="width:120px" />
    </div>
    <NewBanner :count="newCount" @click="toFirst" />

    <div class="card tcard">
      <div class="table-wrap">
        <table v-if="tab === 'inv'" class="table" :class="{ busy: loading && list.length }">
          <thead><tr><th class="xcol" /><th style="width:150px">时间</th><th>指令</th><th style="width:190px">状态</th></tr></thead>
          <tbody>
            <template v-if="loading && !list.length">
              <tr v-for="i in 6" :key="i"><td v-for="j in 4" :key="j"><div class="skel" style="width:70%" /></td></tr>
            </template>
            <template v-for="i in invs" v-else :key="i.id">
              <tr class="clickable" :class="{ opened: open === i.id }" @click="open = open === i.id ? null : i.id">
                <td class="xcol"><ExpandMark can :open="open === i.id" /></td>
                <td class="mono muted num">{{ fmtTime(i.created_at).slice(5) }}</td>
                <td><span class="label">{{ commandMeta(i.module, i.command).label }}</span> <span class="mono faint">{{ i.module }} · {{ i.command }}</span></td>
                <td><span class="pill" :class="statusMeta[i.status]?.cls">{{ statusMeta[i.status]?.text }}<template v-if="invocationNote(i)"> · {{ invocationNote(i) }}</template></span></td>
              </tr>
              <tr v-if="open === i.id" class="exp">
                <td colspan="4">
                  <div class="two">
                    <div><div class="muted cap">参数</div><JsonView :value="i.arguments ?? {}" /></div>
                    <div><div class="muted cap">{{ i.error_message ? '错误' : '结果' }}</div>
                      <div v-if="i.error_message" class="err">{{ i.error_code ? `${i.error_code} · ` : '' }}{{ i.error_message }}</div>
                      <JsonView v-else :value="i.result" /></div>
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
        <table v-else class="table" :class="{ busy: loading && list.length }">
          <thead><tr><th class="xcol" /><th style="width:150px">时间</th><th style="width:64px">级别</th><th style="width:220px">事件</th><th>内容</th></tr></thead>
          <tbody>
            <template v-if="loading && !list.length">
              <tr v-for="i in 6" :key="i"><td v-for="j in 5" :key="j"><div class="skel" style="width:70%" /></td></tr>
            </template>
            <template v-for="e in evs" v-else :key="e.id">
              <tr class="clickable" :class="{ opened: open === e.id }" @click="open = open === e.id ? null : e.id">
                <td class="xcol"><ExpandMark can :open="open === e.id" /></td>
                <td class="mono muted num">{{ fmtTime(e.created_at).slice(5) }}</td>
                <td><span class="pill" :class="levelMeta[e.level]?.cls">{{ levelMeta[e.level]?.text ?? e.level }}</span></td>
                <td>
                  <div v-if="eventMeta(e.type)" class="label">{{ eventMeta(e.type)!.label }}</div>
                  <span class="tbadge" :style="typeBadgeStyle(e.type)">{{ e.type }}</span>
                </td>
                <td :class="{ 'mono muted': !eventMeta(e.type) }">{{ eventSummary(e.type, e.payload) }}</td>
              </tr>
              <tr v-if="open === e.id" class="exp">
                <td colspan="5"><JsonView :value="e.payload ?? {}" /></td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
      <div v-if="!loading && !list.length" class="empty">{{ tab === 'inv' ? '这个范围内没有指令' : '这个范围内没有事件' }}</div>
      <Pager v-model:page="page" v-model:size="size" :total="total" :sizes="[20, 50, 100, 200]" />
    </div>
  </div>
</template>

<style scoped>
.filters { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.spacer { flex: 1; }
.seg { display: flex; background: var(--fill); border-radius: 9px; padding: 2px; }
.seg span { font-size: 13px; padding: 5px 14px; border-radius: 7px; cursor: pointer; color: var(--sub); }
.seg span.on { background: var(--card); color: var(--text); font-weight: 600; box-shadow: 0 1px 3px rgba(0, 0, 0, .08); }
.tcard { padding: 16px 20px; }
.label { font-weight: 600; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.cap { font-size: 12px; margin-bottom: 6px; }
.err { color: var(--red); font-size: 13px; }
@media (max-width: 760px) { .two { grid-template-columns: 1fr; } }
</style>
