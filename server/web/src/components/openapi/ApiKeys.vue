<script setup lang="ts">
// 开放 API · 密钥：每个密钥一张卡片，含权限、范围、最近使用与 24 小时调用走势。
import { computed } from 'vue'
import { NButton, NPopconfirm } from 'naive-ui'
import type { APIKey, APIStats, Device } from '../../api/types'
import Sparkline from '../Sparkline.vue'
import { deviceTitle, fmtAgo, fmtNum, fmtTime } from '../../format'
import { scopeMeta } from '../../openapi/scopes'

const props = defineProps<{ keys: APIKey[]; stats: APIStats | null; devices: Device[]; loading: boolean }>()
const emit = defineEmits<{ create: []; edit: [k: APIKey]; revoke: [k: APIKey]; remove: [k: APIKey]; calls: [k: APIKey] }>()

const deviceName = computed(() => Object.fromEntries(props.devices.map((d) => [d.id, deviceTitle(d)])))
const palette = ['var(--svc)', 'var(--purple)', 'var(--teal)', 'var(--indigo)', 'var(--green)', 'var(--orange)']
const color = (k: APIKey) => palette[k.id % palette.length]

const DAY = 86_400_000
function status(k: APIKey): { text: string; cls: string } {
  if (k.revoked_at) return { text: '已吊销', cls: 'p-wait' }
  if (k.expires_at) {
    const left = new Date(k.expires_at).getTime() - Date.now()
    if (left <= 0) return { text: '已过期', cls: 'p-err' }
    if (left < 14 * DAY) return { text: `${Math.ceil(left / DAY)} 天后到期`, cls: 'p-warn' }
  }
  return { text: '有效', cls: 'p-ok' }
}
const stat = (k: APIKey) => props.stats?.per_key[String(k.id)]
const rate = (k: APIKey) => { const s = stat(k); return s && s.calls ? `${((s.ok / s.calls) * 100).toFixed(1)}%` : '—' }
</script>

<template>
  <div v-if="loading" class="keys"><div v-for="i in 2" :key="i" class="card"><div class="skel" style="width:50%;height:20px" /><div class="skel" style="width:80%;margin-top:14px" /></div></div>
  <div v-else class="keys">
    <div v-for="k in keys" :key="k.id" class="card key" :class="{ revoked: !!k.revoked_at }">
      <div class="h">
        <span class="ic" :style="{ color: k.revoked_at ? 'var(--sub)' : color(k), background: `color-mix(in srgb, ${k.revoked_at ? 'var(--sub)' : color(k)} 12%, transparent)` }">{{ k.name.slice(0, 1) }}</span>
        <div class="nm"><div class="name">{{ k.name }}</div><div class="mono faint">{{ k.key_prefix }}…</div></div>
        <span class="pill" :class="status(k).cls">{{ status(k).text }}</span>
      </div>
      <div class="chips"><span v-for="s in k.scopes" :key="s" class="pill" :class="k.revoked_at ? 'p-wait' : 'p-run'">{{ scopeMeta[s]?.label ?? s }}</span></div>
      <dl class="meta">
        <dt>设备范围</dt><dd>{{ k.device_ids.length ? k.device_ids.map((d) => deviceName[d] ?? `#${d}`).join('、') : '全部设备' }}</dd>
        <dt>来源 IP</dt><dd :class="{ mono: k.allowed_ips.length }">{{ k.allowed_ips.length ? k.allowed_ips.join('、') : '不限' }}</dd>
        <dt>最近使用</dt><dd>{{ k.last_used_at ? fmtAgo(k.last_used_at) : '未使用' }}<span v-if="k.last_used_ip" class="mono faint"> · {{ k.last_used_ip }}</span></dd>
        <dt>{{ k.revoked_at ? '吊销于' : '到期' }}</dt>
        <dd class="num">{{ k.revoked_at ? fmtTime(k.revoked_at).slice(0, 16) : k.expires_at ? fmtTime(k.expires_at).slice(0, 10) : '永不' }}</dd>
      </dl>
      <div v-if="!k.revoked_at" class="use">
        <div class="ut"><span class="muted">24 小时调用</span><b class="num">{{ fmtNum(stat(k)?.calls ?? 0) }}</b><span class="muted">· 成功率 {{ rate(k) }}</span></div>
        <Sparkline :data="stat(k)?.hourly ?? []" :color="color(k)" />
      </div>
      <div class="foot">
        <template v-if="!k.revoked_at">
          <n-button size="small" secondary @click="emit('calls', k)">查看调用</n-button>
          <n-button size="small" secondary @click="emit('edit', k)">编辑</n-button>
          <div class="spacer" />
          <n-popconfirm @positive-click="emit('revoke', k)">
            <template #trigger><n-button size="small" quaternary type="error">吊销</n-button></template>
            吊销后使用该密钥的调用立即失败，确定？
          </n-popconfirm>
        </template>
        <template v-else>
          <n-button size="small" secondary @click="emit('calls', k)">查看调用</n-button>
          <div class="spacer" />
          <n-popconfirm @positive-click="emit('remove', k)">
            <template #trigger><n-button size="small" quaternary>删除</n-button></template>
            删除这条已吊销的密钥记录？不可恢复。
          </n-popconfirm>
        </template>
      </div>
    </div>
    <button class="card newkey" type="button" @click="emit('create')"><span class="plus">＋</span>创建密钥</button>
  </div>
</template>

<style scoped>
.keys { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 16px; }
.key { display: flex; flex-direction: column; gap: 12px; }
.key.revoked { opacity: .65; }
.h { display: flex; align-items: center; gap: 10px; }
.ic { width: 38px; height: 38px; border-radius: 11px; display: grid; place-items: center; font-weight: 700; font-size: 16px; flex: none; }
.nm { flex: 1; min-width: 0; }
.name { font-weight: 700; font-size: 15px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.chips { display: flex; gap: 6px; flex-wrap: wrap; }
.meta { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 6px 14px; margin: 0; font-size: 12.5px; }
.meta dt { color: var(--sub); }
.meta dd { margin: 0; overflow-wrap: anywhere; }
.use :deep(svg) { width: 100%; height: 34px; display: block; }
.ut { display: flex; gap: 6px; align-items: baseline; font-size: 12px; margin-bottom: 4px; }
.ut b { margin-left: auto; font-size: 13px; }
.foot { display: flex; gap: 8px; align-items: center; border-top: 1px solid var(--sep); padding-top: 12px; margin-top: auto; }
.newkey { border: 1.5px dashed var(--sep); box-shadow: none; background: transparent; color: var(--sub); min-height: 240px; cursor: pointer;
  display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px; font: inherit; font-size: 14px; }
.newkey:hover { border-color: var(--svc); color: var(--svc); }
.plus { font-size: 26px; line-height: 1; }
</style>
