export interface GrpcRequest {
  connectionId?: string
  requestId?: string
  timestamp: string
  direction: string
  path: string           // URLPath — "/grpc.health.v1.Health/Check"
  status: number         // HTTP-equivalent status (200, etc.)
  duration: number       // ms
  bytesSent: number
  bytesReceived: number
  grpcService: string
  grpcMethod: string
  grpcStatus: string     // "0", "1", ...
  grpcStatusName: string // "OK", "CANCELLED", ...
  grpcMessage?: string
  tags?: string[]
  process?: {
    exe?: string
    pid?: number
    containerName?: string
    containerImage?: string
    podName?: string
    podNamespace?: string
  }
}
