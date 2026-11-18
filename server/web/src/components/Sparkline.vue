<script setup lang="ts">
// 迷你趋势线。
import { computed } from 'vue'

const props = defineProps<{ data: number[]; color: string }>()
const d = computed(() => {
  const v = props.data.length ? props.data : [0]
  const max = Math.max(1, ...v), n = Math.max(1, v.length - 1)
  return v.map((x, i) => `${i ? 'L' : 'M'}${((i / n) * 100).toFixed(1)} ${(27 - (x / max) * 24).toFixed(1)}`).join(' ')
})
</script>

<template>
  <svg viewBox="0 0 100 30" preserveAspectRatio="none" class="spark">
    <path :d="d" fill="none" :stroke="color" stroke-width="2" vector-effect="non-scaling-stroke" stroke-linejoin="round" />
  </svg>
</template>

<style scoped>
.spark { width: 100%; height: 30px; display: block; margin-top: 8px; }
</style>
