<script setup lang="ts">
// JSON 语法高亮（先转义再着色，不信任内容）。
import { computed } from 'vue'

const props = defineProps<{ value: unknown }>()
const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
const html = computed(() => {
  if (props.value === undefined || props.value === null) return '<span class="jn">—</span>'
  const text = esc(JSON.stringify(props.value, null, 2))
  return text.replace(/("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g,
    (m, str, colon, lit) => {
      if (str) return colon ? `<span class="jk">${str}</span>${colon}` : `<span class="js">${str}</span>`
      if (lit) return `<span class="jb">${m}</span>`
      return `<span class="jn">${m}</span>`
    })
})
</script>

<template>
  <pre class="json" v-html="html" />
</template>

<style scoped>
.json {
  margin: 0; padding: 10px 12px; border-radius: 10px; background: var(--fill); max-height: 320px; overflow: auto;
  font-family: ui-monospace, "SF Mono", Menlo, monospace; font-size: 12px; line-height: 1.6; white-space: pre-wrap; word-break: break-all;
}
.json :deep(.jk) { color: var(--purple); }
.json :deep(.js) { color: var(--green); }
.json :deep(.jn) { color: var(--orange); }
.json :deep(.jb) { color: var(--svc); }
</style>
