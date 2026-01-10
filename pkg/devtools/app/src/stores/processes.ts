import { defineStore } from 'pinia'
import type { Filter } from './filter'
import { usePersistedBuffer } from '@/composables/persistedBuffer'

// Storage configuration
const bufferManager = usePersistedBuffer<Process>({
  storageKey: 'devtools_processes_buffer',
  maxItems: 500,
  maxBytes: 5 * 1024 * 1024, // 5 MiB
  idKey: 'pid',
})

// Process Types
export interface Process {
  binary: string
  container?: ProcessContainer
  pod?: ProcessPod
  createdAt: string // ISO 8601 timestamp
  hostname: string
  path: string
  pid: number
  user: ProcessUser
  status: 'running' | 'exited'
  duration?: number // Duration in milliseconds (present when status is 'exited')
  connectionCount?: number // Number of connections observed for this process
}

export interface ProcessContainer {
  id: string
  image: string
  name: string
  labels?: Record<string, any>
}

export interface ProcessPod {
  name: string
  namespace: string
  labels?: Record<string, any>
}

export interface ProcessUser {
  id: number
  name: string
}

// Processes Store
export const useProcessesStore = defineStore('processes', {
  state: () => ({
    processesBuffer: [] as Process[],
    paused: false,
    filters: [] as Filter[],
  }),
  getters: {
    getProcessByPid: (state) => (pid: number) => {
      return state.processesBuffer.find((proc) => proc.pid === pid)
    },
    getBufferLimits: () => () => {
      return bufferManager.getLimits()
    },
  },
  actions: {
    restoreFromStorage() {
      const restored = bufferManager.restore()
      if (restored) {
        this.processesBuffer = restored
      }
    },

    addProcess(process: Process) {
      // Initialize connection count to 0 for new processes
      process.connectionCount = 0
      bufferManager.addAndPersist(this.processesBuffer, process)
    },

    updateProcess(pid: number, process: Process) {
      const index = this.processesBuffer.findIndex((proc) =>
        proc.pid === pid
      )
      if (index !== -1) {
        const existingProcess = this.processesBuffer[index]
        // Preserve connection count from existing process
        process.connectionCount = existingProcess?.connectionCount || 0
        this.processesBuffer[index] = process
        bufferManager.updateAndPersist(this.processesBuffer)
      }
    },

    incrementConnectionCount(pid: number) {
      const process = this.processesBuffer.find((p) => p.pid === pid)
      if (process) {
        process.connectionCount = (process.connectionCount || 0) + 1
      }
    },

    removeProcess(pid: number) {
      bufferManager.removeAndPersist(
        this.processesBuffer,
        (proc) => proc.pid === pid
      )
    },

    clearProcesses() {
      bufferManager.clearAll(this.processesBuffer)
    },

    startPeriodicPersistence() {
      bufferManager.startPeriodicPersistence(() => this.processesBuffer)
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

