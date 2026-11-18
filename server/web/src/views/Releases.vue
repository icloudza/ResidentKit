<script setup lang="ts">
// 发布：上传已签名的安装包，查看各版本的更新进度。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NInput, NModal, NProgress, NSwitch, useMessage } from 'naive-ui'
import { api, ApiError } from '../api/client'
import { onStream } from '../api/stream'
import type { OTAConfig, Release } from '../api/types'
import { useAuth } from '../stores/auth'
import PageBar from '../components/PageBar.vue'
import Icon from '../components/Icon.vue'
import RepoLinks from '../components/RepoLinks.vue'
import { fmtBytes, fmtTime } from '../format'
import { useRevisit } from '../composables'

const message = useMessage()
const router = useRouter()
const auth = useAuth()
const cfg = ref<OTAConfig | null>(null)
const list = ref<Release[]>([])
const loading = ref(true)
const repo = ref<InstanceType<typeof RepoLinks> | null>(null)

async function load() {
  ;[cfg.value, list.value] = await Promise.all([api.otaConfig(), api.releases()])
  loading.value = false
}

const latest = computed(() => list.value.find((r) => r.status === 'published'))
// 源里提供的版本：已同步到源、未撤回的最新一个（列表新到旧）。
const repoServed = computed(() => list.value.find((r) => r.in_repo && r.status === 'published'))
function progress(r: Release) {
  const c = r.counts
  const total = Object.values(c).reduce((a, b) => a + (b ?? 0), 0)
  return { total, ok: c.installed ?? 0, bad: c.failed ?? 0, back: c.rolled_back ?? 0, running: (c.queued ?? 0) + (c.downloading ?? 0) }
}

// ---- 上传 ----
const show = ref(false)
const bundle = ref<File | null>(null)
const notes = ref('')
const password = ref('')
const totp = ref('')
const uploading = ref(false)
const toRepo = ref(true) // 同步到软件源：源里改为提供这个版本
const ratio = ref(0)

function openUpload() {
  bundle.value = null
  notes.value = password.value = totp.value = ''
  toRepo.value = true
  ratio.value = 0
  show.value = true
}

function pick(e: Event) {
  const input = e.target as HTMLInputElement
  const f = input.files?.[0]
  input.value = ''
  if (!f) return
  if (!f.name.endsWith('.zip')) return message.error('请选择 rk-release sign 生成的 .rkrel.zip')
  bundle.value = f
}

const ready = computed(() => !!bundle.value && !!password.value && (!auth.admin?.totp_enabled || totp.value.length === 6))

async function upload() {
  if (!ready.value) return
  const f = new FormData()
  // 顺序固定：文件最后，服务端先验证身份。
  f.append('password', password.value)
  if (totp.value) f.append('totp_code', totp.value)
  f.append('notes', notes.value)
  if (cfg.value?.repo_enabled) f.append('repo', toRepo.value ? '1' : '0')
  f.append('bundle', bundle.value!)
  uploading.value = true
  try {
    const r = await api.uploadRelease(f, (x) => { ratio.value = x })
    message.success(`已发布 ${r.version}，接下来创建灰度计划`)
    show.value = false
    void router.push(`/releases/${r.id}`)
    void repo.value?.load()
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : '上传失败')
  } finally {
    uploading.value = false
  }
}

let off = () => {}
onMounted(() => {
  void load()
  // 灰度计划变化会改变源里提供的版本。
  off = onStream((topic) => { if (topic === 'update.changed') { void load(); void repo.value?.load() } })
})
onUnmounted(() => off())
// 从别的页面回来时静默刷新（页面保活，不重建）。
useRevisit(() => { void load(); void repo.value?.load() })
</script>

<template>
  <PageBar title="发布">
    <n-button v-if="cfg?.enabled" type="primary" size="small" @click="openUpload">
      <template #icon><Icon name="upload" :size="15" /></template>发布新版本
    </n-button>
  </PageBar>
  <div class="content">
    <div v-if="cfg && !cfg.enabled" class="card warn">
      <div class="card-title"><h3>未配置发布公钥</h3></div>
      <p class="muted">在发布用的电脑上运行 <code>rk-release pubkey</code>，把输出填到服务器配置的 <code>release.public_key</code> 后重启服务。私钥只留在那台电脑上，服务器只用公钥校验。</p>
    </div>

    <div class="card how">
      <div class="steps">
        <div><b>1</b><span>本机签名</span><code>rk-release sign xxx.deb 1.6.1</code></div>
        <div><b>2</b><span>上传</span><em>上传生成的 .rkrel.zip，服务器校验签名</em></div>
        <div><b>3</b><span>灰度</span><em>先选几台设备试装，确认无误再推全部</em></div>
      </div>
    </div>

    <div class="card tcard">
      <div class="table-wrap">
        <table class="table" :class="{ busy: loading && list.length }">
          <thead><tr><th style="width:120px">版本</th><th style="width:90px">状态</th><th>设备</th><th style="width:100px">大小</th><th style="width:150px">发布时间</th><th style="width:30px" /></tr></thead>
          <tbody>
            <template v-if="loading && !list.length"><tr v-for="i in 3" :key="i"><td v-for="j in 6" :key="j"><div class="skel" style="width:70%" /></td></tr></template>
            <tr v-for="r in list" v-else :key="r.id" class="clickable" :class="{ withdrawn: r.status === 'withdrawn' }" @click="router.push(`/releases/${r.id}`)">
              <td><span class="ver">{{ r.version }}</span><span v-if="r === latest" class="pill p-run tiny">最新</span><span v-if="r === repoServed" class="pill p-ok tiny" title="软件源（Sileo）里提供的版本">源</span></td>
              <td><span class="pill" :class="r.status === 'published' ? 'p-ok' : 'p-wait'">{{ r.status === 'published' ? '已发布' : '已撤回' }}</span></td>
              <td>
                <template v-if="progress(r).total">
                  <div class="prog"><i class="ok" :style="{ flex: progress(r).ok }" /><i class="run" :style="{ flex: progress(r).running }" /><i class="bad" :style="{ flex: progress(r).bad }" /><i class="back" :style="{ flex: progress(r).back }" /><i class="rest" :style="{ flex: progress(r).total - progress(r).ok - progress(r).running - progress(r).bad - progress(r).back }" /></div>
                  <span class="muted small num">{{ progress(r).ok }}/{{ progress(r).total }} 已更新<template v-if="progress(r).running"> · {{ progress(r).running }} 进行中</template><template v-if="progress(r).back"> · {{ progress(r).back }} 已回滚</template><template v-if="progress(r).bad"> · {{ progress(r).bad }} 失败</template></span>
                </template>
                <span v-else class="faint small">尚未下发</span>
              </td>
              <td class="muted num">{{ fmtBytes(r.size) }}</td>
              <td class="mono muted num">{{ fmtTime(r.created_at).slice(0, 16) }}</td>
              <td class="faint"><Icon name="chevron" :size="15" /></td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!loading && !list.length" class="empty">还没有发布过版本</div>
    </div>

    <RepoLinks ref="repo" />

    <n-modal v-model:show="show" preset="card" title="发布新版本" style="width:540px;max-width:calc(100vw - 32px)" :bordered="false"
      :mask-closable="!uploading" :closable="!uploading">
      <label class="drop" :class="{ picked: bundle }">
        <input type="file" accept=".zip" @change="pick">
        <Icon :name="bundle ? 'check' : 'upload'" :size="22" />
        <span v-if="bundle" class="mono">{{ bundle.name }}<span class="faint num"> · {{ fmtBytes(bundle.size) }}</span></span>
        <span v-else>选择发布包 <span class="mono faint">.rkrel.zip</span></span>
      </label>
      <div class="field"><label>更新说明（可选）</label>
        <n-input v-model:value="notes" type="textarea" :autosize="{ minRows: 2, maxRows: 6 }" maxlength="2000" placeholder="改了什么" /></div>
      <div v-if="cfg?.repo_enabled" class="repo-row">
        <div>
          <b>同步到软件源</b>
          <span class="faint">{{ toRepo ? '新机器在 Sileo 里装到的就是这个版本' : '软件源保持当前版本不变' }}</span>
        </div>
        <n-switch v-model:value="toRepo" />
      </div>
      <div class="field two">
        <div><label>当前密码</label><n-input v-model:value="password" type="password" show-password-on="click" placeholder="发布需要再次验证" /></div>
        <div v-if="auth.admin?.totp_enabled"><label>两步验证码</label><n-input v-model:value="totp" maxlength="6" placeholder="6 位数字" /></div>
      </div>
      <n-progress v-if="uploading" type="line" :percentage="Math.round(ratio * 100)" :show-indicator="false" style="margin-top:6px" />
      <template #footer>
        <div class="row"><div class="spacer" />
          <n-button :disabled="uploading" @click="show = false">取消</n-button>
          <n-button type="primary" :loading="uploading" :disabled="!ready" @click="upload">上传并发布</n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.warn { border-left: 3px solid var(--orange); }
.warn p { margin: 0; font-size: 13px; line-height: 1.7; }
code { font-family: ui-monospace, "SF Mono", Menlo, monospace; font-size: 12px; background: var(--fill); padding: 1px 6px; border-radius: 5px; }
.how { padding: 16px 20px; }
.steps { display: grid; grid-template-columns: repeat(3, 1fr); gap: 14px; }
.steps > div { display: flex; flex-direction: column; gap: 4px; font-size: 13px; }
.steps b { width: 22px; height: 22px; border-radius: 50%; background: var(--svc-soft); color: var(--svc); display: grid; place-items: center; font-size: 12px; }
.steps span { font-weight: 600; }
.steps em { font-style: normal; color: var(--sub); font-size: 12.5px; }
.steps code { align-self: flex-start; }
.small { font-size: 12px; }
.tcard { padding: 16px 20px; }
.ver { font-weight: 700; font-variant-numeric: tabular-nums; }
.tiny { margin-left: 6px; font-size: 10.5px; padding: 1px 7px; }
tr.withdrawn td { opacity: .55; }
.prog { display: flex; height: 6px; border-radius: 3px; overflow: hidden; background: var(--fill); width: 160px; margin-bottom: 4px; }
.prog i { display: block; }
.prog .ok { background: var(--green); }
.prog .run { background: var(--svc); }
.prog .bad { background: var(--red); }
.prog .back { background: var(--orange); }
.drop { display: flex; flex-direction: column; align-items: center; gap: 8px; padding: 22px; border: 1.5px dashed var(--sep); border-radius: 14px; cursor: pointer; color: var(--sub); font-size: 13px; }
.drop:hover { border-color: var(--svc); color: var(--svc); }
.drop input { display: none; }
.drop.picked { border-style: solid; color: var(--text); }
.drop.picked svg { color: var(--green); }
.field { margin-top: 14px; }
.field label { display: block; font-size: 12px; color: var(--sub); margin-bottom: 6px; font-weight: 600; }
.two { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.repo-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 14px; padding: 10px 12px; border-radius: 10px; background: var(--fill); }
.repo-row b { display: block; font-size: 13px; }
.repo-row span { font-size: 12px; }
@media (max-width: 760px) { .steps { grid-template-columns: 1fr; } .two { grid-template-columns: 1fr; } }
</style>
