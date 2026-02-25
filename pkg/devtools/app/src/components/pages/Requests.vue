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
      storage-key="devtools_requests_panel_width"
      :default-width="40"
      :min-width="10"
      :max-width="90"
      :show-right="!!selectedTransaction"
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
                  'text-right justify-end': column.key === 'size' || column.key === 'time',
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
            v-for="transaction in visibleRequests"
            :key="transaction.request.request_id"
            @click="selectRequest(transaction)"
            class="flex text-xs border-b border-dt-border-light dark:border-dt-border-dark-light cursor-pointer hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover"
            :style="{ height: `${ITEM_HEIGHT}px` }"
            :class="{
              'bg-dt-bg-selected dark:bg-dt-bg-dark-selected': params.request_id === transaction.request.request_id,
            }"
          >
            <template v-for="column in displayedColumns" :key="column.key">
              <!-- Timestamp -->
              <div
                v-if="column.key === 'timestamp'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ formatTimestamp(transaction.transaction_time) }}
              </div>

              <!-- Direction -->
              <div
                v-else-if="column.key === 'direction'"
                class="px-[5px] py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light flex items-center"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                <DirectionIndicator :direction="transaction.direction" />
              </div>

              <!-- Endpoint -->
              <div
                v-else-if="column.key === 'endpoint'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ getEndpointFromUrl(transaction.request.url) }}
              </div>

              <!-- Path -->
              <div
                v-else-if="column.key === 'path'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate font-mono text-[11px] text-dt-text-primary dark:text-dt-text-dark-primary"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ getPathFromUrl(transaction.request.url) }}
              </div>

              <!-- Status -->
              <div
                v-else-if="column.key === 'status'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-center font-semibold"
                :class="getStatusClass(transaction.response.status)"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ transaction.response.status }}
              </div>

              <!-- Method -->
              <div
                v-else-if="column.key === 'method'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light font-semibold"
                :class="getMethodClass(transaction.request.method)"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ transaction.request.method }}
              </div>

              <!-- Type -->
              <div
                v-else-if="column.key === 'type'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-tertiary dark:text-dt-text-dark-tertiary text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ transaction.response.content_type || 'unknown' }}
              </div>

              <!-- Process -->
              <div
                v-else-if="column.key === 'process'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ transaction.metadata.process_exe || '-' }}
              </div>

              <!-- Size -->
              <div
                v-else-if="column.key === 'size'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-right text-dt-text-secondary dark:text-dt-text-dark-secondary"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ formatBytes((transaction.metadata.bytes_sent || 0) + (transaction.metadata.bytes_received || 0)) }}
              </div>

              <!-- Time -->
              <div
                v-else-if="column.key === 'time'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-right text-dt-text-secondary dark:text-dt-text-dark-secondary"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ formatDuration(transaction.duration_ms) }}
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
              <DirectionIndicator :direction="selectedTransaction!.direction" :show-label="false" />
              <span
                class="font-semibold text-sm"
                :class="getMethodClass(selectedTransaction!.request.method)"
              >
                {{ selectedTransaction!.request.method }}
              </span>
              <span
                class="font-semibold text-sm"
                :class="getStatusClass(selectedTransaction!.response.status)"
              >
                {{ selectedTransaction!.response.status }}
              </span>
              <span class="text-xs text-dt-text-secondary dark:text-dt-text-dark-secondary">
                {{ formatDuration(selectedTransaction!.duration_ms) }}
              </span>
            </div>
            <div class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary break-all">
              {{ selectedTransaction!.request.url }}
            </div>
            <div class="flex gap-4 mt-2 text-xs text-dt-text-secondary dark:text-dt-text-dark-secondary">
              <span>Size: {{ formatBytes(totalBytes) }}</span>
              <span v-if="selectedTransaction!.metadata.process_exe">
                Process:
                <button
                  v-if="selectedTransaction!.metadata.process_id"
                  @click="navigateToProcess"
                  class="text-dt-accent dark:text-dt-accent-dark-blue hover:underline cursor-pointer"
                >
                  {{ selectedTransaction!.metadata.process_exe }}
                </button>
                <span v-else>{{ selectedTransaction!.metadata.process_exe }}</span>
              </span>
              <span v-if="selectedTransaction!.metadata.container_name">
                Container: {{ selectedTransaction!.metadata.container_name }}
              </span>
            </div>
            <div class="flex gap-4 mt-2 text-xs">
              <span v-if="selectedTransaction!.metadata.connection_id" class="text-dt-text-secondary dark:text-dt-text-dark-secondary">
                Connection:
                <button
                  @click="navigateToConnection"
                  class="text-dt-accent dark:text-dt-accent-dark-blue hover:underline font-mono cursor-pointer"
                >
                  {{ truncateId(selectedTransaction!.metadata.connection_id) }}
                </button>
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
            @click="activeDetailTab = 'headers'"
            class="px-4 py-2 text-xs relative cursor-pointer"
            :class="{
              'text-dt-text-primary dark:text-dt-text-dark-primary': activeDetailTab === 'headers',
              'text-dt-text-secondary dark:text-dt-text-dark-secondary hover:text-dt-text-primary dark:hover:text-dt-text-dark-primary': activeDetailTab !== 'headers',
            }"
          >
            Headers
            <div
              v-if="activeDetailTab === 'headers'"
              class="absolute bottom-0 left-0 right-0 h-0.5 bg-dt-accent dark:bg-dt-accent-dark-blue"
            ></div>
          </button>
          <button
            v-if="selectedTransaction!.request.body"
            @click="activeDetailTab = 'request'"
            class="px-4 py-2 text-xs relative cursor-pointer"
            :class="{
              'text-dt-text-primary dark:text-dt-text-dark-primary': activeDetailTab === 'request',
              'text-dt-text-secondary dark:text-dt-text-dark-secondary hover:text-dt-text-primary dark:hover:text-dt-text-dark-primary': activeDetailTab !== 'request',
            }"
          >
            Request
            <div
              v-if="activeDetailTab === 'request'"
              class="absolute bottom-0 left-0 right-0 h-0.5 bg-dt-accent dark:bg-dt-accent-dark-blue"
            ></div>
          </button>
          <button
            v-if="selectedTransaction!.response.body"
            @click="activeDetailTab = 'response'"
            class="px-4 py-2 text-xs relative cursor-pointer"
            :class="{
              'text-dt-text-primary dark:text-dt-text-dark-primary': activeDetailTab === 'response',
              'text-dt-text-secondary dark:text-dt-text-dark-secondary hover:text-dt-text-primary dark:hover:text-dt-text-dark-primary': activeDetailTab !== 'response',
            }"
          >
            Response
            <div
              v-if="activeDetailTab === 'response'"
              class="absolute bottom-0 left-0 right-0 h-0.5 bg-dt-accent dark:bg-dt-accent-dark-blue"
            ></div>
          </button>
          <button
            @click="activeDetailTab = 'curl'"
            class="px-4 py-2 text-xs relative cursor-pointer"
            :class="{
              'text-dt-text-primary dark:text-dt-text-dark-primary': activeDetailTab === 'curl',
              'text-dt-text-secondary dark:text-dt-text-dark-secondary hover:text-dt-text-primary dark:hover:text-dt-text-dark-primary': activeDetailTab !== 'curl',
            }"
          >
            cURL
            <div
              v-if="activeDetailTab === 'curl'"
              class="absolute bottom-0 left-0 right-0 h-0.5 bg-dt-accent dark:bg-dt-accent-dark-blue"
            ></div>
          </button>
        </div>

        <!-- Detail Content -->
        <div class="flex-1 overflow-y-auto p-3 bg-dt-bg-primary dark:bg-dt-bg-dark-primary">
          <!-- Headers Tab -->
          <div v-if="activeDetailTab === 'headers'">
            <div class="mb-4">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Request Headers</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light relative">
                <Clipboard
                  v-if="selectedTransaction!.request.headers"
                  :content="JSON.stringify(selectedTransaction!.request.headers, null, 2)"
                  class="absolute top-2 right-2"
                />
                <div
                  v-if="selectedTransaction!.request.headers"
                  class="space-y-1"
                >
                  <div
                    v-for="(value, key) in selectedTransaction!.request.headers"
                    :key="key"
                    class="text-xs font-mono"
                  >
                    <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">{{ key }}:</span>
                    <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ value }}</span>
                  </div>
                </div>
                <p v-else class="text-xs text-dt-text-tertiary dark:text-dt-text-dark-tertiary">No headers</p>
              </div>
            </div>

            <div class="mb-4" v-if="selectedTransaction!.request.body">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Request Body</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light relative">
                <Clipboard
                  :content="formatBody(selectedTransaction!.request.body)"
                  class="absolute top-2 right-2"
                />
                <pre class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary whitespace-pre-wrap break-all">{{ formatBody(selectedTransaction!.request.body) }}</pre>
              </div>
            </div>

            <div>
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Response Headers</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light relative">
                <Clipboard
                  v-if="selectedTransaction!.response.headers && Object.keys(selectedTransaction!.response.headers).length > 0"
                  :content="JSON.stringify(selectedTransaction!.response.headers, null, 2)"
                  class="absolute top-2 right-2"
                />
                <div
                  v-if="selectedTransaction!.response.headers && Object.keys(selectedTransaction!.response.headers).length > 0"
                  class="space-y-1"
                >
                  <div
                    v-for="(value, key) in selectedTransaction!.response.headers"
                    :key="key"
                    class="text-xs font-mono"
                  >
                    <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">{{ key }}:</span>
                    <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ value }}</span>
                  </div>
                </div>
                <p v-else class="text-xs text-dt-text-tertiary dark:text-dt-text-dark-tertiary">No headers</p>
              </div>
            </div>
          </div>

          <!-- Request Tab -->
          <div v-else-if="activeDetailTab === 'request'">
            <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light relative">
              <Clipboard
                v-if="selectedTransaction!.request.body"
                :content="formatBody(selectedTransaction!.request.body)"
                class="absolute top-2 right-2 z-10"
              />
              <!-- Image Request -->
              <div v-if="isImageRequest(selectedTransaction)" class="flex justify-center items-center p-4">
                <img
                  :src="getRequestImageDataUrl(selectedTransaction)"
                  :alt="selectedTransaction!.request.url"
                  class="max-w-full h-auto rounded border border-dt-border-light dark:border-dt-border-dark-light"
                />
              </div>
              <!-- Text/JSON Request -->
              <pre
                v-else
                class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary whitespace-pre-wrap break-all"
                v-html="formatAndHighlightBody(selectedTransaction!.request.body, selectedTransaction!.request.headers?.['Content-Type'] || selectedTransaction!.request.headers?.['content-type'])"
              ></pre>
            </div>
          </div>

          <!-- Response Tab -->
          <div v-else-if="activeDetailTab === 'response'">
            <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light relative">
              <Clipboard
                v-if="selectedTransaction!.response.body"
                :content="formatBody(selectedTransaction!.response.body)"
                class="absolute top-2 right-2 z-10"
              />
              <!-- Image Response -->
              <div v-if="isImageResponse(selectedTransaction)" class="flex justify-center items-center p-4">
                <img
                  :src="getImageDataUrl(selectedTransaction)"
                  :alt="selectedTransaction!.request.url"
                  class="max-w-full h-auto rounded border border-dt-border-light dark:border-dt-border-dark-light"
                />
              </div>
              <!-- Text/JSON Response -->
              <pre
                v-else
                class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary whitespace-pre-wrap break-all"
                v-html="formatAndHighlightBody(selectedTransaction!.response.body, selectedTransaction!.response.content_type)"
              ></pre>
            </div>
          </div>

          <!-- cURL Tab -->
          <div v-else-if="activeDetailTab === 'curl'">
            <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light relative">
              <Clipboard
                :content="curlCommand"
                class="absolute top-2 right-2 z-10"
              />
              <pre class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary whitespace-pre-wrap break-all">{{ curlCommand }}</pre>
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
import { useRequests, useCurl } from '@/composables/requests'
import { formatTimestamp, formatBytes, formatDuration } from '@/composables/formatters'
import { useColumnResize, type ColumnConfig } from '@/composables/columnResize'
import { useHttpStore } from '@/stores/http'
import type { HttpTransaction } from '@/stores/http'
import DirectionIndicator from '@/components/ux/DirectionIndicator.vue'
import Toolbar from '@/components/ux/Toolbar.vue'
import ColumnToggle from '@/components/ux/ColumnToggle.vue'
import BufferSettings from '@/components/ux/BufferSettings.vue'
import SplitPanel from '@/components/ux/SplitPanel.vue'
import UxPill from '@/components/ux/Pill.vue'
import DoubleUpIcon from '@/components/icons/DoubleUpIcon.vue'
import Clipboard from '@/components/ux/Clipboard.vue'
import Prism from 'prismjs'
import 'prismjs/components/prism-json'
import 'prismjs/components/prism-javascript'
import 'prismjs/components/prism-typescript'
import 'prismjs/components/prism-css'
import 'prismjs/components/prism-markup' // HTML/XML
import 'prismjs/components/prism-sql'
import 'prismjs/components/prism-yaml'
import 'prismjs/components/prism-bash'

// Import both themes - Vue/Vite will handle bundling
import 'prismjs/themes/prism.min.css'
import 'prismjs/themes/prism-tomorrow.min.css'

// Default column configuration
const defaultColumns: ColumnConfig[] = [
  { key: 'direction', label: '', width: 26, minWidth: 26, visible: true, resizable: false },
  { key: 'timestamp', label: 'Timestamp', width: 128, minWidth: 80, visible: true, resizable: true },
  { key: 'endpoint', label: 'Endpoint', width: 160, minWidth: 80, visible: true, resizable: true },
  { key: 'path', label: 'Path', width: 300, minWidth: 100, visible: true, resizable: true },
  { key: 'status', label: 'Status', width: 64, minWidth: 50, visible: true, resizable: true },
  { key: 'method', label: 'Method', width: 80, minWidth: 60, visible: true, resizable: true },
  { key: 'type', label: 'Type', width: 96, minWidth: 60, visible: true, resizable: true },
  { key: 'process', label: 'Process', width: 128, minWidth: 60, visible: true, resizable: true },
  { key: 'size', label: 'Size', width: 80, minWidth: 50, visible: true, resizable: true },
  { key: 'time', label: 'Time', width: 80, minWidth: 50, visible: true, resizable: true },
]

// use Requests data
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
} = useRequests()

// use cURL composable
const { generateCurlCommand } = useCurl()

// HTTP store for clearing
const httpStore = useHttpStore()

// Split panel ref for reset
const splitPanelRef = ref<InstanceType<typeof SplitPanel> | null>(null)

// Compute pill center position based on whether details panel is open
const pillCenterPosition = computed(() => {
  if (selectedTransaction.value && splitPanelRef.value?.panelWidth) {
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
  storageKey: 'devtools_requests_columns',
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
  const selected = selectedTransaction.value
  if (selected) {
    const selectedIndex = requests.value.findIndex(
      tx => tx.request.request_id === selected.request.request_id
    )
    if (selectedIndex !== -1 && (selectedIndex < startIndex || selectedIndex >= endIndex)) {
      const visible = requests.value.slice(startIndex, endIndex)
      return [...visible, selected]
    }
  }
  
  return requests.value.slice(startIndex, endIndex)
})

// Track if user has manually resized any column
// Once they have, we stop auto-expanding path
const CUSTOM_RESIZE_KEY = 'devtools_requests_custom_resize'
const hasCustomResizing = ref(localStorage.getItem(CUSTOM_RESIZE_KEY) === 'true')

// Expand path column to fill available space
const expandPathToFillSpace = (force = false) => {
  if (!scrollContainer.value) return
  if (!force && hasCustomResizing.value) return // Don't auto-expand if user has customized (unless forced)
  
  const pathCol = columns.value.find(col => col.key === 'path')
  if (!pathCol || !pathCol.visible) return
  
  const availableWidth = scrollContainer.value.clientWidth
  
  // Calculate sum of NON-PATH visible column widths
  let otherColumnsWidth = 0
  for (const col of visibleColumns.value) {
    if (col.key !== 'path') {
      otherColumnsWidth += col.width
    }
  }
  
  // Set path width to fill remaining space
  const targetPathWidth = Math.max(pathCol.minWidth, availableWidth - otherColumnsWidth)
  pathCol.width = targetPathWidth
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
  // Expand path column to fill remaining space
  requestAnimationFrame(() => expandPathToFillSpace(true))
}

// Handle clear button
function handleClear() {
  httpStore.clearRequests()
}

// Handle column toggle - always expand path to fill available space
function handleColumnToggle(key: string) {
  toggleColumn(key)
  requestAnimationFrame(() => expandPathToFillSpace(true))
}

// Handle reset columns - clear custom resizing flag and auto-expand
function handleResetColumns() {
  resetColumns()
  splitPanelRef.value?.resetWidth()
  // Clear custom resizing flag
  hasCustomResizing.value = false
  localStorage.removeItem(CUSTOM_RESIZE_KEY)
  // Auto-expand path after reset
  requestAnimationFrame(() => expandPathToFillSpace())
}

const params = useUrlParams()
const activeDetailTab = ref<'headers' | 'request' | 'response' | 'curl'>('headers')

// Auto-expand path on initial load - always force since path width is calculated dynamically
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

// cURL command for selected transaction
const curlCommand = computed(() => generateCurlCommand(selectedTransaction.value))

const getStatusClass = (status: number): string => {
  if (status >= 200 && status < 300) return 'text-dt-status-success dark:text-dt-status-dark-success'
  if (status >= 300 && status < 400) return 'text-dt-status-info dark:text-dt-status-dark-info'
  if (status >= 400 && status < 500) return 'text-dt-status-warning dark:text-dt-status-dark-warning'
  if (status >= 500) return 'text-dt-status-error dark:text-dt-status-dark-error'
  return 'text-dt-text-primary dark:text-dt-text-dark-primary'
}

const getMethodClass = (method: string): string => {
  const classes: Record<string, string> = {
    GET: 'text-method-get-light dark:text-method-get-dark',
    POST: 'text-method-post-light dark:text-method-post-dark',
    PUT: 'text-method-put-light dark:text-method-put-dark',
    DELETE: 'text-method-delete-light dark:text-method-delete-dark',
    PATCH: 'text-method-patch-light dark:text-method-patch-dark',
  }
  return classes[method.toUpperCase()] || 'text-dt-text-primary dark:text-dt-text-dark-primary'
}

const getPathFromUrl = (url: string): string => {
  try {
    const urlObj = new URL(url)
    return urlObj.pathname
  } catch {
    return url
  }
}

const getEndpointFromUrl = (url: string): string => {
  try {
    const urlObj = new URL(url)
    return urlObj.hostname
  } catch {
    return '-'
  }
}

const selectRequest = (transaction: HttpTransaction) => {
  params.request_id = transaction.request.request_id || ''
  activeDetailTab.value = 'headers'
}

const closeRequest = () => {
  delete params.request_id
}

const truncateId = (id: string): string => {
  if (id.length <= 16) return id
  return id.substring(0, 8) + '...' + id.substring(id.length - 8)
}

const navigateToConnection = () => {
  if (!selectedTransaction.value?.metadata.connection_id) return
  params.connection_id = selectedTransaction.value.metadata.connection_id
  params.tab = 'connections'
}

const navigateToProcess = () => {
  if (!selectedTransaction.value?.metadata.process_id) return
  params.process_id = selectedTransaction.value.metadata.process_id
  params.tab = 'processes'
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
  if (isAtTop() && !selectedTransaction.value) {
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

const selectedTransaction = computed(() => {
  const requestId = params.request_id as string
  if (!requestId) return null
  return requests.value.find(
    (tx) => tx.request.request_id === requestId
  ) || null
})

const detectLanguage = (contentType?: string, body?: string): string | null => {
  if (!contentType && !body) return null

  // Map content types to Prism language identifiers
  const contentTypeMap: Record<string, string> = {
    'application/json': 'json',
    'application/javascript': 'javascript',
    'text/javascript': 'javascript',
    'application/typescript': 'typescript',
    'text/typescript': 'typescript',
    'text/css': 'css',
    'text/html': 'markup',
    'application/xml': 'markup',
    'text/xml': 'markup',
    'application/xhtml+xml': 'markup',
    'application/sql': 'sql',
    'text/sql': 'sql',
    'application/x-yaml': 'yaml',
    'text/yaml': 'yaml',
    'application/x-sh': 'bash',
    'text/x-sh': 'bash',
  }

  // Check content type first
  if (contentType) {
    const baseContentType = contentType.split(';')[0]?.trim().toLowerCase()
    if (baseContentType && contentTypeMap[baseContentType]) {
      return contentTypeMap[baseContentType]
    }
  }

  // Try to detect JSON by parsing
  if (body) {
    try {
      JSON.parse(body)
      return 'json'
    } catch {
      // Not JSON
    }
  }

  return null
}

const highlightCode = (code: string, language: string): string => {
  try {
    const grammar = Prism.languages[language]
    if (grammar) {
      return Prism.highlight(code, grammar, language)
    }
  } catch (e) {
    console.warn('Prism highlighting error:', e)
  }
  return code
}

const formatAndHighlightBody = (body?: string, contentType?: string): string => {
  if (!body) return 'No body'

  // Try to decode base64 first
  let decodedBody = body
  try {
    decodedBody = atob(body)
  } catch {
    // Not base64 encoded, use original
    decodedBody = body
  }

  // Try to parse and pretty-print JSON
  let formattedBody = decodedBody
  let detectedLanguage = detectLanguage(contentType, decodedBody)

  try {
    const parsed = JSON.parse(decodedBody)
    formattedBody = JSON.stringify(parsed, null, 2)
    detectedLanguage = 'json' // Override detected language if JSON parsing succeeds
  } catch {
    // Not JSON, use decoded body as-is
  }

  // Apply syntax highlighting if language detected
  if (detectedLanguage) {
    return highlightCode(formattedBody, detectedLanguage)
  }

  return formattedBody
}

const formatBody = (body?: string): string => {
  if (!body) return 'No body'

  // Try to decode base64 first
  let decodedBody = body
  try {
    decodedBody = atob(body)
  } catch {
    // Not base64 encoded, use original
    decodedBody = body
  }

  // Try to parse and pretty-print JSON
  try {
    const parsed = JSON.parse(decodedBody)
    return JSON.stringify(parsed, null, 2)
  } catch {
    // Return decoded body as-is if not JSON
    return decodedBody
  }
}

const isImageRequest = (transaction: HttpTransaction | null): boolean => {
  if (!transaction?.request?.headers) return false

  // Check Content-Type header (case-insensitive)
  const contentType = Object.entries(transaction.request.headers).find(
    ([key]) => key.toLowerCase() === 'content-type'
  )?.[1]

  return contentType ? contentType.toLowerCase().startsWith('image/') : false
}

const getRequestImageDataUrl = (transaction: HttpTransaction | null): string => {
  if (!transaction?.request?.body || !transaction?.request?.headers) {
    return ''
  }

  // Get Content-Type header (case-insensitive)
  const contentType = Object.entries(transaction.request.headers).find(
    ([key]) => key.toLowerCase() === 'content-type'
  )?.[1]

  if (!contentType) return ''

  // The body is base64 encoded, so we can use it directly
  // Check if it's already in data URL format
  if (transaction.request.body.startsWith('data:')) {
    return transaction.request.body
  }

  // Otherwise, create a data URL with the content type and base64 body
  return `data:${contentType};base64,${transaction.request.body}`
}

const isImageResponse = (transaction: HttpTransaction | null): boolean => {
  if (!transaction?.response?.content_type) return false
  return transaction.response.content_type.startsWith('image/')
}

const getImageDataUrl = (transaction: HttpTransaction | null): string => {
  if (!transaction?.response?.body || !transaction?.response?.content_type) {
    return ''
  }

  // The body is base64 encoded, so we can use it directly
  // Check if it's already in data URL format
  if (transaction.response.body.startsWith('data:')) {
    return transaction.response.body
  }

  // Otherwise, create a data URL with the content type and base64 body
  return `data:${transaction.response.content_type};base64,${transaction.response.body}`
}

const totalBytes = computed(() => {
  if (!selectedTransaction.value) return 0
  return (
    (selectedTransaction.value.metadata.bytes_sent || 0) +
    (selectedTransaction.value.metadata.bytes_received || 0)
  )
})

// Watch for new requests - set flag if new item at top and not scrolled to top
watch(
  () => requests.value[0]?.request.request_id,
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
  () => selectedTransaction.value,
  (newVal) => {
    if (newVal) {
      frozenMode.value = true
    }
  }
)

// Auto-scroll to selected request if it's not visible
watch(
  [() => selectedTransaction.value?.request.request_id, scrollContainer],
  ([selectedId, container]) => {
    if (!selectedId || !container) return
    
    nextTick(() => {
      const selectedIndex = requests.value.findIndex(
        tx => tx.request.request_id === selectedId
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

<style>
/* Conditionally apply Prism themes based on dark mode */
.dark pre[class*="language-"],
.dark code[class*="language-"] {
  color: #ccc !important;
  background: #2d2d2d !important;
}

.dark .token.comment,
.dark .token.block-comment,
.dark .token.prolog,
.dark .token.doctype,
.dark .token.cdata {
  color: #999 !important;
}

.dark .token.punctuation {
  color: #ccc !important;
}

.dark .token.tag,
.dark .token.attr-name,
.dark .token.namespace,
.dark .token.deleted {
  color: #e2777a !important;
}

.dark .token.function-name {
  color: #6196cc !important;
}

.dark .token.boolean,
.dark .token.number,
.dark .token.function {
  color: #f08d49 !important;
}

.dark .token.property,
.dark .token.class-name,
.dark .token.constant,
.dark .token.symbol {
  color: #f8c555 !important;
}

.dark .token.selector,
.dark .token.important,
.dark .token.atrule,
.dark .token.keyword,
.dark .token.builtin {
  color: #cc99cd !important;
}

.dark .token.string,
.dark .token.char,
.dark .token.attr-value,
.dark .token.regex,
.dark .token.variable {
  color: #7ec699 !important;
}

.dark .token.operator,
.dark .token.entity,
.dark .token.url {
  color: #67cdcc !important;
}
</style>
