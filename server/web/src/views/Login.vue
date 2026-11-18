<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NInput, NButton, useMessage } from 'naive-ui'
import { useAuth } from '../stores/auth'
import { ApiError } from '../api/client'

const auth = useAuth()
const router = useRouter()
const message = useMessage()
const route = useRoute()

const username = ref('')
const password = ref('')
const totp = ref('')
const needTotp = ref(false)
const loading = ref(false)

async function submit() {
  if (!username.value || !password.value) return
  loading.value = true
  try {
    await auth.login(username.value, password.value, totp.value)
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/'
    await router.replace(next)
  } catch (e) {
    if (e instanceof ApiError && e.code === 20004) {
      needTotp.value = true // 需要两步验证码
    } else {
      message.error(e instanceof Error ? e.message : '登录失败')
    }
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="wrap">
    <form class="box" @submit.prevent="submit">
      <div class="brand">
        <img src="/logo.png" alt="" class="logo" />
        <div>
          <div class="name">ResidentKit</div>
          <div class="muted sub">控制台</div>
        </div>
      </div>
      <n-input v-model:value="username" placeholder="用户名" size="large" :input-props="{ autocomplete: 'username' }" />
      <n-input v-model:value="password" type="password" show-password-on="click" placeholder="密码" size="large"
        :input-props="{ autocomplete: 'current-password' }" />
      <n-input v-if="needTotp" v-model:value="totp" placeholder="两步验证码（6 位）" size="large" maxlength="6"
        :input-props="{ autocomplete: 'one-time-code', inputmode: 'numeric' }" autofocus />
      <n-button attr-type="submit" type="primary" size="large" block :loading="loading">登录</n-button>
    </form>
  </div>
</template>

<style scoped>
.wrap { min-height: 100vh; display: grid; place-items: center; padding: 24px; }
.box { width: 360px; max-width: 100%; background: var(--card); border-radius: 24px; padding: 28px; display: flex; flex-direction: column; gap: 14px; }
.brand { display: flex; align-items: center; gap: 14px; margin-bottom: 8px; }
.logo { width: 52px; height: 52px; border-radius: 12px; }
.name { font-size: 22px; font-weight: 700; }
.sub { font-size: 13px; margin-top: 2px; }
</style>
