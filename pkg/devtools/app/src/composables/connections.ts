import { computed, watchEffect } from 'vue'
import { storeToRefs } from 'pinia'
import { useConnectionsStore } from '@/stores/connections'
import { useUrlParams } from '@/composables/urlParams'
import { useBufferSettings } from '@/composables/bufferSettings'
import type { Filter } from '@/stores/filter'

export function useConnections() {
  // get the store
  const store = useConnectionsStore()
  
  // get reactive pause state and filters from store
  const { paused, filters } = storeToRefs(store)

  // watch URL params to sync selected item with buffer GC protection
  const params = useUrlParams()
  watchEffect(() => {
    const connectionId = params.connection_id as string | undefined
    store.setSelectedId(connectionId || null)
  })

  // Buffer settings with localStorage persistence
  const {
    maxItems,
    maxSize,
    maxSizeUnit,
    resetBufferSettings,
  } = useBufferSettings(
    'devtools_connections_buffer_settings',
    (maxItems, maxBytes) => store.updateBufferLimits(maxItems, maxBytes)
  )

  const filtered = computed(() => {
    let result = store.connectionsBuffer
    
    // Apply each filter
    filters.value.forEach(filter => {
      result = result.filter(connection => {
        // Get the value to filter against based on the key
        let fieldValue: string | undefined
        
        switch (filter.key) {
          case 'direction':
            fieldValue = connection.direction
            break
          case 'source':
            fieldValue = `${connection.source.address.ip}:${connection.source.address.port}`
            break
          case 'destination':
            fieldValue = `${connection.destination.address.ip}:${connection.destination.address.port}`
            break
          case 'status':
            fieldValue = connection.status
            break
          case 'socketProtocol':
            fieldValue = connection.socketProtocol
            break
          case 'l7Protocol':
            fieldValue = connection.l7Protocol
            break
          case 'process':
            fieldValue = connection.source.exe
            break
          case 'user':
            fieldValue = connection.source.user
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
    'direction',
    'source',
    'destination',
    'status',
    'socketProtocol',
    'l7Protocol',
    'process',
    'user',
  ]

  const getFilterValues = (key: string): string[] => {
    const values = new Set<string>()
    
    store.connectionsBuffer.forEach(connection => {
      let value: string | undefined
      
      switch (key) {
        case 'direction':
          value = connection.direction
          break
        case 'source':
          value = `${connection.source.address.ip}:${connection.source.address.port}`
          break
        case 'destination':
          value = `${connection.destination.address.ip}:${connection.destination.address.port}`
          break
        case 'status':
          value = connection.status
          break
        case 'socketProtocol':
          value = connection.socketProtocol
          break
        case 'l7Protocol':
          value = connection.l7Protocol
          break
        case 'process':
          value = connection.source.exe
          break
        case 'user':
          value = connection.source.user
          break
      }
      
      if (value) {
        values.add(value)
      }
    })
    
    return Array.from(values).sort()
  }

  return { 
    connections: filtered,
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

