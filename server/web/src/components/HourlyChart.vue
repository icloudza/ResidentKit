<script setup lang="ts">
// 近 24 小时柱状图：Y 轴刻度、悬停显示具体数值（纯 SVG/CSS，无图表库）。
import { computed } from 'vue'

const props = defineProps<{ data: { hour: string; count: number }[]; unit?: string; fill?: boolean }>()

const max = computed(() => {
  const m = Math.max(1, ...props.data.map((d) => d.count))
  const step = Math.pow(10, Math.floor(Math.log10(m)))
  return Math.ceil(m / step) * step
})
// 只用整数刻度：中间值不是整数时只画 0 和最大值。
const ticks = computed(() => (Number.isInteger(max.value / 2) ? [0, max.value / 2, max.value] : [0, max.value]))
const hh = (s: string) => new Date(s).getHours().toString().padStart(2, '0')
</script>

<template>
  <div class="chart" :class="{ fill }">
    <div class="plot">
      <template v-for="t in ticks" :key="t">
        <div class="gl" :style="{ bottom: `${(t / max) * 100}%` }" />
        <div class="yl" :style="{ bottom: `${(t / max) * 100}%` }">{{ t >= 1000 ? `${t / 1000}k` : t }}</div>
      </template>
      <div class="bars">
        <div v-for="(d, i) in data" :key="d.hour" class="col">
          <div class="b" :class="{ z: !d.count }" :style="{ height: `${Math.max(d.count ? 3 : 1.5, (d.count / max) * 100)}%` }" />
          <div class="tip">{{ hh(d.hour) }}:00 · {{ d.count }} {{ unit ?? '条' }}</div>
          <div v-if="i % 4 === 0" class="x">{{ hh(d.hour) }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.chart { position: relative; height: 200px; }
/* 放在可伸展的卡片里时填满剩余高度 */
.chart.fill { height: auto; flex: 1; min-height: 200px; }
.plot { position: absolute; left: 34px; right: 0; top: 10px; bottom: 22px; }
.gl { position: absolute; left: 0; right: 0; border-top: 1px dashed var(--sep); }
.yl { position: absolute; left: -34px; width: 28px; text-align: right; font-size: 11px; color: var(--faint); transform: translateY(50%); font-variant-numeric: tabular-nums; }
.bars { position: absolute; inset: 0; display: flex; align-items: flex-end; gap: 5px; }
.col { flex: 1; height: 100%; display: flex; align-items: flex-end; position: relative; }
.b { width: 100%; border-radius: 5px 5px 2px 2px; background: var(--svc); opacity: .85; transition: opacity .15s, height .4s; }
.b.z { background: var(--fill); opacity: 1; }
.col:hover .b { opacity: 1; }
.x { position: absolute; bottom: -20px; left: 0; font-size: 11px; color: var(--faint); font-variant-numeric: tabular-nums; }
.tip {
  position: absolute; bottom: calc(100% + 8px); left: 50%; transform: translateX(-50%); z-index: 2; display: none;
  background: var(--text); color: var(--bg); font-size: 12px; padding: 5px 8px; border-radius: 7px; white-space: nowrap;
}
.col:hover .tip { display: block; }
</style>
