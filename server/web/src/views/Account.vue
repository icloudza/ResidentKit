<script setup lang="ts">
import { ref } from 'vue'
import { NButton, NInput, useMessage } from 'naive-ui'
import QRCode from 'qrcode'
import { api, ApiError, refresh } from '../api/client'
import { useAuth } from '../stores/auth'
import PageBar from '../components/PageBar.vue'
import { fmtTime } from '../format'

const auth = useAuth()
const message = useMessage()
const fail = (e: unknown) => message.error(e instanceof ApiError ? e.message : '操作失败')

// ---- 修改用户名 ----
const editingName = ref(false)
const newName = ref('')
const namePw = ref('')
const nameCode = ref('')
const saving = ref(false)
function editName() {
  newName.value = auth.admin?.username ?? ''
  namePw.value = nameCode.value = ''
  editingName.value = true
}
async function saveName() {
  const name = newName.value.trim()
  if (!/^[A-Za-z0-9_.-]{3,32}$/.test(name)) return message.warning('用户名为 3–32 位字母、数字或 _ . -')
  if (name === auth.admin?.username) return message.warning('新用户名与当前相同')
  if (!namePw.value) return message.warning('请输入当前密码')
  if (auth.admin?.totp_enabled && nameCode.value.length !== 6) return message.warning('请输入 6 位验证码')
  saving.value = true
  try {
    auth.admin = await api.changeUsername(name, namePw.value, nameCode.value)
    await refresh() // 换一张带新用户名的访问令牌
    message.success('用户名已修改，下次登录请使用新用户名')
    editingName.value = false
  } catch (e) { fail(e) } finally { saving.value = false }
}

// ---- 修改密码 ----
const oldPw = ref('')
const newPw = ref('')
const newPw2 = ref('')
async function changePassword() {
  if (newPw.value.length < 8) return message.warning('新密码至少 8 位')
  if (newPw.value !== newPw2.value) return message.warning('两次输入不一致')
  try {
    await api.changePassword(oldPw.value, newPw.value)
    message.success('已修改，其他会话已失效')
    oldPw.value = newPw.value = newPw2.value = ''
  } catch (e) { fail(e) }
}

// ---- 两步验证 ----
const qr = ref('')
const secret = ref('')
const code = ref('')
async function setup() {
  try {
    const r = await api.totpSetup()
    secret.value = r.secret
    qr.value = await QRCode.toDataURL(r.url, { margin: 1, width: 180 })
  } catch (e) { fail(e) }
}
async function enable() {
  try {
    await api.totpEnable(code.value)
    message.success('两步验证已开启')
    qr.value = secret.value = code.value = ''
    auth.admin = await api.me()
  } catch (e) { fail(e) }
}
async function disable() {
  try {
    await api.totpDisable(code.value)
    message.success('两步验证已关闭')
    code.value = ''
    auth.admin = await api.me()
  } catch (e) { fail(e) }
}
</script>

<template>
  <PageBar title="账号与安全" />
  <div class="content">
    <div class="card profile">
      <div class="av">{{ auth.admin?.username.slice(0, 1).toUpperCase() }}</div>
      <div class="pmain">
        <div class="uname">{{ auth.admin?.username }}</div>
        <div class="muted small">管理员 · 上次登录 {{ fmtTime(auth.admin?.last_login_at) }}{{ auth.admin?.last_login_ip ? ` · ${auth.admin.last_login_ip}` : '' }}</div>
      </div>
      <n-button v-if="!editingName" size="small" @click="editName">修改用户名</n-button>
    </div>
    <div v-if="editingName" class="card">
      <div class="card-title"><h3>修改用户名</h3></div>
      <div class="name-form">
        <n-input v-model:value="newName" placeholder="新用户名" maxlength="32" :input-props="{ autocomplete: 'username' }" />
        <n-input v-model:value="namePw" type="password" placeholder="当前密码" :input-props="{ autocomplete: 'current-password' }" @keyup.enter="saveName" />
        <n-input v-if="auth.admin?.totp_enabled" v-model:value="nameCode" placeholder="6 位验证码" maxlength="6" @keyup.enter="saveName" />
        <div class="actions">
          <n-button @click="editingName = false">取消</n-button>
          <n-button type="primary" :loading="saving" @click="saveName">保存</n-button>
        </div>
      </div>
    </div>

    <div class="grid grid-2">
      <div class="card">
        <div class="card-title"><h3>两步验证</h3>
          <span class="pill" :class="auth.admin?.totp_enabled ? 'p-ok' : 'p-wait'" style="margin-left:auto">{{ auth.admin?.totp_enabled ? '已开启' : '未开启' }}</span></div>
        <div class="col">
          <template v-if="!auth.admin?.totp_enabled">
            <p class="muted small">开启后，登录时除密码外还需输入验证器 App 中的 6 位验证码。控制台开放到公网时强烈建议开启。</p>
            <n-button v-if="!qr" type="primary" @click="setup">开始设置</n-button>
            <template v-else>
              <div class="qr-row">
                <img :src="qr" alt="二维码" class="qr" />
                <div class="muted small">用验证器 App 扫码，或手动输入：<div class="mono secret">{{ secret }}</div></div>
              </div>
              <n-input v-model:value="code" placeholder="6 位验证码" maxlength="6" />
              <n-button type="primary" :disabled="code.length !== 6" @click="enable">开启</n-button>
            </template>
          </template>
          <template v-else>
            <p class="muted small">登录已受两步验证保护。关闭需要输入当前验证码。</p>
            <n-input v-model:value="code" placeholder="当前验证码" maxlength="6" />
            <n-button type="error" ghost :disabled="code.length !== 6" @click="disable">关闭两步验证</n-button>
          </template>
        </div>
      </div>

      <div class="card">
        <div class="card-title"><h3>修改密码</h3></div>
        <div class="col">
          <n-input v-model:value="oldPw" type="password" placeholder="原密码" :input-props="{ autocomplete: 'current-password' }" />
          <n-input v-model:value="newPw" type="password" placeholder="新密码（至少 8 位）" :input-props="{ autocomplete: 'new-password' }" />
          <n-input v-model:value="newPw2" type="password" placeholder="再输一次" :input-props="{ autocomplete: 'new-password' }" />
          <n-button type="primary" @click="changePassword">修改</n-button>
          <p class="muted small">修改后，其他已登录的会话会全部失效。</p>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.profile { display: flex; align-items: center; gap: 14px; }
.av { width: 48px; height: 48px; border-radius: 50%; background: var(--header); color: var(--svc); display: grid; place-items: center; font-weight: 700; font-size: 20px; }
.pmain { flex: 1; min-width: 0; }
.uname { font-size: 18px; font-weight: 700; }
.name-form { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)) auto; gap: 10px; align-items: center; }
.actions { display: flex; gap: 8px; }
@media (max-width: 640px) { .name-form { grid-template-columns: 1fr; } .actions { justify-content: flex-end; } }
.small { font-size: 13px; margin: 0; }
.col { display: flex; flex-direction: column; gap: 10px; }
.qr-row { display: flex; gap: 14px; align-items: center; }
.qr { width: 132px; height: 132px; border-radius: 10px; background: #fff; padding: 4px; }
.secret { margin-top: 6px; word-break: break-all; color: var(--text); }
</style>
