import { computed, watchEffect } from 'vue'
import { storeToRefs } from 'pinia'
import { useRedisStore } from '@/stores/redis'
import type { DatabaseRequest } from '@/types/database'
import { useUrlParams } from '@/composables/urlParams'
import { useBufferSettings } from '@/composables/bufferSettings'
import { usePersistedFilters } from '@/composables/persistedFilters'

// Re-export SizeUnit for components that import from this file
export type { SizeUnit } from '@/composables/bufferSettings'

const FILTER_STORAGE_KEY = 'devtools_redis_filters'

export function useRedis() {
  // get the store
  const store = useRedisStore()

  // get reactive pause state and filters from store
  const { paused, filters } = storeToRefs(store)

  // Filter persistence - restore from localStorage and watch for changes
  usePersistedFilters(FILTER_STORAGE_KEY, filters)

  // watch URL params to sync selected item with buffer GC protection
  const params = useUrlParams()
  watchEffect(() => {
    const requestId = params.redis_id as string | undefined
    store.setSelectedId(requestId || null)
  })

  // Buffer settings with localStorage persistence
  const {
    maxItems,
    maxSize,
    maxSizeUnit,
    resetBufferSettings,
  } = useBufferSettings(
    'devtools_redis_buffer_settings',
    (maxItems, maxBytes) => store.updateBufferLimits(maxItems, maxBytes)
  )

  const filtered = computed(() => {
    let result = store.requestsBuffer
    
    // Apply each filter
    filters.value.forEach(filter => {
      result = result.filter(request => {
        // Get the value to filter against based on the key
        let fieldValue: string | undefined
        
        switch (filter.key) {
          case 'command':
            // Extract first word from statement (e.g., "GET", "SET", "HGET")
            fieldValue = request.statement.split(' ')[0]?.toUpperCase()
            break
          case 'direction':
            fieldValue = request.direction
            break
          case 'resultType':
            fieldValue = request.resultType
            break
          case 'isError':
            fieldValue = String(request.isError)
            break
          case 'process':
            fieldValue = request.process?.exe
            break
        }
        
        // If field value doesn't exist, filter it out
        if (!fieldValue) return false
        
        // Apply the operator
        const filterValue = filter.value.toLowerCase()
        const compareValue = fieldValue.toLowerCase()
        
        switch (filter.operator) {
          case 'is':
            return compareValue === filterValue
          case 'is not':
            return compareValue !== filterValue
          case 'contains':
            return compareValue.includes(filterValue)
          case 'does not contain':
            return !compareValue.includes(filterValue)
          case 'starts with':
            return compareValue.startsWith(filterValue)
          case 'does not start with':
            return !compareValue.startsWith(filterValue)
          case 'ends with':
            return compareValue.endsWith(filterValue)
          case 'does not end with':
            return !compareValue.endsWith(filterValue)
          default:
            return true
        }
      })
    })
    
    return result
  })
  
  const filterableKeys = [
    'command',
    'direction',
    'resultType',
    'isError',
    'process',
  ]

  const getFilterValues = (key: string): string[] => {
    const values = new Set<string>()
    
    store.requestsBuffer.forEach(request => {
      let value: string | undefined
      
      switch (key) {
        case 'command':
          value = request.statement.split(' ')[0]?.toUpperCase()
          break
        case 'direction':
          value = request.direction
          break
        case 'resultType':
          value = request.resultType
          break
        case 'isError':
          value = String(request.isError)
          break
        case 'process':
          value = request.process?.exe
          break
      }
      
      if (value) {
        values.add(value)
      }
    })
    
    return Array.from(values).sort()
  }

  return { 
    requests: filtered,
    isPaused: paused,
    filters,
    filterableKeys,
    getFilterValues,
    // Buffer settings
    maxItems,
    maxSize,
    maxSizeUnit,
    resetBufferSettings,
  }
}

// Helper to extract command from statement
export function getCommandFromStatement(statement: string): string {
  return statement.split(' ')[0]?.toUpperCase() || '-'
}
