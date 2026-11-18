<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { api } from '../api/client'
import { onStream } from '../api/stream'
import type { Device, Invocation } from '../api/types'
import PageBar from '../components/PageBar.vue'
import Pager from '../components/Pager.vue'
import NewBanner from '../components/NewBanner.vue'
import JsonView from '../components/JsonView.vue'
import ExpandMark from '../components/ExpandMark.vue'
import { commandMeta } from '../commands'
import { deviceTitle, fmtTime, invocationNote, statusMeta } from '../format'
import { useRevisit } from '../composables'

const list = ref<Invocation[]>([])
const devices = ref<Device[]>([])
const total = ref(0)
const page = ref(1)
const size = ref(20)
const fresh = ref(0) // 不在第 1 页时到达的新指令数
const loading = ref(true)
const open = ref<number | null>(null)
const names = computed(() => Object.fromEntries(devices.value.map((d) => [d.id, deviceTitle(d)])))

async function load() {
  loading.value = true
  try {
    const p = await api.invocations({ page: page.value, page_size: size.value })
    list.value = p.items
    total.value = p.total
    fresh.value = 0
  } finally {
    loading.value = false
  }
}
function toFirst() { if (page.value === 1) void load(); else page.value = 1 }
watch([page, size], () => { void load(); window.scrollTo({ top: 0 }) })

let off = () => {}
onMounted(async () => {
  void load()
  devices.value = await api.devices()
  off = onStream((topic, data) => {
    if (topic !== 'invocation.updated') return
    const inv = data as Invocation
    const i = list.value.findIndex((x) => x.id === inv.id)
    if (i >= 0) list.value[i] = inv
    else if (page.value === 1) { list.value = [inv, ...list.value].slice(0, size.value); total.value++ }
    else fresh.value++
  })
})
onUnmounted(() => off())
// 从别的页面回来时静默刷新（页面保活，不重建）。
useRevisit(() => void load())
</script>

<template>
  <PageBar title="指令记录" />
  <div class="content">
    <NewBanner :count="fresh" @click="toFirst" />
    <div class="card tcard">
      <div class="table-wrap">
        <table class="table" :class="{ busy: loading && list.length }">
          <thead><tr><th class="xcol" /><th style="width:150px">时间</th><th style="width:130px">设备</th><th>指令</th><th style="width:170px">状态</th></tr></thead>
          <tbody>
            <template v-if="loading && !list.length">
              <tr v-for="i in 5" :key="i"><td v-for="j in 5" :key="j"><div class="skel" style="width:70%" /></td></tr>
            </template>
            <template v-for="i in list" v-else :key="i.id">
              <tr class="clickable" :class="{ opened: open === i.id }" @click="open = open === i.id ? null : i.id">
                <td class="xcol"><ExpandMark can :open="open === i.id" /></td>
                <td class="mono muted num">{{ fmtTime(i.created_at).slice(5) }}</td>
                <td>{{ names[i.device_id] ?? `#${i.device_id}` }}</td>
                <td><span class="label">{{ commandMeta(i.module, i.command).label }}</span> <span class="mono faint">{{ i.module }} · {{ i.command }}</span></td>
                <td><span class="pill" :class="statusMeta[i.status]?.cls">{{ statusMeta[i.status]?.text }}<template v-if="invocationNote(i)"> · {{ invocationNote(i) }}</template></span></td>
              </tr>
              <tr v-if="open === i.id" class="exp">
                <td colspan="5">
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
      </div>
      <div v-if="!loading && !list.length" class="empty">还没有下发过指令。在设备详情页点击指令卡片即可下发。</div>
      <Pager v-model:page="page" v-model:size="size" :total="total" />
    </div>
  </div>
</template>

<style scoped>
.tcard { padding: 16px 20px; }
.label { font-weight: 600; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.cap { font-size: 12px; margin-bottom: 6px; }
.err { color: var(--red); font-size: 13px; }
@media (max-width: 760px) { .two { grid-template-columns: 1fr; } }
</style>
