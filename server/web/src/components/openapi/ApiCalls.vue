<script setup lang="ts">
// 开放 API · 调用记录：按密钥、设备、状态、时间筛选，实时更新，展开看请求与结果。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { commandMeta } from '../../commands'
import { NSelect } from 'naive-ui'
import { api } from '../../api/client'
import { onStream } from '../../api/stream'
import type { APIKey, Device, Invocation } from '../../api/types'
import Pager from '../Pager.vue'
import JsonView from '../JsonView.vue'
import ExpandMark from '../ExpandMark.vue'
import { deviceTitle, fmtNum, fmtTime, statusMeta } from '../../format'

const props = defineProps<{ keys: APIKey[]; devices: Device[] }>()
const keyId = defineModel<number | null>('keyId', { default: null })

const deviceId = ref<number | null>(null)
const status = ref('')
const since = ref('24h')
const list = ref<Invocation[]>([])
const total = ref(0)
const page = ref(1)
const size = ref(50)
const loading = ref(true)
const open = ref<number | null>(null)
let seq = 0   // 只采用最后一次请求的结果，避免筛选快速切换时旧响应覆盖新结果

const keyName = computed(() => Object.fromEntries(props.keys.map((k) => [k.id, k.name])))
const deviceName = computed(() => Object.fromEntries(props.devices.map((d) => [d.id, deviceTitle(d)])))
const keyOptions = computed(() => [{ label: '全部密钥', value: 0 }, ...props.keys.map((k) => ({ label: k.name, value: k.id }))])
const deviceOptions = computed(() => [{ label: '全部设备', value: 0 }, ...props.devices.map((d) => ({ label: deviceTitle(d), value: d.id }))])
const statusOptions = [
  { label: '全部状态', value: '' }, { label: '成功', value: 'succeeded' }, { label: '失败', value: 'failed' },
  { label: '进行中', value: 'pending,sent' }, { label: '结果未知 / 过期', value: 'lost,expired' },
]
const sinceOptions = [{ label: '近 1 小时', value: '1h' }, { label: '近 24 小时', value: '24h' }, { label: '近 7 天', value: '7d' }, { label: '近 30 天', value: '30d' }]

async function load() {
  const my = ++seq
  loading.value = true
  try {
    const p = await api.apiCalls({ key_id: keyId.value || undefined, device_id: deviceId.value || undefined,
      status: status.value || undefined, since: since.value, page: page.value, page_size: size.value })
    if (my !== seq) return
    list.value = p.items
    total.value = p.total
  } finally {
    if (my === seq) loading.value = false
  }
}
watch([keyId, deviceId, status, since], () => { open.value = null; if (page.value !== 1) page.value = 1; else void load() })
watch([page, size], () => void load())

function duration(i: Invocation): string {
  if (!i.sent_at || !i.finished_at) return '—'
  return `${Math.max(1, new Date(i.finished_at).getTime() - new Date(i.sent_at).getTime())} ms`
}
function matches(i: Invocation): boolean {
  if (!i.api_key_id) return false
  if (keyId.value && i.api_key_id !== keyId.value) return false
  if (deviceId.value && i.device_id !== deviceId.value) return false
  return !status.value || status.value.split(',').includes(i.status)
}

let off = () => {}
onMounted(() => {
  void load()
  // 实时：第 1 页时插入新调用、更新状态；其他页只更新已显示的行。
  off = onStream((topic, data) => {
    if (topic !== 'invocation.updated') return
    const inv = data as Invocation
    const idx = list.value.findIndex((x) => x.id === inv.id)
    if (idx >= 0) { list.value[idx] = { ...list.value[idx], ...inv }; return }
    if (page.value === 1 && matches(inv) && (!list.value.length || inv.id > list.value[0].id)) {
      list.value = [inv, ...list.value].slice(0, size.value)
      total.value++
    }
  })
})
onUnmounted(() => off())
</script>

<template>
  <div class="card tcard">
    <div class="filters">
      <n-select :value="keyId ?? 0" :options="keyOptions" size="small" style="width:150px" @update:value="(v: number) => (keyId = v || null)" />
      <n-select :value="deviceId ?? 0" :options="deviceOptions" size="small" style="width:150px" @update:value="(v: number) => (deviceId = v || null)" />
      <n-select v-model:value="status" :options="statusOptions" size="small" style="width:140px" />
      <n-select v-model:value="since" :options="sinceOptions" size="small" style="width:120px" />
      <div class="spacer" />
      <span class="small muted num">共 {{ fmtNum(total) }} 条 · 实时更新</span>
    </div>
    <div class="table-wrap">
      <table class="table">
        <thead><tr><th class="xcol" /><th style="width:150px">时间</th><th>密钥</th><th>设备</th><th>命令</th><th style="width:90px">状态</th><th style="width:80px">耗时</th><th>幂等键</th></tr></thead>
        <tbody>
          <template v-if="loading && !list.length"><tr v-for="i in 5" :key="i"><td v-for="j in 8" :key="j"><div class="skel" style="width:70%" /></td></tr></template>
          <template v-for="i in list" v-else :key="i.id">
            <tr class="clickable" :class="{ opened: open === i.id }" @click="open = open === i.id ? null : i.id">
              <td class="xcol"><ExpandMark can :open="open === i.id" /></td>
              <td class="mono muted num">{{ fmtTime(i.created_at).slice(5) }}</td>
              <td class="nowrap">{{ keyName[i.api_key_id!] ?? `#${i.api_key_id}` }}</td>
              <td class="nowrap">{{ deviceName[i.device_id] ?? `#${i.device_id}` }}</td>
              <td class="nowrap">{{ commandMeta(i.module, i.command).label }} <span class="mono faint">{{ i.module }} · {{ i.command }}</span></td>
              <td><span class="pill" :class="statusMeta[i.status]?.cls">{{ statusMeta[i.status]?.text ?? i.status }}</span></td>
              <td class="mono muted num">{{ duration(i) }}</td>
              <td class="mono faint idem">{{ i.idempotency_key || '—' }}</td>
            </tr>
            <tr v-if="open === i.id" class="exp">
              <td colspan="8">
                <div class="detail">
                  <div class="dcol">
                    <div class="lbl">请求</div>
                    <div class="small muted reqline"><span class="mono">POST /open/v1/devices/{{ i.device_id }}/commands/{{ i.module }}/{{ i.command }}</span>
                      <span v-if="i.source_ip">来源 <span class="mono">{{ i.source_ip }}</span></span>
                      <span v-if="i.idempotency_key">幂等键 <span class="mono">{{ i.idempotency_key }}</span></span></div>
                    <JsonView :value="i.arguments ?? {}" />
                  </div>
                  <div class="dcol">
                    <div class="lbl">结果
                      <span class="faint num">· 创建 {{ fmtTime(i.created_at).slice(11) }}{{ i.sent_at ? ` · 下发 ${fmtTime(i.sent_at).slice(11)}` : '' }}{{ i.finished_at ? ` · 完成 ${fmtTime(i.finished_at).slice(11)}` : '' }}</span></div>
                    <div v-if="i.error_message" class="err">{{ i.error_code ? `${i.error_code} · ` : '' }}{{ i.error_message }}</div>
                    <JsonView v-else-if="i.result" :value="i.result" />
                    <div v-else class="faint small">{{ i.status === 'pending' ? '排队中，等待下发' : i.status === 'sent' ? '已下发，等待设备返回' : '无' }}</div>
                  </div>
                </div>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
    <div v-if="!loading && !list.length" class="empty">该时间范围内没有调用</div>
    <Pager v-model:page="page" v-model:size="size" :total="total" :sizes="[20, 50, 100]" />
  </div>
</template>

<style scoped>
.tcard { padding: 16px 20px; }
.filters { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; margin-bottom: 8px; }
.small { font-size: 12px; }
.nowrap { white-space: nowrap; }
.idem { max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.detail { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 16px; padding: 4px 0 6px; }
.dcol { min-width: 0; }
.lbl { font-size: 11.5px; color: var(--sub); font-weight: 600; margin-bottom: 6px; }
.reqline { display: flex; gap: 12px; flex-wrap: wrap; margin-bottom: 6px; }
.err { color: var(--red); font-size: 13px; }
@media (max-width: 900px) { .detail { grid-template-columns: 1fr; } }
</style>
