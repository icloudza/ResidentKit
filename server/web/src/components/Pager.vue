<script setup lang="ts">
// 分页栏：总条数、页码（带省略）、每页条数。
import { computed } from 'vue'
import { NSelect } from 'naive-ui'

const props = defineProps<{ total: number; sizes?: number[] }>()
const page = defineModel<number>('page', { required: true })
const size = defineModel<number>('size', { required: true })

const pages = computed(() => Math.max(1, Math.ceil(props.total / size.value)))
// 当前页附近 ±2，加首尾页，中间用 0 表示省略号。
const items = computed(() => {
  const n = pages.value, c = page.value, out: number[] = []
  for (let i = 1; i <= n; i++) {
    if (i === 1 || i === n || Math.abs(i - c) <= 2) out.push(i)
    else if (out[out.length - 1] !== 0) out.push(0)
  }
  return out
})
const from = computed(() => (props.total ? (page.value - 1) * size.value + 1 : 0))
const to = computed(() => Math.min(props.total, page.value * size.value))

function go(p: number) { if (p >= 1 && p <= pages.value && p !== page.value) page.value = p }
const sizeOptions = computed(() => (props.sizes ?? [20, 50, 100]).map((s) => ({ label: `${s} 条/页`, value: s })))
function setSize(v: number) {
  size.value = v
  page.value = 1
}
</script>

<template>
  <div v-if="total > 0" class="pager">
    <span class="muted num">{{ from }}–{{ to }}，共 {{ total.toLocaleString('zh-CN') }} 条</span>
    <span class="spacer" />
    <button class="pb" :disabled="page <= 1" @click="go(page - 1)">‹</button>
    <template v-for="(p, i) in items" :key="i">
      <span v-if="p === 0" class="dots">…</span>
      <button v-else class="pb num" :class="{ on: p === page }" @click="go(p)">{{ p }}</button>
    </template>
    <button class="pb" :disabled="page >= pages" @click="go(page + 1)">›</button>
    <n-select class="size" :value="size" :options="sizeOptions" size="small" @update:value="setSize" />
  </div>
</template>

<style scoped>
.pager { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; padding-top: 14px; border-top: 1px solid var(--sep); margin-top: 4px; font-size: 13px; }
.pb {
  min-width: 32px; height: 32px; padding: 0 8px; border: 0; border-radius: 8px; background: transparent;
  color: var(--text); font: inherit; font-size: 13px; cursor: pointer;
}
.pb:hover:not(:disabled) { background: var(--fill); }
.pb.on { background: var(--svc); color: #fff; font-weight: 600; }
.pb:disabled { color: var(--faint); cursor: default; }
.dots { color: var(--faint); padding: 0 4px; }
.size { margin-left: 6px; width: 110px; }
@media (max-width: 760px) { .pager .muted { width: 100%; } .spacer { display: none; } }
</style>
