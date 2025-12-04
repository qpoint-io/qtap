import { defineStore } from 'pinia'
import type { Filter } from './filter'
import { usePersistedBuffer } from '@/composables/persistedBuffer'

// Storage configuration
const bufferManager = usePersistedBuffer<HttpTransaction>({
  storageKey: 'devtools_http_buffer',
  maxItems: 500,
  maxBytes: 5 * 1024 * 1024, // 5 MiB
  idKey: 'request.request_id',
})

// HTTP Transaction Types
export interface HttpTransaction {
  metadata: Metadata
  request: Request
  response: Response
  transaction_time: string
  duration_ms?: number
  direction?: string
}

export interface Metadata {
  process_id?: string
  process_exe?: string
  container_name?: string
  container_image?: string
  pod_name?: string
  pod_namespace?: string
  bytes_sent?: number
  bytes_received?: number
  request_id?: string
  connection_id?: string
  endpoint_id?: string
}

export interface Request {
  method: string
  url: string
  scheme?: string
  path?: string
  authority?: string
  protocol?: string
  request_id?: string
  user_agent?: string
  content_type?: string
  headers?: Record<string, string>
  body?: string
}

export interface Response {
  status: number
  content_type?: string
  headers?: Record<string, string>
  body?: string
}

// HTTP Store
export const useHttpStore = defineStore('http', {
  state: () => ({
    requestsBuffer: [] as HttpTransaction[],
    paused: false,
    filters: [] as Filter[],
  }),
  getters: {
    getRequestById: (state) => (id: string) => {
      return state.requestsBuffer.find((tx) => tx.metadata.request_id === id)
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

    addRequest(transaction: HttpTransaction) {
      bufferManager.addAndPersist(this.requestsBuffer, transaction)
    },

    updateRequest(id: string, transaction: HttpTransaction) {
      const index = this.requestsBuffer.findIndex((tx) =>
        tx.metadata.request_id === id
      )
      if (index !== -1) {
        this.requestsBuffer[index] = transaction
        bufferManager.updateAndPersist(this.requestsBuffer)
      }
    },

    removeRequest(id: string) {
      bufferManager.removeAndPersist(
        this.requestsBuffer,
        (tx) => tx.metadata.request_id === id
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

