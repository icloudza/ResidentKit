<script setup lang="ts">
// 轻量代码高亮（curl / Python / Go / Node.js / JSON）：先切分词法单元再逐段转义，不信任内容。
import { computed } from 'vue'

export type CodeLang = 'bash' | 'python' | 'go' | 'js' | 'json'
const props = defineProps<{ code: string; lang: CodeLang }>()

const keywords: Record<CodeLang, string[]> = {
  bash: ['curl'],
  python: ['import', 'from', 'as', 'if', 'else', 'elif', 'for', 'in', 'while', 'def', 'return', 'raise', 'try', 'except', 'with', 'not', 'and', 'or', 'None', 'True', 'False'],
  go: ['package', 'import', 'func', 'var', 'const', 'type', 'struct', 'map', 'if', 'else', 'for', 'range', 'return', 'defer', 'go', 'nil', 'err', 'true', 'false', 'interface', 'any'],
  js: ['const', 'let', 'var', 'await', 'async', 'function', 'return', 'if', 'else', 'throw', 'new', 'import', 'from', 'export', 'true', 'false', 'null', 'undefined'],
  json: ['true', 'false', 'null'],
}

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

const html = computed(() => {
  const lang = props.lang
  const comment = lang === 'go' || lang === 'js' ? String.raw`\/\/[^\n]*` : lang === 'json' ? String.raw`(?!)` : String.raw`(?<=^|\s)#[^\n]*`
  const str = lang === 'go' || lang === 'js' ? String.raw`"(?:\\.|[^"\\\n])*"|'(?:\\.|[^'\\\n])*'|\x60[^\x60]*\x60` : String.raw`"(?:\\.|[^"\\\n])*"|'(?:\\.|[^'\\\n])*'`
  const flag = lang === 'bash' ? String.raw`(?<=\s)--?[A-Za-z][\w-]*` : String.raw`(?!)`
  const re = new RegExp(`(${comment})|(${str})(\\s*:)?|(${flag})|\\b(-?\\d+(?:\\.\\d+)?)\\b|\\b([A-Za-z_][\\w]*)\\b(\\s*\\()?`, 'g')
  const kw = new Set(keywords[lang])
  let out = ''
  let last = 0
  for (const m of props.code.matchAll(re)) {
    const i = m.index ?? 0
    out += esc(props.code.slice(last, i))
    last = i + m[0].length
    const [all, cmt, s, colon, fl, num, word, call] = m
    if (cmt) out += `<span class="c">${esc(cmt)}</span>`
    else if (s) out += colon ? `<span class="k">${esc(s)}</span>${esc(colon)}` : `<span class="s">${esc(s)}</span>`
    else if (fl) out += `<span class="f">${esc(fl)}</span>`
    else if (num) out += `<span class="n">${esc(num)}</span>`
    else if (word && kw.has(word)) out += `<span class="w">${esc(word)}</span>${esc(call ?? '')}`
    else if (word && call) out += `<span class="fn">${esc(word)}</span>${esc(call)}`
    else out += esc(all)
  }
  return out + esc(props.code.slice(last))
})
</script>

<template>
  <pre class="code" v-html="html" />
</template>

<style scoped>
.code {
  margin: 0; background: var(--fill); border-radius: 10px; padding: 12px 14px; font-size: 12px; line-height: 1.6;
  font-family: ui-monospace, "SF Mono", Menlo, monospace; overflow-x: auto; white-space: pre; max-width: 100%;
}
.code :deep(.c) { color: var(--faint); font-style: italic; }
.code :deep(.s) { color: var(--green); }
.code :deep(.k) { color: var(--purple); }
.code :deep(.n) { color: var(--orange); }
.code :deep(.w) { color: var(--svc); font-weight: 600; }
.code :deep(.fn) { color: var(--purple); }
.code :deep(.f) { color: var(--orange); }
</style>
