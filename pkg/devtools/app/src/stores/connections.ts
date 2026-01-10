import { defineStore } from 'pinia'
import type { Filter } from './filter'
import { usePersistedBuffer } from '@/composables/persistedBuffer'

// Storage configuration
const bufferManager = usePersistedBuffer<Connection>({
  storageKey: 'devtools_connections_buffer',
  maxItems: 500,
  maxBytes: 5 * 1024 * 1024, // 5 MiB
  idKey: 'meta.connectionId',
})

// Connection Types
export interface Connection {
  meta: ConnectionMeta
  tags: Array<{ key: string; value: string }>
  createdAt: string // ISO 8601 timestamp
  direction: 'ingress' | 'egress-internal' | 'egress-external'
  status: 'open' | 'closed'
  duration?: number // Duration in milliseconds (present when status is 'closed')
  part: number
  socketProtocol: string // e.g., "tcp", "udp"
  l7Protocol: string // e.g., "http", "https", "other"
  system: SystemInfo
  source: ConnectionEndpoint
  destination: ConnectionEndpoint
}

export interface ConnectionMeta {
  connectionId: string
  endpointId: string
  tlsProbeTypesDetected?: string[] // e.g., ["openssl", "gotls"]
}

export interface SystemInfo {
  hostname: string
  agent: string // e.g., "tap"
  agentInstance: string
}

export interface ConnectionEndpoint {
  address: NetworkAddress
  hostname?: string // Only present on source
  exe?: string // Only present on source (executable path)
  user?: string // Only present on source
  pid?: number // Only present on source (process ID)
}

export interface NetworkAddress {
  family: 'ipv4' | 'ipv6'
  ip: string
  port: number
}

// Connections Store
export const useConnectionsStore = defineStore('connections', {
  state: () => ({
    connectionsBuffer: [] as Connection[],
    paused: false,
    filters: [] as Filter[],
  }),
  getters: {
    getConnectionById: (state) => (id: string) => {
      return state.connectionsBuffer.find((conn) => conn.meta.connectionId === id)
    },
    getBufferLimits: () => () => {
      return bufferManager.getLimits()
    },
  },
  actions: {
    restoreFromStorage() {
      const restored = bufferManager.restore()
      if (restored) {
        this.connectionsBuffer = restored
      }
    },

    addConnection(connection: Connection) {
      bufferManager.addAndPersist(this.connectionsBuffer, connection)
    },

    updateConnection(id: string, connection: Connection) {
      const index = this.connectionsBuffer.findIndex((conn) =>
        conn.meta.connectionId === id
      )
      if (index !== -1) {
        this.connectionsBuffer[index] = connection
        bufferManager.updateAndPersist(this.connectionsBuffer)
      }
    },

    removeConnection(id: string) {
      bufferManager.removeAndPersist(
        this.connectionsBuffer,
        (conn) => conn.meta.connectionId === id
      )
    },

    clearConnections() {
      bufferManager.clearAll(this.connectionsBuffer)
    },

    startPeriodicPersistence() {
      bufferManager.startPeriodicPersistence(() => this.connectionsBuffer)
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

