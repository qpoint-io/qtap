<template>
  <div class="flex flex-col h-full">
    <!-- Table Container -->
    <div
      ref="scrollContainer"
      class="flex-1 overflow-y-auto overflow-x-auto"
      @scroll="handleScroll"
    >
      <!-- Table Header (Sticky) -->
      <div
        class="flex bg-dt-bg-secondary dark:bg-dt-bg-dark-secondary border-b border-dt-border-light dark:border-dt-border-dark-light text-xs font-semibold text-dt-text-secondary dark:text-dt-text-dark-secondary sticky top-0 z-10"
      >
        <template v-for="(column, index) in displayedColumns" :key="column.key">
          <div
            class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light relative flex items-center group"
            :class="getHeaderClass(column)"
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

      <!-- Top spacer for virtual scrolling -->
      <div :style="{ height: `${topSpacerHeight}px` }"></div>

      <!-- Render only visible items -->
      <div
        v-for="(item, itemIndex) in visibleItems"
        :key="getItemKey(item)"
        @click="$emit('row-click', item)"
        class="flex text-xs border-b border-dt-border-light dark:border-dt-border-dark-light cursor-pointer hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover"
        :class="{
          'bg-dt-bg-selected dark:bg-dt-bg-dark-selected': isItemSelected(item),
        }"
      >
        <template v-for="column in displayedColumns" :key="column.key">
          <div
            class="px-2 py-1.5 border-r border-dt-border-light dark:border-dt-border-dark-light truncate"
            :class="getCellClass(column)"
            :style="{ width: `${column.width}px`, minWidth: `${column.minWidth}px` }"
          >
            <slot :name="`cell-${column.key}`" :item="item" :column="column" :index="itemIndex">
              {{ getDefaultCellValue(item, column) }}
            </slot>
          </div>
        </template>
      </div>

      <!-- Bottom spacer for virtual scrolling -->
      <div :style="{ height: `${bottomSpacerHeight}px` }"></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onUnmounted } from 'vue'
import { useColumnResize, type ColumnConfig } from '@/composables/columnResize'
import { useVirtualScroll } from '@/composables/virtualScroll'

interface Props {
  columns: ColumnConfig[]
  data: any[]
  rowHeight?: number
  storageKey: string
  flexColumn?: string // Column key that should auto-expand to fill space
  selectedId?: string | null
  itemKey?: string // Property name to use as unique key for items
}

const props = withDefaults(defineProps<Props>(), {
  rowHeight: 33,
  flexColumn: undefined,
  selectedId: null,
  itemKey: 'id',
})

const emit = defineEmits<{
  (e: 'row-click', item: any): void
}>()

// Column resize composable
const {
  columns: columnState,
  visibleColumns,
  isResizing: isColumnResizing,
  toggleColumn,
  resetColumns,
  startColumnResize,
} = useColumnResize({
  storageKey: props.storageKey,
  defaultColumns: props.columns,
})

// Computed for data as ref
const dataRef = computed(() => props.data)

// Virtual scroll composable
const {
  scrollContainer,
  visibleItems,
  topSpacerHeight,
  bottomSpacerHeight,
  handleScroll,
  scrollToTop,
  isScrollable,
  isAtBottom,
} = useVirtualScroll({
  items: dataRef,
  itemHeight: props.rowHeight,
  headerHeight: props.rowHeight,
  bufferSize: 50,
})

// Displayed columns (visible ones)
const displayedColumns = computed(() => visibleColumns.value)

// Track container width for flex column expansion
let resizeObserver: ResizeObserver | null = null
let lastContainerWidth = 0

// Calculate the minimum flex column width based on available space
const calculateFlexColumnMinWidth = (): number => {
  if (!scrollContainer.value || !props.flexColumn) return 100

  const flexCol = props.columns.find(col => col.key === props.flexColumn)
  const defaultFlexWidth = flexCol?.minWidth || 100

  const availableWidth = scrollContainer.value.clientWidth

  // Calculate sum of non-flex visible column widths
  let otherColumnsWidth = 0
  for (const col of visibleColumns.value) {
    if (col.key !== props.flexColumn) {
      otherColumnsWidth += col.width
    }
  }

  return Math.max(defaultFlexWidth, availableWidth - otherColumnsWidth)
}

// Expand flex column to fill available space
const expandFlexColumn = (containerWidth: number, forceRecalculate = false) => {
  if (!scrollContainer.value || !props.flexColumn) return

  const flexCol = columnState.value.find(col => col.key === props.flexColumn)
  if (!flexCol || !flexCol.visible) return

  const defaultFlexCol = props.columns.find(col => col.key === props.flexColumn)
  const defaultFlexWidth = defaultFlexCol?.minWidth || 100

  const availableWidth = containerWidth

  // Calculate sum of non-flex visible column widths
  let otherColumnsWidth = 0
  for (const col of visibleColumns.value) {
    if (col.key !== props.flexColumn) {
      otherColumnsWidth += col.width
    }
  }

  // Calculate what flex column width should be
  const calculatedWidth = availableWidth - otherColumnsWidth
  const targetWidth = Math.max(defaultFlexWidth, calculatedWidth)

  // If forcing recalculate, always set the width
  // Otherwise only expand (never shrink)
  if (forceRecalculate) {
    flexCol.width = targetWidth
  } else if (targetWidth > flexCol.width) {
    flexCol.width = targetWidth
  }
}

// Handle column resize start - for flex column, set minimum to calculated width
const handleColumnResizeStart = (e: MouseEvent, key: string) => {
  if (key === props.flexColumn) {
    const flexCol = columnState.value.find(col => col.key === props.flexColumn)
    if (flexCol) {
      flexCol.minWidth = calculateFlexColumnMinWidth()
    }
  }
  startColumnResize(e, key)
}

// Handle auto-fit column on double-click
const handleAutoFit = (key: string) => {
  const defaultCol = props.columns.find(c => c.key === key)
  if (defaultCol) {
    const col = columnState.value.find(c => c.key === key)
    if (col) {
      col.width = defaultCol.width
    }
  }
}

// Handle column toggle
const handleColumnToggle = (key: string) => {
  toggleColumn(key)
  requestAnimationFrame(() => {
    if (scrollContainer.value) {
      expandFlexColumn(scrollContainer.value.clientWidth, true)
    }
  })
}

// Handle reset columns
const handleResetColumns = () => {
  resetColumns()
  requestAnimationFrame(() => {
    if (scrollContainer.value) {
      expandFlexColumn(scrollContainer.value.clientWidth, true)
    }
  })
}

// Get item key for v-for
const getItemKey = (item: any): string => {
  if (typeof props.itemKey === 'string') {
    // Support nested keys like 'request.request_id'
    return props.itemKey.split('.').reduce((obj, key) => obj?.[key], item) ?? ''
  }
  return ''
}

// Check if item is selected
const isItemSelected = (item: any): boolean => {
  if (!props.selectedId) return false
  return getItemKey(item) === props.selectedId
}

// Get default cell value (for when no slot is provided)
const getDefaultCellValue = (item: any, column: ColumnConfig): string => {
  return item[column.key] ?? ''
}

// Get header class based on column type
const getHeaderClass = (column: ColumnConfig): string => {
  // Can be extended for alignment classes etc.
  return ''
}

// Get cell class based on column type
const getCellClass = (column: ColumnConfig): string => {
  return 'text-dt-text-primary dark:text-dt-text-dark-primary'
}

// Watch for scroll container changes
watch(scrollContainer, (container) => {
  if (resizeObserver) {
    resizeObserver.disconnect()
  }

  if (container) {
    lastContainerWidth = container.clientWidth
    expandFlexColumn(lastContainerWidth, false)

    resizeObserver = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const newWidth = entry.contentRect.width
        if (newWidth > lastContainerWidth) {
          expandFlexColumn(newWidth, false)
        }
        lastContainerWidth = newWidth
      }
    })
    resizeObserver.observe(container)
  }
}, { immediate: true })

// Re-calculate when data changes (scrollbar may appear/disappear)
watch(() => props.data.length, () => {
  if (scrollContainer.value) {
    nextTick(() => {
      expandFlexColumn(scrollContainer.value!.clientWidth, false)
    })
  }
})

// Re-calculate when columns change
watch([
  () => displayedColumns.value.length,
  () => displayedColumns.value.map(c => c.key).join(','),
  () => displayedColumns.value.filter(c => c.key !== props.flexColumn).map(c => c.width).join(',')
], () => {
  if (scrollContainer.value) {
    requestAnimationFrame(() => {
      if (scrollContainer.value) {
        expandFlexColumn(scrollContainer.value.clientWidth, true)
      }
    })
  }
}, { flush: 'post' })

// Cleanup
onUnmounted(() => {
  if (resizeObserver) {
    resizeObserver.disconnect()
  }
})

// Expose for parent components
defineExpose({
  columns: columnState,
  visibleColumns,
  toggleColumn: handleColumnToggle,
  resetColumns: handleResetColumns,
  scrollToTop,
  isScrollable,
  isAtBottom,
  scrollContainer,
})
</script>

