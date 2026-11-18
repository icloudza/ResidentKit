<script setup lang="ts">
import { computed } from 'vue'
import { NConfigProvider, NMessageProvider, NDialogProvider, darkTheme, useOsTheme, zhCN, dateZhCN } from 'naive-ui'
import type { GlobalThemeOverrides } from 'naive-ui'

const os = useOsTheme()
const dark = computed(() => os.value === 'dark')

// 与 App 一致的配色；深色模式提高文字对比度。
const overrides = computed<GlobalThemeOverrides>(() => {
  const primary = dark.value ? '#639EFF' : '#2E70ED'
  return {
    common: {
      primaryColor: primary,
      primaryColorHover: dark.value ? '#7DB0FF' : '#4A85F2',
      primaryColorPressed: dark.value ? '#4F8AF0' : '#2460D4',
      primaryColorSuppl: primary,
      borderRadius: '10px',
      fontFamily: '-apple-system, BlinkMacSystemFont, "SF Pro Text", "PingFang SC", "Helvetica Neue", sans-serif',
      bodyColor: dark.value ? '#000000' : '#F2F2F7',
      cardColor: dark.value ? '#1C1C1E' : '#FFFFFF',
      modalColor: dark.value ? '#1C1C1E' : '#FFFFFF',
      popoverColor: dark.value ? '#2C2C2E' : '#FFFFFF',
      ...(dark.value ? { textColor1: '#F5F5F7', textColor2: '#E5E5EA', textColor3: '#A1A1A6', placeholderColor: '#6E6E73' } : {}),
    },
    Card: { borderRadius: '20px' },
    Drawer: { borderRadius: '22px' },
  }
})
</script>

<template>
  <n-config-provider :theme="dark ? darkTheme : null" :theme-overrides="overrides" :locale="zhCN" :date-locale="dateZhCN">
    <n-message-provider>
      <n-dialog-provider>
        <router-view />
      </n-dialog-provider>
    </n-message-provider>
  </n-config-provider>
</template>
