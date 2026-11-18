<script setup lang="ts">
// 外框：桌面为分组侧边栏，手机为底部标签栏 +「更多」面板。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NDrawer } from 'naive-ui'
import { useAuth } from '../stores/auth'
import Icon, { paths } from '../components/Icon.vue'
import EnrollPrompt from '../components/EnrollPrompt.vue'
import { onStream } from '../api/stream'
import { loadCommandCatalog } from '../commands'

type Nav = { name: string; label: string; icon: keyof typeof paths }

const auth = useAuth()
const route = useRoute()
const router = useRouter()

const groups: { title?: string; items: Nav[] }[] = [
  { items: [{ name: 'dashboard', label: '总览', icon: 'dashboard' }] },
  { title: '设备', items: [
    { name: 'devices', label: '设备', icon: 'device' },
    { name: 'events', label: '事件', icon: 'pulse' },
    { name: 'invocations', label: '指令记录', icon: 'terminal' },
  ] },
  { title: '系统', items: [
    { name: 'releases', label: '发布', icon: 'upload' },
    { name: 'openapi', label: '开放 API', icon: 'code' },
    { name: 'audit', label: '审计日志', icon: 'shield' },
    { name: 'tokens', label: '设备令牌', icon: 'key' },
    { name: 'account', label: '账号与安全', icon: 'user' },
  ] },
]
const tabs: Nav[] = [
  { name: 'dashboard', label: '总览', icon: 'dashboard' },
  { name: 'devices', label: '设备', icon: 'device' },
  { name: 'events', label: '事件', icon: 'pulse' },
  { name: 'invocations', label: '指令', icon: 'terminal' },
]
const moreItems = groups[2].items

const active = computed(() => (route.name === 'device' || route.name === 'device-activity' ? 'devices' : route.name === 'release' ? 'releases' : String(route.name)))
const moreActive = computed(() => moreItems.some((i) => i.name === active.value))
const showMore = ref(false)

const kept = ['Dashboard', 'Devices', 'Events', 'Invocations', 'Releases', 'OpenAPI', 'Audit', 'Tokens']

// 空闲时预取其他页面的代码，第一次点开时不用再等下载。
// 命令名称来自设备上报的说明：登录后加载一次，设备上线（可能换了版本）时刷新。
let offCatalog = () => {}
onMounted(() => {
  void loadCommandCatalog()
  offCatalog = onStream((topic) => { if (topic === 'device.online') void loadCommandCatalog() })
})
onUnmounted(() => offCatalog())
onMounted(() => {
  const idle = (window as Window & { requestIdleCallback?: (cb: () => void) => void }).requestIdleCallback ?? ((cb: () => void) => setTimeout(cb, 800))
  idle(() => {
    for (const r of router.getRoutes()) {
      const c = r.components?.default
      if (typeof c === 'function') void (c as () => Promise<unknown>)().catch(() => {})
    }
  })
})

async function logout() {
  await auth.logout()
  await router.replace({ name: 'login' })
}
function go(name: string) {
  showMore.value = false
  void router.push({ name })
}
</script>

<template>
  <div class="shell">
    <aside class="side">
      <div class="brand"><img src="/logo.png" alt="" class="logo" />ResidentKit</div>
      <template v-for="g in groups" :key="g.title ?? 'top'">
        <div v-if="g.title" class="group">{{ g.title }}</div>
        <router-link v-for="n in g.items" :key="n.name" :to="{ name: n.name }" class="nav" :class="{ on: active === n.name }">
          <Icon :name="n.icon" />{{ n.label }}
        </router-link>
      </template>
      <div class="me">
        <div class="av">{{ auth.admin?.username.slice(0, 1).toUpperCase() }}</div>
        <div class="who">
          <div class="n">{{ auth.admin?.username }}</div>
          <div class="r">{{ auth.admin?.totp_enabled ? '两步验证已开启' : '未开启两步验证' }}</div>
        </div>
        <button class="out" title="退出登录" @click="logout"><Icon name="logout" :size="16" /></button>
      </div>
    </aside>

    <main class="main">
      <!-- 列表类页面保活：来回切换不重建、不闪骨架屏，回到页面时静默刷新（useRevisit）。
           key 用 path 而不是 fullPath：只改查询参数（标签、筛选）时不重建整个页面。 -->
      <router-view v-slot="{ Component }">
        <keep-alive :include="kept" :max="10">
          <component :is="Component" :key="route.path" />
        </keep-alive>
      </router-view>
    </main>

    <nav class="tabbar">
      <a v-for="t in tabs" :key="t.name" :class="{ on: active === t.name }" @click="go(t.name)">
        <Icon :name="t.icon" :size="22" />{{ t.label }}
      </a>
      <a :class="{ on: moreActive }" @click="showMore = true"><Icon name="more" :size="22" />更多</a>
    </nav>

    <n-drawer v-model:show="showMore" placement="bottom" :height="'auto'" class="sheet">
      <div class="sheet-body">
        <div class="sheet-user"><div class="av">{{ auth.admin?.username.slice(0, 1).toUpperCase() }}</div>{{ auth.admin?.username }}</div>
        <a v-for="n in moreItems" :key="n.name" class="nav" :class="{ on: active === n.name }" @click="go(n.name)">
          <Icon :name="n.icon" />{{ n.label }}</a>
        <a class="nav danger" @click="logout"><Icon name="logout" />退出登录</a>
      </div>
    </n-drawer>

  </div>
  <EnrollPrompt />
</template>

<style scoped>
.shell { display: flex; min-height: 100vh; }
.side {
  width: 232px; flex: none; position: sticky; top: 0; height: 100vh; padding: 18px 12px;
  display: flex; flex-direction: column; gap: 2px; background: var(--card); border-right: 1px solid var(--sep);
}
.brand { display: flex; align-items: center; gap: 10px; padding: 2px 8px 16px; font-weight: 700; font-size: 16px; }
.logo { width: 30px; height: 30px; border-radius: 8px; }
.group { font-size: 11px; color: var(--faint); font-weight: 600; padding: 14px 10px 6px; letter-spacing: .3px; }
.nav {
  display: flex; align-items: center; gap: 10px; padding: 9px 10px; border-radius: 10px;
  font-size: 14px; font-weight: 500; color: var(--sub); text-decoration: none; cursor: pointer;
}
.nav:hover { background: var(--fill); color: var(--text); }
.nav.on { background: var(--svc-soft); color: var(--svc); font-weight: 600; }
.nav.danger { color: var(--red); }
.me { margin-top: auto; display: flex; align-items: center; gap: 10px; padding: 10px; border-radius: 12px; background: var(--fill); }
.av { width: 30px; height: 30px; border-radius: 50%; background: var(--header); color: var(--svc); display: grid; place-items: center; font-weight: 700; font-size: 13px; flex: none; }
.who { flex: 1; min-width: 0; }
.who .n { font-size: 13px; font-weight: 600; }
.who .r { font-size: 11px; color: var(--sub); }
.out { border: 0; background: transparent; color: var(--sub); cursor: pointer; padding: 6px; border-radius: 8px; display: grid; }
.out:hover { background: var(--card); color: var(--red); }
.main { flex: 1; min-width: 0; padding-bottom: 24px; }
.tabbar { display: none; }

@media (max-width: 760px) {
  .side { display: none; }
  .main { padding-bottom: 84px; }
  .tabbar {
    display: flex; position: fixed; left: 0; right: 0; bottom: 0; z-index: 20;
    padding: 6px 6px calc(8px + env(safe-area-inset-bottom));
    background: color-mix(in srgb, var(--card) 92%, transparent); backdrop-filter: blur(14px); border-top: 1px solid var(--sep);
  }
  .tabbar a { flex: 1; display: flex; flex-direction: column; align-items: center; gap: 3px; font-size: 10.5px; color: var(--sub); padding: 4px 0; cursor: pointer; }
  .tabbar a.on { color: var(--svc); }
}
.sheet-body { padding: 16px 16px calc(20px + env(safe-area-inset-bottom)); display: flex; flex-direction: column; gap: 4px; }
.sheet-user { display: flex; align-items: center; gap: 10px; font-weight: 600; padding: 4px 10px 12px; }
</style>
