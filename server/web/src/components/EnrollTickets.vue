<script setup lang="ts">
// 接入码列表：「添加设备」生成的二维码，可查看使用情况、提前吊销。
import { onMounted, ref } from 'vue'
import { NButton, NPopconfirm, useMessage } from 'naive-ui'
import { api, ApiError } from '../api/client'
import type { EnrollTicket } from '../api/types'
import Icon from './Icon.vue'
import AddDevice from './AddDevice.vue'
import { fmtAgo, fmtTime } from '../format'

const message = useMessage()
const list = ref<EnrollTicket[]>([])
const loading = ref(true)
const adding = ref(false)

async function load() {
  try { list.value = (await api.enrollTickets()).tickets } finally { loading.value = false }
}
defineExpose({ load })
onMounted(load)

const statusMeta: Record<EnrollTicket['status'], { text: string; cls: string }> = {
  active: { text: '可用', cls: 'p-ok' },
  expired: { text: '已过期', cls: 'p-wait' },
  exhausted: { text: '已用完', cls: 'p-wait' },
  revoked: { text: '已吊销', cls: 'p-wait' },
}
async function revoke(id: number) {
  try { await api.revokeEnrollTicket(id); message.success('已吊销'); await load() } catch (e) { message.error(e instanceof ApiError ? e.message : '吊销失败') }
}
</script>

<template>
  <div class="card tcard">
    <div class="card-title">
      <h3><Icon name="key" :size="16" />接入码</h3>
      <div class="spacer" />
      <n-button size="small" @click="adding = true">添加设备</n-button>
    </div>
    <p class="muted desc">「添加设备」生成的二维码。设备扫码后直接接入，不用再点「同意」；过期或用完自动失效。</p>
    <div class="table-wrap">
      <table class="table">
        <thead><tr><th>备注</th><th style="width:90px">状态</th><th style="width:100px">已接入</th><th style="width:140px">有效期至</th><th style="width:120px">最近使用</th><th style="width:70px" /></tr></thead>
        <tbody>
          <tr v-for="t in list" :key="t.id" :class="{ dim: t.status !== 'active' }">
            <td><span class="label">{{ t.name }}</span> <span class="mono faint">{{ t.token_prefix }}…</span></td>
            <td><span class="pill" :class="statusMeta[t.status].cls">{{ statusMeta[t.status].text }}</span></td>
            <td class="num">{{ t.uses }} / {{ t.max_uses || '不限' }}</td>
            <td class="mono muted num">{{ fmtTime(t.expires_at).slice(0, 16) }}</td>
            <td class="muted">{{ t.last_used_at ? fmtAgo(t.last_used_at) : '—' }}</td>
            <td>
              <n-popconfirm v-if="t.status === 'active'" @positive-click="revoke(t.id)">
                <template #trigger><n-button size="small" type="error" quaternary>吊销</n-button></template>
                吊销后二维码立即失效，已接入的设备不受影响，确定？
              </n-popconfirm>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="!loading && !list.length" class="empty">还没有生成过接入码</div>
    <AddDevice v-model:show="adding" @created="load" />
  </div>
</template>

<style scoped>
.tcard { padding: 16px 20px; }
.card-title { display: flex; align-items: center; gap: 8px; }
.card-title h3 { display: flex; align-items: center; gap: 6px; margin: 0; }
.spacer { flex: 1; }
.desc { font-size: 13px; line-height: 1.7; margin: 8px 0 12px; }
.label { font-weight: 600; }
tr.dim td { opacity: .55; }
</style>
