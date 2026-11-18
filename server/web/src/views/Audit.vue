<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api } from '../api/client'
import type { AuditLog, Device, DeviceToken } from '../api/types'
import PageBar from '../components/PageBar.vue'
import Pager from '../components/Pager.vue'
import JsonView from '../components/JsonView.vue'
import ExpandMark from '../components/ExpandMark.vue'
import { auditActionName, deviceTitle, fmtTime } from '../format'
import { useRevisit } from '../composables'

const list = ref<AuditLog[]>([])
const total = ref(0)
const page = ref(1)
const size = ref(20)
const loading = ref(true)
const open = ref<number | null>(null)

async function load() {
  loading.value = true
  try {
    const p = await api.audit({ page: page.value, page_size: size.value })
    list.value = p.items
    total.value = p.total
  } finally {
    loading.value = false
  }
}
watch([page, size], () => { void load(); window.scrollTo({ top: 0 }) })
// 对象列：把设备 / 令牌 ID 换成名称，便于阅读。
const devices = ref<Device[]>([])
const tokens = ref<DeviceToken[]>([])
const deviceName = computed(() => Object.fromEntries(devices.value.map((d) => [String(d.id), deviceTitle(d)])))
const tokenName = computed(() => Object.fromEntries(tokens.value.map((t) => [String(t.id), t.name])))

function target(r: AuditLog): { kind?: string; text: string; to?: string } {
  if (r.action.startsWith('device.') || r.action === 'update.retry') {
    return { kind: '设备', text: deviceName.value[r.target] ?? `#${r.target}`, to: `/devices/${r.target}` }
  }
  if (r.action.startsWith('token.')) return { kind: '令牌', text: tokenName.value[r.target] ?? `#${r.target}`, to: '/tokens' }
  if (r.action.startsWith('release.') || r.action === 'rollout.create') return { kind: '版本', text: r.target, to: '/releases' }
  if (r.action.startsWith('apikey.')) return { kind: '密钥', text: (r.detail as { name?: string })?.name ?? `#${r.target}`, to: '/open-api' }
  if (r.action.startsWith('enroll.')) return { kind: '设备', text: (r.detail as { name?: string })?.name ?? r.target.slice(0, 8) }
  if (r.action === 'rollout.change') return { kind: '计划', text: `#${r.target}`, to: '/releases' }
  return { text: r.target || '—' }
}

onMounted(async () => {
  void load()
  ;[devices.value, tokens.value] = await Promise.all([api.devices(), api.tokens()])
})
// 从别的页面回来时静默刷新（页面保活，不重建）。
useRevisit(() => void load())
</script>

<template>
  <PageBar title="审计日志" />
  <div class="content">
    <div class="card tcard">
      <div class="table-wrap">
        <table class="table" :class="{ busy: loading && list.length }">
          <thead><tr><th class="xcol" /><th style="width:150px">时间</th><th style="width:110px">操作人</th><th>动作</th><th style="width:190px">对象</th><th style="width:140px">IP</th><th style="width:70px">结果</th></tr></thead>
          <tbody>
            <template v-if="loading && !list.length">
              <tr v-for="i in 6" :key="i"><td v-for="j in 7" :key="j"><div class="skel" style="width:70%" /></td></tr>
            </template>
            <template v-for="r in list" v-else :key="r.id">
              <tr :class="{ clickable: !!r.detail, opened: open === r.id }" @click="r.detail && (open = open === r.id ? null : r.id)">
                <td class="xcol"><ExpandMark :can="!!r.detail" :open="open === r.id" /></td>
                <td class="mono muted num">{{ fmtTime(r.created_at).slice(5) }}</td>
                <td>{{ r.username || '—' }}</td>
                <td>{{ auditActionName[r.action] ?? r.action }}</td>
                <td class="target">
                  <span v-if="target(r).kind" class="kind">{{ target(r).kind }}</span>
                  <router-link v-if="target(r).to" :to="target(r).to!" class="tlink" @click.stop>{{ target(r).text }}</router-link>
                  <span v-else class="muted">{{ target(r).text }}</span>
                </td>
                <td class="mono muted">{{ r.ip }}</td>
                <td><span class="pill" :class="r.success ? 'p-ok' : 'p-err'">{{ r.success ? '成功' : '失败' }}</span></td>
              </tr>
              <tr v-if="open === r.id" class="exp"><td colspan="7"><JsonView :value="r.detail" /></td></tr>
            </template>
          </tbody>
        </table>
      </div>
      <div v-if="!loading && !list.length" class="empty">暂无记录</div>
      <Pager v-model:page="page" v-model:size="size" :total="total" />
    </div>
  </div>
</template>

<style scoped>
.target { white-space: nowrap; }
.kind { font-size: 11px; color: var(--sub); background: var(--fill); border-radius: 5px; padding: 1px 6px; margin-right: 6px; }
.tlink { color: var(--text); text-decoration: none; }
.tlink:hover { color: var(--svc); }
.tcard { padding: 16px 20px; }
</style>
