import type { HttpTransaction } from '@/stores/http'
import type { Connection } from '@/stores/connections'
import type { Process } from '@/stores/processes'
import type { DatabaseRequest } from '@/types/database'

/**
 * Message types for Web Worker communication
 * 
 * These types define the contract between the main thread and the worker
 * for SSE event stream processing.
 */

// Messages from main thread -> worker
export type WorkerInboundMessage =
  | { type: 'init'; endpoint: string }
  | { type: 'pause'; paused: { http: boolean; connections: boolean; processes: boolean; redis: boolean; mysql: boolean } }
  | { type: 'close' }

// Messages from worker -> main thread  
export type WorkerOutboundMessage =
  | { type: 'status'; status: 'CONNECTING' | 'OPEN' | 'CLOSED' }
  | { type: 'error'; error: string }
  | { type: 'event'; eventType: string; data: ParsedEventData }

// Parsed event data types
export type ParsedEventData =
  | { type: 'http_transaction'; transaction: HttpTransaction }
  | { type: 'connection_opened'; connection: Connection }
  | { type: 'connection_updated'; connection: Connection }
  | { type: 'connection_closed'; connection: Connection; duration: number }
  | { type: 'process_started'; process: Process }
  | { type: 'process_stopped'; pid: number }
  | { type: 'database_request'; request: DatabaseRequest }

