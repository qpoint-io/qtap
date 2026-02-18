import { defineStore } from 'pinia'
import type { Filter } from './filter'
import { usePersistedBuffer } from '@/composables/persistedBuffer'
import type { DatabaseRequest } from '@/stores/redis'

// Storage configuration
const bufferManager = usePersistedBuffer<DatabaseRequest>({
  storageKey: 'devtools_mysql_buffer',
  maxItems: 500,
  maxBytes: 5 * 1024 * 1024, // 5 MiB
  idKey: 'requestId',
})

// Re-export for convenience
export type { DatabaseRequest }

// MySQL Store
export const useMySQLStore = defineStore('mysql', {
  state: () => ({
    requestsBuffer: [] as DatabaseRequest[],
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

    addRequest(request: DatabaseRequest) {
      bufferManager.addAndPersist(this.requestsBuffer, request)
    },

    updateRequest(id: string, request: DatabaseRequest) {
      const index = this.requestsBuffer.findIndex((req) =>
        req.requestId === id
      )
      if (index !== -1) {
        this.requestsBuffer[index] = request
        bufferManager.updateAndPersist(this.requestsBuffer)
      }
    },

    removeRequest(id: string) {
      bufferManager.removeAndPersist(
        this.requestsBuffer,
        (req) => req.requestId === id
      )
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
