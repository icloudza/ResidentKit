<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NButton, NPopconfirm, useMessage } from 'naive-ui'
import { api, ApiError } from '../api/client'
import type { Device, DeviceToken } from '../api/types'
import PageBar from '../components/PageBar.vue'
import Pager from '../components/Pager.vue'
import EnrollTickets from '../components/EnrollTickets.vue'
import { deviceTitle, fmtAgo, fmtTime } from '../format'
import { useRevisit } from '../composables'

const message = useMessage()
const tokens = ref<DeviceToken[]>([])
const loading = ref(true)
const tickets = ref<InstanceType<typeof EnrollTickets> | null>(null)
// 令牌数量少（最多 500），在前端分页。
const page = ref(1)
const size = ref(20)
const shown = computed(() => tokens.value.slice((page.value - 1) * size.value, page.value * size.value))

const deviceName = ref<Record<number, string>>({})
async function load() {
  const [t, d] = await Promise.all([api.tokens(), api.devices()])
  tokens.value = t
  deviceName.value = Object.fromEntries(d.map((x: Device) => [x.id, deviceTitle(x)]))
  loading.value = false
}
async function revoke(id: number) {
  try { await api.revokeToken(id); message.success('已吊销'); await load() } catch (e) { message.error(e instanceof ApiError ? e.message : '吊销失败') }
}
async function remove(id: number) {
  try { await api.deleteToken(id); message.success('已删除'); await load() } catch (e) { message.error(e instanceof ApiError ? e.message : '删除失败') }
}
onMounted(() => void load())
// 从别的页面回来时静默刷新（页面保活，不重建）。
useRevisit(() => { void load(); void tickets.value?.load() })
</script>

<template>
  <PageBar title="设备令牌" />
  <div class="content">
    <p class="muted desc">设备装好后会自动申请接入，在控制台弹窗里同意后自动签发令牌；也可以用「添加设备」二维码扫码接入，免去确认。每台设备一个令牌，可单独吊销；被吊销的设备会重新申请，需要再次同意。</p>
    <div class="card tcard">
      <div class="table-wrap">
        <table class="table" :class="{ busy: loading && shown.length }">
          <thead><tr><th>名称</th><th>令牌</th><th>绑定设备</th><th>最近使用</th><th>签发时间</th><th /></tr></thead>
          <tbody>
            <template v-if="loading && !shown.length"><tr v-for="i in 3" :key="i"><td v-for="j in 6" :key="j"><div class="skel" style="width:70%" /></td></tr></template>
            <tr v-for="t in shown" v-else :key="t.id" :class="{ revoked: !!t.revoked_at }">
              <td class="name">{{ t.name }}</td>
              <td class="mono muted">{{ t.token_prefix }}…</td>
              <td>
                <router-link v-if="t.device_id" :to="`/devices/${t.device_id}`" class="dlink">{{ deviceName[t.device_id] ?? `#${t.device_id}` }}</router-link>
                <span v-else class="muted">未使用</span>
              </td>
              <td class="muted">{{ fmtAgo(t.last_used_at) }}</td>
              <td class="mono muted num">{{ fmtTime(t.created_at).slice(0, 16) }}</td>
              <td class="op">
                <template v-if="t.revoked_at">
                  <span class="pill p-wait">已吊销</span>
                  <n-popconfirm @positive-click="remove(t.id)">
                    <template #trigger><n-button size="small" quaternary>删除</n-button></template>
                    删除这条已吊销的令牌记录？不可恢复。
                  </n-popconfirm>
                </template>
                <n-popconfirm v-else @positive-click="revoke(t.id)">
                  <template #trigger><n-button size="small" type="error" quaternary>吊销</n-button></template>
                  吊销后，使用该令牌的设备将无法再连接，确定？
                </n-popconfirm>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!loading && !tokens.length" class="empty">还没有设备接入</div>
      <Pager v-model:page="page" v-model:size="size" :total="tokens.length" />
    </div>

    <EnrollTickets ref="tickets" />
  </div>
</template>

<style scoped>
.desc { margin: 0 4px; font-size: 13px; line-height: 1.7; }
.tcard { padding: 16px 20px; }
.name { font-weight: 600; }
.dlink { color: var(--text); text-decoration: none; }
.dlink:hover { color: var(--svc); }
.op { text-align: right; white-space: nowrap; }
.op .pill { margin-right: 4px; }
tr.revoked td { opacity: .55; }
</style>
