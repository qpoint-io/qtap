<template>
  <div 
    ref="containerRef"
    class="flex h-full relative"
    :class="{ 'select-none': isResizing }"
  >
    <!-- Left Panel -->
    <div 
      class="flex flex-col absolute inset-0"
      :class="leftPanelClass"
    >
      <slot name="left"></slot>
    </div>

    <!-- Resize Divider (only shown when right panel is visible) -->
    <div
      v-if="showRight"
      @mousedown="handleResizeStart"
      class="absolute top-0 bottom-0 w-1 cursor-col-resize hover:bg-dt-accent dark:hover:bg-dt-accent-dark-blue transition-colors z-40"
      :class="{ 'bg-dt-accent dark:bg-dt-accent-dark-blue': isResizing }"
      :style="{ left: `${panelWidth}%` }"
    ></div>

    <!-- Right Panel -->
    <div
      v-if="showRight"
      class="flex flex-col flex-1 min-w-0 absolute top-0 bottom-0 right-0 bg-dt-bg-primary dark:bg-dt-bg-dark-primary shadow-lg z-30"
      :class="rightPanelClass"
      :style="{ width: `${100 - panelWidth}%` }"
    >
      <slot name="right"></slot>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { usePanelResize } from '@/composables/panelResize'

interface Props {
  storageKey: string
  defaultWidth?: number // percentage for left panel
  minWidth?: number // minimum percentage for left panel
  maxWidth?: number // maximum percentage for left panel
  showRight?: boolean // whether to show the right panel
  leftPanelClass?: string
  rightPanelClass?: string
}

const props = withDefaults(defineProps<Props>(), {
  defaultWidth: 40,
  minWidth: 10,
  maxWidth: 90,
  showRight: true,
  leftPanelClass: '',
  rightPanelClass: '',
})

const emit = defineEmits<{
  (e: 'reset'): void
}>()

const containerRef = ref<HTMLElement | null>(null)

const {
  panelWidth,
  isResizing,
  startResize,
  resetWidth,
} = usePanelResize({
  storageKey: props.storageKey,
  defaultWidth: props.defaultWidth,
  minWidth: props.minWidth,
  maxWidth: props.maxWidth,
})

const handleResizeStart = (e: MouseEvent) => {
  if (containerRef.value) {
    startResize(e, containerRef.value)
  }
}

// Expose reset function and panelWidth for parent components
defineExpose({
  resetWidth,
  panelWidth,
})
</script>

