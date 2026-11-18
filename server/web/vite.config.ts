import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import { readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { brotliCompressSync, constants, gzipSync } from 'node:zlib'

// 构建后为文本资源生成 .br / .gz 预压缩文件，服务端按 Accept-Encoding 直接返回，不用每次请求现压。
function precompress(): Plugin {
  let outDir = 'dist'
  return {
    name: 'rk-precompress',
    apply: 'build',
    configResolved(c) { outDir = c.build.outDir },
    closeBundle() {
      const walk = (dir: string): string[] =>
        readdirSync(dir).flatMap((n) => {
          const p = join(dir, n)
          return statSync(p).isDirectory() ? walk(p) : [p]
        })
      for (const file of walk(outDir)) {
        if (!/\.(js|css|html|svg|json)$/.test(file)) continue
        const raw = readFileSync(file)
        if (raw.length < 1024) continue // 太小的文件压缩收益抵不过一次额外的查找
        const br = brotliCompressSync(raw, {
          params: { [constants.BROTLI_PARAM_QUALITY]: 11, [constants.BROTLI_PARAM_SIZE_HINT]: raw.length },
        })
        const gz = gzipSync(raw, { level: 9 })
        if (br.length < raw.length * 0.9) writeFileSync(`${file}.br`, br)
        if (gz.length < raw.length * 0.9) writeFileSync(`${file}.gz`, gz)
      }
    },
  }
}

// 开发时把 /api 代理到本机的 rk-server 控制台；RK_API 可改为其他地址（如本机的假数据服务）。
export default defineConfig({
  plugins: [vue(), precompress()],
  define: {
    // 只用组合式 API：去掉 Vue 选项式 API 与生产环境调试相关的代码。
    __VUE_OPTIONS_API__: false,
    __VUE_PROD_DEVTOOLS__: false,
    __VUE_PROD_HYDRATION_MISMATCH_DETAILS__: false,
  },
  server: {
    proxy: { '/api': process.env.RK_API ?? 'http://127.0.0.1:8765' },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2022', // 控制台只面向现代浏览器
    modulePreload: { polyfill: false },
    reportCompressedSize: false, // 压缩体积由 precompress 生成，构建时不重复计算
    chunkSizeWarningLimit: 600,
    rolldownOptions: {
      output: {
        // Vue 运行时单独成包：业务代码更新时它的文件名不变，浏览器继续用缓存。
        // naive-ui 不合并，按页面用到的组件自动拆分，首屏只加载登录页需要的部分。
        codeSplitting: {
          groups: [
            { name: 'vue', test: /node_modules[\\/](@vue|vue|vue-router|pinia)[\\/]/, priority: 20 },
          ],
        },
      },
    },
  },
})
