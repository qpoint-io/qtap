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
          entity="request"
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
      storage-key="devtools_redis_panel_width"
      :default-width="40"
      :min-width="10"
      :max-width="90"
      :show-right="!!selectedRequest"
      :class="{ 'select-none': isColumnResizing }"
    >
      <template #left>
        <!-- New Requests Button (shown when frozen with new items and scrollable) -->
        <UxPill
          v-if="frozenMode && newUnseenItems && isScrollable"
          :on-click="jumpToTop"
          size="small"
          class="absolute top-10 -translate-x-1/2 z-30 shadow-lg transition-colors"
          :style="{ left: pillCenterPosition }"
        >
          <DoubleUpIcon class="w-3.5 h-3.5" />
          New Requests
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
                  'text-right justify-end': column.key === 'size' || column.key === 'duration',
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
            v-for="request in visibleRequests"
            :key="request.requestId"
            @click="selectRequest(request)"
            class="flex text-xs border-b border-dt-border-light dark:border-dt-border-dark-light cursor-pointer hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover"
            :style="{ height: `${ITEM_HEIGHT}px` }"
            :class="{
              'bg-dt-bg-selected dark:bg-dt-bg-dark-selected': params.redis_id === request.requestId,
            }"
          >
            <template v-for="column in displayedColumns" :key="column.key">
              <!-- Timestamp -->
              <div
                v-if="column.key === 'timestamp'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ formatTimestamp(request.timestamp) }}
              </div>

              <!-- Direction -->
              <div
                v-else-if="column.key === 'direction'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light flex items-center"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                <DirectionIndicator :direction="request.direction" />
              </div>

              <!-- Command -->
              <div
                v-else-if="column.key === 'command'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light font-semibold"
                :class="getCommandClass(getCommand(request.statement))"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ getCommand(request.statement) }}
              </div>

              <!-- Statement -->
              <div
                v-else-if="column.key === 'statement'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate font-mono text-[11px] text-dt-text-primary dark:text-dt-text-dark-primary"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ request.statement }}
              </div>

              <!-- Result -->
              <div
                v-else-if="column.key === 'result'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-tertiary dark:text-dt-text-dark-tertiary text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ request.resultType || '-' }}
              </div>

              <!-- Error -->
              <div
                v-else-if="column.key === 'error'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-center font-semibold"
                :class="request.isError ? 'text-dt-status-error dark:text-dt-status-dark-error' : 'text-dt-text-tertiary dark:text-dt-text-dark-tertiary'"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ request.isError ? 'Error' : '-' }}
              </div>

              <!-- Duration -->
              <div
                v-else-if="column.key === 'duration'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-right text-dt-text-secondary dark:text-dt-text-dark-secondary"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ formatDuration(request.duration) }}
              </div>

              <!-- Size -->
              <div
                v-else-if="column.key === 'size'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-right text-dt-text-secondary dark:text-dt-text-dark-secondary"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ formatBytes(request.bytesSent + request.bytesReceived) }}
              </div>

              <!-- Process -->
              <div
                v-else-if="column.key === 'process'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ getProcessName(request.process?.exe) }}
              </div>
            </template>
          </div>

          <!-- Bottom spacer for non-rendered items below viewport -->
          <div :style="{ height: `${bottomSpacerHeight}px` }"></div>
        </div>
      </template>

      <template #right>
        <!-- Request Summary -->
        <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary border-b border-dt-border-light dark:border-dt-border-dark-light p-3 flex items-start justify-between">
          <div class="flex-1">
            <div class="flex items-center gap-3 mb-2">
              <DirectionIndicator :direction="selectedRequest!.direction" :show-label="false" />
              <span
                class="font-semibold text-sm"
                :class="getCommandClass(getCommand(selectedRequest!.statement))"
              >
                {{ getCommand(selectedRequest!.statement) }}
              </span>
              <span
                v-if="selectedRequest!.isError"
                class="font-semibold text-sm text-dt-status-error dark:text-dt-status-dark-error"
              >
                Error
              </span>
              <span class="text-xs text-dt-text-secondary dark:text-dt-text-dark-secondary">
                {{ formatDuration(selectedRequest!.duration) }}
              </span>
            </div>
            <div class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary break-all">
              {{ selectedRequest!.statement }}
            </div>
            <div class="flex gap-4 mt-2 text-xs text-dt-text-secondary dark:text-dt-text-dark-secondary">
              <span>Size: {{ formatBytes(totalBytes) }}</span>
              <span v-if="selectedRequest!.process?.exe">
                Process: {{ selectedRequest!.process.exe }}
              </span>
              <span v-if="selectedRequest!.process?.containerName">
                Container: {{ selectedRequest!.process.containerName }}
              </span>
            </div>
          </div>

          <!-- Close Button -->
          <button
            @click="closeRequest"
            class="ml-3 p-1 hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover rounded text-dt-text-secondary dark:text-dt-text-dark-secondary hover:text-dt-text-primary dark:hover:text-dt-text-dark-primary cursor-pointer"
            title="Close"
          >
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <!-- Detail Tabs -->
        <div class="flex bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary border-b border-dt-border-dark dark:border-dt-border-dark-dark">
          <button
            @click="activeDetailTab = 'command'"
            class="px-4 py-2 text-xs relative cursor-pointer"
            :class="{
              'text-dt-text-primary dark:text-dt-text-dark-primary': activeDetailTab === 'command',
              'text-dt-text-secondary dark:text-dt-text-dark-secondary hover:text-dt-text-primary dark:hover:text-dt-text-dark-primary': activeDetailTab !== 'command',
            }"
          >
            Command
            <div
              v-if="activeDetailTab === 'command'"
              class="absolute bottom-0 left-0 right-0 h-0.5 bg-dt-accent dark:bg-dt-accent-dark-blue"
            ></div>
          </button>
          <button
            @click="activeDetailTab = 'result'"
            class="px-4 py-2 text-xs relative cursor-pointer"
            :class="{
              'text-dt-text-primary dark:text-dt-text-dark-primary': activeDetailTab === 'result',
              'text-dt-text-secondary dark:text-dt-text-dark-secondary hover:text-dt-text-primary dark:hover:text-dt-text-dark-primary': activeDetailTab !== 'result',
            }"
          >
            Result
            <div
              v-if="activeDetailTab === 'result'"
              class="absolute bottom-0 left-0 right-0 h-0.5 bg-dt-accent dark:bg-dt-accent-dark-blue"
            ></div>
          </button>
          <button
            @click="activeDetailTab = 'timing'"
            class="px-4 py-2 text-xs relative cursor-pointer"
            :class="{
              'text-dt-text-primary dark:text-dt-text-dark-primary': activeDetailTab === 'timing',
              'text-dt-text-secondary dark:text-dt-text-dark-secondary hover:text-dt-text-primary dark:hover:text-dt-text-dark-primary': activeDetailTab !== 'timing',
            }"
          >
            Timing
            <div
              v-if="activeDetailTab === 'timing'"
              class="absolute bottom-0 left-0 right-0 h-0.5 bg-dt-accent dark:bg-dt-accent-dark-blue"
            ></div>
          </button>
          <button
            v-if="selectedRequest!.process"
            @click="activeDetailTab = 'process'"
            class="px-4 py-2 text-xs relative cursor-pointer"
            :class="{
              'text-dt-text-primary dark:text-dt-text-dark-primary': activeDetailTab === 'process',
              'text-dt-text-secondary dark:text-dt-text-dark-secondary hover:text-dt-text-primary dark:hover:text-dt-text-dark-primary': activeDetailTab !== 'process',
            }"
          >
            Process
            <div
              v-if="activeDetailTab === 'process'"
              class="absolute bottom-0 left-0 right-0 h-0.5 bg-dt-accent dark:bg-dt-accent-dark-blue"
            ></div>
          </button>
        </div>

        <!-- Detail Content -->
        <div class="flex-1 overflow-y-auto p-3 bg-dt-bg-primary dark:bg-dt-bg-dark-primary">
          <!-- Command Tab -->
          <div v-if="activeDetailTab === 'command'">
            <div class="mb-4">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Statement</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light relative">
                <Clipboard
                  :content="selectedRequest!.statement"
                  class="absolute top-2 right-2"
                />
                <pre class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary whitespace-pre-wrap break-all">{{ selectedRequest!.statement }}</pre>
              </div>
            </div>

            <div>
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Timestamp</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <p class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary">{{ selectedRequest!.timestamp }}</p>
              </div>
            </div>
          </div>

          <!-- Result Tab -->
          <div v-else-if="activeDetailTab === 'result'">
            <div class="mb-4">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Result Type</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <p class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary">{{ selectedRequest!.resultType || '-' }}</p>
              </div>
            </div>

            <div v-if="selectedRequest!.isError" class="mb-4">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Error Message</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light relative">
                <Clipboard
                  v-if="selectedRequest!.errorMsg"
                  :content="selectedRequest!.errorMsg"
                  class="absolute top-2 right-2"
                />
                <pre class="text-xs font-mono text-dt-status-error dark:text-dt-status-dark-error whitespace-pre-wrap break-all">{{ selectedRequest!.errorMsg || '-' }}</pre>
              </div>
            </div>

            <div v-if="selectedRequest!.affectedCount !== undefined" class="mb-4">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Affected Count</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <p class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary">{{ selectedRequest!.affectedCount }}</p>
              </div>
            </div>

            <div v-if="selectedRequest!.resultCount !== undefined">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Result Count</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <p class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary">{{ selectedRequest!.resultCount }}</p>
              </div>
            </div>
          </div>

          <!-- Timing Tab -->
          <div v-else-if="activeDetailTab === 'timing'">
            <div class="mb-4">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Duration</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <p class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary">{{ formatDuration(selectedRequest!.duration) }}</p>
              </div>
            </div>

            <div class="mb-4">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Bytes Sent</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <p class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary">{{ formatBytes(selectedRequest!.bytesSent) }}</p>
              </div>
            </div>

            <div>
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Bytes Received</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <p class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary">{{ formatBytes(selectedRequest!.bytesReceived) }}</p>
              </div>
            </div>
          </div>

          <!-- Process Tab -->
          <div v-else-if="activeDetailTab === 'process'">
            <div v-if="selectedRequest!.process?.exe" class="mb-4">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Executable</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <p class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary">{{ selectedRequest!.process.exe }}</p>
              </div>
            </div>

            <div v-if="selectedRequest!.process?.pid" class="mb-4">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">PID</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <p class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary">{{ selectedRequest!.process.pid }}</p>
              </div>
            </div>

            <div v-if="selectedRequest!.process?.containerName" class="mb-4">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Container Name</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <p class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary">{{ selectedRequest!.process.containerName }}</p>
              </div>
            </div>

            <div v-if="selectedRequest!.process?.containerImage" class="mb-4">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Container Image</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <p class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary">{{ selectedRequest!.process.containerImage }}</p>
              </div>
            </div>

            <div v-if="selectedRequest!.process?.podName" class="mb-4">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Pod Name</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <p class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary">{{ selectedRequest!.process.podName }}</p>
              </div>
            </div>

            <div v-if="selectedRequest!.process?.podNamespace">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Pod Namespace</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <p class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary">{{ selectedRequest!.process.podNamespace }}</p>
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
import { useRedis, getCommandFromStatement } from '@/composables/redis'
import { formatTimestamp, formatBytes, formatDuration } from '@/composables/formatters'
import { useColumnResize, type ColumnConfig } from '@/composables/columnResize'
import { useRedisStore } from '@/stores/redis'
import type { DatabaseRequest } from '@/stores/redis'
import DirectionIndicator from '@/components/ux/DirectionIndicator.vue'
import Toolbar from '@/components/ux/Toolbar.vue'
import ColumnToggle from '@/components/ux/ColumnToggle.vue'
import BufferSettings from '@/components/ux/BufferSettings.vue'
import SplitPanel from '@/components/ux/SplitPanel.vue'
import UxPill from '@/components/ux/Pill.vue'
import DoubleUpIcon from '@/components/icons/DoubleUpIcon.vue'
import Clipboard from '@/components/ux/Clipboard.vue'

// Default column configuration
const defaultColumns: ColumnConfig[] = [
  { key: 'timestamp', label: 'Timestamp', width: 128, minWidth: 80, visible: true, resizable: true },
  { key: 'direction', label: 'Direction', width: 96, minWidth: 60, visible: true, resizable: true },
  { key: 'command', label: 'Command', width: 96, minWidth: 60, visible: true, resizable: true },
  { key: 'statement', label: 'Statement', width: 300, minWidth: 100, visible: true, resizable: true },
  { key: 'result', label: 'Result', width: 96, minWidth: 60, visible: true, resizable: true },
  { key: 'error', label: 'Error', width: 64, minWidth: 50, visible: true, resizable: true },
  { key: 'duration', label: 'Duration', width: 80, minWidth: 50, visible: true, resizable: true },
  { key: 'size', label: 'Size', width: 80, minWidth: 50, visible: true, resizable: true },
  { key: 'process', label: 'Process', width: 128, minWidth: 60, visible: true, resizable: true },
]

// use Redis data
const { 
  requests, 
  isPaused, 
  filters, 
  filterableKeys, 
  getFilterValues,
  maxItems,
  maxSize,
  maxSizeUnit,
  resetBufferSettings,
} = useRedis()

// Redis store for clearing
const redisStore = useRedisStore()

// Split panel ref for reset
const splitPanelRef = ref<InstanceType<typeof SplitPanel> | null>(null)

// Compute pill center position based on whether details panel is open
const pillCenterPosition = computed(() => {
  if (selectedRequest.value && splitPanelRef.value?.panelWidth) {
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
  storageKey: 'devtools_redis_columns',
  defaultColumns,
})

// Displayed columns (just use visibleColumns directly since details overlay the table)
const displayedColumns = computed(() => visibleColumns.value)

// Virtual scrolling
const ITEM_HEIGHT = 29
const HEADER_HEIGHT = 29

const scrollContainer = ref<HTMLElement | null>(null)
const frozenMode = ref(false)
const newUnseenItems = ref(false)

// Local scrollable state (we manage scroll handling ourselves for frozen mode)
const isScrollable = ref(false)

// Reactive scroll position for virtual scrolling
const scrollTop = ref(0)
const containerHeight = ref(0)

// Scroll to top helper
const scrollToTop = () => {
  if (scrollContainer.value) {
    scrollContainer.value.scrollTop = 0
  }
}

// Spacer heights to maintain scroll position
const topSpacerHeight = computed(() => visibleRange.value.start * ITEM_HEIGHT)
const bottomSpacerHeight = computed(() => 
  (requests.value.length - visibleRange.value.end) * ITEM_HEIGHT
)

// Get only items that should be rendered (including selected item if outside viewport)
const visibleRequests = computed(() => {
  // Use reactive scrollTop for virtual scrolling
  if (!scrollContainer.value) {
    return requests.value.slice(0, 50)
  }
  
  const currentScrollTop = scrollTop.value
  const viewportHeight = containerHeight.value || scrollContainer.value.clientHeight
  const bufferSize = 50
  
  const startIndex = Math.max(0, Math.floor(currentScrollTop / ITEM_HEIGHT) - bufferSize)
  const endIndex = Math.min(
    requests.value.length,
    Math.ceil((currentScrollTop + viewportHeight) / ITEM_HEIGHT) + bufferSize
  )
  
  // Always include selected item if it exists and is outside viewport
  const selected = selectedRequest.value
  if (selected) {
    const selectedIndex = requests.value.findIndex(
      req => req.requestId === selected.requestId
    )
    if (selectedIndex !== -1 && (selectedIndex < startIndex || selectedIndex >= endIndex)) {
      const visible = requests.value.slice(startIndex, endIndex)
      return [...visible, selected]
    }
  }
  
  return requests.value.slice(startIndex, endIndex)
})

// Track if user has manually resized any column
// Once they have, we stop auto-expanding statement
const CUSTOM_RESIZE_KEY = 'devtools_redis_custom_resize'
const hasCustomResizing = ref(localStorage.getItem(CUSTOM_RESIZE_KEY) === 'true')

// Expand statement column to fill available space
const expandStatementToFillSpace = (force = false) => {
  if (!scrollContainer.value) return
  if (!force && hasCustomResizing.value) return // Don't auto-expand if user has customized (unless forced)
  
  const statementCol = columns.value.find(col => col.key === 'statement')
  if (!statementCol || !statementCol.visible) return
  
  const availableWidth = scrollContainer.value.clientWidth
  
  // Calculate sum of NON-STATEMENT visible column widths
  let otherColumnsWidth = 0
  for (const col of visibleColumns.value) {
    if (col.key !== 'statement') {
      otherColumnsWidth += col.width
    }
  }
  
  // Set statement width to fill remaining space
  const targetStatementWidth = Math.max(statementCol.minWidth, availableWidth - otherColumnsWidth)
  statementCol.width = targetStatementWidth
}

// Handle column resize start - mark as custom resizing
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
  // Expand statement column to fill remaining space
  requestAnimationFrame(() => expandStatementToFillSpace(true))
}

// Handle clear button
function handleClear() {
  redisStore.clearRequests()
}

// Handle column toggle - always expand statement to fill available space
function handleColumnToggle(key: string) {
  toggleColumn(key)
  requestAnimationFrame(() => expandStatementToFillSpace(true))
}

// Handle reset columns - clear custom resizing flag and auto-expand
function handleResetColumns() {
  resetColumns()
  splitPanelRef.value?.resetWidth()
  // Clear custom resizing flag
  hasCustomResizing.value = false
  localStorage.removeItem(CUSTOM_RESIZE_KEY)
  // Auto-expand statement after reset
  requestAnimationFrame(() => expandStatementToFillSpace())
}

const params = useUrlParams()
const activeDetailTab = ref<'command' | 'result' | 'timing' | 'process'>('command')

// Auto-expand statement on initial load - always force since statement width is calculated dynamically
watch(scrollContainer, (container) => {
  if (container) {
    containerHeight.value = container.clientHeight
    // Always expand statement to fill available space on initial load
    // (statement width is not persisted, so it must be calculated fresh)
    nextTick(() => expandStatementToFillSpace(true))
  }
}, { immediate: true })

// ResizeObserver to auto-expand statement when container width changes
let resizeObserver: ResizeObserver | null = null
let lastContainerWidth = 0

onMounted(() => {
  resizeObserver = new ResizeObserver((entries) => {
    for (const entry of entries) {
      const newWidth = entry.contentRect.width
      // Only trigger if width actually changed (not just height)
      if (newWidth !== lastContainerWidth) {
        lastContainerWidth = newWidth
        expandStatementToFillSpace(true)
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

// Calculate visible range for spacers
const visibleRange = computed(() => {
  if (!scrollContainer.value) {
    return { start: 0, end: requests.value.length }
  }
  
  // Use reactive scrollTop instead of DOM property
  const currentScrollTop = scrollTop.value
  const viewportHeight = containerHeight.value || scrollContainer.value.clientHeight
  const bufferSize = 50
  
  const startIndex = Math.max(0, Math.floor(currentScrollTop / ITEM_HEIGHT) - bufferSize)
  const endIndex = Math.min(
    requests.value.length,
    Math.ceil((currentScrollTop + viewportHeight) / ITEM_HEIGHT) + bufferSize
  )
  
  return { start: startIndex, end: endIndex }
})

// Get command from statement
const getCommand = (statement: string): string => {
  return getCommandFromStatement(statement)
}

// Get command class for color-coding
const getCommandClass = (command: string): string => {
  const cmd = command.toUpperCase()
  
  // Read commands (blue)
  const readCommands = ['GET', 'HGET', 'HGETALL', 'LRANGE', 'SMEMBERS', 'ZRANGE', 'MGET', 'KEYS', 'SCAN', 'EXISTS', 'TTL', 'TYPE']
  if (readCommands.includes(cmd)) {
    return 'text-method-get-light dark:text-method-get-dark'
  }
  
  // Write commands (green)
  const writeCommands = ['SET', 'HSET', 'LPUSH', 'RPUSH', 'SADD', 'ZADD', 'DEL', 'EXPIRE', 'INCR', 'DECR', 'SETEX', 'MSET']
  if (writeCommands.includes(cmd)) {
    return 'text-method-post-light dark:text-method-post-dark'
  }
  
  // Default
  return 'text-dt-text-primary dark:text-dt-text-dark-primary'
}

// Get process name (basename)
const getProcessName = (exePath?: string): string => {
  if (!exePath) return '-'
  const parts = exePath.split('/')
  return parts[parts.length - 1] || exePath
}

const selectRequest = (request: DatabaseRequest) => {
  params.redis_id = request.requestId || ''
  activeDetailTab.value = 'command'
}

const closeRequest = () => {
  delete params.redis_id
}

// Check if container is scrollable
const checkScrollable = () => {
  if (!scrollContainer.value) return
  isScrollable.value = scrollContainer.value.scrollHeight > scrollContainer.value.clientHeight
}

// Check if scrolled to top (with 5px threshold)
const isAtTop = (): boolean => {
  if (!scrollContainer.value) return true
  return scrollContainer.value.scrollTop < 5
}

// Handle manual scroll events
const handleScroll = () => {
  // Update reactive scroll position for virtual scrolling
  if (scrollContainer.value) {
    scrollTop.value = scrollContainer.value.scrollTop
    containerHeight.value = scrollContainer.value.clientHeight
  }
  
  checkScrollable()
  
  // If user manually scrolls to top and nothing is selected, exit frozen mode
  if (isAtTop() && !selectedRequest.value) {
    frozenMode.value = false
    newUnseenItems.value = false
  } else if (!isAtTop() && !frozenMode.value) {
    // If user scrolls down from top, enter frozen mode
    frozenMode.value = true
  }
}

// Jump to top button handler
const jumpToTop = () => {
  frozenMode.value = false
  newUnseenItems.value = false
  nextTick(() => scrollToTop())
}

const selectedRequest = computed(() => {
  const requestId = params.redis_id as string
  if (!requestId) return null
  return requests.value.find(
    (req) => req.requestId === requestId
  ) || null
})

const totalBytes = computed(() => {
  if (!selectedRequest.value) return 0
  return selectedRequest.value.bytesSent + selectedRequest.value.bytesReceived
})

// Watch for new requests - set flag if new item at top and not scrolled to top
watch(
  () => requests.value[0]?.requestId,
  (newId, oldId) => {
    if (newId && newId !== oldId) {
      if (frozenMode.value) {
        const container = scrollContainer.value
        if (container) {
          // In frozen mode, new items increase the top spacer height
          // which naturally preserves scroll position visually
          const oldScrollTop = container.scrollTop
          
          if (!isAtTop()) {
            newUnseenItems.value = true
          }
          
          nextTick(() => {
            // Adjust scroll by the height of newly added items (1 item * height)
            container.scrollTop = oldScrollTop + ITEM_HEIGHT
            checkScrollable()
          })
        }
      } else if (isAtTop()) {
        // Live mode at top: stay at top
        nextTick(() => {
          if (scrollContainer.value) {
            scrollContainer.value.scrollTop = 0
          }
          checkScrollable()
        })
      }
    }
  }
)

// Enter frozen mode when item is selected
watch(
  () => selectedRequest.value,
  (newVal) => {
    if (newVal) {
      frozenMode.value = true
    }
  }
)

// Auto-scroll to selected request if it's not visible
watch(
  [() => selectedRequest.value?.requestId, scrollContainer],
  ([selectedId, container]) => {
    if (!selectedId || !container) return
    
    nextTick(() => {
      const selectedIndex = requests.value.findIndex(
        req => req.requestId === selectedId
      )
      
      if (selectedIndex === -1) return
      
      // Calculate viewport bounds
      const scrollTop = container.scrollTop
      const viewportHeight = container.clientHeight
      const itemTop = selectedIndex * ITEM_HEIGHT
      const itemBottom = itemTop + ITEM_HEIGHT
      
      // Check if item is outside viewport (accounting for sticky header)
      const effectiveScrollTop = scrollTop + HEADER_HEIGHT
      const effectiveViewportHeight = viewportHeight - HEADER_HEIGHT
      const isAboveViewport = itemTop < effectiveScrollTop
      const isBelowViewport = itemBottom > (effectiveScrollTop + effectiveViewportHeight)
      
      if (isAboveViewport || isBelowViewport) {
        // Position item at 30% from top of effective viewport (below sticky header)
        const targetScroll = itemTop - (effectiveViewportHeight * 0.3) - HEADER_HEIGHT
        container.scrollTop = Math.max(0, targetScroll)
      }
    })
  },
  { flush: 'post' }
)
</script>
