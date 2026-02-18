import type { HttpTransaction } from '@/stores/http'
import type { Connection } from '@/stores/connections'
import type { Process } from '@/stores/processes'
import type { DatabaseRequest } from '@/stores/redis'
import type { WorkerInboundMessage, WorkerOutboundMessage } from './events.types'

/**
 * SSE Event Stream Web Worker
 * 
 * Handles EventSource connection, parsing, and filtering off the main thread
 * to prevent UI blocking during high-traffic periods.
 */

// Worker state
let eventSource: EventSource | null = null
let pauseState = {
  http: false,
  connections: false,
  processes: false,
  redis: false,
  mysql: false,
}
let reconnectAttempt = 0
let reconnectTimeout: number | null = null

// Event type names we listen for
const eventNames = [
  'system.connected',
  'request.http_transaction',
  'request.database',
  'connection.opened',
  'connection.updated',
  'connection.closed',
  'process.started',
  'process.stopped',
]

/**
 * Handle messages from main thread
 */
self.onmessage = (event: MessageEvent<WorkerInboundMessage>) => {
  const message = event.data

  switch (message.type) {
    case 'init':
      connectToSSE(message.endpoint)
      break

    case 'pause':
      pauseState = message.paused
      break

    case 'close':
      cleanup()
      break
  }
}

/**
 * Connect to SSE endpoint with auto-reconnection
 */
function connectToSSE(endpoint: string) {
  // Clean up existing connection
  if (eventSource) {
    eventSource.close()
  }

  // Clear any pending reconnection
  if (reconnectTimeout !== null) {
    clearTimeout(reconnectTimeout)
    reconnectTimeout = null
  }

  sendStatus('CONNECTING')

  try {
    eventSource = new EventSource(endpoint)

    // Connection opened
    eventSource.onopen = () => {
      reconnectAttempt = 0
      sendStatus('OPEN')
      console.log('DevTools Worker: Connected to SSE endpoint')
    }

    // Connection error
    eventSource.onerror = (error) => {
      console.error('DevTools Worker: SSE connection error', error)
      sendError('SSE connection error')
      
      // EventSource will automatically try to reconnect, but if it closes,
      // we need to handle manual reconnection
      if (eventSource?.readyState === EventSource.CLOSED) {
        sendStatus('CLOSED')
        scheduleReconnect(endpoint)
      }
    }

    // Register event listeners for all event types
    eventNames.forEach((eventName) => {
      eventSource?.addEventListener(eventName, (event: MessageEvent) => {
        handleSSEEvent(eventName, event.data)
      })
    })
  } catch (error) {
    console.error('DevTools Worker: Failed to create EventSource', error)
    sendError(`Failed to create EventSource: ${error}`)
    scheduleReconnect(endpoint)
  }
}

/**
 * Schedule reconnection attempt with exponential backoff (capped at 2s)
 */
function scheduleReconnect(endpoint: string) {
  reconnectAttempt++
  const delay = 2000 // Fixed 2 second delay as per plan
  
  console.log(`DevTools Worker: Scheduling reconnection attempt ${reconnectAttempt} in ${delay}ms`)
  
  reconnectTimeout = setTimeout(() => {
    connectToSSE(endpoint)
  }, delay) as unknown as number
}

/**
 * Handle incoming SSE event
 */
function handleSSEEvent(eventType: string, data: string) {
  try {
    // Parse the SSE data: { ts: string, data: any }
    const parsed = JSON.parse(data)

    // Route based on event type
    switch (eventType) {
      case 'system.connected':
        // Just log, don't send to main thread
        console.log('DevTools Worker: System connected event received')
        break

      case 'request.http_transaction':
        // Check if HTTP events are paused
        if (pauseState.http) return

        const httpTransaction = parseHttpTransaction(parsed)
        if (httpTransaction) {
          sendEvent(eventType, {
            type: 'http_transaction',
            transaction: httpTransaction,
          })
        }
        break

      case 'request.database':
        const databaseRequest = parseDatabaseRequest(parsed)
        if (databaseRequest) {
          const dbType = databaseRequest.databaseType?.toLowerCase()
          if (dbType === 'mysql' && pauseState.mysql) return
          if (dbType === 'redis' && pauseState.redis) return
          sendEvent(eventType, {
            type: 'database_request',
            request: databaseRequest,
          })
        }
        break

      case 'connection.opened':
        // Check if connection events are paused
        if (pauseState.connections) return

        const newConnection = parseConnection(parsed)
        if (newConnection) {
          sendEvent(eventType, {
            type: 'connection_opened',
            connection: { ...newConnection, status: 'open' as const },
          })
        }
        break

      case 'connection.updated':
        // Check if connection events are paused
        if (pauseState.connections) return

        const updatedConnection = parseConnection(parsed)
        if (updatedConnection) {
          sendEvent(eventType, {
            type: 'connection_updated',
            connection: { ...updatedConnection, status: 'open' as const },
          })
        }
        break

      case 'connection.closed':
        // Check if connection events are paused
        if (pauseState.connections) return

        const closedConnection = parseConnection(parsed)
        if (closedConnection) {
          const duration = calculateDuration(closedConnection.createdAt)
          sendEvent(eventType, {
            type: 'connection_closed',
            connection: { ...closedConnection, status: 'closed' as const },
            duration,
          })
        }
        break

      case 'process.started':
        // Check if process events are paused
        if (pauseState.processes) return

        const newProcess = parseProcess(parsed)
        if (newProcess) {
          sendEvent(eventType, {
            type: 'process_started',
            process: { ...newProcess, status: 'running' as const },
          })
        }
        break

      case 'process.stopped':
        // Check if process events are paused
        if (pauseState.processes) return

        const { pid } = parsed.data
        sendEvent(eventType, {
          type: 'process_stopped',
          pid,
        })
        break

      default:
        console.log('DevTools Worker: Unhandled event type:', eventType, parsed)
    }
  } catch (err) {
    console.error('DevTools Worker: Failed to parse SSE data:', err, data)
    sendError(`Failed to parse SSE data: ${err}`)
  }
}

/**
 * Parse HTTP transaction from event data
 */
function parseHttpTransaction(event: any): HttpTransaction | null {
  // Backend sends: { ts, data: { data: { data: base64EncodedHttpTransaction } } }

  if (!event?.data?.data) {
    console.warn('DevTools Worker: Unexpected request.http_transaction structure:', event)
    return null
  }

  try {
    const base64String = event.data.data.data
    const decodedData = atob(base64String)
    const httpTransaction = JSON.parse(decodedData) as HttpTransaction
    return httpTransaction
  } catch (err) {
    console.error('DevTools Worker: Failed to parse HTTP transaction:', err, event)
    return null
  }
}

/**
 * Parse database request from event data
 */
function parseDatabaseRequest(event: any): DatabaseRequest | null {
  // Backend sends: { ts, data: { data: <DatabaseRequest> } }

  if (!event?.data?.data) {
    console.warn('DevTools Worker: Unexpected request.database structure:', event)
    return null
  }

  return event.data.data as DatabaseRequest
}

/**
 * Parse connection from event data
 */
function parseConnection(event: any): Connection | null {
  // Backend sends: { ts, data: { data: <connection> } }

  if (!event?.data?.data) {
    console.warn('DevTools Worker: Unexpected connection event structure:', event)
    return null
  }

  // extract the connection from the event
  const { tags, ...connection } = event.data.data

  // format the tags: convert { key: [values] } to [{ key, value }]
  // taking the first value from each array
  const formattedTags = Object.entries(tags || {}).map(([key, values]) => ({
    key,
    value: (values as string[])[0] || '',
  }))

  // format properly
  return {
    ...connection,
    tags: formattedTags,
  } as Connection
}

/**
 * Parse process from event data
 */
function parseProcess(event: any): Process | null {
  // Backend sends: { ts, data: <process> }

  if (!event?.data) {
    console.warn('DevTools Worker: Unexpected process event structure:', event)
    return null
  }

  return event.data as Process
}

/**
 * Calculate duration in milliseconds between a timestamp and now
 */
function calculateDuration(timestamp: string): number {
  const startTime = new Date(timestamp).getTime()
  const now = Date.now()
  return now - startTime
}

/**
 * Send status update to main thread
 */
function sendStatus(status: 'CONNECTING' | 'OPEN' | 'CLOSED') {
  const message: WorkerOutboundMessage = {
    type: 'status',
    status,
  }
  self.postMessage(message)
}

/**
 * Send error to main thread
 */
function sendError(error: string) {
  const message: WorkerOutboundMessage = {
    type: 'error',
    error,
  }
  self.postMessage(message)
}

/**
 * Send parsed event to main thread
 */
function sendEvent(eventType: string, data: any) {
  const message: WorkerOutboundMessage = {
    type: 'event',
    eventType,
    data,
  }
  self.postMessage(message)
}

/**
 * Cleanup resources
 */
function cleanup() {
  if (reconnectTimeout !== null) {
    clearTimeout(reconnectTimeout)
    reconnectTimeout = null
  }

  if (eventSource) {
    eventSource.close()
    eventSource = null
  }

  sendStatus('CLOSED')
}

