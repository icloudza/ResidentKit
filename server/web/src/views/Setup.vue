<script setup lang="ts">
// 首次初始化向导：初始化码 → 管理员账号 → 两步验证（可跳过）。
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { NInput, NButton, useMessage } from 'naive-ui'
import QRCode from 'qrcode'
import { api, ApiError } from '../api/client'
import { useAuth } from '../stores/auth'
import { setupState } from '../router'

const router = useRouter()
const message = useMessage()
const auth = useAuth()

const step = ref(1)
const loading = ref(false)

const code = ref('')
const username = ref('admin')
const password = ref('')
const password2 = ref('')

const totpQR = ref('')
const totpSecret = ref('')
const totpCode = ref('')

const steps = ['验证身份', '创建管理员', '两步验证']
const pwOK = computed(() => password.value.length >= 8 && password.value === password2.value)
const userOK = computed(() => /^[A-Za-z0-9_.-]{3,32}$/.test(username.value))

function fail(e: unknown) {
  message.error(e instanceof ApiError ? e.message : '操作失败')
}

function next1() {
  if (code.value.replace(/[-\s]/g, '').length !== 12) {
    message.error('初始化码为 12 位')
    return
  }
  step.value = 2
}

async function create() {
  if (!userOK.value) { message.error('用户名为 3–32 位字母、数字或 _ . -'); return }
  if (!pwOK.value) { message.error(password.value.length < 8 ? '密码至少 8 位' : '两次输入的密码不一致'); return }
  loading.value = true
  try {
    const r = await api.setup(code.value, username.value, password.value)
    await auth.setupDone(r)
    setupState.needed = false
    const t = await api.totpSetup()
    totpSecret.value = t.secret
    totpQR.value = await QRCode.toDataURL(t.url, { margin: 1, width: 180 })
    step.value = 3
  } catch (e) {
    fail(e)
    // 初始化码错误时回到第一步。
    if (e instanceof ApiError && e.message.includes('初始化码')) step.value = 1
  } finally {
    loading.value = false
  }
}

async function enableTotp() {
  loading.value = true
  try {
    await api.totpEnable(totpCode.value)
    auth.admin = await api.me()
    await router.replace({ name: 'dashboard' })
  } catch (e) { fail(e) } finally { loading.value = false }
}

const finish = () => router.replace({ name: 'dashboard' })
</script>

<template>
  <div class="wrap">
    <div class="box">
      <div class="brand">
        <img src="/logo.png" alt="" class="logo" />
        <div>
          <div class="name">欢迎使用 ResidentKit</div>
          <div class="muted sub">首次使用，请完成初始化</div>
        </div>
      </div>

      <div class="steps">
        <div v-for="(s, i) in steps" :key="s" class="step" :class="{ on: step === i + 1, done: step > i + 1 }">
          <span class="n">{{ step > i + 1 ? '✓' : i + 1 }}</span>{{ s }}
        </div>
      </div>


      <!-- 第 1 步：初始化码 -->
      <form v-if="step === 1" class="form" @submit.prevent="next1">
        <p class="muted tip">为防止他人抢先初始化，需要输入服务器日志中的一次性初始化码。在服务器上执行：</p>
        <pre class="json cmd">journalctl -u rk-server | grep 初始化码</pre>
        <n-input v-model:value="code" placeholder="XXXX-XXXX-XXXX" size="large" maxlength="16" class="mono" autofocus />
        <n-button attr-type="submit" type="primary" size="large" block>下一步</n-button>
      </form>

      <!-- 第 2 步：管理员账号 -->
      <form v-else-if="step === 2" class="form" @submit.prevent="create">
        <n-input v-model:value="username" placeholder="用户名" size="large" maxlength="32" :input-props="{ autocomplete: 'username' }" />
        <n-input v-model:value="password" type="password" show-password-on="click" placeholder="密码（至少 8 位）" size="large"
          :input-props="{ autocomplete: 'new-password' }" />
        <n-input v-model:value="password2" type="password" show-password-on="click" placeholder="再输一次" size="large"
          :input-props="{ autocomplete: 'new-password' }" />
        <div class="row">
          <n-button size="large" @click="step = 1">上一步</n-button>
          <n-button attr-type="submit" type="primary" size="large" class="grow" :loading="loading">创建管理员</n-button>
        </div>
      </form>

      <!-- 第 3 步：两步验证（可跳过） -->
      <form v-else class="form" @submit.prevent="enableTotp">
        <p class="muted tip">管理员已创建并登录。建议开启两步验证：用验证器 App（如 Google Authenticator、1Password）扫码。</p>
        <div class="qr-row">
          <img :src="totpQR" alt="二维码" class="qr" />
          <div class="muted small">无法扫码时手动输入：<div class="mono secret">{{ totpSecret }}</div></div>
        </div>
        <n-input v-model:value="totpCode" placeholder="6 位验证码" size="large" maxlength="6"
          :input-props="{ autocomplete: 'one-time-code', inputmode: 'numeric' }" />
        <div class="row">
          <n-button size="large" @click="finish">暂不开启</n-button>
          <n-button attr-type="submit" type="primary" size="large" class="grow" :loading="loading"
            :disabled="totpCode.length !== 6">开启并进入控制台</n-button>
        </div>
      </form>
    </div>
  </div>
</template>

<style scoped>
.wrap { min-height: 100vh; display: grid; place-items: center; padding: 24px; }
.box { width: 440px; max-width: 100%; background: var(--card); border-radius: 24px; padding: 28px; display: flex; flex-direction: column; gap: 16px; }
.brand { display: flex; align-items: center; gap: 14px; }
.logo { width: 52px; height: 52px; border-radius: 12px; }
.name { font-size: 20px; font-weight: 700; }
.sub { font-size: 13px; margin-top: 2px; }
.steps { display: flex; gap: 6px; }
.step {
  flex: 1; display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--sub);
  padding: 8px 10px; border-radius: 12px; background: var(--fill);
}
.step.on { background: var(--header); color: var(--text); font-weight: 600; }
.step.done { color: var(--svc); }
.n {
  width: 20px; height: 20px; border-radius: 50%; display: inline-grid; place-items: center; font-size: 12px;
  background: var(--card); font-weight: 700;
}
.step.on .n { background: var(--svc); color: #fff; }
.form { display: flex; flex-direction: column; gap: 12px; }
.tip { margin: 0; font-size: 13px; line-height: 1.6; }
.cmd { margin: 0; }
.row { display: flex; gap: 10px; }
.grow { flex: 1; }
.qr-row { display: flex; gap: 14px; align-items: center; }
.qr { width: 132px; height: 132px; border-radius: 10px; background: #fff; padding: 4px; }
.small { font-size: 13px; }
.secret { margin-top: 6px; word-break: break-all; color: var(--text); }
</style>
