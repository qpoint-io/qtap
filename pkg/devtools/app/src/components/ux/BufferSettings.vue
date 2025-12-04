<template>
  <div class="relative" ref="containerRef">
    <!-- Toggle Button -->
    <button
      @click="toggleDropdown"
      class="p-1 rounded border border-dt-border-light dark:border-dt-border-dark-light bg-dt-bg-primary dark:bg-dt-bg-dark-primary hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover transition-colors text-dt-text-primary dark:text-dt-text-dark-primary shrink-0 cursor-pointer"
      aria-label="Buffer settings"
      title="Buffer settings"
    >
      <SettingsIcon class="w-3.5 h-3.5" />
    </button>

    <!-- Dropdown Menu -->
    <div
      v-if="isOpen"
      class="absolute right-0 top-full mt-1 bg-dt-bg-primary dark:bg-dt-bg-dark-primary border border-dt-border-light dark:border-dt-border-dark-light rounded shadow-lg min-w-56 z-50"
    >
      <!-- Settings Form -->
      <div class="p-3 space-y-3">
        <!-- Max Items -->
        <div>
          <label class="block text-xs text-dt-text-secondary dark:text-dt-text-dark-secondary mb-1">
            Max {{ entityLabel }}s
          </label>
          <input
            type="number"
            :value="maxItems"
            @input="handleMaxItemsInput"
            min="1"
            max="10000"
            class="w-full px-2 py-1 text-xs bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary border border-dt-border-light dark:border-dt-border-dark-light rounded text-dt-text-primary dark:text-dt-text-dark-primary focus:outline-none focus:ring-1 focus:ring-dt-accent dark:focus:ring-dt-accent-dark-blue"
          />
        </div>

        <!-- Max Size -->
        <div>
          <label class="block text-xs text-dt-text-secondary dark:text-dt-text-dark-secondary mb-1">
            {{ entityLabel }}s Data Memory Limit
          </label>
          <div class="flex gap-2">
            <input
              type="number"
              :value="maxSize"
              @input="handleMaxSizeInput"
              min="1"
              max="1000"
              class="flex-1 px-2 py-1 text-xs bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary border border-dt-border-light dark:border-dt-border-dark-light rounded text-dt-text-primary dark:text-dt-text-dark-primary focus:outline-none focus:ring-1 focus:ring-dt-accent dark:focus:ring-dt-accent-dark-blue"
            />
            <select
              :value="maxSizeUnit"
              @change="handleUnitChange"
              class="px-2 py-1 text-xs bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary border border-dt-border-light dark:border-dt-border-dark-light rounded text-dt-text-primary dark:text-dt-text-dark-primary focus:outline-none focus:ring-1 focus:ring-dt-accent dark:focus:ring-dt-accent-dark-blue cursor-pointer"
            >
              <option value="KB">KB</option>
              <option value="MB">MB</option>
              <option value="GB">GB</option>
            </select>
          </div>
        </div>
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
import { ref, onMounted, onUnmounted, computed } from 'vue'
import SettingsIcon from '@/components/icons/SettingsIcon.vue'
import type { SizeUnit } from '@/composables/bufferSettings'

interface Props {
  entity: string
  maxItems: number
  maxSize: number
  maxSizeUnit: SizeUnit
}

const props = defineProps<Props>()

const emit = defineEmits<{
  'update:maxItems': [value: number]
  'update:maxSize': [value: number]
  'update:maxSizeUnit': [value: SizeUnit]
  reset: []
}>()

const isOpen = ref(false)
const containerRef = ref<HTMLElement | null>(null)

const toggleDropdown = () => {
  isOpen.value = !isOpen.value
}

const entityLabel = computed(() => {
  return props.entity.charAt(0).toUpperCase() + props.entity.slice(1)
})

const handleMaxItemsInput = (e: Event) => {
  const value = parseInt((e.target as HTMLInputElement).value, 10)
  if (!isNaN(value) && value >= 1 && value <= 10000) {
    emit('update:maxItems', value)
  }
}

const handleMaxSizeInput = (e: Event) => {
  const value = parseInt((e.target as HTMLInputElement).value, 10)
  if (!isNaN(value) && value >= 1 && value <= 1000) {
    emit('update:maxSize', value)
  }
}

const handleUnitChange = (e: Event) => {
  const value = (e.target as HTMLSelectElement).value as SizeUnit
  emit('update:maxSizeUnit', value)
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

