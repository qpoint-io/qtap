import { defineStore } from 'pinia'
import type { Filter } from './filter'
import { usePersistedBuffer } from '@/composables/persistedBuffer'
import type { GrpcRequest } from '@/types/grpc'

// Storage configuration
const bufferManager = usePersistedBuffer<GrpcRequest>({
  storageKey: 'devtools_grpc_buffer',
  maxItems: 500,
  maxBytes: 5 * 1024 * 1024, // 5 MiB
  idKey: 'requestId',
})

// Re-export for convenience
export type { GrpcRequest }

// gRPC Store
export const useGrpcStore = defineStore('grpc', {
  state: () => ({
    requestsBuffer: [] as GrpcRequest[],
    paused: false,
    filters: [] as Filter[],
  }),
  getters: {
    getRequestById: (state) => (id: string) => {
      return state.requestsBuffer.find((req) => req.requestId === id)
    },
    getBufferLimits: () => () => {
      return bufferManager.getLimits()
    },
  },
  actions: {
    restoreFromStorage() {
      const restored = bufferManager.restore()
      if (restored) {
        this.requestsBuffer = restored
      }
    },

    addRequest(request: GrpcRequest) {
      bufferManager.addAndPersist(this.requestsBuffer, request)
    },

    clearRequests() {
      bufferManager.clearAll(this.requestsBuffer)
    },

    startPeriodicPersistence() {
      bufferManager.startPeriodicPersistence(() => this.requestsBuffer)
    },

    stopPeriodicPersistence() {
      bufferManager.stopPeriodicPersistence()
    },

    setSelectedId(id: string | null) {
      bufferManager.setSelectedItemId(id)
    },

    updateBufferLimits(maxItems: number, maxBytes: number) {
      bufferManager.updateLimits(maxItems, maxBytes)
    },
  },
})
