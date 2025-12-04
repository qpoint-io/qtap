import { computed, watchEffect } from 'vue'
import { storeToRefs } from 'pinia'
import { useHttpStore, type HttpTransaction } from '@/stores/http'
import { useUrlParams } from '@/composables/urlParams'
import { useBufferSettings } from '@/composables/bufferSettings'

// Re-export SizeUnit for components that import from this file
export type { SizeUnit } from '@/composables/bufferSettings'

export function useRequests() {
  // get the store
  const store = useHttpStore()
  
  // get reactive pause state and filters from store
  const { paused, filters } = storeToRefs(store)

  // watch URL params to sync selected item with buffer GC protection
  const params = useUrlParams()
  watchEffect(() => {
    const requestId = params.request_id as string | undefined
    store.setSelectedId(requestId || null)
  })

  // Buffer settings with localStorage persistence
  const {
    maxItems,
    maxSize,
    maxSizeUnit,
    resetBufferSettings,
  } = useBufferSettings(
    'devtools_http_buffer_settings',
    (maxItems, maxBytes) => store.updateBufferLimits(maxItems, maxBytes)
  )

  const filtered = computed(() => {
    let result = store.requestsBuffer
    
    // Apply each filter
    filters.value.forEach(filter => {
      result = result.filter(transaction => {
        // Get the value to filter against based on the key
        let fieldValue: string | undefined
        
        switch (filter.key) {
          case 'method':
            fieldValue = transaction.request.method
            break
          case 'status':
            fieldValue = transaction.response.status?.toString()
            break
          case 'endpoint':
            fieldValue = getEndpointFromUrl(transaction.request.url)
            if (fieldValue === '-') fieldValue = undefined
            break
          case 'path':
            fieldValue = getPathFromUrl(transaction.request.url)
            if (fieldValue === '-') fieldValue = undefined
            break
          case 'direction':
            fieldValue = transaction.direction
            break
          case 'process':
            fieldValue = transaction.metadata.process_exe
            break
          case 'type':
            fieldValue = transaction.response.content_type
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
    'method',
    'status',
    'endpoint',
    'path',
    'direction',
    'process',
    'type',
  ]

  const getFilterValues = (key: string): string[] => {
    const values = new Set<string>()
    
    store.requestsBuffer.forEach(transaction => {
      let value: string | undefined
      
      switch (key) {
        case 'method':
          value = transaction.request.method
          break
        case 'status':
          value = transaction.response.status?.toString()
          break
        case 'endpoint':
          value = getEndpointFromUrl(transaction.request.url)
          if (value === '-') value = undefined
          break
        case 'path':
          value = getPathFromUrl(transaction.request.url)
          break
        case 'direction':
          value = transaction.direction
          break
        case 'process':
          value = transaction.metadata.process_exe
          break
        case 'type':
          value = transaction.response.content_type
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

export function useCurl() {
  const generateCurlCommand = (transaction: HttpTransaction | null): string => {
    if (!transaction) return ''
    
    const { request } = transaction
    const parts: string[] = [`curl -X ${request.method}`]
    
    // Add URL (quoted)
    parts.push(`'${request.url}'`)
    
    // Add headers (exclude pseudo-headers that start with ":")
    if (request.headers) {
      Object.entries(request.headers).forEach(([key, value]) => {
        if (!key.startsWith(':')) {
          parts.push(`-H '${key}: ${value}'`)
        }
      })
    }
    
    // Add body if present
    if (request.body) {
      let bodyContent = request.body
      // Try to decode if base64
      try {
        bodyContent = atob(request.body)
      } catch {
        // Use as-is if not base64
      }
      
      // Escape single quotes in body
      const escapedBody = bodyContent.replace(/'/g, "'\\''")
      parts.push(`--data '${escapedBody}'`)
    }
    
    // Join with backslash and newline for readability
    return parts.join(' \\\n  ')
  }
  
  return { generateCurlCommand }
}

// Helper functions for URL parsing
function getPathFromUrl(url: string): string {
  try {
    const urlObj = new URL(url)
    return urlObj.pathname
  } catch {
    return '-'
  }
}

function getEndpointFromUrl(url: string): string {
  try {
    const urlObj = new URL(url)
    return urlObj.hostname
  } catch {
    return '-'
  }
}
