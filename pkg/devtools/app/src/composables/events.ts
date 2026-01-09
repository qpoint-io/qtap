import { ref, watch, onUnmounted, type Ref } from 'vue'
import { useConfig } from '@/composables/config'
import { useHttpStore } from '@/stores/http'
import { useConnectionsStore } from '@/stores/connections'
import { useProcessesStore } from '@/stores/processes'
import type { WorkerOutboundMessage } from './events.types'

/**
 * SSE Events Composable
 * 
 * Manages a Web Worker that handles the SSE connection and event parsing.
 * This offloads heavy parsing/decoding work from the main thread to keep the UI responsive.
 */
export function useEvents() {
  // get the SSE endpoint from the config
  const { sseEndpoint } = useConfig()

  // get the stores
  const httpStore = useHttpStore()
  const connectionsStore = useConnectionsStore()
  const processesStore = useProcessesStore()

  // reactive state for connection status and errors
  const status = ref<'CONNECTING' | 'OPEN' | 'CLOSED'>('CONNECTING')
  const error = ref<string | null>(null)

  // create the Web Worker
  const worker = new Worker(
    new URL('./events.worker.ts', import.meta.url),
    { type: 'module' }
  )

  // handle messages from the worker
  worker.onmessage = (event: MessageEvent<WorkerOutboundMessage>) => {
    const message = event.data

    switch (message.type) {
      case 'status':
        status.value = message.status
        if (message.status === 'CONNECTING') {
          console.log('DevTools: Connecting to SSE endpoint...')
        } else if (message.status === 'OPEN') {
          console.log('DevTools: Connected to SSE endpoint')
        } else if (message.status === 'CLOSED') {
          console.log('DevTools: Disconnected from SSE endpoint')
        }
        break

      case 'error':
        error.value = message.error
        console.error('DevTools: SSE error:', message.error)
        break

      case 'event':
        // Route parsed events to appropriate store actions
        // Note: pause checks already done in worker, so we can directly add
        handleParsedEvent(message.eventType, message.data)
        break
    }
  }

  // handle worker errors
  worker.onerror = (event) => {
    console.error('DevTools: Worker error:', event)
    error.value = `Worker error: ${event.message}`
    status.value = 'CLOSED'
  }

  // initialize the worker with the SSE endpoint
  worker.postMessage({
    type: 'init',
    endpoint: sseEndpoint,
  })

  // watch pause states from all stores and sync to worker
  watch(
    [
      () => httpStore.paused,
      () => connectionsStore.paused,
      () => processesStore.paused,
    ],
    ([http, connections, processes]) => {
      worker.postMessage({
        type: 'pause',
        paused: { http, connections, processes },
      })
    },
    { immediate: true }
  )

  // cleanup on unmount
  onUnmounted(() => {
    worker.postMessage({ type: 'close' })
    worker.terminate()
  })

  // function to manually close the connection
  const close = () => {
    worker.postMessage({ type: 'close' })
    worker.terminate()
  }

  return {
    status: status as Ref<'CONNECTING' | 'OPEN' | 'CLOSED'>,
    error,
    close,
  }
}

/**
 * Handle parsed events from the worker and route to stores
 */
function handleParsedEvent(eventType: string, data: any) {
  const httpStore = useHttpStore()
  const connectionsStore = useConnectionsStore()
  const processesStore = useProcessesStore()

  switch (data.type) {
    case 'http_transaction':
      httpStore.addRequest(data.transaction)
      break

    case 'connection_opened':
      connectionsStore.addConnection(data.connection)
      break

    case 'connection_updated':
      connectionsStore.updateConnection(
        data.connection.meta.connectionId,
        data.connection
      )
      break

    case 'connection_closed':
      connectionsStore.updateConnection(
        data.connection.meta.connectionId,
        {
          ...data.connection,
          duration: data.duration,
        }
      )
      break

    case 'process_started':
      if (processesStore.getProcessByPid(data.process.pid)) {
        processesStore.updateProcess(data.process.pid, data.process)
      } else {
        processesStore.addProcess(data.process)
      }
      break

    case 'process_stopped':
      const process = processesStore.getProcessByPid(data.pid)
      if (process) {
        processesStore.updateProcess(data.pid, {
          ...process,
          status: 'exited',
        })
      }
      break

    default:
      console.log('DevTools: Unhandled parsed event type:', data.type)
  }
}

/**
 * Calculate duration in milliseconds between a timestamp and now
 * @param timestamp ISO 8601 timestamp string
 * @returns Duration in milliseconds
 */
export function calculateDuration(timestamp: string): number {
  const startTime = new Date(timestamp).getTime()
  const now = Date.now()
  return now - startTime
}
