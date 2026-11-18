<script setup lang="ts">
// 下发指令面板：按设备上报的命令说明生成参数表单（没有说明的命令用 JSON），选择离线策略，展示结果。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { NDrawer, NSwitch, NInput, NInputNumber, NSelect, NButton, useMessage } from 'naive-ui'
import { api, ApiError } from '../api/client'
import { onStream } from '../api/stream'
import type { Invocation } from '../api/types'
import { commandMeta, type CommandMeta } from '../commands'
import type { CommandParam } from '../api/types'
import { invocationNote, statusMeta } from '../format'
import JsonView from './JsonView.vue'

const props = defineProps<{ deviceId: number; module: string; command: string; online: boolean; meta?: CommandMeta }>()
const show = defineModel<boolean>('show', { required: true })
const message = useMessage()

const meta = computed(() => props.meta ?? commandMeta(props.module, props.command))
type Val = boolean | number | string | null
const values = ref<Record<string, Val>>({})
const json = ref('{}')
const policy = ref<'fail' | '10m' | '1d'>('10m')
const running = ref(false)
const result = ref<Invocation | null>(null)

const mobile = ref(window.matchMedia('(max-width: 760px)').matches)

watch(show, (v) => {
  if (!v) return
  result.value = null
  json.value = '{}'
  values.value = Object.fromEntries((meta.value.params ?? []).map((p) => [p.key, initial(p)]))
})

// 表单初值：声明了默认值就用默认值；object / array 在输入框里写 JSON。
function initial(p: CommandParam): Val {
  const d = p.default
  if (p.type === 'object' || p.type === 'array') return d !== undefined ? JSON.stringify(d) : ''
  if (d !== undefined && d !== null) return d as Val
  if (p.type === 'bool') return false
  if (p.type === 'int' || p.type === 'number') return null
  return ''
}
const isNum = (p: CommandParam) => p.type === 'int' || p.type === 'number'
const isJSON = (p: CommandParam) => p.type === 'object' || p.type === 'array'
const opts = (p: CommandParam) => (p.options ?? []).map((o) => ({ label: o, value: o }))

function args(): Record<string, unknown> | null {
  if (meta.value.known && meta.value.params) {
    // 空着的可选参数不传；必填的空着时提示。服务端还会再按说明校验一次。
    const out: Record<string, unknown> = {}
    for (const p of meta.value.params) {
      const v = values.value[p.key]
      const empty = v === null || v === undefined || v === ''
      if (empty) {
        if (p.required) { message.error(`请填写「${p.label || p.key}」`); return null }
        continue
      }
      if (isJSON(p)) {
        try { out[p.key] = JSON.parse(String(v)) } catch { message.error(`「${p.label || p.key}」不是有效的 JSON`); return null }
      } else out[p.key] = v
    }
    return out
  }
  try {
    const v = JSON.parse(json.value || '{}')
    if (typeof v !== 'object' || Array.isArray(v) || v === null) throw new Error()
    return v
  } catch {
    message.error('参数必须是 JSON 对象')
    return null
  }
}

async function run() {
  const a = args()
  if (!a) return
  running.value = true
  try {
    result.value = await api.invoke(props.deviceId, {
      module: props.module, command: props.command, arguments: a, wait_seconds: 10,
      ttl_seconds: policy.value === 'fail' ? -1 : policy.value === '10m' ? 600 : 86400,
    })
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : '下发失败')
  } finally {
    running.value = false
  }
}

let off = () => {}
onMounted(() => {
  off = onStream((topic, data) => {
    if (topic === 'invocation.updated' && result.value && (data as Invocation).id === result.value.id) result.value = data as Invocation
  })
})
onUnmounted(() => off())
</script>

<template>
  <n-drawer v-model:show="show" :placement="mobile ? 'bottom' : 'right'" :width="mobile ? undefined : 420"
    :height="mobile ? '80%' : undefined">
    <div class="drawer">
      <div class="head">
        <div>
          <div class="mono muted mod">{{ module }} · {{ command }}</div>
          <h3>{{ meta.label }}</h3>
          <div v-if="meta.desc" class="muted desc">{{ meta.desc }}</div>
        </div>
        <button class="x" @click="show = false">×</button>
      </div>

      <div v-if="meta.known && meta.params?.length" class="field">
        <label>参数</label>
        <div v-for="p in meta.params" :key="p.key" class="param" :class="{ wide: isJSON(p) }">
          <span class="pl"><span>{{ p.label || p.key }}<span v-if="p.required" class="req">*</span></span>
            <span v-if="p.desc" class="pd muted">{{ p.desc }}</span></span>
          <n-switch v-if="p.type === 'bool'" v-model:value="values[p.key] as boolean" />
          <n-input-number v-else-if="isNum(p)" v-model:value="values[p.key] as number | null" size="small" style="width:150px"
            :min="p.min" :max="p.max" :precision="p.type === 'int' ? 0 : undefined" clearable />
          <n-select v-else-if="p.type === 'enum'" v-model:value="values[p.key] as string" :options="opts(p)" size="small" style="width:180px" />
          <n-input v-else-if="isJSON(p)" v-model:value="values[p.key] as string" type="textarea" class="mono" size="small"
            :autosize="{ minRows: 2, maxRows: 8 }" :placeholder="p.type === 'array' ? '[ ]' : '{ }'" />
          <n-input v-else v-model:value="values[p.key] as string" size="small" style="width:180px" />
        </div>
      </div>
      <div v-else-if="!meta.known || !meta.params" class="field">
        <label>参数（JSON 对象）</label>
        <n-input v-model:value="json" type="textarea" :autosize="{ minRows: 3, maxRows: 10 }" class="mono" />
      </div>

      <div class="field">
        <label>设备离线时{{ online ? '' : '（当前离线）' }}</label>
        <div class="seg">
          <span :class="{ on: policy === 'fail' }" @click="policy = 'fail'">直接失败</span>
          <span :class="{ on: policy === '10m' }" @click="policy = '10m'">排队 10 分钟</span>
          <span :class="{ on: policy === '1d' }" @click="policy = '1d'">排队 1 天</span>
        </div>
      </div>

      <n-button type="primary" size="large" block :loading="running" @click="run">下发指令</n-button>

      <div v-if="result" class="res" :class="statusMeta[result.status]?.cls">
        <div class="rh"><span class="pill" :class="statusMeta[result.status]?.cls">{{ statusMeta[result.status]?.text }}</span>
          <span class="muted">{{ invocationNote(result) }}</span></div>
        <div v-if="result.error_message" class="err">{{ result.error_code ? `${result.error_code} · ` : '' }}{{ result.error_message }}</div>
        <JsonView v-else-if="result.result" :value="result.result" />
        <div v-else class="muted wait">{{ result.status === 'pending' ? '设备离线，上线后自动下发' : '等待设备返回…' }}</div>
      </div>
    </div>
  </n-drawer>
</template>

<style scoped>
.drawer { padding: 22px; display: flex; flex-direction: column; gap: 16px; }
.head { display: flex; align-items: flex-start; gap: 12px; }
.head > div { flex: 1; }
.mod { font-size: 12px; }
h3 { font-size: 20px; margin: 2px 0 0; }
.desc { font-size: 13px; margin-top: 4px; }
.x { border: 0; background: var(--fill); color: var(--sub); width: 30px; height: 30px; border-radius: 50%; font-size: 18px; cursor: pointer; }
.field { display: flex; flex-direction: column; gap: 8px; }
.field label { font-size: 13px; color: var(--sub); font-weight: 500; }
.param { display: flex; align-items: center; justify-content: space-between; gap: 12px; background: var(--fill); border-radius: 12px; padding: 12px 14px; font-size: 14px; }
.param.wide { flex-direction: column; align-items: stretch; }
.pl { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.pd { font-size: 12px; }
.req { color: var(--red); margin-left: 3px; }
.seg { display: flex; gap: 6px; }
.seg span { flex: 1; text-align: center; padding: 9px 4px; border-radius: 10px; background: var(--fill); font-size: 13px; cursor: pointer; }
.seg span.on { background: var(--svc-soft); color: var(--svc); font-weight: 600; }
.res { border-radius: 14px; padding: 12px; display: flex; flex-direction: column; gap: 8px; background: var(--fill); }
.res.p-ok { background: color-mix(in srgb, var(--green) 7%, transparent); }
.res.p-err { background: color-mix(in srgb, var(--red) 7%, transparent); }
.rh { display: flex; align-items: center; gap: 8px; font-size: 12.5px; }
.err { color: var(--red); font-size: 13px; }
.wait { font-size: 13px; }
</style>
