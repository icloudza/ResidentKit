// 登录状态。刷新页面后通过 refresh Cookie 恢复会话。

import { defineStore } from 'pinia'
import { api, refresh, setAccessToken } from '../api/client'
import type { Admin } from '../api/types'
import { startStream, stopStream } from '../api/stream'

export const useAuth = defineStore('auth', {
  state: () => ({ admin: null as Admin | null, restored: false }),
  actions: {
    async restore() {
      if (this.restored) return
      this.restored = true
      if (await refresh()) {
        try {
          this.admin = await api.me()
          startStream()
        } catch { this.admin = null }
      }
    },
    async login(username: string, password: string, totp?: string) {
      const r = await api.login(username, password, totp)
      setAccessToken(r.access_token)
      this.admin = r.admin
      startStream()
    },
    // 首次初始化成功后直接进入已登录状态。
    async setupDone(r: { access_token: string; admin: Admin }) {
      setAccessToken(r.access_token)
      this.admin = r.admin
      this.restored = true
      startStream()
    },
    async logout() {
      try { await api.logout() } catch { /* 已失效也照常退出 */ }
      this.clear()
    },
    clear() {
      setAccessToken('')
      stopStream()
      this.admin = null
    },
  },
})
