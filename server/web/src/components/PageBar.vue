<script setup lang="ts">
// 页面顶部标题栏：标题、面包屑、页面操作、实时状态，滚动时吸顶。
import { onMounted, onUnmounted, ref } from 'vue'
import { everConnected, streamConnected } from '../api/stream'

// 把标题栏高度写入 --bar-h，表头据此吸顶在标题栏下方。
const el = ref<HTMLElement | null>(null)
let ro: ResizeObserver | undefined
onMounted(() => {
  ro = new ResizeObserver(() => {
    document.documentElement.style.setProperty('--bar-h', `${el.value?.offsetHeight ?? 0}px`)
  })
  if (el.value) ro.observe(el.value)
})
onUnmounted(() => ro?.disconnect())

defineProps<{ title: string; crumb?: string; crumbTo?: string; status?: string }>()
</script>

<template>
  <header ref="el" class="bar">
    <div class="titles">
      <router-link v-if="crumb" :to="crumbTo || '/'" class="crumb">{{ crumb }} ›</router-link>
      <h2>{{ title }}</h2>
    </div>
    <div class="spacer" />
    <slot />
    <div class="live" :title="streamConnected ? '实时推送已连接' : '实时推送未连接，正在重连'">
      <span v-if="streamConnected" class="pulse" /><span v-else class="dot off" />
      {{ status ?? (streamConnected ? '实时' : everConnected ? '重连中' : '连接中') }}
    </div>
  </header>
</template>

<style scoped>
.bar {
  position: sticky; top: 0; z-index: 10; display: flex; align-items: center; gap: 12px;
  padding: 16px 28px; max-width: 1240px; width: 100%; margin: 0 auto;
  background: color-mix(in srgb, var(--bg) 86%, transparent); backdrop-filter: blur(14px);
}
.titles { min-width: 0; }
h2 { font-size: 24px; font-weight: 700; letter-spacing: -.3px; margin: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.crumb { font-size: 13px; color: var(--sub); text-decoration: none; }
.crumb:hover { color: var(--svc); }
.live {
  display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--sub); white-space: nowrap;
  background: var(--card); border: 1px solid var(--sep); border-radius: 99px; padding: 6px 10px;
}
@media (max-width: 760px) {
  .bar { padding: 12px 16px; }
  h2 { font-size: 22px; }
}
</style>
