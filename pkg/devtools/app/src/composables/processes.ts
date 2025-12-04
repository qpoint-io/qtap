import { computed, watchEffect } from 'vue'
import { storeToRefs } from 'pinia'
import { useProcessesStore } from '@/stores/processes'
import { useUrlParams } from '@/composables/urlParams'
import { useBufferSettings } from '@/composables/bufferSettings'
import type { Filter } from '@/stores/filter'

export function useProcesses() {
  // get the store
  const store = useProcessesStore()
  
  // get reactive pause state and filters from store
  const { paused, filters } = storeToRefs(store)

  // watch URL params to sync selected item with buffer GC protection
  const params = useUrlParams()
  watchEffect(() => {
    const processId = params.process_id as string | undefined
    store.setSelectedId(processId || null)
  })

  // Buffer settings with localStorage persistence
  const {
    maxItems,
    maxSize,
    maxSizeUnit,
    resetBufferSettings,
  } = useBufferSettings(
    'devtools_processes_buffer_settings',
    (maxItems, maxBytes) => store.updateBufferLimits(maxItems, maxBytes)
  )

  const filtered = computed(() => {
    let result = store.processesBuffer
    
    // Apply each filter
    filters.value.forEach(filter => {
      result = result.filter(process => {
        // Get the value to filter against based on the key
        let fieldValue: string | undefined
        
        switch (filter.key) {
          case 'binary':
            fieldValue = process.binary
            break
          case 'path':
            fieldValue = process.path
            break
          case 'user':
            fieldValue = process.user.name
            break
          case 'status':
            fieldValue = process.status
            break
          case 'container':
            fieldValue = process.container?.name
            break
          case 'containerImage':
            fieldValue = process.container?.image
            break
          case 'pod':
            fieldValue = process.pod?.name
            break
          case 'podNamespace':
            fieldValue = process.pod?.namespace
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
    'binary',
    'path',
    'user',
    'status',
    'container',
    'containerImage',
    'pod',
    'podNamespace',
  ]

  const getFilterValues = (key: string): string[] => {
    const values = new Set<string>()
    
    store.processesBuffer.forEach(process => {
      let value: string | undefined
      
      switch (key) {
        case 'binary':
          value = process.binary
          break
        case 'path':
          value = process.path
          break
        case 'user':
          value = process.user.name
          break
        case 'status':
          value = process.status
          break
        case 'container':
          value = process.container?.name
          break
        case 'containerImage':
          value = process.container?.image
          break
        case 'pod':
          value = process.pod?.name
          break
        case 'podNamespace':
          value = process.pod?.namespace
          break
      }
      
      if (value) {
        values.add(value)
      }
    })
    
    return Array.from(values).sort()
  }

  return { 
    processes: filtered,
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

