<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api/client'
import { onStream } from '../api/stream'
import type { Device } from '../api/types'
import PageBar from '../components/PageBar.vue'
import AddDevice from '../components/AddDevice.vue'
import Icon from '../components/Icon.vue'
import { NButton } from 'naive-ui'
import { deviceTitle, fmtAgo, fmtBytes, modelName, networkName } from '../format'
import { useRevisit } from '../composables'

const adding = ref(false) // 「添加设备」二维码弹窗
const router = useRouter()
const list = ref<Device[]>([])
const loading = ref(true)

async function load() {
  list.value = (await api.devices()).sort((a, b) => Number(b.online) - Number(a.online))
  loading.value = false
}

let timer: ReturnType<typeof setTimeout> | undefined
let off = () => {}
onMounted(() => {
  void load()
  off = onStream((topic) => {
    if (topic.startsWith('device.')) timer ??= setTimeout(() => { timer = undefined; void load() }, 2000)
  })
})
onUnmounted(() => { off(); clearTimeout(timer) })
// 从别的页面回来时静默刷新（页面保活，不重建）。
useRevisit(() => void load())
</script>

<template>
  <PageBar title="设备">
    <n-button type="primary" size="small" @click="adding = true">
      <template #icon><Icon name="device" :size="15" /></template>添加设备
    </n-button>
  </PageBar>
  <AddDevice v-model:show="adding" />
  <div class="content">
    <div class="card tcard">
      <div class="table-wrap">
        <table class="table" :class="{ busy: loading && list.length }">
          <thead><tr><th>设备</th><th>型号</th><th>版本</th><th>网络</th><th>延迟</th><th>服务内存</th><th>IP</th><th>最后在线</th></tr></thead>
          <tbody>
            <template v-if="loading && !list.length">
              <tr v-for="i in 3" :key="i"><td v-for="j in 8" :key="j"><div class="skel" style="width:70%" /></td></tr>
            </template>
            <tr v-for="d in list" v-else :key="d.id" class="clickable" @click="router.push({ name: 'device', params: { id: d.id } })">
              <td>
                <div class="row">
                  <span class="dot" :class="d.online ? 'on' : 'off'" />
                  <div><div class="name">{{ deviceTitle(d) }}</div><div class="mono faint uid">{{ d.device_uid }}</div></div>
                </div>
              </td>
              <td><div>{{ modelName(d.info?.model) || '—' }}</div><div v-if="d.info?.os" class="faint" style="font-size:12px">{{ d.info.os }}</div></td>
              <td class="num">v{{ d.version }}</td>
              <td>{{ d.online ? networkName[d.network] || '—' : '—' }}</td>
              <td class="num">{{ d.online && d.rtt_ms ? `${d.rtt_ms} ms` : '—' }}</td>
              <td class="num">{{ d.online ? fmtBytes(d.metrics?.svc_footprint) : '—' }}</td>
              <td class="mono muted">{{ d.last_ip }}</td>
              <td><span v-if="d.online" class="pill p-ok">在线</span><span v-else class="muted">{{ fmtAgo(d.last_seen_at) }}</span></td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!loading && !list.length" class="empty">还没有设备接入。点右上角「添加设备」生成二维码，在手机 App 的「服务器连接」里扫码即可接入。</div>
    </div>
  </div>
</template>

<style scoped>
.tcard { padding: 16px 20px; }
.name { font-weight: 600; }
.uid { font-size: 11px; margin-top: 2px; }
</style>
