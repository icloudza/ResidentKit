<script setup lang="ts">
// 开放 API：概览 / 密钥 / 调用记录 / 接口文档（文档见 docs/open-api.md）。当前标签记在地址栏，刷新不丢。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NModal, useMessage } from 'naive-ui'
import { api, ApiError } from '../api/client'
import { onStream } from '../api/stream'
import type { APIKey, APIStats, Device, IssuedAPIKey, Invocation, OpenAPIInfo } from '../api/types'
import PageBar from '../components/PageBar.vue'
import Icon from '../components/Icon.vue'
import ApiOverview from '../components/openapi/ApiOverview.vue'
import ApiKeys from '../components/openapi/ApiKeys.vue'
import ApiCalls from '../components/openapi/ApiCalls.vue'
import ApiDocs from '../components/openapi/ApiDocs.vue'
import KeyForm from '../components/openapi/KeyForm.vue'
import { useRevisit } from '../composables'

type Tab = 'overview' | 'keys' | 'calls' | 'docs'
const route = useRoute()
const router = useRouter()
const message = useMessage()

const tabs: { id: Tab; label: string }[] = [
  { id: 'overview', label: '概览' }, { id: 'keys', label: '密钥' }, { id: 'calls', label: '调用记录' }, { id: 'docs', label: '接口文档' },
]
const tab = computed<Tab>(() => (tabs.some((t) => t.id === route.query.tab) ? route.query.tab as Tab : 'overview'))
const callsKey = computed<number | null>({
  get: () => (route.query.key ? Number(route.query.key) : null),
  set: (v) => void router.replace({ query: { ...route.query, key: v ? String(v) : undefined } }),
})
function go(t: Tab, extra: Record<string, string | undefined> = {}) {
  void router.replace({ query: { tab: t === 'overview' ? undefined : t, ...extra } })
}

const keys = ref<APIKey[]>([])
const devices = ref<Device[]>([])
const info = ref<OpenAPIInfo | null>(null)
const stats = ref<APIStats | null>(null)
const loading = ref(true)
const activeKeys = computed(() => keys.value.filter((k) => !k.revoked_at).length)

async function loadKeys() { keys.value = await api.apiKeys() }
async function loadStats() { stats.value = await api.apiStats() }
async function load() {
  try {
    ;[keys.value, devices.value, info.value, stats.value] = await Promise.all([api.apiKeys(), api.devices(), api.apiKeyInfo(), api.apiStats()])
  } finally {
    loading.value = false
  }
}

// ---- 密钥操作 ----
const formShow = ref(false)
const editing = ref<APIKey | null>(null)
const issued = ref<IssuedAPIKey | null>(null)
function create() { editing.value = null; formShow.value = true }
function edit(k: APIKey) { editing.value = k; formShow.value = true }
async function saved(k: IssuedAPIKey | null) {
  if (k) issued.value = k
  await Promise.all([loadKeys(), loadStats()])
}
async function act(f: () => Promise<unknown>, ok: string) {
  try { await f(); message.success(ok); await Promise.all([loadKeys(), loadStats()]) } catch (e) { message.error(e instanceof ApiError ? e.message : '操作失败') }
}
async function copy(v: string) {
  await navigator.clipboard.writeText(v)
  message.success('已复制')
}

// 有开放 API 调用时只做标记，每 15 秒（页面可见时）刷新一次统计：
// 不随调用量变化，高并发时也不会让每个打开的页面频繁查询统计。
let off = () => {}
let dirty = false
let tick: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  off = onStream((topic, data) => {
    if (topic === 'invocation.updated' && (data as Invocation).api_key_id) dirty = true
  })
  tick = setInterval(() => {
    if (!dirty || document.hidden) return
    dirty = false
    void loadStats(); void loadKeys()
  }, 15000)
})
onUnmounted(() => { off(); clearInterval(tick) })
// 从别的页面回来时静默刷新（页面保活，不重建）。
useRevisit(() => void load())
</script>

<template>
  <PageBar title="开放 API">
    <n-button type="primary" size="small" @click="create"><template #icon><Icon name="key" :size="15" /></template>创建密钥</n-button>
  </PageBar>
  <div class="content">
    <div class="tabs">
      <a v-for="t in tabs" :key="t.id" class="tab" :class="{ on: tab === t.id }" @click="go(t.id)">
        {{ t.label }}<span v-if="t.id === 'keys' && activeKeys" class="n num">{{ activeKeys }}</span></a>
    </div>

    <ApiOverview v-if="tab === 'overview'" :stats="stats" :info="info" :keys="keys" @create="create" @docs="go('docs')" />
    <ApiKeys v-else-if="tab === 'keys'" :keys="keys" :stats="stats" :devices="devices" :loading="loading"
      @create="create" @edit="edit" @calls="(k) => go('calls', { key: String(k.id) })"
      @revoke="(k) => act(() => api.revokeAPIKey(k.id), '已吊销，立即生效')" @remove="(k) => act(() => api.deleteAPIKey(k.id), '已删除')" />
    <ApiCalls v-else-if="tab === 'calls'" v-model:key-id="callsKey" :keys="keys" :devices="devices" />
    <ApiDocs v-else :info="info" :devices="devices" :keys="keys" @create="create" />

    <KeyForm v-model:show="formShow" :devices="devices" :edit="editing" @saved="saved" />

    <n-modal :show="!!issued" preset="card" title="密钥已创建" style="width:520px;max-width:calc(100vw - 32px)" :bordered="false"
      :mask-closable="false" @close="issued = null">
      <p class="muted">请现在复制并妥善保存，关闭后无法再次查看；泄露时在「密钥」里吊销即可。</p>
      <pre class="token mono">{{ issued?.key }}</pre>
      <template #footer>
        <div class="row"><div class="spacer" />
          <n-button @click="copy(issued!.key)">复制</n-button>
          <n-button type="primary" @click="issued = null; go('docs')">去调试</n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.tabs { display: flex; gap: 4px; border-bottom: 1px solid var(--sep); margin-top: -4px; overflow-x: auto; }
.tab { padding: 9px 14px; font-size: 14px; color: var(--sub); cursor: pointer; border-bottom: 2px solid transparent; margin-bottom: -1px;
  display: flex; gap: 6px; align-items: center; white-space: nowrap; }
.tab:hover { color: var(--text); }
.tab.on { color: var(--text); font-weight: 600; border-color: var(--svc); }
.n { font-size: 11px; background: var(--fill); border-radius: 99px; padding: 0 7px; color: var(--sub); font-weight: 500; }
.token { background: var(--fill); border-radius: 10px; padding: 12px; word-break: break-all; white-space: pre-wrap; font-size: 13px; }
</style>
