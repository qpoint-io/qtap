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
          entity="connection"
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
      storage-key="devtools_connections_panel_width"
      :default-width="40"
      :min-width="10"
      :max-width="90"
      :show-right="!!selectedConnection"
      :class="{ 'select-none': isColumnResizing }"
    >
      <template #left>
        <!-- New Connections Button (shown when frozen with new items and scrollable) -->
        <UxPill
          v-if="frozenMode && newUnseenItems && isScrollable"
          :on-click="jumpToTop"
          size="small"
          class="absolute top-10 -translate-x-1/2 z-30 shadow-lg transition-colors"
          :style="{ left: pillCenterPosition }"
        >
          <DoubleUpIcon class="w-3.5 h-3.5" />
          New Connections
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
                  'justify-between': column.key === 'destination',
                }"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                <span class="truncate">{{ column.label }}</span>
                
                <!-- Show IPs checkbox for destination column -->
                <label
                  v-if="column.key === 'destination'"
                  class="flex items-center gap-1.5 text-[10px] cursor-pointer shrink-0 ml-2"
                  @click.stop
                >
                  <input
                    type="checkbox"
                    v-model="showIPs"
                    class="w-3 h-3 cursor-pointer"
                  />
                  <span class="text-dt-text-tertiary dark:text-dt-text-dark-tertiary">show IPs</span>
                </label>
                
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
            v-for="connection in visibleConnections"
            :key="connection.meta.connectionId"
            @click="selectConnection(connection)"
            class="flex text-xs border-b border-dt-border-light dark:border-dt-border-dark-light cursor-pointer hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover"
            :class="{
              'bg-dt-bg-selected dark:bg-dt-bg-dark-selected': params.connection_id === connection.meta.connectionId,
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
                {{ formatTimestamp(connection.createdAt) }}
              </div>

              <!-- Direction -->
              <div
                v-else-if="column.key === 'direction'"
                class="px-[5px] py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light flex items-center"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                <DirectionIndicator :direction="connection.direction" />
              </div>

              <!-- Source -->
              <div
                v-else-if="column.key === 'source'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px] font-mono"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ formatAddress(connection.source) }}
              </div>

              <!-- Destination -->
              <div
                v-else-if="column.key === 'destination'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px] font-mono"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ getDestinationDisplay(connection) }}
              </div>

              <!-- Status -->
              <div
                v-else-if="column.key === 'status'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-center flex items-center justify-center"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                <StatusBadge :status="connection.status" type="connection" />
              </div>

              <!-- Socket -->
              <div
                v-else-if="column.key === 'socket'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-dt-text-secondary dark:text-dt-text-dark-secondary font-mono text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ connection.socketProtocol }}
              </div>

              <!-- L7 -->
              <div
                v-else-if="column.key === 'l7'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-dt-text-secondary dark:text-dt-text-dark-secondary font-mono text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ connection.l7Protocol }}
              </div>

              <!-- Process -->
              <div
                v-else-if="column.key === 'process'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px] font-mono"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ connection.source.exe || '-' }}
              </div>

              <!-- Duration -->
              <div
                v-else-if="column.key === 'duration'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-right text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ formatDuration(connection.duration) }}
              </div>
            </template>
          </div>

          <!-- Bottom spacer for non-rendered items below viewport -->
          <div :style="{ height: `${bottomSpacerHeight}px` }"></div>
        </div>
      </template>

      <template #right>
        <!-- Connection Summary -->
        <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary border-b border-dt-border-light dark:border-dt-border-dark-light p-3 flex items-start justify-between">
          <div class="flex-1">
            <div class="flex items-center gap-3 mb-2">
              <DirectionIndicator :direction="selectedConnection!.direction" :show-label="false" />
              <StatusBadge :status="selectedConnection!.status" type="connection" />
              <span class="text-xs text-dt-text-secondary dark:text-dt-text-dark-secondary font-mono">
                {{ selectedConnection!.socketProtocol }}/{{ selectedConnection!.l7Protocol }}
              </span>
              <span class="text-xs text-dt-text-secondary dark:text-dt-text-dark-secondary" v-if="selectedConnection!.duration">
                {{ formatDuration(selectedConnection!.duration) }}
              </span>
            </div>
            <div class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary break-all">
              {{ formatAddress(selectedConnection!.source) }} → {{ formatAddress(selectedConnection!.destination) }}
            </div>
            <div class="flex gap-4 mt-2 text-xs text-dt-text-tertiary dark:text-dt-text-dark-tertiary">
              <span>ID: {{ truncateId(selectedConnection!.meta.connectionId) }}</span>
            </div>
          </div>

          <!-- Close Button -->
          <button
            @click="closeConnection"
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
          <!-- Connection Info Section -->
          <div class="mb-4">
            <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Connection Info</h3>
            <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
              <div class="space-y-1">
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Connection ID:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedConnection!.meta.connectionId }}</span>
                </div>
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Endpoint ID:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedConnection!.meta.endpointId }}</span>
                </div>
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Part:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedConnection!.part }}</span>
                </div>
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Created At:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedConnection!.createdAt }}</span>
                </div>
              </div>
            </div>
          </div>

          <!-- Source Section -->
          <div class="mb-4">
            <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Source</h3>
            <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
              <div class="space-y-1">
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Address:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ formatAddressWithFamily(selectedConnection!.source) }}</span>
                </div>
                <div class="text-xs font-mono" v-if="selectedConnection!.source.hostname">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Hostname:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedConnection!.source.hostname }}</span>
                </div>
                <div class="text-xs font-mono" v-if="selectedConnection!.source.exe">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Executable:</span>
                  <button
                    v-if="selectedConnection!.source.pid"
                    @click="navigateToProcess"
                    class="text-dt-accent dark:text-dt-accent-dark-blue hover:underline cursor-pointer ml-2"
                  >
                    {{ selectedConnection!.source.exe }}
                  </button>
                  <span v-else class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedConnection!.source.exe }}</span>
                </div>
                <div class="text-xs font-mono" v-if="selectedConnection!.source.user">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">User:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedConnection!.source.user }}</span>
                </div>
              </div>
            </div>
          </div>

          <!-- Destination Section -->
          <div class="mb-4">
            <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Destination</h3>
            <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
              <div class="space-y-1">
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Address:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ formatAddressWithFamily(selectedConnection!.destination) }}</span>
                </div>
                <div class="text-xs font-mono" v-if="selectedConnection!.destination.hostname">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Hostname:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedConnection!.destination.hostname }}</span>
                </div>
              </div>
            </div>
          </div>

          <!-- System Section -->
          <div class="mb-4">
            <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">System</h3>
            <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
              <div class="space-y-1">
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Hostname:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedConnection!.system.hostname }}</span>
                </div>
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Agent:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedConnection!.system.agent }}</span>
                </div>
                <div class="text-xs font-mono">
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">Agent Instance:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ selectedConnection!.system.agentInstance }}</span>
                </div>
              </div>
            </div>
          </div>

          <!-- TLS Detection Section -->
          <div class="mb-4" v-if="selectedConnection!.meta.tlsProbeTypesDetected && selectedConnection!.meta.tlsProbeTypesDetected.length > 0">
            <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">TLS Detection</h3>
            <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
              <div class="flex flex-wrap gap-2">
                <span
                  v-for="probe in selectedConnection!.meta.tlsProbeTypesDetected"
                  :key="probe"
                  class="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-mono bg-blue-100 dark:bg-blue-900/30 text-blue-800 dark:text-blue-300 border border-blue-300 dark:border-blue-700"
                >
                  {{ probe }}
                </span>
              </div>
            </div>
          </div>

          <!-- Tags Section -->
          <div v-if="selectedConnection!.tags && selectedConnection!.tags.length > 0">
            <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Tags</h3>
            <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
              <div class="space-y-1">
                <div
                  v-for="(tag, index) in selectedConnection!.tags"
                  :key="index"
                  class="text-xs font-mono"
                >
                  <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">{{ tag.key }}:</span>
                  <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ tag.value }}</span>
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
import { useConnectionsStore, type Connection, type ConnectionEndpoint } from '@/stores/connections'
import { useConnections } from '@/composables/connections'
import { useColumnResize, type ColumnConfig } from '@/composables/columnResize'
import DirectionIndicator from '@/components/ux/DirectionIndicator.vue'
import StatusBadge from '@/components/ux/StatusBadge.vue'
import Toolbar from '@/components/ux/Toolbar.vue'
import ColumnToggle from '@/components/ux/ColumnToggle.vue'
import BufferSettings from '@/components/ux/BufferSettings.vue'
import SplitPanel from '@/components/ux/SplitPanel.vue'
import UxPill from '@/components/ux/Pill.vue'
import DoubleUpIcon from '@/components/icons/DoubleUpIcon.vue'

// Default column configuration
const defaultColumns: ColumnConfig[] = [
  { key: 'direction', label: '', width: 26, minWidth: 26, visible: true, resizable: false },
  { key: 'timestamp', label: 'Timestamp', width: 128, minWidth: 80, visible: true, resizable: true },
  { key: 'source', label: 'Source', width: 140, minWidth: 80, visible: true, resizable: true },
  { key: 'destination', label: 'Destination', width: 200, minWidth: 100, visible: true, resizable: true },
  { key: 'status', label: 'Status', width: 80, minWidth: 60, visible: true, resizable: true },
  { key: 'socket', label: 'Socket', width: 70, minWidth: 50, visible: true, resizable: true },
  { key: 'l7', label: 'L7', width: 80, minWidth: 50, visible: true, resizable: true },
  { key: 'process', label: 'Process', width: 150, minWidth: 80, visible: true, resizable: true },
  { key: 'duration', label: 'Duration', width: 80, minWidth: 50, visible: true, resizable: true },
]

// use Connections data
const { 
  connections, 
  isPaused, 
  filters, 
  filterableKeys, 
  getFilterValues,
  maxItems,
  maxSize,
  maxSizeUnit,
  resetBufferSettings,
} = useConnections()

const store = useConnectionsStore()

// Split panel ref for reset
const splitPanelRef = ref<InstanceType<typeof SplitPanel> | null>(null)

// Compute pill center position based on whether details panel is open
const pillCenterPosition = computed(() => {
  if (selectedConnection.value && splitPanelRef.value?.panelWidth) {
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
  storageKey: 'devtools_connections_columns',
  defaultColumns,
})

// Displayed columns
const displayedColumns = computed(() => visibleColumns.value)

// Track if user has manually resized any column
const CUSTOM_RESIZE_KEY = 'devtools_connections_custom_resize'
const hasCustomResizing = ref(localStorage.getItem(CUSTOM_RESIZE_KEY) === 'true')

// Expand destination column to fill available space
const expandDestinationToFillSpace = (force = false) => {
  if (!scrollContainer.value) return
  if (!force && hasCustomResizing.value) return // Don't auto-expand if user has customized (unless forced)
  
  const destCol = columns.value.find(col => col.key === 'destination')
  if (!destCol || !destCol.visible) return
  
  const availableWidth = scrollContainer.value.clientWidth
  
  let otherColumnsWidth = 0
  for (const col of visibleColumns.value) {
    if (col.key !== 'destination') {
      otherColumnsWidth += col.width
    }
  }
  
  const targetWidth = Math.max(destCol.minWidth, availableWidth - otherColumnsWidth)
  destCol.width = targetWidth
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
  // Expand destination column to fill remaining space
  requestAnimationFrame(() => expandDestinationToFillSpace(true))
}

// Handle clear button
function handleClear() {
  store.clearConnections()
}

// Handle column toggle - always expand destination to fill available space
function handleColumnToggle(key: string) {
  toggleColumn(key)
  requestAnimationFrame(() => expandDestinationToFillSpace(true))
}

// Handle reset columns
function handleResetColumns() {
  resetColumns()
  splitPanelRef.value?.resetWidth()
  hasCustomResizing.value = false
  localStorage.removeItem(CUSTOM_RESIZE_KEY)
  requestAnimationFrame(() => expandDestinationToFillSpace())
}

const params = useUrlParams()
const scrollContainer = ref<HTMLElement | null>(null)
const frozenMode = ref(false)
const newUnseenItems = ref(false)
const isScrollable = ref(false)

// Reactive scroll position for virtual scrolling
const scrollTop = ref(0)
const containerHeight = ref(0)

// Auto-expand on initial load - always force since destination width is calculated dynamically
watch(scrollContainer, (container) => {
  if (container) {
    containerHeight.value = container.clientHeight
    // Always expand destination to fill available space on initial load
    // (destination width is not persisted, so it must be calculated fresh)
    nextTick(() => expandDestinationToFillSpace(true))
  }
}, { immediate: true })

// ResizeObserver to auto-expand destination when container width changes
let resizeObserver: ResizeObserver | null = null
let lastContainerWidth = 0

onMounted(() => {
  resizeObserver = new ResizeObserver((entries) => {
    for (const entry of entries) {
      const newWidth = entry.contentRect.width
      // Only trigger if width actually changed (not just height)
      if (newWidth !== lastContainerWidth) {
        lastContainerWidth = newWidth
        expandDestinationToFillSpace(true)
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
    return { start: 0, end: connections.value.length }
  }
  
  // Use reactive scrollTop instead of DOM property
  const currentScrollTop = scrollTop.value
  const viewportHeight = containerHeight.value || scrollContainer.value.clientHeight
  
  const startIndex = Math.max(0, Math.floor(currentScrollTop / ITEM_HEIGHT) - BUFFER_SIZE)
  const endIndex = Math.min(
    connections.value.length,
    Math.ceil((currentScrollTop + viewportHeight) / ITEM_HEIGHT) + BUFFER_SIZE
  )
  
  return { start: startIndex, end: endIndex }
})

// Get only items that should be rendered
const visibleConnections = computed(() => {
  const { start, end } = visibleRange.value
  
  const selected = selectedConnection.value
  if (selected) {
    const selectedIndex = connections.value.findIndex(
      conn => conn.meta.connectionId === selected.meta.connectionId
    )
    if (selectedIndex !== -1 && (selectedIndex < start || selectedIndex >= end)) {
      const visible = connections.value.slice(start, end)
      return [...visible, selected]
    }
  }
  
  return connections.value.slice(start, end)
})

// Spacer heights to maintain scroll position
const topSpacerHeight = computed(() => visibleRange.value.start * ITEM_HEIGHT)
const bottomSpacerHeight = computed(() => 
  (connections.value.length - visibleRange.value.end) * ITEM_HEIGHT
)

const formatAddress = (endpoint: ConnectionEndpoint): string => {
  return `${endpoint.address.ip}:${endpoint.address.port}`
}

const formatAddressWithFamily = (endpoint: ConnectionEndpoint): string => {
  return `${endpoint.address.family} ${endpoint.address.ip}:${endpoint.address.port}`
}

// State for showing IPs instead of endpointId
const showIPs = ref(false)

// Get destination display text (IP if showIPs is checked, otherwise endpointId with port if available, fallback to IP)
const getDestinationDisplay = (connection: Connection): string => {
  if (showIPs.value) {
    return formatAddress(connection.destination)
  }
  if (connection.meta.endpointId && connection.meta.endpointId.trim() !== '') {
    return `${connection.meta.endpointId}:${connection.destination.address.port}`
  }
  return formatAddress(connection.destination)
}

const truncateId = (id: string): string => {
  if (id.length <= 16) return id
  return id.substring(0, 8) + '...' + id.substring(id.length - 8)
}

const selectConnection = (connection: Connection) => {
  params.connection_id = connection.meta.connectionId
}

const closeConnection = () => {
  delete params.connection_id
}

const navigateToProcess = () => {
  // PID is on source for egress, destination for ingress
  const pid = selectedConnection.value?.source?.pid || selectedConnection.value?.destination?.pid
  if (!pid) return
  params.process_id = String(pid)
  params.tab = 'processes'
}

const selectedConnection = computed(() => {
  const connectionId = params.connection_id as string
  if (!connectionId) return null
  return store.getConnectionById(connectionId)
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
  
  if (isAtTop() && !selectedConnection.value) {
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

// Watch for new connections
watch(
  () => connections.value[0]?.meta.connectionId,
  (newId, oldId) => {
    if (newId && newId !== oldId) {
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
  () => selectedConnection.value,
  (newVal) => {
    if (newVal) {
      frozenMode.value = true
    }
  }
)

// Auto-scroll to selected connection if it's not visible
watch(
  [() => selectedConnection.value?.meta.connectionId, scrollContainer],
  ([selectedId, container]) => {
    if (!selectedId || !container) return
    
    nextTick(() => {
      const selectedIndex = connections.value.findIndex(
        conn => conn.meta.connectionId === selectedId
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

