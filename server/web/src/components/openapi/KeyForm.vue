<script setup lang="ts">
// 创建 / 编辑 API 密钥（都需要再次输入密码）。创建成功后显示一次明文。
import { computed, ref, watch } from 'vue'
import { NButton, NInput, NModal, NSelect, useMessage } from 'naive-ui'
import ChoiceCard from '../ChoiceCard.vue'
import { api, ApiError } from '../../api/client'
import type { APIKey, APIScope, Device, IssuedAPIKey } from '../../api/types'
import { useAuth } from '../../stores/auth'
import { deviceTitle } from '../../format'
import { allScopes, scopeMeta } from '../../openapi/scopes'

const props = defineProps<{ devices: Device[]; edit: APIKey | null }>()
const show = defineModel<boolean>('show', { required: true })
const emit = defineEmits<{ saved: [issued: IssuedAPIKey | null] }>()
const message = useMessage()
const auth = useAuth()

const empty = () => ({ name: '', scopes: ['devices:read', 'commands:invoke'] as APIScope[], allDevices: true, deviceIds: [] as number[],
  ips: '', expires: (props.edit ? -1 : 0) as number, password: '', totp: '' })
const form = ref(empty())
const saving = ref(false)
watch(show, (v) => {
  if (!v) return
  form.value = empty()
  const k = props.edit
  if (k) {
    form.value.name = k.name
    form.value.scopes = [...k.scopes]
    form.value.allDevices = !k.device_ids.length
    form.value.deviceIds = [...k.device_ids]
    form.value.ips = k.allowed_ips.join(' ')
  }
})
const expiresOptions = computed(() => [
  ...(props.edit ? [{ label: '保持不变', value: -1 }] : []),
  { label: '永不过期', value: 0 }, { label: '30 天', value: 30 }, { label: '90 天', value: 90 }, { label: '1 年', value: 365 },
])
const toggle = <T,>(list: T[], v: T, on: boolean): T[] => (on ? [...list, v] : list.filter((x) => x !== v))
const ready = computed(() => {
  const f = form.value
  return !!f.name.trim() && f.scopes.length > 0 && (f.allDevices || f.deviceIds.length > 0) && !!f.password &&
    (!auth.admin?.totp_enabled || f.totp.length === 6)
})

async function save() {
  if (!ready.value) return
  const f = form.value
  const body = {
    name: f.name.trim(), scopes: f.scopes, device_ids: f.allDevices ? [] : f.deviceIds,
    allowed_ips: f.ips.split(/[\s,，]+/).map((x) => x.trim()).filter(Boolean),
    expires_days: f.expires < 0 ? undefined : f.expires, password: f.password, totp_code: f.totp || undefined,
  }
  saving.value = true
  try {
    if (props.edit) {
      await api.updateAPIKey(props.edit.id, body)
      message.success('已保存，立即生效')
      emit('saved', null)
    } else {
      emit('saved', await api.createAPIKey({ ...body, expires_days: body.expires_days ?? 0 }))
    }
    show.value = false
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : '保存失败')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <n-modal v-model:show="show" preset="card" :title="edit ? `编辑密钥 · ${edit.name}` : '创建 API 密钥'" style="width:600px;max-width:calc(100vw - 32px)" :bordered="false">
    <div class="field"><div class="flabel">名称</div><n-input v-model:value="form.name" maxlength="64" placeholder="例如：工单系统" /></div>
    <div class="field"><div class="flabel">权限</div>
      <div class="opts">
        <ChoiceCard v-for="sc in allScopes" :key="sc" :selected="form.scopes.includes(sc)"
          @toggle="form.scopes = toggle(form.scopes, sc, !form.scopes.includes(sc))">
          <b>{{ scopeMeta[sc].label }}</b>
          <span class="mono faint small">{{ sc }}</span>
          <span class="muted small">{{ scopeMeta[sc].desc }}</span>
        </ChoiceCard>
      </div>
    </div>
    <div class="field"><div class="flabel">设备范围</div>
      <div class="seg">
        <span :class="{ on: form.allDevices }" @click="form.allDevices = true">全部设备</span>
        <span :class="{ on: !form.allDevices }" @click="form.allDevices = false">指定设备</span>
      </div>
      <div v-if="!form.allDevices" class="picks">
        <ChoiceCard v-for="d in devices" :key="d.id" :selected="form.deviceIds.includes(d.id)"
          @toggle="form.deviceIds = toggle(form.deviceIds, d.id, !form.deviceIds.includes(d.id))">
          <span class="dn"><span class="dot" :class="d.online ? 'on' : 'off'" />{{ deviceTitle(d) }}</span>
          <span class="faint small num">#{{ d.id }} · {{ d.online ? '在线' : '离线' }}</span>
        </ChoiceCard>
      </div>
    </div>
    <div class="field two">
      <div><label>有效期{{ edit ? '（从现在起）' : '' }}</label><n-select v-model:value="form.expires" :options="expiresOptions" /></div>
      <div><label>来源 IP 白名单（可选）</label><n-input v-model:value="form.ips" placeholder="IP 或 CIDR，空格分隔" /></div>
    </div>
    <div class="field two">
      <div><label>当前密码</label><n-input v-model:value="form.password" type="password" show-password-on="click" placeholder="需要再次验证" /></div>
      <div v-if="auth.admin?.totp_enabled"><label>两步验证码</label><n-input v-model:value="form.totp" maxlength="6" placeholder="6 位数字" /></div>
    </div>
    <template #footer>
      <div class="row"><div class="spacer" />
        <n-button @click="show = false">取消</n-button>
        <n-button type="primary" :loading="saving" :disabled="!ready" @click="save">{{ edit ? '保存' : '创建' }}</n-button>
      </div>
    </template>
  </n-modal>
</template>

<style scoped>
.small { font-size: 12px; }
.field { margin-top: 14px; }
.field:first-child { margin-top: 0; }
.flabel, .two label { display: block; font-size: 12px; color: var(--sub); margin-bottom: 6px; font-weight: 600; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.opts { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
.opts b { font-size: 13.5px; }
.seg { display: inline-flex; background: var(--fill); border-radius: 9px; padding: 2px; }
.seg span { font-size: 12.5px; padding: 4px 12px; border-radius: 7px; cursor: pointer; color: var(--sub); }
.seg span.on { background: var(--card); color: var(--text); font-weight: 600; box-shadow: 0 1px 3px rgba(0, 0, 0, .08); }
.picks { margin-top: 10px; max-height: 220px; overflow-y: auto; display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.dn { font-weight: 600; display: inline-flex; align-items: center; gap: 6px; }
.dot { width: 7px; height: 7px; border-radius: 50%; }
.dot.on { background: var(--green); }
.dot.off { background: var(--faint); }
@media (max-width: 760px) { .two, .opts, .picks { grid-template-columns: 1fr; } }
</style>
