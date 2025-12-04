<template>
  <div class="toolbar border-b border-dt-border-light dark:border-dt-border-dark-light bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary px-1 py-1 flex items-center gap-2">
    <!-- Pause/Resume -->
    <button
      @click="togglePause"
      class="p-1 rounded border border-dt-border-light dark:border-dt-border-dark-light bg-dt-bg-primary dark:bg-dt-bg-dark-primary hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover transition-colors shrink-0 cursor-pointer text-dt-text-primary dark:text-dt-text-dark-primary"
      :class="{ 'bg-dt-accent hover:bg-dt-accent-hover dark:bg-dt-accent-dark-blue dark:hover:bg-dt-accent-dark-blueHover': props.pause }"
      :aria-label="props.pause ? 'Resume' : 'Pause'"
      title="Pause/Resume traffic"
    >
      <component :is="props.pause ? PlayIcon : PauseIcon" class="w-3.5 h-3.5" />
    </button>

    <!-- Clear -->
    <button
      @click="clearStorage"
      class="p-1 rounded border border-dt-border-light dark:border-dt-border-dark-light bg-dt-bg-primary dark:bg-dt-bg-dark-primary hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover transition-colors text-dt-text-primary dark:text-dt-text-dark-primary shrink-0 cursor-pointer"
      :aria-label="'Clear storage'"
      title="Clear storage"
    >
      <ClearIcon class="w-3.5 h-3.5" />
    </button>

    <!-- Add Filter Button (Fixed) -->
    <button
      ref="addFilterButton"
      @click="addFilter"
      class="p-1 rounded border border-dt-border-light dark:border-dt-border-dark-light bg-dt-bg-primary dark:bg-dt-bg-dark-primary hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover transition-colors text-dt-text-primary dark:text-dt-text-dark-primary shrink-0 cursor-pointer"
      aria-label="Add filter"
      title="Add filter"
    >
      <FilterIcon class="w-3.5 h-3.5" />
    </button>

    <!-- Filter Chips Wrapper (allows vertical overflow for dropdowns) -->
    <div class="filters-wrapper flex-1 min-w-0">
      <!-- Filter Chips Container (Scrollable) -->
      <div class="filters-scroll-container">
      <div
        v-for="filter in activeFilters"
        :key="filter.id"
        class="filter-item flex items-center border border-dt-border-light dark:border-dt-border-dark-light bg-dt-bg-primary dark:bg-dt-bg-dark-primary rounded shrink-0"
      >
        <!-- Summary Mode -->
        <div
          v-if="!filter.isEditMode && isFilterComplete(filter)"
          @click="enterEditMode(filter)"
          class="flex items-center cursor-pointer hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover transition-colors rounded-l px-3 py-1"
        >
          <span class="text-xs text-dt-text-primary dark:text-dt-text-dark-primary">
            {{ filter.key }} {{ filter.operator }} {{ filter.value }}
          </span>
        </div>

        <!-- Edit Mode -->
        <template v-if="filter.isEditMode || !isFilterComplete(filter)">
          <!-- Key Autocomplete -->
          <div class="relative">
            <input
              :ref="el => setKeyInputRef(filter.id, el)"
              v-model="filter.key"
              @input="onKeyInput(filter)"
              @focus="onKeyFocus(filter)"
              @blur="onKeyBlur(filter)"
              @keydown="onKeyKeydown($event, filter)"
              class="text-xs pl-2 pr-2 py-1 focus:outline-none focus:ring-1 focus:ring-dt-accent dark:focus:ring-dt-accent-dark-blue bg-transparent rounded-l w-32 text-dt-text-primary dark:text-dt-text-dark-primary placeholder-dt-text-tertiary dark:placeholder-dt-text-dark-tertiary"
              placeholder="Key..."
              aria-label="Filter key"
            />
            <!-- Autocomplete Dropdown -->
            <div
              v-if="filter.showKeyAutocomplete && getFilteredKeys(filter).length > 0"
              class="absolute top-full left-0 mt-1 bg-dt-bg-primary dark:bg-dt-bg-dark-primary border border-dt-border-light dark:border-dt-border-dark-light rounded shadow-lg max-h-48 overflow-y-auto min-w-full"
              style="z-index: 1000;"
              :data-autocomplete-key="filter.id"
            >
              <div
                v-for="(key, index) in getFilteredKeys(filter)"
                :key="key"
                :data-index="index"
                @mousedown.prevent="selectKey(filter, key)"
                @mouseenter="filter.keyAutocompleteIndex = index"
                :class="[
                  'text-xs px-3 py-2 cursor-pointer whitespace-nowrap',
                  index === filter.keyAutocompleteIndex
                    ? 'bg-dt-accent dark:bg-dt-accent-dark-blue text-white'
                    : 'hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover text-dt-text-primary dark:text-dt-text-dark-primary'
                ]"
              >
                {{ key }}
              </div>
            </div>
          </div>

          <!-- Separator -->
          <div class="h-5 border-r border-dt-border-light dark:border-dt-border-dark-light mx-2"></div>

          <!-- Operator Autocomplete -->
          <div class="relative">
            <input
              :ref="el => setOperatorInputRef(filter.id, el)"
              v-model="filter.operator"
              @input="onOperatorInput(filter)"
              @focus="onOperatorFocus(filter)"
              @blur="onOperatorBlur(filter)"
              @keydown="onOperatorKeydown($event, filter)"
              class="text-xs pl-2 pr-2 py-1 focus:outline-none focus:ring-1 focus:ring-dt-accent dark:focus:ring-dt-accent-dark-blue bg-transparent w-28 text-dt-text-primary dark:text-dt-text-dark-primary placeholder-dt-text-tertiary dark:placeholder-dt-text-dark-tertiary"
              placeholder="Operator..."
              aria-label="Filter operator"
              :disabled="!filter.key"
            />
            <!-- Autocomplete Dropdown -->
            <div
              v-if="filter.showOperatorAutocomplete && getFilteredOperators(filter).length > 0"
              class="absolute top-full left-0 mt-1 bg-dt-bg-primary dark:bg-dt-bg-dark-primary border border-dt-border-light dark:border-dt-border-dark-light rounded shadow-lg max-h-48 overflow-y-auto min-w-full"
              style="z-index: 1000;"
              :data-autocomplete-operator="filter.id"
            >
              <div
                v-for="(operator, index) in getFilteredOperators(filter)"
                :key="operator"
                :data-index="index"
                @mousedown.prevent="selectOperator(filter, operator)"
                @mouseenter="filter.operatorAutocompleteIndex = index"
                :class="[
                  'text-xs px-3 py-2 cursor-pointer whitespace-nowrap',
                  index === filter.operatorAutocompleteIndex
                    ? 'bg-dt-accent dark:bg-dt-accent-dark-blue text-white'
                    : 'hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover text-dt-text-primary dark:text-dt-text-dark-primary'
                ]"
              >
                {{ operator }}
              </div>
            </div>
          </div>

          <!-- Separator -->
          <div class="h-5 border-r border-dt-border-light dark:border-dt-border-dark-light mx-2"></div>

          <!-- Value Autocomplete -->
          <div class="relative">
            <input
              :ref="el => setValueInputRef(filter.id, el)"
              v-model="filter.value"
              @input="onValueInput(filter)"
              @focus="onValueFocus(filter)"
              @blur="onValueBlur(filter)"
              @keydown="onValueKeydown($event, filter)"
              class="text-xs pl-2 pr-2 py-1 focus:outline-none focus:ring-1 focus:ring-dt-accent dark:focus:ring-dt-accent-dark-blue bg-transparent w-32 text-dt-text-primary dark:text-dt-text-dark-primary placeholder-dt-text-tertiary dark:placeholder-dt-text-dark-tertiary"
              placeholder="Type value..."
              aria-label="Filter value"
              :disabled="!filter.key"
            />
            <!-- Autocomplete Dropdown -->
            <div
              v-if="filter.showAutocomplete && getFilteredValues(filter).length > 0"
              class="absolute top-full left-0 mt-1 bg-dt-bg-primary dark:bg-dt-bg-dark-primary border border-dt-border-light dark:border-dt-border-dark-light rounded shadow-lg max-h-48 overflow-y-auto min-w-full"
              style="z-index: 1000;"
              :data-autocomplete-value="filter.id"
            >
              <div
                v-for="(value, index) in getFilteredValues(filter)"
                :key="value"
                :data-index="index"
                @mousedown.prevent="selectValue(filter, value)"
                @mouseenter="filter.autocompleteIndex = index"
                :class="[
                  'text-xs px-3 py-2 cursor-pointer',
                  index === filter.autocompleteIndex
                    ? 'bg-dt-accent dark:bg-dt-accent-dark-blue text-white'
                    : 'hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover text-dt-text-primary dark:text-dt-text-dark-primary'
                ]"
              >
                {{ value }}
              </div>
            </div>
          </div>
        </template>

        <!-- Remove Button -->
        <button
          @click="removeFilter(filter.id)"
          tabindex="-1"
          class="px-2 text-dt-text-secondary dark:text-dt-text-dark-secondary hover:text-dt-text-primary dark:hover:text-dt-text-dark-primary hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover transition-colors rounded-r cursor-pointer"
          aria-label="Remove filter"
        >
          ×
        </button>
      </div>
      </div>
    </div>

    <!-- Append slot for additional toolbar items -->
    <slot name="append"></slot>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import PlayIcon from '@/components/icons/PlayIcon.vue'
import PauseIcon from '@/components/icons/PauseIcon.vue'
import ClearIcon from '@/components/icons/ClearIcon.vue'
import FilterIcon from '@/components/icons/FilterIcon.vue'
import type { Filter, Operator } from '@/stores/filter'

// Types
interface FilterItem {
  id: string
  key: string | null
  operator: Operator | null
  value: string | null
  showAutocomplete?: boolean
  autocompleteIndex?: number
  showOperatorAutocomplete?: boolean
  operatorAutocompleteIndex?: number
  showKeyAutocomplete?: boolean
  keyAutocompleteIndex?: number
  isEditMode?: boolean
}

// Props
interface Props {
  filter: Filter[]
  filters: string[]
  valuesCb: (key: string) => string[]
  pause: boolean
}

const props = defineProps<Props>()

// Emits
const emit = defineEmits<{
  'update:filter': [filter: Filter[]]
  'update:pause': [paused: boolean]
  'clear': []
}>()

// State
const activeFilters = ref<FilterItem[]>([])
let filterIdCounter = 0

// Refs
const addFilterButton = ref<HTMLButtonElement | null>(null)
const keyInputRefs = new Map<string, HTMLInputElement>()
const operatorInputRefs = new Map<string, HTMLInputElement>()
const valueInputRefs = new Map<string, HTMLInputElement>()

// Set key input ref
function setKeyInputRef(filterId: string, el: any) {
  if (el) {
    keyInputRefs.set(filterId, el as HTMLInputElement)
  } else {
    keyInputRefs.delete(filterId)
  }
}

// Set operator input ref
function setOperatorInputRef(filterId: string, el: any) {
  if (el) {
    operatorInputRefs.set(filterId, el as HTMLInputElement)
  } else {
    operatorInputRefs.delete(filterId)
  }
}

// Set value input ref
function setValueInputRef(filterId: string, el: any) {
  if (el) {
    valueInputRefs.set(filterId, el as HTMLInputElement)
  } else {
    valueInputRefs.delete(filterId)
  }
}

// Check if focus is still within filter inputs
function isFocusWithinFilter(filterId: string): boolean {
  const activeEl = document.activeElement
  return activeEl === keyInputRefs.get(filterId) ||
         activeEl === operatorInputRefs.get(filterId) ||
         activeEl === valueInputRefs.get(filterId)
}

// Available operators
const availableOperators: Operator[] = [
  'is',
  'is not',
  'contains',
  'starts with',
  'does not start with',
  'does not contain',
  'ends with',
  'does not end with'
]

// Generate unique ID for filters
function generateFilterId(): string {
  return `filter-${filterIdCounter++}-${Date.now()}`
}

// Add new empty filter
function addFilter() {
  const newFilter = {
    id: generateFilterId(),
    key: null,
    operator: null,
    value: null,
    showAutocomplete: false,
    autocompleteIndex: 0,
    showOperatorAutocomplete: false,
    operatorAutocompleteIndex: 0,
    showKeyAutocomplete: false,
    keyAutocompleteIndex: 0,
    isEditMode: true, // Start in edit mode
  }
  
  activeFilters.value.unshift(newFilter) // Add to beginning of array
  
  // Focus the key input field after the DOM updates
  nextTick(() => {
    const inputEl = keyInputRefs.get(newFilter.id)
    if (inputEl) {
      inputEl.focus()
    }
  })
}

// Enter edit mode for a filter
function enterEditMode(filter: FilterItem) {
  filter.isEditMode = true
  
  // Focus the key input field after the DOM updates
  nextTick(() => {
    const inputEl = keyInputRefs.get(filter.id)
    if (inputEl) {
      inputEl.focus()
    }
  })
}

// Exit edit mode if filter is complete
function exitEditModeIfComplete(filter: FilterItem) {
  if (isFilterComplete(filter)) {
    filter.isEditMode = false
  }
}

// Remove filter by ID
function removeFilter(id: string) {
  activeFilters.value = activeFilters.value.filter(f => f.id !== id)
  // Clean up refs
  keyInputRefs.delete(id)
  operatorInputRefs.delete(id)
  valueInputRefs.delete(id)
  emitFilters() // Immediate emit on removal
}

// Get filtered keys based on current input
function getFilteredKeys(filter: FilterItem): string[] {
  if (!filter.key) return props.filters
  
  const searchTerm = filter.key.toLowerCase()
  const filtered = props.filters.filter(k => k.toLowerCase().includes(searchTerm))
  
  return filtered.length > 0 ? filtered : props.filters
}

// Handle key input
function onKeyInput(filter: FilterItem) {
  filter.showKeyAutocomplete = true
  filter.keyAutocompleteIndex = 0
}

// Handle key focus
function onKeyFocus(filter: FilterItem) {
  filter.showKeyAutocomplete = true
  filter.keyAutocompleteIndex = 0
}

// Handle key blur
function onKeyBlur(filter: FilterItem) {
  // Delay to allow click events on dropdown items to fire and focus transitions
  setTimeout(() => {
    filter.showKeyAutocomplete = false
    
    // Validate key on blur
    if (filter.key && typeof filter.key === 'string') {
      const exactMatch = props.filters.find(k => k === filter.key)
      if (!exactMatch) {
        // Clear invalid key and dependent fields
        filter.key = null
        filter.operator = null
        filter.value = null
      }
    }
    
    // Only remove if focus left the filter AND it's incomplete
    if (!isFocusWithinFilter(filter.id) && !isFilterComplete(filter)) {
      removeFilter(filter.id)
      return
    }
    
    onFilterChange(filter)
    exitEditModeIfComplete(filter)
  }, 150)
}

// Select key from autocomplete
function selectKey(filter: FilterItem, key: string) {
  filter.key = key
  filter.showKeyAutocomplete = false
  
  // Reset operator and value when key changes
  filter.operator = null
  filter.value = null
  
  onFilterChange(filter)
}

// Handle keyboard navigation in key autocomplete
function onKeyKeydown(event: KeyboardEvent, filter: FilterItem) {
  const filteredKeys = getFilteredKeys(filter)
  
  if (!filter.showKeyAutocomplete || filteredKeys.length === 0) {
    return
  }
  
  const currentIndex = filter.keyAutocompleteIndex ?? 0
  
  switch (event.key) {
    case 'ArrowDown':
      event.preventDefault()
      filter.keyAutocompleteIndex = Math.min(currentIndex + 1, filteredKeys.length - 1)
      // Scroll the highlighted item into view
      nextTick(() => {
        const dropdown = document.querySelector(`[data-autocomplete-key="${filter.id}"]`)
        const item = dropdown?.querySelector(`[data-index="${filter.keyAutocompleteIndex}"]`)
        if (item) {
          item.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
        }
      })
      break
    case 'ArrowUp':
      event.preventDefault()
      filter.keyAutocompleteIndex = Math.max(currentIndex - 1, 0)
      // Scroll the highlighted item into view
      nextTick(() => {
        const dropdown = document.querySelector(`[data-autocomplete-key="${filter.id}"]`)
        const item = dropdown?.querySelector(`[data-index="${filter.keyAutocompleteIndex}"]`)
        if (item) {
          item.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
        }
      })
      break
    case 'Enter':
    case 'Tab':
      event.preventDefault()
      if (filteredKeys[currentIndex]) {
        selectKey(filter, filteredKeys[currentIndex])
        // Move focus to operator field
        nextTick(() => {
          const operatorInput = operatorInputRefs.get(filter.id)
          if (operatorInput) {
            operatorInput.focus()
          }
        })
      }
      break
    case 'Escape':
      event.preventDefault()
      filter.showKeyAutocomplete = false
      break
  }
}

// Get filtered operators based on current input
function getFilteredOperators(filter: FilterItem): Operator[] {
  if (!filter.operator || typeof filter.operator !== 'string') return availableOperators
  
  const searchTerm = filter.operator.toLowerCase()
  const filtered = availableOperators.filter(op => op.toLowerCase().includes(searchTerm))
  
  return filtered.length > 0 ? filtered : availableOperators
}

// Handle operator input
function onOperatorInput(filter: FilterItem) {
  filter.showOperatorAutocomplete = true
  filter.operatorAutocompleteIndex = 0
}

// Handle operator focus
function onOperatorFocus(filter: FilterItem) {
  filter.showOperatorAutocomplete = true
  filter.operatorAutocompleteIndex = 0
}

// Handle operator blur
function onOperatorBlur(filter: FilterItem) {
  // Delay to allow click events on dropdown items to fire and focus transitions
  setTimeout(() => {
    filter.showOperatorAutocomplete = false
    
    // Validate operator on blur
    if (filter.operator && typeof filter.operator === 'string') {
      const exactMatch = availableOperators.find(op => op === filter.operator)
      if (!exactMatch) {
        // Clear invalid operator
        filter.operator = null
      }
    }
    
    // Only remove if focus left the filter AND it's incomplete
    if (!isFocusWithinFilter(filter.id) && !isFilterComplete(filter)) {
      removeFilter(filter.id)
      return
    }
    
    onFilterChange(filter)
    exitEditModeIfComplete(filter)
  }, 150)
}

// Select operator from autocomplete
function selectOperator(filter: FilterItem, operator: Operator) {
  filter.operator = operator
  filter.showOperatorAutocomplete = false
  onFilterChange(filter)
}

// Handle keyboard navigation in operator autocomplete
function onOperatorKeydown(event: KeyboardEvent, filter: FilterItem) {
  const filteredOperators = getFilteredOperators(filter)
  
  if (!filter.showOperatorAutocomplete || filteredOperators.length === 0) {
    return
  }
  
  const currentIndex = filter.operatorAutocompleteIndex ?? 0
  
  switch (event.key) {
    case 'ArrowDown':
      event.preventDefault()
      filter.operatorAutocompleteIndex = Math.min(currentIndex + 1, filteredOperators.length - 1)
      // Scroll the highlighted item into view
      nextTick(() => {
        const dropdown = document.querySelector(`[data-autocomplete-operator="${filter.id}"]`)
        const item = dropdown?.querySelector(`[data-index="${filter.operatorAutocompleteIndex}"]`)
        if (item) {
          item.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
        }
      })
      break
    case 'ArrowUp':
      event.preventDefault()
      filter.operatorAutocompleteIndex = Math.max(currentIndex - 1, 0)
      // Scroll the highlighted item into view
      nextTick(() => {
        const dropdown = document.querySelector(`[data-autocomplete-operator="${filter.id}"]`)
        const item = dropdown?.querySelector(`[data-index="${filter.operatorAutocompleteIndex}"]`)
        if (item) {
          item.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
        }
      })
      break
    case 'Enter':
    case 'Tab':
      event.preventDefault()
      if (filteredOperators[currentIndex]) {
        selectOperator(filter, filteredOperators[currentIndex])
        // Move focus to value field
        nextTick(() => {
          const valueInput = valueInputRefs.get(filter.id)
          if (valueInput) {
            valueInput.focus()
          }
        })
      }
      break
    case 'Escape':
      event.preventDefault()
      filter.showOperatorAutocomplete = false
      break
  }
}

// Check if filter is complete
function isFilterComplete(filter: FilterItem): filter is FilterItem & { key: string; operator: Operator; value: string } {
  return filter.key !== null && filter.operator !== null && filter.value !== null
}

// Emit complete filters
function emitFilters() {
  const completeFilters: Filter[] = activeFilters.value
    .filter(isFilterComplete)
    .map(f => ({
      key: f.key,
      operator: f.operator,
      value: f.value,
    }))
  
  emit('update:filter', completeFilters)
}

// Debounced emit
const debouncedEmitFilters = useDebounceFn(() => {
  emitFilters()
}, 300)

// Handle filter changes
function onFilterChange(filter: FilterItem) {
  // Only emit if filter is complete
  if (isFilterComplete(filter)) {
    debouncedEmitFilters()
  }
}

// Toggle pause state
function togglePause() {
  emit('update:pause', !props.pause)
}

// Clear storage
function clearStorage() {
  emit('clear')
}

// Get filtered values based on current input
function getFilteredValues(filter: FilterItem): string[] {
  if (!filter.key) return []
  const allValues = props.valuesCb(filter.key)
  if (!filter.value) return allValues
  
  const searchTerm = filter.value.toLowerCase()
  return allValues.filter(v => v.toLowerCase().includes(searchTerm))
}

// Handle value input
function onValueInput(filter: FilterItem) {
  filter.showAutocomplete = true
  filter.autocompleteIndex = 0
  onFilterChange(filter)
}

// Handle value focus
function onValueFocus(filter: FilterItem) {
  filter.showAutocomplete = true
  filter.autocompleteIndex = 0
}

// Handle value blur
function onValueBlur(filter: FilterItem) {
  // Delay to allow click events on dropdown items to fire and focus transitions
  setTimeout(() => {
    filter.showAutocomplete = false
    
    // Only remove if focus left the filter AND it's incomplete
    if (!isFocusWithinFilter(filter.id) && !isFilterComplete(filter)) {
      removeFilter(filter.id)
      return
    }
    
    exitEditModeIfComplete(filter)
  }, 150)
}

// Select value from autocomplete
function selectValue(filter: FilterItem, value: string) {
  filter.value = value
  filter.showAutocomplete = false
  onFilterChange(filter)
}

// Handle keyboard navigation in autocomplete
function onValueKeydown(event: KeyboardEvent, filter: FilterItem) {
  const filteredValues = getFilteredValues(filter)
  
  // If autocomplete is showing and has items, handle navigation
  if (filter.showAutocomplete && filteredValues.length > 0) {
    const currentIndex = filter.autocompleteIndex ?? 0
    
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault()
        filter.autocompleteIndex = Math.min(currentIndex + 1, filteredValues.length - 1)
        // Scroll the highlighted item into view
        nextTick(() => {
          const dropdown = document.querySelector(`[data-autocomplete-value="${filter.id}"]`)
          const item = dropdown?.querySelector(`[data-index="${filter.autocompleteIndex}"]`)
          if (item) {
            item.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
          }
        })
        return
      case 'ArrowUp':
        event.preventDefault()
        filter.autocompleteIndex = Math.max(currentIndex - 1, 0)
        // Scroll the highlighted item into view
        nextTick(() => {
          const dropdown = document.querySelector(`[data-autocomplete-value="${filter.id}"]`)
          const item = dropdown?.querySelector(`[data-index="${filter.autocompleteIndex}"]`)
          if (item) {
            item.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
          }
        })
        return
      case 'Enter':
      case 'Tab':
        event.preventDefault()
        if (filteredValues[currentIndex]) {
          selectValue(filter, filteredValues[currentIndex])
          // Remove focus and return to window
          nextTick(() => {
            const activeEl = document.activeElement as HTMLElement
            if (activeEl) {
              activeEl.blur()
            }
          })
        }
        return
      case 'Escape':
        event.preventDefault()
        filter.showAutocomplete = false
        return
    }
  }
  
  // If Tab or Enter and no autocomplete, remove focus and return to window
  if (event.key === 'Tab' || event.key === 'Enter') {
    event.preventDefault()
    const activeEl = document.activeElement as HTMLElement
    if (activeEl) {
      activeEl.blur()
    }
  }
}

// Watch for external changes to activeFilters
watch(
  () => activeFilters.value.length,
  () => {
    // Emit when filters are added or removed
    const hasCompleteFilters = activeFilters.value.some(isFilterComplete)
    if (hasCompleteFilters || activeFilters.value.length === 0) {
      debouncedEmitFilters()
    }
  }
)

// Watch for incoming filter prop changes and sync to activeFilters
watch(
  () => props.filter,
  (newFilters) => {
    // Get current complete filters
    const currentCompleteFilters = activeFilters.value
      .filter(isFilterComplete)
      .map(f => ({
        key: f.key,
        operator: f.operator,
        value: f.value,
      }))
    
    // Only sync if the incoming filters are different from our current complete filters
    // This prevents sync loops when the change originated from our own emit
    const filtersChanged = 
      newFilters.length !== currentCompleteFilters.length ||
      newFilters.some((newFilter, index) => {
        const current = currentCompleteFilters[index]
        return !current || 
               newFilter.key !== current.key || 
               newFilter.operator !== current.operator || 
               newFilter.value !== current.value
      })
    
    if (!filtersChanged) {
      return
    }
    
    // Convert Filter[] to FilterItem[] with UI state
    activeFilters.value = newFilters.map(filter => ({
      id: generateFilterId(),
      key: filter.key,
      operator: filter.operator,
      value: filter.value,
      showAutocomplete: false,
      autocompleteIndex: 0,
      showOperatorAutocomplete: false,
      operatorAutocompleteIndex: 0,
      showKeyAutocomplete: false,
      keyAutocompleteIndex: 0,
      isEditMode: false,
    }))
  },
  { deep: true, immediate: true }
)
</script>

<style scoped>
.toolbar {
  min-height: 40px;
  overflow: visible;
  position: relative;
  z-index: 100;
}

/* Wrapper allows vertical overflow for dropdowns */
.filters-wrapper {
  position: relative;
}

/* Scrollable container for horizontal overflow only */
.filters-scroll-container {
  overflow-x: auto;
  scroll-behavior: smooth;
  display: flex;
  align-items: center;
  gap: 0.5rem;
  /* Create space for dropdowns to render below */
  padding-bottom: 200px;
  margin-bottom: -200px;
  /* Don't capture pointer events in the padding area */
  pointer-events: none;
  /* Hide scrollbar - Firefox */
  scrollbar-width: none;
  /* Hide scrollbar - IE/Edge */
  -ms-overflow-style: none;
}

/* Hide scrollbar - WebKit (Chrome, Safari) */
.filters-scroll-container::-webkit-scrollbar {
  display: none;
}

/* Ensure filter items maintain positioning context for dropdowns */
.filter-item {
  position: relative;
  flex-shrink: 0;
  /* Re-enable pointer events on the actual filter items */
  pointer-events: auto;
}

/* Make dropdowns render outside scroll container using higher z-index */
.filter-item :deep(.absolute) {
  z-index: 200;
}
</style>

