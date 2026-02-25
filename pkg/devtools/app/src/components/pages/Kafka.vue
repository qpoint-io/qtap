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
      storage-key="devtools_kafka_panel_width"
      :default-width="40"
      :min-width="10"
      :max-width="90"
      :show-right="!!selectedRequest"
      :class="{ 'select-none': isColumnResizing }"
    >
      <template #left>
        <!-- New Requests Button -->
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
                <div
                  v-if="column.resizable && index < displayedColumns.length - 1"
                  @mousedown="(e) => handleColumnResizeStart(e, column.key)"
                  @dblclick="() => handleAutoFit(column.key)"
                  class="absolute right-0 top-0 bottom-0 w-1 cursor-col-resize hover:bg-dt-accent dark:hover:bg-dt-accent-dark-blue opacity-0 group-hover:opacity-100 transition-opacity z-20"
                ></div>
              </div>
            </template>
          </div>

          <div :style="{ height: `${topSpacerHeight}px` }"></div>

          <div
            v-for="request in visibleRequests"
            :key="request.requestId"
            @click="selectRequest(request)"
            class="flex text-xs border-b border-dt-border-light dark:border-dt-border-dark-light cursor-pointer hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover"
            :style="{ height: `${ITEM_HEIGHT}px` }"
            :class="{
              'bg-dt-bg-selected dark:bg-dt-bg-dark-selected': params.kafka_id === request.requestId,
            }"
          >
            <template v-for="column in displayedColumns" :key="column.key">
              <div
                v-if="column.key === 'timestamp'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ formatTimestamp(request.timestamp) }}
              </div>

              <div
                v-else-if="column.key === 'direction'"
                class="px-[5px] py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light flex items-center"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                <DirectionIndicator :direction="request.direction" />
              </div>

              <div
                v-else-if="column.key === 'operation'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light font-semibold truncate"
                :class="getOperationClass(getOperationType(request.statement))"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ getOperationType(request.statement) }}
              </div>

              <div
                v-else-if="column.key === 'statement'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate font-mono text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ request.statement || '-' }}
              </div>

              <div
                v-else-if="column.key === 'error'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-center font-semibold"
                :class="request.isError ? 'text-dt-status-error dark:text-dt-status-dark-error' : 'text-dt-text-tertiary dark:text-dt-text-dark-tertiary'"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ request.isError ? 'Error' : '-' }}
              </div>

              <div
                v-else-if="column.key === 'duration'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-right text-dt-text-secondary dark:text-dt-text-dark-secondary"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ formatDuration(request.duration) }}
              </div>

              <div
                v-else-if="column.key === 'size'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light text-right text-dt-text-secondary dark:text-dt-text-dark-secondary"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ formatBytes(request.bytesSent + request.bytesReceived) }}
              </div>

              <div
                v-else-if="column.key === 'process'"
                class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate text-dt-text-secondary dark:text-dt-text-dark-secondary text-[11px]"
                :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
              >
                {{ getProcessName(request.process?.exe) }}
              </div>
            </template>
          </div>

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
                :class="getOperationClass(getOperationType(selectedRequest!.statement))"
              >
                {{ getOperationType(selectedRequest!.statement) }}
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
                Process:
                <button
                  v-if="selectedRequest!.process?.pid"
                  @click="navigateToProcess"
                  class="text-dt-accent dark:text-dt-accent-dark-blue hover:underline cursor-pointer"
                >
                  {{ selectedRequest!.process.exe }}
                </button>
                <span v-else>{{ selectedRequest!.process.exe }}</span>
              </span>
              <span v-if="selectedRequest!.process?.containerName">
                Container: {{ selectedRequest!.process.containerName }}
              </span>
            </div>
            <div v-if="selectedRequest!.connectionId" class="flex gap-4 mt-2 text-xs">
              <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">
                Connection:
                <button
                  @click="navigateToConnection"
                  class="text-dt-accent dark:text-dt-accent-dark-blue hover:underline font-mono cursor-pointer"
                >
                  {{ truncateId(selectedRequest!.connectionId) }}
                </button>
              </span>
            </div>
          </div>

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
            @click="activeDetailTab = 'operation'"
            class="px-4 py-2 text-xs relative cursor-pointer"
            :class="{
              'text-dt-text-primary dark:text-dt-text-dark-primary': activeDetailTab === 'operation',
              'text-dt-text-secondary dark:text-dt-text-dark-secondary hover:text-dt-text-primary dark:hover:text-dt-text-dark-primary': activeDetailTab !== 'operation',
            }"
          >
            Operation
            <div
              v-if="activeDetailTab === 'operation'"
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
            v-if="selectedRequest!.responseSummary"
            @click="activeDetailTab = 'messages'"
            class="px-4 py-2 text-xs relative cursor-pointer"
            :class="{
              'text-dt-text-primary dark:text-dt-text-dark-primary': activeDetailTab === 'messages',
              'text-dt-text-secondary dark:text-dt-text-dark-secondary hover:text-dt-text-primary dark:hover:text-dt-text-dark-primary': activeDetailTab !== 'messages',
            }"
          >
            Messages
            <div
              v-if="activeDetailTab === 'messages'"
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
          <!-- Operation Tab -->
          <div v-if="activeDetailTab === 'operation'">
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

            <div class="mb-4">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Timestamp</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <p class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary">{{ selectedRequest!.timestamp }}</p>
              </div>
            </div>

            <div v-if="selectedRequest!.tags && selectedRequest!.tags.length > 0">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Tags</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light">
                <div class="space-y-1">
                  <div
                    v-for="(tag, index) in selectedRequest!.tags"
                    :key="index"
                    class="text-xs font-mono"
                  >
                    <span class="text-dt-text-secondary dark:text-dt-text-dark-secondary">{{ tag.split(':')[0] }}:</span>
                    <span class="text-dt-text-primary dark:text-dt-text-dark-primary ml-2">{{ tag.split(':').slice(1).join(':') }}</span>
                  </div>
                </div>
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
          </div>

          <!-- Messages Tab -->
          <div v-else-if="activeDetailTab === 'messages'">
            <div class="mb-4">
              <h3 class="text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary mb-2">Message Samples</h3>
              <div class="bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary p-2 rounded border border-dt-border-light dark:border-dt-border-dark-light relative">
                <Clipboard
                  v-if="selectedRequest!.responseSummary"
                  :content="selectedRequest!.responseSummary"
                  class="absolute top-2 right-2"
                />
                <pre class="text-xs font-mono text-dt-text-primary dark:text-dt-text-dark-primary whitespace-pre-wrap break-all">{{ selectedRequest!.responseSummary }}</pre>
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
import { useKafka, getOperationType } from '@/composables/kafka'
import { formatTimestamp, formatBytes, formatDuration } from '@/composables/formatters'
import { useColumnResize, type ColumnConfig } from '@/composables/columnResize'
import { useKafkaStore } from '@/stores/kafka'
import type { DatabaseRequest } from '@/types/database'
import DirectionIndicator from '@/components/ux/DirectionIndicator.vue'
import Toolbar from '@/components/ux/Toolbar.vue'
import ColumnToggle from '@/components/ux/ColumnToggle.vue'
import BufferSettings from '@/components/ux/BufferSettings.vue'
import SplitPanel from '@/components/ux/SplitPanel.vue'
import UxPill from '@/components/ux/Pill.vue'
import DoubleUpIcon from '@/components/icons/DoubleUpIcon.vue'
import Clipboard from '@/components/ux/Clipboard.vue'

const defaultColumns: ColumnConfig[] = [
  { key: 'direction', label: '', width: 26, minWidth: 26, visible: true, resizable: false },
  { key: 'timestamp', label: 'Timestamp', width: 128, minWidth: 80, visible: true, resizable: true },
  { key: 'operation', label: 'Operation', width: 128, minWidth: 60, visible: true, resizable: true },
  { key: 'statement', label: 'Statement', width: 400, minWidth: 100, visible: true, resizable: true },
  { key: 'error', label: 'Error', width: 64, minWidth: 50, visible: true, resizable: true },
  { key: 'duration', label: 'Duration', width: 80, minWidth: 50, visible: true, resizable: true },
  { key: 'size', label: 'Size', width: 80, minWidth: 50, visible: true, resizable: true },
  { key: 'process', label: 'Process', width: 128, minWidth: 60, visible: true, resizable: true },
]

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
} = useKafka()

const kafkaStore = useKafkaStore()
const splitPanelRef = ref<InstanceType<typeof SplitPanel> | null>(null)

const pillCenterPosition = computed(() => {
  if (selectedRequest.value && splitPanelRef.value?.panelWidth) {
    return `${splitPanelRef.value.panelWidth / 2}%`
  }
  return '50%'
})

const {
  columns,
  visibleColumns,
  isResizing: isColumnResizing,
  toggleColumn,
  resetColumns,
  startColumnResize,
} = useColumnResize({
  storageKey: 'devtools_kafka_columns',
  defaultColumns,
})

const displayedColumns = computed(() => visibleColumns.value)

const ITEM_HEIGHT = 29
const HEADER_HEIGHT = 29

const scrollContainer = ref<HTMLElement | null>(null)
const frozenMode = ref(false)
const newUnseenItems = ref(false)
const isScrollable = ref(false)
const scrollTop = ref(0)
const containerHeight = ref(0)

const scrollToTop = () => {
  if (scrollContainer.value) {
    scrollContainer.value.scrollTop = 0
  }
}

const topSpacerHeight = computed(() => visibleRange.value.start * ITEM_HEIGHT)
const bottomSpacerHeight = computed(() =>
  (requests.value.length - visibleRange.value.end) * ITEM_HEIGHT
)

const visibleRequests = computed(() => {
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

const CUSTOM_RESIZE_KEY = 'devtools_kafka_custom_resize'
const hasCustomResizing = ref(localStorage.getItem(CUSTOM_RESIZE_KEY) === 'true')

const expandStatementToFillSpace = (force = false) => {
  if (!scrollContainer.value) return
  if (!force && hasCustomResizing.value) return

  const statementCol = columns.value.find(col => col.key === 'statement')
  if (!statementCol || !statementCol.visible) return

  const availableWidth = scrollContainer.value.clientWidth

  let otherColumnsWidth = 0
  for (const col of visibleColumns.value) {
    if (col.key !== 'statement') {
      otherColumnsWidth += col.width
    }
  }

  const targetStatementWidth = Math.max(statementCol.minWidth, availableWidth - otherColumnsWidth)
  statementCol.width = targetStatementWidth
}

const handleColumnResizeStart = (e: MouseEvent, key: string) => {
  hasCustomResizing.value = true
  localStorage.setItem(CUSTOM_RESIZE_KEY, 'true')
  startColumnResize(e, key)
}

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
  requestAnimationFrame(() => expandStatementToFillSpace(true))
}

function handleClear() {
  kafkaStore.clearRequests()
}

function handleColumnToggle(key: string) {
  toggleColumn(key)
  requestAnimationFrame(() => expandStatementToFillSpace(true))
}

function handleResetColumns() {
  resetColumns()
  splitPanelRef.value?.resetWidth()
  hasCustomResizing.value = false
  localStorage.removeItem(CUSTOM_RESIZE_KEY)
  requestAnimationFrame(() => expandStatementToFillSpace())
}

const params = useUrlParams()
const activeDetailTab = ref<'operation' | 'result' | 'messages' | 'timing' | 'process'>('operation')

watch(scrollContainer, (container) => {
  if (container) {
    containerHeight.value = container.clientHeight
    nextTick(() => expandStatementToFillSpace(true))
  }
}, { immediate: true })

let resizeObserver: ResizeObserver | null = null
let lastContainerWidth = 0

onMounted(() => {
  resizeObserver = new ResizeObserver((entries) => {
    for (const entry of entries) {
      const newWidth = entry.contentRect.width
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

const visibleRange = computed(() => {
  if (!scrollContainer.value) {
    return { start: 0, end: requests.value.length }
  }

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

// Color-code by Kafka operation type
const getOperationClass = (operation: string): string => {
  // Produce = green (write)
  if (operation === 'PRODUCE') {
    return 'text-method-post-light dark:text-method-post-dark'
  }

  // Fetch = blue (read)
  if (operation === 'FETCH') {
    return 'text-method-get-light dark:text-method-get-dark'
  }

  // Admin operations = orange
  if (['METADATA', 'CREATETOPICS', 'DELETETOPICS', 'DELETERECORDS'].includes(operation)) {
    return 'text-method-put-light dark:text-method-put-dark'
  }

  return 'text-dt-text-primary dark:text-dt-text-dark-primary'
}

const getProcessName = (exePath?: string): string => {
  if (!exePath) return '-'
  const parts = exePath.split('/')
  return parts[parts.length - 1] || exePath
}

const selectRequest = (request: DatabaseRequest) => {
  params.kafka_id = request.requestId || ''
  activeDetailTab.value = 'operation'
}

const closeRequest = () => {
  delete params.kafka_id
}

const truncateId = (id: string): string => {
  if (id.length <= 16) return id
  return id.substring(0, 8) + '...' + id.substring(id.length - 8)
}

const navigateToConnection = () => {
  if (!selectedRequest.value?.connectionId) return
  params.connection_id = selectedRequest.value.connectionId
  params.tab = 'connections'
}

const navigateToProcess = () => {
  if (!selectedRequest.value?.process?.pid) return
  params.process_id = String(selectedRequest.value.process.pid)
  params.tab = 'processes'
}

const checkScrollable = () => {
  if (!scrollContainer.value) return
  isScrollable.value = scrollContainer.value.scrollHeight > scrollContainer.value.clientHeight
}

const isAtTop = (): boolean => {
  if (!scrollContainer.value) return true
  return scrollContainer.value.scrollTop < 5
}

const handleScroll = () => {
  if (scrollContainer.value) {
    scrollTop.value = scrollContainer.value.scrollTop
    containerHeight.value = scrollContainer.value.clientHeight
  }

  checkScrollable()

  if (isAtTop() && !selectedRequest.value) {
    frozenMode.value = false
    newUnseenItems.value = false
  } else if (!isAtTop() && !frozenMode.value) {
    frozenMode.value = true
  }
}

const jumpToTop = () => {
  frozenMode.value = false
  newUnseenItems.value = false
  nextTick(() => scrollToTop())
}

const selectedRequest = computed(() => {
  const requestId = params.kafka_id as string
  if (!requestId) return null
  return requests.value.find(
    (req) => req.requestId === requestId
  ) || null
})

const totalBytes = computed(() => {
  if (!selectedRequest.value) return 0
  return selectedRequest.value.bytesSent + selectedRequest.value.bytesReceived
})

watch(
  () => requests.value[0]?.requestId,
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
          if (scrollContainer.value) {
            scrollContainer.value.scrollTop = 0
          }
          checkScrollable()
        })
      }
    }
  }
)

watch(
  () => selectedRequest.value,
  (newVal) => {
    if (newVal) {
      frozenMode.value = true
    }
  }
)

watch(
  [() => selectedRequest.value?.requestId, scrollContainer],
  ([selectedId, container]) => {
    if (!selectedId || !container) return

    nextTick(() => {
      const selectedIndex = requests.value.findIndex(
        req => req.requestId === selectedId
      )

      if (selectedIndex === -1) return

      const st = container.scrollTop
      const viewportHeight = container.clientHeight
      const itemTop = selectedIndex * ITEM_HEIGHT
      const itemBottom = itemTop + ITEM_HEIGHT

      const effectiveScrollTop = st + HEADER_HEIGHT
      const effectiveViewportHeight = viewportHeight - HEADER_HEIGHT
      const isAboveViewport = itemTop < effectiveScrollTop
      const isBelowViewport = itemBottom > (effectiveScrollTop + effectiveViewportHeight)

      if (isAboveViewport || isBelowViewport) {
        const targetScroll = itemTop - (effectiveViewportHeight * 0.3) - HEADER_HEIGHT
        container.scrollTop = Math.max(0, targetScroll)
      }
    })
  },
  { flush: 'post' }
)
</script>
