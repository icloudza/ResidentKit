// 页面公用的组合函数。

import { onActivated } from 'vue'

// useRevisit 保活页面再次显示时调用 fn（首次进入由 onMounted 负责，这里跳过）。
export function useRevisit(fn: () => unknown) {
  let first = true
  onActivated(() => {
    if (first) { first = false; return }
    void fn()
  })
}
