/**
 * Persisted buffer composable - manages a ringbuffer with sessionStorage persistence
 * Uses size-based limits (bytes) instead of item counts for more predictable memory usage
 */

import { ref } from 'vue'
import {
  saveToSessionStorage,
  loadFromSessionStorage,
  removeFromSessionStorage,
  getStorageSize,
  isSessionStorageAvailable,
} from './storage'

interface PersistedData<T> {
  timestamp: number  // Unix timestamp in milliseconds
  data: T[]
}

export interface PersistedBufferOptions {
  storageKey: string
  maxBytes?: number
  maxItems?: number
  maxAgeMs?: number  // Default: 24 hours (86400000)
  idKey?: string
}

export function usePersistedBuffer<T>(options: PersistedBufferOptions) {
  const { 
    storageKey, 
    maxBytes: initialMaxBytes = 5 * 1024 * 1024, 
    maxItems: initialMaxItems = 500, 
    maxAgeMs = 24 * 60 * 60 * 1000,
    idKey = 'id',
  } = options

  // Reactive limits that can be updated dynamically
  const currentMaxBytes = ref(initialMaxBytes)
  const currentMaxItems = ref(initialMaxItems)

  // store a ref to the selected item id
  const selectedItemId = ref<string | null>(null)
  
  // Track periodic persistence cleanup function
  let periodicCleanup: (() => void) | null = null

  // pre-generate the nested path
  const nestedPath = idKey.split('.')
  
  // Helper to get nested property value using dot notation
  const getNestedValue = (obj: any): any => {
    return nestedPath.reduce((current, prop) => current?.[prop], obj)
  }

  // check if the item is the selected item
  const isSelectedItem = (item: T) => {
    if (!selectedItemId.value) {
      return false
    }
    const itemId = getNestedValue(item)
    return itemId === selectedItemId.value
  }

  // Set the selected item ID (used to protect item from GC)
  const setSelectedItemId = (id: string | null): void => {  
    selectedItemId.value = id
  }

  // Clear buffer from sessionStorage
  const clear = (): void => {
    removeFromSessionStorage(storageKey)
  }

  // Persist buffer to sessionStorage
  const persist = (buffer: T[]): void => {
    if (!isSessionStorageAvailable()) {
      return
    }

    const wrapped: PersistedData<T> = {
      timestamp: Date.now(),
      data: buffer
    }
    saveToSessionStorage(storageKey, wrapped)
  }

  // Enforce size limit by removing oldest items until under maxBytes
  const enforceSizeLimit = (buffer: T[]): void => {
    if (!isSessionStorageAvailable()) {
      return
    }

    let currentSize = getStorageSize(buffer)

    while (currentSize > currentMaxBytes.value && buffer.length > 0) {
      // Check the oldest item (at the end of the buffer)
      const oldestItem = buffer[buffer.length - 1]
      
      // If the oldest item is selected, remove the second-oldest instead
      if (oldestItem && isSelectedItem(oldestItem)) {
        if (buffer.length > 1) {
          // Remove second-oldest item (at index length - 2)
          buffer.splice(buffer.length - 2, 1)
          currentSize = getStorageSize(buffer)
        } else {
          // Only one item and it's selected - can't remove anything
          break
        }
      } else {
        buffer.pop() // Remove from back (oldest)
        currentSize = getStorageSize(buffer)
      }
    }
  }

  // Restore buffer from sessionStorage on initialization
  const restore = (): T[] | null => {
    if (!isSessionStorageAvailable()) {
      console.warn('SessionStorage not available, persistence disabled')
      return null
    }

    const restored = loadFromSessionStorage<PersistedData<T>>(storageKey)
    if (!restored) {
      return null
    }

    // Check if data has timestamp (new format) or is raw array (old format)
    if (!restored.timestamp || !Array.isArray(restored.data)) {
      // Old format or corrupt data - treat as expired
      console.log(`Flushing old/corrupt data from ${storageKey}`)
      clear()
      return null
    }

    // Check expiration
    const age = Date.now() - restored.timestamp
    if (age > maxAgeMs) {
      console.log(`Data expired (${Math.round(age / 3600000)}h old), flushing ${storageKey}`)
      clear()
      return null
    }

    console.log(`Restored ${restored.data.length} items from ${storageKey}`)
    return restored.data
  }

  // Add item to buffer and enforce maxItems limit (size limit enforced periodically)
  const addAndPersist = (buffer: T[], item: T): T[] => {
    buffer.unshift(item) // Add to front (newest)
    
    // Enforce maxItems limit - keep removing oldest until under limit
    while (buffer.length > currentMaxItems.value) {
      const oldestItem = buffer[buffer.length - 1]
      
      // If the oldest item is selected, remove the second-oldest instead
      if (oldestItem && isSelectedItem(oldestItem)) {
        if (buffer.length > 1) {
          // Remove second-oldest item (at index length - 2)
          buffer.splice(buffer.length - 2, 1)
        } else {
          // Only one item and it's selected - can't remove anything
          break
        }
      } else {
        buffer.pop() // Remove from back (oldest)
      }
    }
    
    return buffer
  }

  // Update buffer and persist
  const updateAndPersist = (buffer: T[]): void => {
    persist(buffer)
  }

  // Remove item from buffer and persist
  const removeAndPersist = (buffer: T[], predicate: (item: T) => boolean): T[] => {
    const index = buffer.findIndex(predicate)
    if (index !== -1) {
      buffer.splice(index, 1)
      persist(buffer)
    }
    return buffer
  }

  // Clear buffer in memory and storage
  const clearAll = (buffer: T[]): T[] => {
    buffer.length = 0
    clear()
    return buffer
  }

  // Start periodic persistence with 5-second interval and browser event listeners
  const startPeriodicPersistence = (getBuffer: () => T[]): void => {
    // Don't start if already running
    if (periodicCleanup !== null) {
      return
    }
    
    // Persist and enforce size limit on interval
    const persistAndEnforce = () => {
      const buffer = getBuffer()
      enforceSizeLimit(buffer)
      persist(buffer)
    }

    // Set up 5-second interval
    const intervalId = setInterval(persistAndEnforce, 5000)

    // Set up browser event listeners for navigation/close
    const handleBeforeUnload = () => {
      const buffer = getBuffer()
      persist(buffer)
    }

    window.addEventListener('beforeunload', handleBeforeUnload)
    window.addEventListener('unload', handleBeforeUnload)

    // Store cleanup function
    periodicCleanup = () => {
      clearInterval(intervalId)
      window.removeEventListener('beforeunload', handleBeforeUnload)
      window.removeEventListener('unload', handleBeforeUnload)
    }
  }

  // Stop periodic persistence
  const stopPeriodicPersistence = (): void => {
    if (periodicCleanup !== null) {
      periodicCleanup()
      periodicCleanup = null
    }
  }

  // Update buffer limits dynamically
  const updateLimits = (maxItems: number, maxBytes: number): void => {
    currentMaxItems.value = maxItems
    currentMaxBytes.value = maxBytes
  }

  // Get current buffer limits
  const getLimits = (): { maxItems: number; maxBytes: number } => {
    return {
      maxItems: currentMaxItems.value,
      maxBytes: currentMaxBytes.value,
    }
  }

  return {
    restore,
    enforceSizeLimit,
    persist,
    clear,
    addAndPersist,
    updateAndPersist,
    removeAndPersist,
    clearAll,
    startPeriodicPersistence,
    stopPeriodicPersistence,
    selectedItemId,
    setSelectedItemId,
    updateLimits,
    getLimits,
  }
}
