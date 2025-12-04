<template>
  <div 
    class="flex items-center gap-2 font-bold rounded-full cursor-pointer select-none bg-[#edfffa] border border-[#7642e2] text-[#7642e2] dark:bg-black dark:border-none dark:text-[#a067f0]"
    :class="sizeClasses"
    @click="props.onClick"
  >
    <slot />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

interface Props {
  onClick?: () => void
  size?: 'small' | 'medium' | 'large'
}

const props = withDefaults(defineProps<Props>(), {
  size: 'large'
})

const sizeClasses = computed(() => {
  switch (props.size) {
    case 'small':
      return 'px-3 py-1.5 text-xs'
    case 'medium':
      return 'px-5 py-2 text-sm'
    case 'large':
    default:
      return 'px-8 py-2 text-[18px]'
  }
})
</script>

