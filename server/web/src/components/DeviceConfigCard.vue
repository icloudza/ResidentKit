<script setup lang="ts">
// 设备配置：按设备上报的可配置项生成表单；保存后立即下发，离线设备上线后生效。
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { NButton, NInput, NInputNumber, NPopconfirm, NSelect, NSwitch, useMessage } from 'naive-ui'
import { api, ApiError } from '../api/client'
import { onStream } from '../api/stream'
import type { ConfigItem, DeviceConfig } from '../api/types'
import { fmtAgo, moduleName } from '../format'

const props = defineProps<{ deviceId: number; online: boolean }>()
const message = useMessage()
const cfg = ref<DeviceConfig | null>(null)
const form = ref<Record<string, Record<string, unknown>>>({})
const saving = ref(false)

// 按 moduleName 的登记顺序排（框架模块在前，示例在后），未登记的排最后。
const order = Object.keys(moduleName)
const rank = (m: string) => (order.includes(m) ? order.indexOf(m) : order.length)
const modules = computed(() => Object.entries(cfg.value?.schema ?? {}).sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b)))

// 表单值：期望配置覆盖默认值。
function current(): Record<string, Record<string, unknown>> {
  const out: Record<string, Record<string, unknown>> = {}
  for (const [mod, items] of modules.value) {
    out[mod] = {}
    for (const it of items) out[mod][it.key] = cfg.value!.config[mod]?.[it.key] ?? it.default
  }
  return out
}
const dirty = computed(() => JSON.stringify(form.value) !== JSON.stringify(current()))
const isDefault = (mod: string, it: ConfigItem) => form.value[mod]?.[it.key] === it.default

async function load(keepEdits = false) {
  cfg.value = await api.deviceConfig(props.deviceId)
  if (!keepEdits) form.value = current()
}

async function save() {
  // 数字框清空时为 null：视为恢复默认。
  for (const [mod, items] of modules.value) {
    for (const it of items) if (form.value[mod][it.key] === null) form.value[mod][it.key] = it.default
  }
  saving.value = true
  try {
    cfg.value = await api.saveDeviceConfig(props.deviceId, form.value)
    form.value = current()
    message.success(props.online ? '已保存并下发' : '已保存，设备上线后生效')
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    saving.value = false
  }
}

function resetAll() {
  for (const [mod, items] of modules.value) for (const it of items) form.value[mod][it.key] = it.default
}

const status = computed(() => {
  const c = cfg.value
  if (!c) return null
  if (c.status === 'rejected') return { cls: 'p-err', text: '设备拒绝', note: c.error }
  if (c.status === 'pending') return props.online
    ? { cls: 'p-run', text: '下发中', note: `版本 ${c.version}` }
    : { cls: 'p-wait', text: '待生效', note: '设备离线，上线后自动下发' }
  if (!c.version) return { cls: 'p-wait', text: '默认配置', note: '' }
  return { cls: 'p-ok', text: '已生效', note: `版本 ${c.version}${c.updated_at ? ` · ${fmtAgo(c.updated_at)}修改` : ''}` }
})

let off = () => {}
onMounted(() => {
  void load()
  off = onStream((topic, data) => {
    if (topic === 'config.changed' && (data as { device_id: number }).device_id === props.deviceId) void load(dirty.value)
  })
})
onUnmounted(() => off())
// 设备重新上线时可能上报了新的可配置项。
watch(() => props.online, () => void load(dirty.value))
</script>

<template>
  <div v-if="cfg && (modules.length || !cfg.supported)" class="card">
    <div class="card-title"><h3>配置</h3>
      <span v-if="status" class="pill" :class="status.cls">{{ status.text }}</span>
      <span v-if="status?.note" class="hint">{{ status.note }}</span>
    </div>
    <div v-if="!cfg.supported" class="empty">设备版本过旧，不支持远程配置</div>
    <template v-else>
      <div v-for="[mod, items] in modules" :key="mod" class="group">
        <div class="gtitle">{{ moduleName[mod] ?? mod }}<span class="mono faint">{{ mod }}</span></div>
        <div v-for="it in items" :key="it.key" class="item">
          <div class="label">
            <span class="n">{{ it.label }}<span v-if="!isDefault(mod, it)" class="changed" title="已修改，非默认值" /></span>
            <span v-if="it.desc" class="d">{{ it.desc }}</span>
            <span class="d mono">{{ it.key }} · 默认 {{ it.type === 'bool' ? (it.default ? '开' : '关') : it.default }}</span>
          </div>
          <n-switch v-if="it.type === 'bool'" v-model:value="form[mod][it.key] as boolean" />
          <n-input-number v-else-if="it.type === 'int' || it.type === 'number'" v-model:value="form[mod][it.key] as number"
            :min="it.min" :max="it.max" :precision="it.type === 'int' ? 0 : undefined" size="small" style="width:150px" />
          <n-select v-else-if="it.type === 'enum'" v-model:value="form[mod][it.key] as string"
            :options="(it.options ?? []).map((o) => ({ label: o, value: o }))" size="small" style="width:150px" />
          <n-input v-else v-model:value="form[mod][it.key] as string" :maxlength="it.max_len ?? 256" size="small" style="width:220px" />
        </div>
      </div>
      <div class="row foot">
        <n-popconfirm @positive-click="resetAll">
          <template #trigger><n-button size="small" quaternary>全部恢复默认</n-button></template>
          把表单恢复为默认值（保存后才生效）。
        </n-popconfirm>
        <div class="spacer" />
        <n-button v-if="dirty" size="small" quaternary @click="form = current()">撤销修改</n-button>
        <n-button size="small" type="primary" :disabled="!dirty" :loading="saving" @click="save">保存并下发</n-button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.hint { color: var(--faint); font-size: 12px; }
.group + .group { margin-top: 16px; }
.gtitle { font-size: 12.5px; font-weight: 700; color: var(--sub); display: flex; gap: 8px; align-items: baseline; margin-bottom: 4px; }
.gtitle .mono { font-size: 11px; font-weight: 400; }
.item { display: flex; align-items: center; gap: 16px; padding: 10px 0; border-top: 1px solid var(--sep); }
.label { flex: 1; display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.n { font-size: 13px; font-weight: 600; display: inline-flex; align-items: center; gap: 6px; }
.d { font-size: 12px; color: var(--faint); }
.changed { width: 6px; height: 6px; border-radius: 50%; background: var(--svc); }
.foot { margin-top: 14px; }
</style>
