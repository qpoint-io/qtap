import { computed, watchEffect } from 'vue'
import { storeToRefs } from 'pinia'
import { useMySQLStore } from '@/stores/mysql'
import type { DatabaseRequest } from '@/stores/redis'
import { useUrlParams } from '@/composables/urlParams'
import { useBufferSettings } from '@/composables/bufferSettings'
import { usePersistedFilters } from '@/composables/persistedFilters'

export type { SizeUnit } from '@/composables/bufferSettings'

const FILTER_STORAGE_KEY = 'devtools_mysql_filters'

export function useMySQL() {
  const store = useMySQLStore()
  const { paused, filters } = storeToRefs(store)

  usePersistedFilters(FILTER_STORAGE_KEY, filters)

  const params = useUrlParams()
  watchEffect(() => {
    const requestId = params.mysql_id as string | undefined
    store.setSelectedId(requestId || null)
  })

  const {
    maxItems,
    maxSize,
    maxSizeUnit,
    resetBufferSettings,
  } = useBufferSettings(
    'devtools_mysql_buffer_settings',
    (maxItems, maxBytes) => store.updateBufferLimits(maxItems, maxBytes)
  )

  const filtered = computed(() => {
    let result = store.requestsBuffer
    
    filters.value.forEach(filter => {
      result = result.filter(request => {
        let fieldValue: string | undefined
        
        switch (filter.key) {
          case 'type':
            fieldValue = getStatementType(request.statement)
            break
          case 'statement':
            fieldValue = request.statement
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
        
        if (!fieldValue) return false
        
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
    'type',
    'statement',
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
        case 'type':
          value = getStatementType(request.statement)
          break
        case 'statement':
          value = request.statement
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
    maxItems,
    maxSize,
    maxSizeUnit,
    resetBufferSettings,
  }
}

// Extract the first SQL keyword from a statement
export function getStatementType(statement: string): string {
  const firstWord = statement.trim().split(/\s+/)[0]?.toUpperCase()
  return firstWord || '-'
}
