/**
 * Storage utilities for persisting data to sessionStorage with size management
 */

/**
 * Calculate the byte size of a serialized object
 */
export function getStorageSize(data: unknown): number {
  try {
    const serialized = JSON.stringify(data)
    // Use Blob to get accurate byte size (handles UTF-16 encoding correctly)
    return new Blob([serialized]).size
  } catch (error) {
    console.error('Failed to calculate storage size:', error)
    return 0
  }
}

/**
 * Save data to sessionStorage with error handling
 */
export function saveToSessionStorage<T>(key: string, data: T): boolean {
  try {
    const serialized = JSON.stringify(data)
    sessionStorage.setItem(key, serialized)
    return true
  } catch (error) {
    if (error instanceof DOMException && error.name === 'QuotaExceededError') {
      console.warn('SessionStorage quota exceeded for key:', key)
    } else {
      console.error('Failed to save to sessionStorage:', error)
    }
    return false
  }
}

/**
 * Load data from sessionStorage with validation
 */
export function loadFromSessionStorage<T>(key: string): T | null {
  try {
    const serialized = sessionStorage.getItem(key)
    if (!serialized) {
      return null
    }
    return JSON.parse(serialized) as T
  } catch (error) {
    console.error('Failed to load from sessionStorage:', error)
    // Clear corrupt data
    try {
      sessionStorage.removeItem(key)
    } catch {
      // Ignore cleanup errors
    }
    return null
  }
}

/**
 * Remove data from sessionStorage
 */
export function removeFromSessionStorage(key: string): void {
  try {
    sessionStorage.removeItem(key)
  } catch (error) {
    console.error('Failed to remove from sessionStorage:', error)
  }
}

/**
 * Check if sessionStorage is available
 */
export function isSessionStorageAvailable(): boolean {
  try {
    const test = '__storage_test__'
    sessionStorage.setItem(test, test)
    sessionStorage.removeItem(test)
    return true
  } catch {
    return false
  }
}
