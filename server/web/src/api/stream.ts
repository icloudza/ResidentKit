// SSE 实时推送。EventSource 无法带 Authorization 头，所以用 fetch 读取流；断开后自动重连。
//
// 多个标签页共用一条连接：浏览器对同一主机最多 6 条 HTTP/1.1 连接（所有标签页共享），
// 每页各开一条长连接时，开几个控制台标签页就会把连接占满，其他请求排队，页面切换卡住。
// 所以用 Web Locks 选出一个标签页持有连接，事件经 BroadcastChannel 转发给其他标签页；
// 持有的标签页关闭后锁释放，另一个标签页自动接手。

import { ref } from 'vue'
import { getAccessToken, refresh } from './client'

export type StreamHandler = (topic: string, data: any) => void

const handlers = new Set<StreamHandler>()
let controller: AbortController | null = null
let lockAbort: AbortController | null = null
let running = false

// 实时通道是否已连上，供页面显示「实时」指示。
export const streamConnected = ref(false)
// 本次登录后是否连上过，用于区分「连接中」和「重连中」。
export const everConnected = ref(false)

type Msg = { kind: 'event'; topic: string; data: string } | { kind: 'status'; connected: boolean } | { kind: 'ask' }
const channel = typeof BroadcastChannel !== 'undefined' ? new BroadcastChannel('rk-stream') : null
let leader = false

channel?.addEventListener('message', (e: MessageEvent<Msg>) => {
  const m = e.data
  if (!running) return
  if (m.kind === 'event' && !leader) deliver(m.topic, m.data)
  else if (m.kind === 'status' && !leader) setConnected(m.connected)
  else if (m.kind === 'ask' && leader) channel?.postMessage({ kind: 'status', connected: streamConnected.value } satisfies Msg)
})

function setConnected(v: boolean) {
  streamConnected.value = v
  if (v) everConnected.value = true
}

export function onStream(h: StreamHandler): () => void {
  handlers.add(h)
  return () => handlers.delete(h)
}

export function startStream() {
  if (running) return
  running = true
  const locks = (navigator as Navigator & { locks?: LockManager }).locks
  if (!locks || !channel) { void loop(); return }
  // 先问一下当前持有连接的标签页的状态，再排队等锁。
  channel.postMessage({ kind: 'ask' } satisfies Msg)
  lockAbort = new AbortController()
  locks.request('rk-stream', { signal: lockAbort.signal }, async () => {
    leader = true
    try { await loop() } finally { leader = false }
  }).catch(() => { /* 退出登录时取消排队 */ })
}

export function stopStream() {
  running = false
  setConnected(false)
  controller?.abort()
  lockAbort?.abort()
}

function announce(connected: boolean) {
  setConnected(connected)
  if (leader) channel?.postMessage({ kind: 'status', connected } satisfies Msg)
}

async function loop() {
  let delay = 1000
  while (running) {
    controller = new AbortController()
    try {
      const res = await fetch('/api/v1/stream', {
        headers: { Authorization: `Bearer ${getAccessToken()}` },
        signal: controller.signal,
      })
      if (res.status === 401) {
        if (!(await refresh())) { running = false; announce(false); return }
        continue
      }
      if (!res.ok || !res.body) throw new Error(String(res.status))
      delay = 1000
      announce(true)
      const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
      let buf = ''
      for (;;) {
        const { value, done } = await reader.read()
        if (done) break
        buf += value
        let i: number
        while ((i = buf.indexOf('\n\n')) >= 0) {
          dispatch(buf.slice(0, i))
          buf = buf.slice(i + 2)
        }
      }
    } catch {
      if (!running) { announce(false); return }
    }
    announce(false)
    await new Promise((r) => setTimeout(r, delay))
    delay = Math.min(delay * 2, 30000)
  }
}

function dispatch(block: string) {
  let topic = ''
  let data = ''
  for (const line of block.split('\n')) {
    if (line.startsWith('event: ')) topic = line.slice(7)
    else if (line.startsWith('data: ')) data += line.slice(6)
  }
  if (!topic) return
  deliver(topic, data)
  if (leader) channel?.postMessage({ kind: 'event', topic, data } satisfies Msg)
}

function deliver(topic: string, data: string) {
  let parsed: unknown = null
  try { parsed = JSON.parse(data) } catch { /* 忽略 */ }
  handlers.forEach((h) => h(topic, parsed))
}
