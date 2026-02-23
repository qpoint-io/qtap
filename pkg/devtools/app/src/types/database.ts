export interface DatabaseRequest {
  connectionId?: string
  requestId: string
  timestamp: string
  direction: string
  databaseType: string  // e.g. "redis", "mysql"
  statement: string     // The query/command (e.g., "GET mykey", "SELECT 1")
  resultType?: string
  isError: boolean
  errorMsg?: string
  affectedCount?: number
  resultCount?: number
  duration: number      // milliseconds
  bytesSent: number
  bytesReceived: number
  tags?: string[]
  responseSummary?: string
  // Process metadata (from tags/meta)
  process?: {
    exe?: string
    pid?: number
    containerName?: string
    containerImage?: string
    podName?: string
    podNamespace?: string
  }
}
