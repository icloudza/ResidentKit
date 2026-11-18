<script setup lang="ts">
// 环形进度：中间显示数值与说明。
import { computed } from 'vue'

const props = defineProps<{ value: number; color: string; text: string; caption: string; size?: number }>()
const s = computed(() => props.size ?? 86)
const r = computed(() => s.value / 2 - 7)
const c = computed(() => 2 * Math.PI * r.value)
const off = computed(() => {
  const v = Math.max(0, Math.min(1, props.value))
  return c.value * (1 - (v > 0 && v < 0.02 ? 0.02 : v))
})
</script>

<template>
  <div class="ring" :style="{ width: `${s}px`, height: `${s}px` }">
    <svg :width="s" :height="s">
      <circle :cx="s / 2" :cy="s / 2" :r="r" :stroke="`color-mix(in srgb, ${color} 18%, transparent)`" stroke-width="8" fill="none" />
      <circle :cx="s / 2" :cy="s / 2" :r="r" :stroke="color" stroke-width="8" fill="none" stroke-linecap="round"
        :stroke-dasharray="c" :stroke-dashoffset="off" class="arc" />
    </svg>
    <div class="in"><b class="num">{{ text }}</b><span>{{ caption }}</span></div>
  </div>
</template>

<style scoped>
.ring { position: relative; flex: none; }
svg { transform: rotate(-90deg); }
.arc { transition: stroke-dashoffset .6s ease; }
.in { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; }
b { font-size: 15px; }
span { font-size: 11px; color: var(--sub); margin-top: 1px; }
</style>
