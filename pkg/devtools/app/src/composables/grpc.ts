import { computed, watchEffect } from 'vue'
import { storeToRefs } from 'pinia'
import { useGrpcStore } from '@/stores/grpc'
import type { GrpcRequest } from '@/types/grpc'
import { useUrlParams } from '@/composables/urlParams'
import { useBufferSettings } from '@/composables/bufferSettings'
import { usePersistedFilters } from '@/composables/persistedFilters'

export type { SizeUnit } from '@/composables/bufferSettings'

const FILTER_STORAGE_KEY = 'devtools_grpc_filters'

export function useGrpc() {
  const store = useGrpcStore()
  const { paused, filters } = storeToRefs(store)

  usePersistedFilters(FILTER_STORAGE_KEY, filters)

  const params = useUrlParams()
  watchEffect(() => {
    const requestId = params.grpc_id as string | undefined
    store.setSelectedId(requestId || null)
  })

  const {
    maxItems,
    maxSize,
    maxSizeUnit,
    resetBufferSettings,
  } = useBufferSettings(
    'devtools_grpc_buffer_settings',
    (maxItems, maxBytes) => store.updateBufferLimits(maxItems, maxBytes)
  )

  const filtered = computed(() => {
    let result = store.requestsBuffer

    filters.value.forEach(filter => {
      result = result.filter((request: GrpcRequest) => {
        let fieldValue: string | undefined

        switch (filter.key) {
          case 'service':
            fieldValue = request.grpcService
            break
          case 'method':
            fieldValue = request.grpcMethod
            break
          case 'statusName':
            fieldValue = request.grpcStatusName
            break
          case 'direction':
            fieldValue = request.direction
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
    'service',
    'method',
    'statusName',
    'direction',
    'process',
  ]

  const getFilterValues = (key: string): string[] => {
    const values = new Set<string>()

    store.requestsBuffer.forEach((request: GrpcRequest) => {
      let value: string | undefined

      switch (key) {
        case 'service':
          value = request.grpcService
          break
        case 'method':
          value = request.grpcMethod
          break
        case 'statusName':
          value = request.grpcStatusName
          break
        case 'direction':
          value = request.direction
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
