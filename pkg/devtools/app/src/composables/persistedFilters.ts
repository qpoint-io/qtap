import { watch, type Ref } from 'vue'
import type { Filter } from '@/stores/filter'

/**
 * Check if localStorage is available
 */
function isLocalStorageAvailable(): boolean {
  try {
    const test = '__storage_test__'
    localStorage.setItem(test, test)
    localStorage.removeItem(test)
    return true
  } catch {
    return false
  }
}

/**
 * Load filters from localStorage
 */
function loadFilters(storageKey: string): Filter[] | null {
  if (!isLocalStorageAvailable()) {
    return null
  }

  try {
    const stored = localStorage.getItem(storageKey)
    if (!stored) {
      return null
    }

    const parsed = JSON.parse(stored)

    // Handle both wrapped format { data: Filter[] } and direct array format
    if (Array.isArray(parsed)) {
      return parsed
    }
    if (parsed.data && Array.isArray(parsed.data)) {
      return parsed.data
    }

    // Invalid format - clear it
    localStorage.removeItem(storageKey)
    return null
  } catch (error) {
    console.warn(`Failed to load filters from ${storageKey}:`, error)
    try {
      localStorage.removeItem(storageKey)
    } catch {
      // Ignore cleanup errors
    }
    return null
  }
}

/**
 * Save filters to localStorage
 */
function saveFilters(storageKey: string, filters: Filter[]): void {
  if (!isLocalStorageAvailable()) {
    return
  }

  try {
    localStorage.setItem(storageKey, JSON.stringify(filters))
  } catch (error) {
    console.warn(`Failed to save filters to ${storageKey}:`, error)
  }
}

/**
 * Composable for managing persisted filters.
 * Automatically restores filters from localStorage and watches for changes.
 * @param storageKey - localStorage key for persisting filters
 * @param filtersRef - Reactive ref to the filters array (from store)
 */
export function usePersistedFilters(
  storageKey: string,
  filtersRef: Ref<Filter[]>
): void {
  // Restore filters from localStorage
  const restored = loadFilters(storageKey)
  if (restored && restored.length > 0) {
    filtersRef.value = restored
  }

  // Watch for filter changes and persist automatically
  watch(
    filtersRef,
    (newFilters) => {
      saveFilters(storageKey, newFilters)
    },
    { deep: true }
  )
}
