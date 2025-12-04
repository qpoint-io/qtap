<template>
  <div 
    class="rounded-md border-2 border-[#7642e2] text-[#7642e2] inline font-bold cursor-pointer select-none dark:bg-gray-200 dark:border-none dark:text-[#532bca]"
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
      return 'px-4 py-2 text-sm'
    case 'large':
    default:
      return 'px-5 py-3 text-[18px]'
  }
})
</script>
