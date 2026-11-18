<script setup lang="ts">
// 设备接入审批：有设备申请接入时，任何页面都弹出对话框，同意后设备自动领取令牌并连接。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { NButton, NModal, useMessage } from 'naive-ui'
import { api, ApiError } from '../api/client'
import { onStream } from '../api/stream'
import type { Enrollment } from '../api/types'
import Icon from './Icon.vue'
import { fmtAgo } from '../format'

const message = useMessage()
const pending = ref<Enrollment[]>([])
const busy = ref(false)
const current = computed(() => pending.value[0])

async function load() {
  try { pending.value = await api.enrollments() } catch { /* 未登录或网络问题时忽略 */ }
}

async function decide(action: 'approve' | 'reject') {
  const e = current.value
  if (!e) return
  busy.value = true
  try {
    await api.decideEnrollment(e.id, action)
    message.success(action === 'approve' ? `已同意，${e.name} 几秒内会自动连接` : '已拒绝，1 小时内不再提示该设备')
  } catch (err) {
    message.error(err instanceof ApiError ? err.message : '操作失败')
  } finally {
    busy.value = false
    await load()
  }
}

let off = () => {}
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  off = onStream((topic) => { if (topic === 'enroll.changed') void load() })
  timer = setInterval(load, 60_000) // 兜底：推送断开期间的申请
})
onUnmounted(() => { off(); clearInterval(timer) })
</script>

<template>
  <n-modal :show="!!current" preset="card" :closable="false" :mask-closable="false" :bordered="false"
    style="width:440px;max-width:calc(100vw - 32px)">
    <template #header>
      <div class="hd"><span class="ic"><Icon name="device" :size="20" /></span>新设备请求接入</div>
    </template>
    <div v-if="current" class="body">
      <div class="dev">
        <b>{{ current.name }}</b>
        <span class="muted">{{ current.model }}{{ current.version ? ` · v${current.version}` : '' }}</span>
      </div>
      <dl>
        <dt>来源 IP</dt><dd class="mono">{{ current.ip }}</dd>
        <dt>设备 ID</dt><dd class="mono">{{ current.device_uid }}</dd>
        <dt>申请时间</dt><dd>{{ fmtAgo(current.created_at) }}</dd>
      </dl>
      <div class="code">
        <span class="faint">配对码</span>
        <b class="num">{{ current.code.slice(0, 3) }} {{ current.code.slice(3) }}</b>
        <span class="faint small">与手机 App「服务器连接」里显示的一致才是你的设备</span>
      </div>
      <p v-if="pending.length > 1" class="faint small more">还有 {{ pending.length - 1 }} 个申请等待处理</p>
    </div>
    <template #footer>
      <div class="row"><div class="spacer" />
        <n-button :disabled="busy" @click="decide('reject')">拒绝</n-button>
        <n-button type="primary" :loading="busy" @click="decide('approve')">同意接入</n-button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.hd { display: flex; align-items: center; gap: 10px; font-size: 17px; font-weight: 700; }
.ic { width: 34px; height: 34px; border-radius: 10px; background: var(--svc-soft); color: var(--svc); display: grid; place-items: center; }
.dev { display: flex; flex-direction: column; gap: 2px; }
.dev b { font-size: 18px; }
.dev .muted { font-size: 13px; }
dl { display: grid; grid-template-columns: auto 1fr; gap: 6px 14px; margin: 14px 0; font-size: 13px; }
dt { color: var(--sub); }
dd { margin: 0; word-break: break-all; }
.code { display: flex; flex-direction: column; align-items: center; gap: 4px; background: var(--fill); border-radius: 12px; padding: 12px; }
.code b { font-size: 28px; letter-spacing: 4px; }
.small { font-size: 12px; }
.more { margin: 10px 0 0; text-align: center; }
</style>
