<script setup lang="ts">
// 设备活动柱状图：每个时间桶两根柱（指令、事件），失败 / 告警部分用深色叠在底部；悬停显示数值（纯 CSS，无图表库）。
import { computed } from 'vue'
import type { ActivityBucket } from '../api/types'

const props = defineProps<{ buckets: ActivityBucket[]; daily: boolean }>()

const max = computed(() => {
  const m = Math.max(1, ...props.buckets.map((b) => Math.max(b.invocations, b.events)))
  const step = 10 ** Math.floor(Math.log10(m))
  return Math.ceil(m / step) * step
})
const ticks = computed(() => [0, max.value / 2, max.value])
const pct = (n: number) => `${(n / max.value) * 100}%`
const label = (at: string) => {
  const d = new Date(at)
  return props.daily ? `${d.getMonth() + 1}-${String(d.getDate()).padStart(2, '0')}` : `${String(d.getHours()).padStart(2, '0')}:00`
}
const every = computed(() => (props.buckets.length > 12 ? Math.ceil(props.buckets.length / 6) : 1))
const fmt = (n: number) => (n >= 1000 ? `${+(n / 1000).toFixed(1)}k` : String(n))
</script>

<template>
  <div class="chart">
    <div class="plot">
      <template v-for="t in ticks" :key="t">
        <div class="gl" :style="{ bottom: pct(t) }" />
        <div class="yl" :style="{ bottom: pct(t) }">{{ fmt(t) }}</div>
      </template>
      <div class="cols">
        <div v-for="(b, i) in buckets" :key="b.at" class="col">
          <div class="pair">
            <div class="bar inv" :class="{ z: !b.invocations }" :style="{ height: b.invocations ? pct(b.invocations) : '2px' }">
              <div v-if="b.failed" class="bad" :style="{ height: `${(b.failed / b.invocations) * 100}%` }" />
            </div>
            <div class="bar ev" :class="{ z: !b.events }" :style="{ height: b.events ? pct(b.events) : '2px' }">
              <div v-if="b.warnings" class="bad" :style="{ height: `${(b.warnings / b.events) * 100}%` }" />
            </div>
          </div>
          <div class="tip">
            <b>{{ label(b.at) }}</b>
            <span>指令 {{ b.invocations }}<template v-if="b.failed"> · 失败 {{ b.failed }}</template></span>
            <span>事件 {{ b.events }}<template v-if="b.warnings"> · 告警 {{ b.warnings }}</template></span>
          </div>
          <div v-if="i % every === 0" class="x">{{ label(b.at) }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.chart { position: relative; height: 220px; }
.plot { position: absolute; left: 34px; right: 0; top: 10px; bottom: 22px; }
.gl { position: absolute; left: 0; right: 0; border-top: 1px dashed var(--sep); }
.yl { position: absolute; left: -34px; width: 28px; text-align: right; font-size: 11px; color: var(--faint); transform: translateY(50%); font-variant-numeric: tabular-nums; }
.cols { position: absolute; inset: 0; display: flex; gap: 6px; }
.col { flex: 1; min-width: 0; position: relative; display: flex; align-items: flex-end; }
.pair { width: 100%; height: 100%; display: flex; align-items: flex-end; gap: 2px; }
.bar { flex: 1; position: relative; border-radius: 4px 4px 2px 2px; transition: height .4s, opacity .15s; opacity: .85; overflow: hidden; display: flex; align-items: flex-end; }
.bar.inv { background: var(--svc); }
.bar.ev { background: var(--purple); }
.bar.z { background: var(--fill); opacity: 1; }
.bad { width: 100%; background: var(--red); }
.bar.ev .bad { background: var(--orange); }
.col:hover .bar { opacity: 1; }
.x { position: absolute; bottom: -20px; left: 0; font-size: 11px; color: var(--faint); white-space: nowrap; font-variant-numeric: tabular-nums; }
.tip {
  position: absolute; bottom: calc(100% + 6px); left: 50%; transform: translateX(-50%); z-index: 2; pointer-events: none;
  display: none; flex-direction: column; gap: 2px; white-space: nowrap; font-size: 12px; padding: 7px 10px; border-radius: 9px;
  background: var(--card); box-shadow: 0 4px 16px rgba(0, 0, 0, .12); color: var(--text);
}
.col:hover .tip { display: flex; }
.col:first-child .tip { left: 0; transform: none; }
.col:last-child .tip { left: auto; right: 0; transform: none; }
</style>
