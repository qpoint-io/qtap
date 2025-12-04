import { ref, computed, watch, type Ref, type ComputedRef } from 'vue'

export interface VirtualScrollOptions<T> {
  items: Ref<T[]> | ComputedRef<T[]>
  itemHeight: number
  headerHeight?: number
  bufferSize?: number
}

export interface VirtualScrollReturn<T> {
  scrollContainer: Ref<HTMLElement | null>
  visibleItems: ComputedRef<T[]>
  topSpacerHeight: ComputedRef<number>
  bottomSpacerHeight: ComputedRef<number>
  handleScroll: () => void
  scrollToTop: () => void
  scrollToBottom: () => void
  isScrollable: ComputedRef<boolean>
  isAtBottom: Ref<boolean>
}

export function useVirtualScroll<T>(options: VirtualScrollOptions<T>): VirtualScrollReturn<T> {
  const {
    items,
    itemHeight,
    headerHeight = 0,
    bufferSize = 50,
  } = options

  const scrollContainer = ref<HTMLElement | null>(null)
  const scrollTop = ref(0)
  const containerHeight = ref(0)
  const isAtBottom = ref(true)

  // Calculate visible range based on scroll position
  const visibleRange = computed(() => {
    if (!scrollContainer.value) {
      return { start: 0, end: Math.min(bufferSize, items.value.length) }
    }

    const scrollOffset = scrollTop.value
    const viewportHeight = containerHeight.value

    // Calculate which items are in view
    const startIndex = Math.floor(scrollOffset / itemHeight)
    const endIndex = Math.ceil((scrollOffset + viewportHeight) / itemHeight)

    // Add buffer for smoother scrolling
    const bufferedStart = Math.max(0, startIndex - bufferSize)
    const bufferedEnd = Math.min(items.value.length, endIndex + bufferSize)

    return { start: bufferedStart, end: bufferedEnd }
  })

  // Get only the visible items
  const visibleItems = computed(() => {
    const { start, end } = visibleRange.value
    return items.value.slice(start, end)
  })

  // Calculate spacer heights for proper scrollbar
  const topSpacerHeight = computed(() => {
    return visibleRange.value.start * itemHeight
  })

  const bottomSpacerHeight = computed(() => {
    const totalHeight = items.value.length * itemHeight
    const renderedHeight = (visibleRange.value.end - visibleRange.value.start) * itemHeight
    return Math.max(0, totalHeight - topSpacerHeight.value - renderedHeight)
  })

  // Check if container is scrollable
  const isScrollable = computed(() => {
    if (!scrollContainer.value) return false
    return scrollContainer.value.scrollHeight > scrollContainer.value.clientHeight
  })

  // Handle scroll event
  const handleScroll = () => {
    if (!scrollContainer.value) return

    scrollTop.value = scrollContainer.value.scrollTop
    containerHeight.value = scrollContainer.value.clientHeight

    // Check if at bottom (with small threshold for float precision)
    const scrollBottom = scrollContainer.value.scrollHeight - scrollContainer.value.scrollTop - scrollContainer.value.clientHeight
    isAtBottom.value = scrollBottom < 5
  }

  // Scroll to top
  const scrollToTop = () => {
    if (scrollContainer.value) {
      scrollContainer.value.scrollTop = 0
    }
  }

  // Scroll to bottom
  const scrollToBottom = () => {
    if (scrollContainer.value) {
      scrollContainer.value.scrollTop = scrollContainer.value.scrollHeight
    }
  }

  // Update container height on mount/resize
  watch(scrollContainer, (container) => {
    if (container) {
      containerHeight.value = container.clientHeight
      
      // Use ResizeObserver to track container size changes
      const resizeObserver = new ResizeObserver(() => {
        if (container) {
          containerHeight.value = container.clientHeight
        }
      })
      resizeObserver.observe(container)
    }
  }, { immediate: true })

  return {
    scrollContainer,
    visibleItems,
    topSpacerHeight,
    bottomSpacerHeight,
    handleScroll,
    scrollToTop,
    scrollToBottom,
    isScrollable,
    isAtBottom,
  }
}

