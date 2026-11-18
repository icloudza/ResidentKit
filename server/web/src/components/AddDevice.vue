<script setup lang="ts">
// 添加设备：生成接入码二维码。设备在 App「服务器连接」页点「扫码」或用系统相机扫描，
// 带着接入码申请接入，服务端直接批准，不用再在控制台点「同意」。
import { computed, onUnmounted, ref, watch } from 'vue'
import { NButton, NInput, NModal, NSelect, useMessage } from 'naive-ui'
import QRCode from 'qrcode'
import { api, ApiError } from '../api/client'
import { onStream } from '../api/stream'
import type { Enrollment, EnrollTicketOverview, IssuedEnrollTicket } from '../api/types'
import Icon from './Icon.vue'

const show = defineModel<boolean>('show', { required: true })
const emit = defineEmits<{ created: [] }>()
const message = useMessage()

const name = ref('')
const expires = ref(10)
const uses = ref(1)
const busy = ref(false)
const expiresOptions = [
  { label: '10 分钟', value: 10 }, { label: '1 小时', value: 60 }, { label: '24 小时', value: 60 * 24 }, { label: '7 天', value: 60 * 24 * 7 },
]
const usesOptions = [
  { label: '1 台', value: 1 }, { label: '5 台', value: 5 }, { label: '20 台', value: 20 }, { label: '不限', value: 0 },
]

const info = ref<EnrollTicketOverview | null>(null)
const issued = ref<IssuedEnrollTicket | null>(null)
const qr = ref('')
const joined = ref<string[]>([]) // 二维码生成后新接入的设备
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | undefined
let off = () => {}

watch(show, (v) => {
  if (!v) { stop(); return }
  name.value = ''
  expires.value = 10
  uses.value = 1
  issued.value = null
  qr.value = ''
  joined.value = []
  void api.enrollTickets().then((o) => { info.value = o }).catch(() => {})
})

// 设备连接地址：优先用服务端配置的对外地址，否则按控制台当前地址推出（单端口部署时相同）。
const serverURL = computed(() => {
  if (info.value?.server_url) return info.value.server_url
  const scheme = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${scheme}://${location.host}${info.value?.path || '/rk'}`
})
const link = computed(() => {
  if (!issued.value || !info.value) return ''
  let s = `${info.value.app_scheme}://enroll?u=${encodeURIComponent(serverURL.value)}&t=${issued.value.token}`
  if (info.value.pin) s += `&p=${info.value.pin}`
  return s
})
const left = computed(() => {
  if (!issued.value) return ''
  const ms = new Date(issued.value.expires_at).getTime() - now.value
  if (ms <= 0) return '已过期'
  const m = Math.floor(ms / 60000)
  if (m >= 60 * 24) return `${Math.floor(m / 1440)} 天后失效`
  if (m >= 60) return `${Math.floor(m / 60)} 小时 ${m % 60} 分钟后失效`
  return m > 0 ? `${m} 分 ${Math.floor((ms % 60000) / 1000)} 秒后失效` : `${Math.floor(ms / 1000)} 秒后失效`
})

async function create() {
  busy.value = true
  try {
    if (!info.value) info.value = await api.enrollTickets()
    issued.value = await api.createEnrollTicket({ name: name.value.trim(), expires_minutes: expires.value, max_uses: uses.value })
    qr.value = await QRCode.toDataURL(link.value, { margin: 1, width: 240, errorCorrectionLevel: 'M' })
    timer = setInterval(() => { now.value = Date.now() }, 1000)
    // 有设备凭接入码完成接入时在这里提示（服务端推送接入状态变化）。
    const since = Date.now()
    off = onStream((topic, data) => {
      const e = data as Enrollment
      if (topic === 'enroll.changed' && e?.status === 'completed' && new Date(e.created_at).getTime() >= since - 5000
        && !joined.value.includes(e.name)) joined.value.push(e.name)
    })
    emit('created')
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : '生成失败')
  } finally {
    busy.value = false
  }
}
function stop() {
  if (timer) clearInterval(timer)
  timer = undefined
  off()
  off = () => {}
}
onUnmounted(stop)
async function copy() {
  await navigator.clipboard.writeText(link.value)
  message.success('已复制')
}
</script>

<template>
  <n-modal v-model:show="show" preset="card" :title="issued ? '扫码接入' : '添加设备'" style="width:460px;max-width:calc(100vw - 32px)"
    :bordered="false" :mask-closable="!busy">
    <template v-if="!issued">
      <p class="muted small lead">生成一个接入二维码。手机装好 App 后，在「服务器连接」页点「扫码」，或用系统相机扫描，扫完自动接入，不需要再点「同意」。</p>
      <div class="field"><label>备注（可选）</label><n-input v-model:value="name" maxlength="64" placeholder="比如：张三的 iPhone" /></div>
      <div class="field two">
        <div><label>有效期</label><n-select v-model:value="expires" :options="expiresOptions" /></div>
        <div><label>可接入</label><n-select v-model:value="uses" :options="usesOptions" /></div>
      </div>
    </template>
    <template v-else>
      <div class="qr-wrap">
        <img v-if="qr" :src="qr" alt="接入二维码" width="240" height="240">
        <div class="meta">
          <span class="pill" :class="left === '已过期' ? 'p-wait' : 'p-run'">{{ left }}</span>
          <span class="muted small">{{ issued.max_uses ? `最多接入 ${issued.max_uses} 台` : '接入台数不限' }}</span>
        </div>
        <p class="muted small center">用 App「服务器连接」页的「扫码」或系统相机扫描</p>
        <p class="faint small center mono addr">{{ serverURL }}</p>
      </div>
      <div v-for="n in joined" :key="n" class="joined"><Icon name="check" :size="15" />{{ n }} 已接入</div>
    </template>
    <template #footer>
      <div class="row"><div class="spacer" />
        <template v-if="!issued">
          <n-button :disabled="busy" @click="show = false">取消</n-button>
          <n-button type="primary" :loading="busy" @click="create">生成二维码</n-button>
        </template>
        <template v-else>
          <n-button @click="copy">复制链接</n-button>
          <n-button type="primary" @click="show = false">完成</n-button>
        </template>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.small { font-size: 12.5px; }
.lead { margin: 0 0 4px; line-height: 1.7; }
.field { margin-top: 14px; }
.field label { display: block; font-size: 12px; color: var(--sub); margin-bottom: 6px; font-weight: 600; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.qr-wrap { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 4px 0; }
.qr-wrap img { border-radius: 14px; background: #fff; padding: 8px; }
.meta { display: flex; align-items: center; gap: 10px; }
.center { text-align: center; margin: 0; }
.addr { word-break: break-all; }
.joined { display: flex; align-items: center; gap: 6px; justify-content: center; color: var(--green); font-weight: 600; font-size: 13px; margin-top: 8px; }
@media (max-width: 760px) { .two { grid-template-columns: 1fr; } }
</style>
