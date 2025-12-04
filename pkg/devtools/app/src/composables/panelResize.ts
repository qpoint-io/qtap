import { ref, onMounted, onUnmounted } from 'vue'

export interface PanelResizeOptions {
  storageKey: string
  defaultWidth: number // percentage (0-100)
  minWidth?: number // percentage (0-100)
  maxWidth?: number // percentage (0-100)
}

export function usePanelResize(options: PanelResizeOptions) {
  const {
    storageKey,
    defaultWidth,
    minWidth = 20,
    maxWidth = 80,
  } = options

  const panelWidth = ref(defaultWidth)
  const isResizing = ref(false)
  let containerElement: HTMLElement | null = null

  // Load persisted width from localStorage
  const loadFromStorage = () => {
    try {
      const stored = localStorage.getItem(storageKey)
      if (stored) {
        const parsed = parseFloat(stored)
        if (!isNaN(parsed) && parsed >= minWidth && parsed <= maxWidth) {
          panelWidth.value = parsed
        }
      }
    } catch (e) {
      console.warn('Failed to load panel width from localStorage:', e)
    }
  }

  // Save width to localStorage
  const saveToStorage = () => {
    try {
      localStorage.setItem(storageKey, panelWidth.value.toString())
    } catch (e) {
      console.warn('Failed to save panel width to localStorage:', e)
    }
  }

  // Handle mouse move during resize
  const handleMouseMove = (e: MouseEvent) => {
    if (!isResizing.value || !containerElement) return

    const containerRect = containerElement.getBoundingClientRect()
    const containerWidth = containerRect.width
    const mouseX = e.clientX - containerRect.left

    // Calculate percentage
    let newWidth = (mouseX / containerWidth) * 100

    // Clamp to min/max
    newWidth = Math.max(minWidth, Math.min(maxWidth, newWidth))

    panelWidth.value = newWidth
  }

  // Handle mouse up - end resize
  const handleMouseUp = () => {
    if (isResizing.value) {
      isResizing.value = false
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
      saveToStorage()
    }
  }

  // Start resize - call this from mousedown on divider
  const startResize = (e: MouseEvent, container: HTMLElement) => {
    e.preventDefault()
    containerElement = container
    isResizing.value = true
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
  }

  // Reset to default width
  const resetWidth = () => {
    panelWidth.value = defaultWidth
    saveToStorage()
  }

  onMounted(() => {
    loadFromStorage()
    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)
  })

  onUnmounted(() => {
    document.removeEventListener('mousemove', handleMouseMove)
    document.removeEventListener('mouseup', handleMouseUp)
  })

  return {
    panelWidth,
    isResizing,
    startResize,
    resetWidth,
  }
}

