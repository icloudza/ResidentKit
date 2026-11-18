import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createDiscreteApi } from 'naive-ui'
import App from './App.vue'
import { router } from './router'
import { ApiError } from './api/client'
import './styles.css'

createApp(App).use(createPinia()).use(router).mount('#app')

// 兜底：页面里未处理的请求失败统一提示，而不是静默卡在加载状态。
const { message } = createDiscreteApi(['message'])
let last = 0
window.addEventListener('unhandledrejection', (e) => {
  const err = e.reason
  if (!(err instanceof ApiError) && !(err instanceof TypeError)) return
  e.preventDefault()
  if (Date.now() - last < 3000) return
  last = Date.now()
  message.error(err instanceof ApiError ? err.message : '网络连接失败，请检查网络或服务状态')
})
