<template>
  <div class="relative" ref="containerRef">
    <!-- Toggle Button -->
    <button
      @click="toggleDropdown"
      class="p-1 rounded border border-dt-border-light dark:border-dt-border-dark-light bg-dt-bg-primary dark:bg-dt-bg-dark-primary hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover transition-colors text-dt-text-primary dark:text-dt-text-dark-primary shrink-0 cursor-pointer"
      aria-label="Toggle columns"
      title="Show/hide columns"
    >
      <ColumnsIcon class="w-3.5 h-3.5" />
    </button>

    <!-- Dropdown Menu -->
    <div
      v-if="isOpen"
      class="absolute right-0 top-full mt-1 bg-dt-bg-primary dark:bg-dt-bg-dark-primary border border-dt-border-light dark:border-dt-border-dark-light rounded shadow-lg min-w-48 z-50"
    >
      <!-- Column List -->
      <div class="py-1 max-h-64 overflow-y-auto">
        <label
          v-for="column in columns"
          :key="column.key"
          class="flex items-center gap-2 px-3 py-1.5 text-xs text-dt-text-primary dark:text-dt-text-dark-primary hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover cursor-pointer"
        >
          <input
            type="checkbox"
            :checked="column.visible"
            @change="toggleColumn(column.key)"
            class="w-3.5 h-3.5 rounded border-dt-border-light dark:border-dt-border-dark-light text-dt-accent focus:ring-dt-accent cursor-pointer"
          />
          <span>{{ column.label }}</span>
        </label>
      </div>

      <!-- Separator -->
      <div class="border-t border-dt-border-light dark:border-dt-border-dark-light"></div>

      <!-- Reset Button -->
      <button
        @click="handleReset"
        class="w-full px-3 py-2 text-xs text-dt-text-secondary dark:text-dt-text-dark-secondary hover:text-dt-text-primary dark:hover:text-dt-text-dark-primary hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover text-left cursor-pointer"
      >
        Reset to defaults
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import ColumnsIcon from '@/components/icons/ColumnsIcon.vue'
import type { ColumnConfig } from '@/composables/columnResize'

interface Props {
  columns: ColumnConfig[]
}

defineProps<Props>()

const emit = defineEmits<{
  toggle: [key: string]
  reset: []
}>()

const isOpen = ref(false)
const containerRef = ref<HTMLElement | null>(null)

const toggleDropdown = () => {
  isOpen.value = !isOpen.value
}

const toggleColumn = (key: string) => {
  emit('toggle', key)
}

const handleReset = () => {
  emit('reset')
  isOpen.value = false
}

// Close dropdown when clicking outside
const handleClickOutside = (e: MouseEvent) => {
  if (containerRef.value && !containerRef.value.contains(e.target as Node)) {
    isOpen.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>

