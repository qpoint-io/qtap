import { ref, computed, onMounted } from 'vue'

export interface ColumnConfig {
  key: string
  label: string
  width: number // pixels
  minWidth: number // pixels
  visible: boolean
  resizable: boolean
}

export interface ColumnResizeOptions {
  storageKey: string
  defaultColumns: ColumnConfig[]
}

interface StoredColumnState {
  width: number
  visible: boolean
}

export function useColumnResize(options: ColumnResizeOptions) {
  const { storageKey, defaultColumns } = options

  // Deep clone default columns to avoid mutating the original
  const columns = ref<ColumnConfig[]>(
    defaultColumns.map((col) => ({ ...col }))
  )

  const isResizing = ref(false)
  let resizingColumnKey: string | null = null
  let startX = 0
  let startWidth = 0

  // Load persisted state from localStorage
  const loadFromStorage = () => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored) {
        const parsed: Record<string, StoredColumnState> = JSON.parse(stored)
        columns.value.forEach((col) => {
          // Skip loading width for path column (it's calculated dynamically)
          // But still load visibility
          const storedCol = parsed[col.key]
          if (storedCol) {
            if (col.key !== 'path') {
              col.width = Math.max(col.minWidth, storedCol.width)
            }
            col.visible = storedCol.visible
          }
        })
      }
    } catch (e) {
      console.warn('Failed to load column state from localStorage:', e)
    }
  }

  // Save state to localStorage
  const saveToStorage = () => {
    try {
      const state: Record<string, StoredColumnState> = {}
      columns.value.forEach((col) => {
        // Don't save path column width (it's calculated dynamically)
        // But do save visibility
        state[col.key] = {
          width: col.key === 'path' ? (defaultColumns.find(c => c.key === 'path')?.width || col.width) : col.width,
          visible: col.visible,
        }
      })
      localStorage.setItem(storageKey, JSON.stringify(state))
    } catch (e) {
      console.warn('Failed to save column state to localStorage:', e)
    }
  }

  // Get visible columns
  const visibleColumns = computed(() =>
    columns.value.filter((col) => col.visible)
  )

  // Get column by key
  const getColumn = (key: string): ColumnConfig | undefined => {
    return columns.value.find((col) => col.key === key)
  }

  // Resize a column
  const resizeColumn = (key: string, width: number) => {
    const col = getColumn(key)
    if (col) {
      col.width = Math.max(col.minWidth, width)
      saveToStorage()
    }
  }

  // Toggle column visibility
  const toggleColumn = (key: string) => {
    const col = getColumn(key)
    if (col) {
      col.visible = !col.visible
      saveToStorage()
    }
  }

  // Set column visibility explicitly
  const setColumnVisible = (key: string, visible: boolean) => {
    const col = getColumn(key)
    if (col) {
      col.visible = visible
      saveToStorage()
    }
  }

  // Reset all columns to defaults
  const resetColumns = () => {
    columns.value = defaultColumns.map((col) => ({ ...col }))
    saveToStorage()
  }

  // Track the adjacent column for redistributive resizing
  let adjacentColumnKey: string | null = null
  let adjacentStartWidth = 0

  // Start column resize - call from mousedown on resize handle
  const startColumnResize = (e: MouseEvent, key: string) => {
    e.preventDefault()
    const col = getColumn(key)
    if (!col || !col.resizable) return

    // Find the adjacent column (next visible column)
    const visibleCols = columns.value.filter(c => c.visible)
    const colIndex = visibleCols.findIndex(c => c.key === key)
    const adjacentCol = colIndex >= 0 && colIndex < visibleCols.length - 1 
      ? visibleCols[colIndex + 1] 
      : null

    resizingColumnKey = key
    adjacentColumnKey = adjacentCol?.key || null
    startX = e.clientX
    startWidth = col.width
    adjacentStartWidth = adjacentCol?.width || 0
    isResizing.value = true
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'

    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)
  }

  // Handle mouse move during column resize
  const handleMouseMove = (e: MouseEvent) => {
    if (!isResizing.value || !resizingColumnKey) return

    const col = getColumn(resizingColumnKey)
    if (!col) return

    const deltaX = e.clientX - startX
    
    // Redistributive resize: grow one column, shrink the adjacent
    if (adjacentColumnKey) {
      const adjacentCol = getColumn(adjacentColumnKey)
      if (adjacentCol) {
        // Calculate new widths
        let newWidth = startWidth + deltaX
        let newAdjacentWidth = adjacentStartWidth - deltaX
        
        // Enforce minimum widths
        if (newWidth < col.minWidth) {
          newWidth = col.minWidth
          newAdjacentWidth = adjacentStartWidth + startWidth - col.minWidth
        }
        if (newAdjacentWidth < adjacentCol.minWidth) {
          newAdjacentWidth = adjacentCol.minWidth
          newWidth = startWidth + adjacentStartWidth - adjacentCol.minWidth
        }
        
        col.width = newWidth
        adjacentCol.width = newAdjacentWidth
        return
      }
    }
    
    // Fallback: just resize the column (for last column)
    const newWidth = Math.max(col.minWidth, startWidth + deltaX)
    col.width = newWidth
  }

  // Handle mouse up - end column resize
  const handleMouseUp = () => {
    if (isResizing.value) {
      isResizing.value = false
      resizingColumnKey = null
      adjacentColumnKey = null
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
      saveToStorage()

      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
    }
  }

  // Auto-fit column width based on content
  // Pass a function that measures the max content width for a column
  const autoFitColumn = (key: string, measureFn: () => number) => {
    const col = getColumn(key)
    if (!col) return

    const measuredWidth = measureFn()
    col.width = Math.max(col.minWidth, measuredWidth)
    saveToStorage()
  }

  onMounted(() => {
    loadFromStorage()
  })

  return {
    columns,
    visibleColumns,
    isResizing,
    getColumn,
    resizeColumn,
    toggleColumn,
    setColumnVisible,
    resetColumns,
    startColumnResize,
    autoFitColumn,
  }
}

