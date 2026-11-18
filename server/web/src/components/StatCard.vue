<script setup lang="ts">
// 统计卡：图标、数值、补充说明（涨跌或单位）与迷你趋势线。
import Icon, { paths } from './Icon.vue'
import Sparkline from './Sparkline.vue'

defineProps<{
  label: string; icon: keyof typeof paths; color: string
  value: string; note?: string; noteClass?: string; trend?: number[]; loading?: boolean
}>()
</script>

<template>
  <div class="card stat">
    <div class="lab">
      <span class="ic" :style="{ background: `color-mix(in srgb, ${color} 14%, transparent)`, color }"><Icon :name="icon" :size="16" /></span>
      {{ label }}
    </div>
    <div v-if="loading" class="skel" style="width:50%;height:26px;margin-top:12px" />
    <div v-else class="v num">{{ value }}<small v-if="note" :class="noteClass">{{ note }}</small></div>
    <Sparkline v-if="trend" :data="trend" :color="color" />
  </div>
</template>

<style scoped>
.lab { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--sub); font-weight: 500; }
.ic { width: 28px; height: 28px; border-radius: 8px; display: grid; place-items: center; }
.v { font-size: 28px; font-weight: 700; margin-top: 10px; letter-spacing: -.4px; display: flex; align-items: baseline; gap: 8px; }
.v small { font-size: 13px; font-weight: 600; color: var(--sub); }
.v small.up { color: var(--green); }
.v small.down { color: var(--red); }
@media (max-width: 760px) { .v { font-size: 24px; } }
</style>
