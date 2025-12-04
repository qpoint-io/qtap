<template>
  <span 
    class="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-semibold uppercase tracking-wide"
    :class="badgeClass"
  >
    {{ status }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'

interface Props {
  status: string
  type: 'connection' | 'process'
}

const props = defineProps<Props>()

const badgeClass = computed(() => {
  if (props.type === 'connection') {
    if (props.status === 'open') {
      return 'bg-green-100 text-green-800 border border-green-300 dark:bg-green-900/30 dark:text-green-300 dark:border-green-700'
    }
    if (props.status === 'closed') {
      return 'bg-gray-100 text-gray-600 border border-gray-300 dark:bg-gray-800/50 dark:text-gray-400 dark:border-gray-600'
    }
  }

  if (props.type === 'process') {
    if (props.status === 'running') {
      return 'bg-green-100 text-green-800 border border-green-300 dark:bg-green-900/30 dark:text-green-300 dark:border-green-700'
    }
    if (props.status === 'exited') {
      return 'bg-gray-100 text-gray-600 border border-gray-300 dark:bg-gray-800/50 dark:text-gray-400 dark:border-gray-600'
    }
  }

  // Default fallback
  return 'bg-gray-100 text-gray-600 border border-gray-300 dark:bg-gray-800/50 dark:text-gray-400 dark:border-gray-600'
})
</script>

