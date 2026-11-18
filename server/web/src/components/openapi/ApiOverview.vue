<script setup lang="ts">
// 开放 API · 概览：24 小时调用量、成功率、耗时、走势、热门命令与快速接入。
import { computed } from 'vue'
import { commandMeta } from '../../commands'
import { useMessage } from 'naive-ui'
import type { APIKey, APIStats, OpenAPIInfo } from '../../api/types'
import StatCard from '../StatCard.vue'
import { fmtNum } from '../../format'

const props = defineProps<{ stats: APIStats | null; info: OpenAPIInfo | null; keys: APIKey[] }>()
const emit = defineEmits<{ create: []; docs: [] }>()
const message = useMessage()

const s = computed(() => props.stats)
const base = computed(() => props.info?.base_url || '')
const done = computed(() => (s.value ? s.value.succeeded + s.value.failed + s.value.lost : 0))
const rate = computed(() => (done.value ? (s.value!.succeeded / done.value) * 100 : 0))
const change = computed(() => {
  if (!s.value || !s.value.calls_prev) return ''
  const d = ((s.value.calls - s.value.calls_prev) / s.value.calls_prev) * 100
  return `${d >= 0 ? '↑' : '↓'} ${Math.abs(d).toFixed(0)}% 较前一天`
})
const max = computed(() => Math.max(1, ...(s.value?.hourly ?? []).map((h) => h.total)))
const topMax = computed(() => Math.max(1, ...(s.value?.top ?? []).map((t) => t.count)))
const hourLabel = (iso: string) => new Date(iso).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })

async function copy(v: string) {
  await navigator.clipboard.writeText(v)
  message.success('已复制')
}
</script>

<template>
  <div class="stats">
    <StatCard label="24 小时调用" icon="code" color="var(--svc)" :loading="!s" :value="s ? fmtNum(s.calls) : '—'"
      :note="change || '近 24 小时'" :note-class="change.startsWith('↓') ? 'down' : change ? 'up' : ''" :trend="s?.hourly.map((h) => h.total)" />
    <StatCard label="成功率" icon="check" color="var(--green)" :loading="!s" :value="done ? `${rate.toFixed(1)}%` : '—'"
      :note="s ? `失败 ${s.failed} · 结果未知 ${s.lost}${s.running ? ` · 进行中 ${s.running}` : ''}` : ''" />
    <StatCard label="平均耗时（下发到结果）" icon="clock" color="var(--teal)" :loading="!s"
      :value="s && s.avg_ms ? `${Math.round(s.avg_ms)} ms` : '—'" :note="s && s.p95_ms ? `p95 ${Math.round(s.p95_ms)} ms` : '只统计成功的调用'" />
    <StatCard label="有效密钥" icon="key" color="var(--purple)" :loading="!s" :value="s ? String(s.active_keys) : '—'"
      :note="s ? `24 小时内活跃 ${s.used_keys}` : ''" />
  </div>

  <div class="two">
    <div class="card">
      <div class="card-title"><h3>调用量</h3><span class="hint">近 24 小时 · 按小时</span>
        <div class="legend"><span><i class="lg ok" />成功</span><span><i class="lg bad" />失败 / 未知</span></div></div>
      <div v-if="s && s.calls" class="chart">
        <div v-for="h in s.hourly" :key="h.hour" class="col" :title="`${hourLabel(h.hour)}　共 ${h.total}，成功 ${h.ok}，失败 ${h.bad}`">
          <div class="stack" :style="{ height: `${(h.total / max) * 100}%` }">
            <i class="bad" :style="{ flex: h.bad }" /><i class="run" :style="{ flex: h.total - h.ok - h.bad }" /><i class="ok" :style="{ flex: h.ok }" />
          </div>
        </div>
      </div>
      <div v-else class="empty">近 24 小时还没有调用</div>
      <div v-if="s && s.calls" class="axis num"><span>{{ hourLabel(s.hourly[0].hour) }}</span><span>{{ hourLabel(s.hourly[12].hour) }}</span><span>现在</span></div>
    </div>
    <div class="card">
      <div class="card-title"><h3>调用最多的命令</h3><span class="hint">24 小时</span></div>
      <div v-if="!s?.top.length" class="empty">暂无</div>
      <div v-for="t in s?.top ?? []" :key="t.module + t.command" class="rank">
        <span class="cmd">{{ commandMeta(t.module, t.command).label }} <span class="mono faint">{{ t.module }} · {{ t.command }}</span></span>
        <span class="rbar"><i :style="{ width: `${(t.count / topMax) * 100}%` }" /></span>
        <span class="num muted cnt">{{ fmtNum(t.count) }}</span>
      </div>
    </div>
  </div>

  <div class="card">
    <div class="card-title"><h3>快速接入</h3>
      <a class="link" href="/api/v1/api-keys/openapi.json" download>下载 OpenAPI 规范</a></div>
    <div class="steps">
      <div class="step clickable" @click="emit('create')"><b>1</b><span class="st">创建密钥</span>
        <span class="sd">按需勾选权限，限定设备和来源 IP；明文只显示一次</span></div>
      <div class="step clickable" @click="emit('docs')"><b>2</b><span class="st">发第一个请求</span>
        <span class="sd">在「接口文档」里直接调试，或复制示例代码</span></div>
      <div class="step"><b>3</b><span class="st">查看调用</span>
        <span class="sd">每次调用都有记录：参数、结果、耗时、来源 IP</span></div>
    </div>
    <dl class="kv">
      <dt>接口地址</dt>
      <dd><code class="mono">{{ base || '未配置（服务器配置 gateway.public_url）' }}</code>
        <a v-if="base" class="cp" @click="copy(base)">复制</a></dd>
      <dt>鉴权</dt><dd><code class="mono">Authorization: Bearer rkk_…</code></dd>
      <dt>响应</dt><dd class="muted"><code class="mono">{code, msg, data}</code>，code 为 0 表示成功</dd>
    </dl>
  </div>
</template>

<style scoped>
.stats { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px; }
.two { display: grid; grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr); gap: 16px; }
.hint { color: var(--faint); font-size: 12px; }
.legend { margin-left: auto; display: flex; gap: 14px; font-size: 12px; color: var(--sub); }
.lg { display: inline-block; width: 8px; height: 8px; border-radius: 2px; margin-right: 5px; }
.lg.ok { background: var(--svc); }
.lg.bad { background: var(--red); }
.chart { display: flex; align-items: flex-end; gap: 3px; height: 150px; }
.col { flex: 1; height: 100%; display: flex; align-items: flex-end; min-width: 0; }
.stack { width: 100%; display: flex; flex-direction: column; border-radius: 3px 3px 0 0; overflow: hidden; min-height: 2px; }
.stack i { display: block; }
.stack .ok { background: var(--svc); opacity: .85; }
.stack .run { background: var(--faint); }
.stack .bad { background: var(--red); }
.axis { display: flex; justify-content: space-between; font-size: 11px; color: var(--faint); margin-top: 6px; }
.rank { display: flex; align-items: center; gap: 10px; padding: 9px 0; border-top: 1px solid var(--sep); }
.rank:first-of-type { border-top: 0; }
.cmd { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rbar { width: 64px; height: 6px; background: var(--fill); border-radius: 3px; overflow: hidden; flex: none; }
.rbar i { display: block; height: 100%; background: var(--svc); opacity: .75; }
.cnt { width: 56px; text-align: right; font-size: 12px; flex: none; }
.link { margin-left: auto; color: var(--svc); font-size: 12.5px; text-decoration: none; }
.steps { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.step { background: var(--fill); border-radius: 14px; padding: 14px; display: flex; flex-direction: column; gap: 4px; }
.step.clickable:hover { background: var(--svc-soft); }
.step b { width: 22px; height: 22px; border-radius: 50%; background: var(--svc-soft); color: var(--svc); display: grid; place-items: center; font-size: 12px; margin-bottom: 4px; }
.st { font-weight: 600; font-size: 13.5px; }
.sd { font-size: 12px; color: var(--sub); }
.kv { display: grid; grid-template-columns: 80px minmax(0, 1fr); gap: 10px 14px; margin: 16px 0 0; font-size: 13px; align-items: baseline; }
.kv dt { color: var(--sub); }
.kv dd { margin: 0; min-width: 0; overflow-wrap: anywhere; }
code { font-size: 12px; background: var(--fill); padding: 2px 7px; border-radius: 6px; }
.cp { margin-left: 8px; color: var(--svc); cursor: pointer; font-size: 12px; }
@media (max-width: 900px) {
  .stats { grid-template-columns: 1fr 1fr; }
  .two, .steps { grid-template-columns: 1fr; }
}
</style>
