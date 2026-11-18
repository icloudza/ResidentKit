<script setup lang="ts">
// 可选卡片：代替复选框。整张卡片可点，选中时蓝色描边、浅蓝底、左侧圆形对勾。
import Icon from './Icon.vue'

defineProps<{ selected: boolean; disabled?: boolean }>()
const emit = defineEmits<{ toggle: [] }>()
</script>

<template>
  <button type="button" class="choice" :class="{ on: selected, dis: disabled }" :disabled="disabled"
    :aria-pressed="selected" @click="emit('toggle')">
    <span class="tick"><Icon v-if="selected" name="check" :size="12" /></span>
    <span class="body"><slot /></span>
  </button>
</template>

<style scoped>
.choice {
  display: flex; align-items: flex-start; gap: 10px; width: 100%; text-align: left; font: inherit; color: var(--text);
  padding: 10px 12px; border-radius: 12px; border: 1.5px solid var(--sep); background: var(--card); cursor: pointer;
  transition: border-color .15s, background .15s;
}
.choice:hover { border-color: color-mix(in srgb, var(--svc) 45%, transparent); }
.choice.on { border-color: var(--svc); background: var(--svc-soft); }
.choice.dis { opacity: .5; cursor: not-allowed; }
.choice.dis:hover { border-color: var(--sep); }
.tick {
  width: 18px; height: 18px; border-radius: 50%; border: 1.5px solid var(--faint); flex: none; margin-top: 1px;
  display: grid; place-items: center; color: #fff; transition: .15s;
}
.on .tick { background: var(--svc); border-color: var(--svc); }
.body { min-width: 0; display: flex; flex-direction: column; gap: 2px; font-size: 13px; overflow-wrap: anywhere; }
</style>
