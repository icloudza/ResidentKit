import { createRouter, createWebHistory } from 'vue-router'
import { useAuth } from './stores/auth'
import { api, setUnauthorizedHandler } from './api/client'

export const router = createRouter({
  history: createWebHistory(),
  // 前进后退回到原来的位置；切换到别的页面回到顶部；同一页面只改查询参数（标签、筛选）时不动。
  scrollBehavior(to, from, saved) {
    if (saved) return saved
    return to.path !== from.path ? { top: 0 } : false
  },
  routes: [
    { path: '/login', name: 'login', component: () => import('./views/Login.vue'), meta: { public: true } },
    { path: '/setup', name: 'setup', component: () => import('./views/Setup.vue'), meta: { public: true, title: '初始化' } },
    {
      path: '/',
      component: () => import('./views/Layout.vue'),
      children: [
        { path: '', name: 'dashboard', component: () => import('./views/Dashboard.vue'), meta: { title: '总览' } },
        { path: 'devices', name: 'devices', component: () => import('./views/Devices.vue'), meta: { title: '设备' } },
        { path: 'devices/:id', name: 'device', component: () => import('./views/DeviceDetail.vue'), meta: { title: '设备详情' } },
        { path: 'devices/:id/activity', name: 'device-activity', component: () => import('./views/DeviceActivity.vue'), meta: { title: '设备活动' } },
        { path: 'events', name: 'events', component: () => import('./views/Events.vue'), meta: { title: '事件' } },
        { path: 'invocations', name: 'invocations', component: () => import('./views/Invocations.vue'), meta: { title: '指令记录' } },
        { path: 'releases', name: 'releases', component: () => import('./views/Releases.vue'), meta: { title: '发布' } },
        { path: 'releases/:id', name: 'release', component: () => import('./views/ReleaseDetail.vue'), meta: { title: '版本详情' } },
        { path: 'open-api', name: 'openapi', component: () => import('./views/OpenAPI.vue'), meta: { title: '开放 API' } },
        { path: 'audit', name: 'audit', component: () => import('./views/Audit.vue'), meta: { title: '审计日志' } },
        { path: 'tokens', name: 'tokens', component: () => import('./views/Tokens.vue'), meta: { title: '设备令牌' } },
        { path: 'account', name: 'account', component: () => import('./views/Account.vue'), meta: { title: '账号与安全' } },
        { path: 'settings', redirect: '/account' },
      ],
    },
    { path: '/:rest(.*)*', redirect: '/' },
  ],
})

// 是否需要首次初始化；只查一次，初始化完成后置为 false。
export const setupState = { checked: false, needed: false }

router.beforeEach(async (to) => {
  const auth = useAuth()
  await auth.restore()
  if (!auth.admin && !setupState.checked) {
    try { setupState.needed = (await api.setupStatus()).needs_setup } catch { /* 忽略 */ }
    setupState.checked = true
  }
  if (setupState.needed && to.name !== 'setup') return { name: 'setup' }
  if (!setupState.needed && to.name === 'setup') return { name: auth.admin ? 'dashboard' : 'login' }
  if (!to.meta.public && !auth.admin) return { name: 'login', query: to.fullPath !== '/' ? { next: to.fullPath } : {} }
  if (to.name === 'login' && auth.admin) return { name: 'dashboard' }
})

router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} · ResidentKit` : 'ResidentKit 控制台'
})

// 会话彻底失效（刷新也失败）时回到登录页。
setUnauthorizedHandler(() => {
  useAuth().clear()
  void router.push({ name: 'login' })
})
