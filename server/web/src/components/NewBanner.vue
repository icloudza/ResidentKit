<script setup lang="ts">
// 不在第 1 页时有新数据到达：提示而不是插入，避免当前页内容跳动。
defineProps<{ count: number }>()
defineEmits<{ click: [] }>()
</script>

<template>
  <button v-if="count > 0" class="banner" @click="$emit('click')">
    <span class="pulse" />有 {{ count }} 条新数据，点击回到第 1 页查看
  </button>
</template>

<style scoped>
.banner {
  align-self: center; display: inline-flex; align-items: center; gap: 8px; border: 0; cursor: pointer;
  background: var(--svc); color: #fff; font: inherit; font-size: 13px; font-weight: 600; padding: 7px 14px; border-radius: 99px;
}
</style>
