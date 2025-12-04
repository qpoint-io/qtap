/**
 * Shared buffer settings composable
 * Provides reusable logic for managing buffer limits across stores
 */

import { ref, computed, watch } from 'vue'

// Buffer settings types
export type SizeUnit = 'KB' | 'MB' | 'GB'

export interface BufferSettings {
  maxItems: number
  maxSize: number
  maxSizeUnit: SizeUnit
}

// Default values
export const DEFAULT_MAX_ITEMS = 500
export const DEFAULT_MAX_SIZE = 5
export const DEFAULT_MAX_SIZE_UNIT: SizeUnit = 'MB'

// Convert size and unit to bytes
export function sizeToBytes(size: number, unit: SizeUnit): number {
  switch (unit) {
    case 'KB':
      return size * 1024
    case 'MB':
      return size * 1024 * 1024
    case 'GB':
      return size * 1024 * 1024 * 1024
    default:
      return size * 1024 * 1024 // Default to MB
  }
}

// Load buffer settings from localStorage
function loadBufferSettings(storageKey: string): BufferSettings {
  try {
    const stored = localStorage.getItem(storageKey)
    if (stored) {
      const parsed = JSON.parse(stored)
      return {
        maxItems: parsed.maxItems ?? DEFAULT_MAX_ITEMS,
        maxSize: parsed.maxSize ?? DEFAULT_MAX_SIZE,
        maxSizeUnit: parsed.maxSizeUnit ?? DEFAULT_MAX_SIZE_UNIT,
      }
    }
  } catch {
    // Ignore parse errors
  }
  return {
    maxItems: DEFAULT_MAX_ITEMS,
    maxSize: DEFAULT_MAX_SIZE,
    maxSizeUnit: DEFAULT_MAX_SIZE_UNIT,
  }
}

// Save buffer settings to localStorage
function saveBufferSettings(storageKey: string, settings: BufferSettings): void {
  try {
    localStorage.setItem(storageKey, JSON.stringify(settings))
  } catch {
    // Ignore storage errors
  }
}

/**
 * Composable for managing buffer settings with localStorage persistence
 * @param storageKey - localStorage key for persisting settings
 * @param updateStoreFn - Function to call when settings change (updates store's buffer limits)
 */
export function useBufferSettings(
  storageKey: string,
  updateStoreFn: (maxItems: number, maxBytes: number) => void
) {
  // Load initial settings from localStorage
  const initialSettings = loadBufferSettings(storageKey)
  
  // Reactive refs for settings
  const maxItems = ref(initialSettings.maxItems)
  const maxSize = ref(initialSettings.maxSize)
  const maxSizeUnit = ref<SizeUnit>(initialSettings.maxSizeUnit)

  // Computed maxBytes from size and unit
  const maxBytes = computed(() => sizeToBytes(maxSize.value, maxSizeUnit.value))

  // Apply initial settings to store
  updateStoreFn(maxItems.value, maxBytes.value)

  // Watch settings and update store + localStorage when they change
  watch(
    [maxItems, maxSize, maxSizeUnit],
    ([newMaxItems, newMaxSize, newMaxSizeUnit]) => {
      const newMaxBytes = sizeToBytes(newMaxSize, newMaxSizeUnit)
      updateStoreFn(newMaxItems, newMaxBytes)
      saveBufferSettings(storageKey, {
        maxItems: newMaxItems,
        maxSize: newMaxSize,
        maxSizeUnit: newMaxSizeUnit,
      })
    }
  )

  // Reset buffer settings to defaults
  const resetBufferSettings = () => {
    maxItems.value = DEFAULT_MAX_ITEMS
    maxSize.value = DEFAULT_MAX_SIZE
    maxSizeUnit.value = DEFAULT_MAX_SIZE_UNIT
  }

  return {
    maxItems,
    maxSize,
    maxSizeUnit,
    maxBytes,
    resetBufferSettings,
  }
}

