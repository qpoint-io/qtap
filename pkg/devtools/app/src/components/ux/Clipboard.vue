<template>
  <button
    @click="copyToClipboard"
    class="p-1 rounded border border-dt-border-light bg-dt-bg-primary hover:bg-dt-bg-hover transition-colors text-dt-text-primary shrink-0 cursor-pointer dark:bg-dt-bg-dark-primary dark:hover:bg-dt-bg-dark-hover dark:border-dt-border-dark-light dark:text-dt-text-dark-primary"
    :aria-label="copied ? 'Copied' : 'Copy to clipboard'"
    title="Copy to clipboard"
  >
    <component :is="currentIcon" class="w-3.5 h-3.5" />
  </button>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import CopyIcon from '@/components/icons/CopyIcon.vue'
import CheckIcon from '@/components/icons/CheckIcon.vue'

// Props
interface Props {
  content: string
}

const props = defineProps<Props>()

// State
const copied = ref(false)

// Computed icon
const currentIcon = computed(() => {
  return copied.value ? CheckIcon : CopyIcon
})

// Copy to clipboard handler
async function copyToClipboard() {
  try {
    // Check if Clipboard API is available (requires secure context)
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(props.content)
    } else {
      // Fallback for non-secure contexts
      const textArea = document.createElement('textarea')
      textArea.value = props.content
      textArea.style.position = 'fixed'
      textArea.style.left = '-9999px'
      document.body.appendChild(textArea)
      textArea.select()
      document.execCommand('copy')
      document.body.removeChild(textArea)
    }
    copied.value = true
    
    // Reset after 3 seconds
    setTimeout(() => {
      copied.value = false
    }, 3000)
  } catch (error) {
    console.error('Failed to copy to clipboard:', error)
  }
}
</script>

