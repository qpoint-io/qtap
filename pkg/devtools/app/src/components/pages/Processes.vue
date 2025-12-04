<template>
  <div class="h-full flex flex-col">
    <!-- Toolbar -->
    <Toolbar
      :filters="filterableKeys"
      :values-cb="getFilterValues"
      v-model:filter="filters"
      v-model:pause="isPaused"
      @clear="handleClear"
    >
      <template #append>
        <ColumnToggle
          :columns="columns"
          @toggle="handleColumnToggle"
          @reset="handleResetColumns"
        />
        <BufferSettings
          entity="process"
          v-model:maxItems="maxItems"
          v-model:maxSize="maxSize"
          v-model:maxSizeUnit="maxSizeUnit"
          @reset="resetBufferSettings"
        />
      </template>
    </Toolbar>

    <!-- Main Content with Split Panel -->
    <SplitPanel
      ref="splitPanelRef"
      storage-key="devtools_processes_panel_width"
      :default-width="30"
      :min-width="10"
      :max-width="90"
      :show-right="!!selectedProcess"
      :class="{ 'select-none': isColumnResizing }"
    >
      <template #left>
        <!-- New Processes Button (shown when frozen with new items and scrollable) -->
        <UxPill
          v-if="frozenMode && newUnseenItems && isScrollable"
          :on-click="jumpToTop"
          size="small"
          class="absolute top-10 -translate-x-1/2 z-30 shadow-lg transition-colors"
          :style="{ left: pillCenterPosition }"
        >
          <DoubleUpIcon class="w-3.5 h-3.5" />
          New Processes
          <DoubleUpIcon class="w-3.5 h-3.5" />
        </UxPill>

        <!-- Table Body -->
        <div 
          ref="scrollContainer"
          class="flex-1 overflow-y-auto overflow-x-auto"
          @scroll="handleScroll"
        >
          <!-- Table Header (Sticky) -->
          <div
            class="flex bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary border-b border-dt-border-light dark:border-dt-border-dark-light text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary sticky top-0 z-10"
            :style="{ height: `${ITEM_HEIGHT}px` }"
          >
            <template v-for="(column, index) in displayedColumns" :key="column.key">
              <div
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light relative flex items-center group"
                :class="{
                  'text-right justify-end': column.key === 'duration',
                  'text-center justify-center': column.key === 'status',
                }"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                <span class="truncate">{{ column.label }}</span>
                
                <!-- Resize Handle -->
                <div
                  v-if="column.resizable && index < displayedColumns.length - 1"
                  @mousedown="(e) => handleColumnResizeStart(e, column.key)"
                  @dblclick="() => handleAutoFit(column.key)"
                  class="absolute right-0 top-0 bottom-0 w-1 cursor-col-resize hover:bg-dt-accent dark:hover:bg-dt-accent-dark-blue opacity-0 group-hover:opacity-100 transition-opacity z-20"
                ></div>
              </div>
            </template>
          </div>

          <!-- Top spacer for non-rendered items above viewport -->
          <div :style="{ height: `${topSpacerHeight}px` }"></div>

          <!-- Render only visible items -->
          <div
            v-for="process in visibleProcesses"
            :key="process.pid"
            @click="selectProcess(process)"
            class="flex text-xs border-b border-dt-border-light dark:border-dt-border-dark-light cursor-pointer hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover"
            :class="{
              'bg-dt-bg-selected dark:bg-dt-bg-dark-selected': params.process_id === String(process.pid),
            }"
            :style="{ height: `${ITEM_HEIGHT}px` }"
          >
            <template v-for="column in displayedColumns" :key="column.key">
              <!-- Timestamp -->
              <div
                v-if="column.key === 'timestamp'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ formatTimestamp(process.createdAt) }}
              </div>

              <!-- Status -->
              <div
                v-else-if="column.key === 'status'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light flex items-center justify-center"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                <StatusBadge :status="process.status" type="process" />
              </div>

              <!-- PID -->
              <div
                v-else-if="column.key === 'pid'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-dt-text-secondary dark:text-dt-text-dark-secondary font-mono text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ process.pid }}
              </div>

              <!-- Binary -->
              <div
                v-else-if="column.key === 'binary'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-primary dark:text-dt-text-dark-primary text-[11px] font-mono"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ process.binary }}
              </div>

              <!-- Path -->
              <div
                v-else-if="column.key === 'path'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px] font-mono"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ process.path }}
              </div>

              <!-- User -->
              <div
                v-else-if="column.key === 'user'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ process.user.name }}
              </div>

              <!-- Container -->
              <div
                v-else-if="column.key === 'container'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ process.container?.name || '-' }}
              </div>

              <!-- Pod -->
              <div
                v-else-if="column.key === 'pod'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ process.pod?.name || '-' }}
              </div>

              <!-- Duration -->
              <div
                v-else-if="column.key === 'duration'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-right text-dt-text-secondary dark:text-dt-text-dark-secondary"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ formatDuration(process.duration) }}
              </div>
            </template>
          </div>

          <!-- Bottom spacer for non-rendered items below viewport -->
          <div :style="{ height: `${bottomSpacerHeight}px` }"></div>
        </div>
      </template>

      <template #right>
        <!-- Process Summary -->
        <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary border-b border-dt-border-light dark:border-dt-border-dark-light p-3 flex items-start justify-between">
          <div class="flex-1">
            <div class="flex items-center gap-3 mb-2">
              <StatusBadge :status="selectedProcess!.status" type="process" />
              <span class="text-xs text-dt-text-secondary dark:text-dt-text-dark-secondary font-mono">
                PID: {{ selectedProcess!.pid }}
              </span>
              <span class="text-sm font-semibold text-dt-text-primary dark:text-dt-text-dark-primary">
                {{ selectedProcess!.binary }}
              </span>
              <span class="text-xs text-dt-text-secondary dark:text-dt-text-dark-secondary" v-if="selectedProcess!.duration">
                {{ formatDuration(selectedProcess!.duration) }}
              </span>
            </div>
            <div class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary break-all">
              {{ selectedProcess!.path }}
            </div>
          </div>

          <!-- Close Button -->
          <button
            @click="closeProcess"
            class="ml-3 p-1 hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover rounded text-dt-text-secondary dark:text-dt-text-dark-secondary hover:text-dt-text-primary dark:hover:text-dt-text-dark-primary cursor-pointer"
            title="Close"
          >
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <!-- Detail Content -->
        <div class="flex-1 overflow-y-auto p-3 bg-dt-bg-primary dark:bg-dt-bg-dark-primary">
          <!-- Process Info Section -->
          <div class="mb-4">
            <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Process Info</h3>
            <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
              <div class="space-y-1">
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Binary:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedProcess!.binary }}</span>
                </div>
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Path:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedProcess!.path }}</span>
                </div>
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Hostname:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedProcess!.hostname }}</span>
                </div>
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">PID:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedProcess!.pid }}</span>
                </div>
              </div>
            </div>
          </div>

          <!-- User Section -->
          <div class="mb-4">
            <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">User</h3>
            <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
              <div class="space-y-1">
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Name:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedProcess!.user.name }}</span>
                </div>
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">ID:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedProcess!.user.id }}</span>
                </div>
              </div>
            </div>
          </div>

          <!-- Container Section -->
          <div class="mb-4" v-if="selectedProcess!.container">
            <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Container</h3>
            <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
              <div class="space-y-1">
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Name:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedProcess!.container.name }}</span>
                </div>
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Image:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedProcess!.container.image }}</span>
                </div>
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">ID:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedProcess!.container.id }}</span>
                </div>
                <div v-if="selectedProcess!.container.labels" class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Labels:</span>
                  <pre class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2 mt-1 p-2 bg-dt-bg-primary dark:bg-dt-bg-dark-primary rounded border border-dt-border-light dark:border-dt-border-dark-light">{{ formatJson(selectedProcess!.container.labels) }}</pre>
                </div>
              </div>
            </div>
          </div>

          <!-- Pod Section -->
          <div v-if="selectedProcess!.pod">
            <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Pod</h3>
            <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
              <div class="space-y-1">
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Name:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedProcess!.pod.name }}</span>
                </div>
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Namespace:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedProcess!.pod.namespace }}</span>
                </div>
                <div v-if="selectedProcess!.pod.labels" class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Labels:</span>
                  <pre class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2 mt-1 p-2 bg-dt-bg-primary dark:bg-dt-bg-dark-primary rounded border border-dt-border-light dark:border-dt-border-dark-light">{{ formatJson(selectedProcess!.pod.labels) }}</pre>
                </div>
              </div>
            </div>
          </div>
        </div>
      </template>
    </SplitPanel>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { useUrlParams } from '@/composables/urlParams'
import { formatTimestamp, formatDuration } from '@/composables/formatters'
import { useProcessesStore, type Process } from '@/stores/processes'
import { useProcesses } from '@/composables/processes'
import { useColumnResize, type ColumnConfig } from '@/composables/columnResize'
import StatusBadge from '@/components/ux/StatusBadge.vue'
import Toolbar from '@/components/ux/Toolbar.vue'
import ColumnToggle from '@/components/ux/ColumnToggle.vue'
import BufferSettings from '@/components/ux/BufferSettings.vue'
import SplitPanel from '@/components/ux/SplitPanel.vue'
import UxPill from '@/components/ux/Pill.vue'
import DoubleUpIcon from '@/components/icons/DoubleUpIcon.vue'

// Default column configuration
const defaultColumns: ColumnConfig[] = [
  { key: 'timestamp', label: 'Timestamp', width: 128, minWidth: 80, visible: true, resizable: true },
  { key: 'status', label: 'Status', width: 80, minWidth: 60, visible: true, resizable: true },
  { key: 'pid', label: 'PID', width: 80, minWidth: 50, visible: true, resizable: true },
  { key: 'binary', label: 'Binary', width: 140, minWidth: 80, visible: true, resizable: true },
  { key: 'path', label: 'Path', width: 300, minWidth: 100, visible: true, resizable: true },
  { key: 'user', label: 'User', width: 100, minWidth: 60, visible: true, resizable: true },
  { key: 'container', label: 'Container', width: 140, minWidth: 80, visible: true, resizable: true },
  { key: 'pod', label: 'Pod', width: 140, minWidth: 80, visible: true, resizable: true },
  { key: 'duration', label: 'Duration', width: 80, minWidth: 50, visible: true, resizable: true },
]

// use Processes data
const { 
  processes, 
  isPaused, 
  filters, 
  filterableKeys, 
  getFilterValues,
  maxItems,
  maxSize,
  maxSizeUnit,
  resetBufferSettings,
} = useProcesses()

const store = useProcessesStore()

// Split panel ref for reset
const splitPanelRef = ref<InstanceType<typeof SplitPanel> | null>(null)

// Compute pill center position based on whether details panel is open
const pillCenterPosition = computed(() => {
  if (selectedProcess.value && splitPanelRef.value?.panelWidth) {
    return `${splitPanelRef.value.panelWidth / 2}%`
  }
  return '50%'
})

// Column resize composable
const {
  columns,
  visibleColumns,
  isResizing: isColumnResizing,
  toggleColumn,
  resetColumns,
  startColumnResize,
} = useColumnResize({
  storageKey: 'devtools_processes_columns',
  defaultColumns,
})

// Displayed columns
const displayedColumns = computed(() => visibleColumns.value)

// Track if user has manually resized any column
const CUSTOM_RESIZE_KEY = 'devtools_processes_custom_resize'
const hasCustomResizing = ref(localStorage.getItem(CUSTOM_RESIZE_KEY) === 'true')

// Expand path column to fill available space
const expandPathToFillSpace = (force = false) => {
  if (!scrollContainer.value) return
  if (!force && hasCustomResizing.value) return // Don't auto-expand if user has customized (unless forced)
  
  const pathCol = columns.value.find(col => col.key === 'path')
  if (!pathCol || !pathCol.visible) return
  
  const availableWidth = scrollContainer.value.clientWidth
  
  let otherColumnsWidth = 0
  for (const col of visibleColumns.value) {
    if (col.key !== 'path') {
      otherColumnsWidth += col.width
    }
  }
  
  const targetWidth = Math.max(pathCol.minWidth, availableWidth - otherColumnsWidth)
  pathCol.width = targetWidth
}

// Handle column resize start
const handleColumnResizeStart = (e: MouseEvent, key: string) => {
  hasCustomResizing.value = true
  localStorage.setItem(CUSTOM_RESIZE_KEY, 'true')
  startColumnResize(e, key)
}

// Handle auto-fit column on double-click
const handleAutoFit = (key: string) => {
  hasCustomResizing.value = true
  localStorage.setItem(CUSTOM_RESIZE_KEY, 'true')
  
  const defaultCol = defaultColumns.find(c => c.key === key)
  if (defaultCol) {
    const col = columns.value.find(c => c.key === key)
    if (col) {
      col.width = defaultCol.width
    }
  }
  // Expand path column to fill remaining space
  requestAnimationFrame(() => expandPathToFillSpace(true))
}

// Handle clear button
function handleClear() {
  store.clearProcesses()
}

// Handle column toggle - always expand path to fill available space
function handleColumnToggle(key: string) {
  toggleColumn(key)
  requestAnimationFrame(() => expandPathToFillSpace(true))
}

// Handle reset columns
function handleResetColumns() {
  resetColumns()
  splitPanelRef.value?.resetWidth()
  hasCustomResizing.value = false
  localStorage.removeItem(CUSTOM_RESIZE_KEY)
  requestAnimationFrame(() => expandPathToFillSpace())
}

const params = useUrlParams()
const scrollContainer = ref<HTMLElement | null>(null)
const frozenMode = ref(false)
const newUnseenItems = ref(false)
const isScrollable = ref(false)

// Reactive scroll position for virtual scrolling
const scrollTop = ref(0)
const containerHeight = ref(0)

// Auto-expand on initial load - always force since path width is calculated dynamically
watch(scrollContainer, (container) => {
  if (container) {
    containerHeight.value = container.clientHeight
    // Always expand path to fill available space on initial load
    // (path width is not persisted, so it must be calculated fresh)
    nextTick(() => expandPathToFillSpace(true))
  }
}, { immediate: true })

// ResizeObserver to auto-expand path when container width changes
let resizeObserver: ResizeObserver | null = null
let lastContainerWidth = 0

onMounted(() => {
  resizeObserver = new ResizeObserver((entries) => {
    for (const entry of entries) {
      const newWidth = entry.contentRect.width
      // Only trigger if width actually changed (not just height)
      if (newWidth !== lastContainerWidth) {
        lastContainerWidth = newWidth
        expandPathToFillSpace(true)
      }
    }
  })
  
  if (scrollContainer.value) {
    lastContainerWidth = scrollContainer.value.clientWidth
    resizeObserver.observe(scrollContainer.value)
  }
})

onUnmounted(() => {
  resizeObserver?.disconnect()
})

// Virtual scrolling configuration - row height must match style binding
const ITEM_HEIGHT = 29
const HEADER_HEIGHT = 29
const BUFFER_SIZE = 50

// Calculate visible range based on scroll position
const visibleRange = computed(() => {
  if (!scrollContainer.value) {
    return { start: 0, end: processes.value.length }
  }
  
  // Use reactive scrollTop instead of DOM property
  const currentScrollTop = scrollTop.value
  const viewportHeight = containerHeight.value || scrollContainer.value.clientHeight
  
  const startIndex = Math.max(0, Math.floor(currentScrollTop / ITEM_HEIGHT) - BUFFER_SIZE)
  const endIndex = Math.min(
    processes.value.length,
    Math.ceil((currentScrollTop + viewportHeight) / ITEM_HEIGHT) + BUFFER_SIZE
  )
  
  return { start: startIndex, end: endIndex }
})

// Get only items that should be rendered
const visibleProcesses = computed(() => {
  const { start, end } = visibleRange.value
  
  const selected = selectedProcess.value
  if (selected) {
    const selectedIndex = processes.value.findIndex(
      proc => proc.pid === selected.pid
    )
    if (selectedIndex !== -1 && (selectedIndex < start || selectedIndex >= end)) {
      const visible = processes.value.slice(start, end)
      return [...visible, selected]
    }
  }
  
  return processes.value.slice(start, end)
})

// Spacer heights to maintain scroll position
const topSpacerHeight = computed(() => visibleRange.value.start * ITEM_HEIGHT)
const bottomSpacerHeight = computed(() => 
  (processes.value.length - visibleRange.value.end) * ITEM_HEIGHT
)

const formatJson = (obj: any): string => {
  return JSON.stringify(obj, null, 2)
}

const selectProcess = (process: Process) => {
  params.process_id = String(process.pid)
}

const closeProcess = () => {
  delete params.process_id
}

const selectedProcess = computed(() => {
  const processId = params.process_id as string
  if (!processId) return null
  const pid = parseInt(processId, 10)
  if (isNaN(pid)) return null
  return store.getProcessByPid(pid)
})

// Check if container is scrollable
const checkScrollable = () => {
  if (!scrollContainer.value) {
    isScrollable.value = false
    return
  }
  isScrollable.value = scrollContainer.value.scrollHeight > scrollContainer.value.clientHeight
}

// Check if scrolled to top (with 5px threshold)
const isAtTop = (): boolean => {
  if (!scrollContainer.value) return true
  return scrollContainer.value.scrollTop < 5
}

// Scroll to top of container
const scrollToTop = () => {
  if (!scrollContainer.value) return
  scrollContainer.value.scrollTop = 0
}

// Handle manual scroll events
const handleScroll = () => {
  // Update reactive scroll position for virtual scrolling
  if (scrollContainer.value) {
    scrollTop.value = scrollContainer.value.scrollTop
    containerHeight.value = scrollContainer.value.clientHeight
  }
  
  checkScrollable()
  
  if (isAtTop() && !selectedProcess.value) {
    frozenMode.value = false
    newUnseenItems.value = false
  } else if (!isAtTop() && !frozenMode.value) {
    frozenMode.value = true
  }
}

// Jump to top button handler
const jumpToTop = () => {
  frozenMode.value = false
  newUnseenItems.value = false
  nextTick(() => scrollToTop())
}

// Watch for new processes
watch(
  () => processes.value[0]?.pid,
  (newPid, oldPid) => {
    if (newPid && newPid !== oldPid) {
      if (frozenMode.value) {
        const container = scrollContainer.value
        if (container) {
          const oldScrollTop = container.scrollTop
          
          if (!isAtTop()) {
            newUnseenItems.value = true
          }
          
          nextTick(() => {
            container.scrollTop = oldScrollTop + ITEM_HEIGHT
            checkScrollable()
          })
        }
      } else if (isAtTop()) {
        nextTick(() => {
          scrollToTop()
          checkScrollable()
        })
      }
    }
  }
)

// Enter frozen mode when item is selected
watch(
  () => selectedProcess.value,
  (newVal) => {
    if (newVal) {
      frozenMode.value = true
    }
  }
)

// Auto-scroll to selected process if it's not visible
watch(
  [() => selectedProcess.value?.pid, scrollContainer],
  ([selectedPid, container]) => {
    if (selectedPid === undefined || selectedPid === null || !container) return
    
    nextTick(() => {
      const selectedIndex = processes.value.findIndex(
        proc => proc.pid === selectedPid
      )
      
      if (selectedIndex === -1) return
      
      const scrollTop = container.scrollTop
      const viewportHeight = container.clientHeight
      const itemTop = selectedIndex * ITEM_HEIGHT
      const itemBottom = itemTop + ITEM_HEIGHT
      
      const effectiveScrollTop = scrollTop + HEADER_HEIGHT
      const effectiveViewportHeight = viewportHeight - HEADER_HEIGHT
      const isAboveViewport = itemTop < effectiveScrollTop
      const isBelowViewport = itemBottom > (effectiveScrollTop + effectiveViewportHeight)
      
      if (isAboveViewport || isBelowViewport) {
        const targetScroll = itemTop - (effectiveViewportHeight * 0.01) - HEADER_HEIGHT
        container.scrollTop = Math.max(0, targetScroll)
      }
    })
  },
  { flush: 'post' }
)
</script>
