<script setup lang="ts">
// 表格行首的展开箭头：可展开时显示 ›，展开后转为向下；不可展开时留空占位。
import Icon from './Icon.vue'

defineProps<{ can: boolean; open: boolean }>()
</script>

<template>
  <span class="mark" :class="{ open }"><Icon v-if="can" name="chevron" :size="14" /></span>
</template>

<style scoped>
.mark { display: inline-grid; place-items: center; width: 18px; height: 18px; color: var(--faint); transition: transform .15s, color .15s; }
.mark.open { transform: rotate(90deg); color: var(--svc); }
</style>
