# OpenTelemetry EventStore

The OpenTelemetry EventStore service exports events to OTLP (OpenTelemetry Protocol) endpoints using structured logging.

## Features

- **Multiple Protocol Support**: Supports gRPC, HTTP, and stdout protocols
- **Flexible Endpoint Configuration**: Accepts both host:port and full URL formats
- **Comprehensive Event Types**: Handles all qtap event types (Request, Issue, PIIEntity, ArtifactRecord, Connection)
- **Authentication Support**: Configurable headers for API keys and authentication
- **TLS Configuration**: Supports secure and insecure connections

## Configuration

### Basic Configuration

```yaml
eventstore:
  - type: otel
    endpoint: "localhost:4317"    # gRPC endpoint
    protocol: grpc                 # "grpc", "http", or "stdout"
    service_name: "qtap"
    environment: "production"
```

### Environment Variable Support

Environment variables can be referenced in the endpoint configuration using mustache-style syntax:

```yaml
eventstore:
  - type: otel
    endpoint: "{{ OTEL_ENDPOINT }}"
    protocol: grpc
    service_name: "qtap"
    environment: "production"
```

```yaml
eventstore:
  - type: otel
    endpoint: "https://example.com/org/{{ OTEL_ORG_ID }}/dataset/{{ OTEL_DATASET_ID }}"
    protocol: grpc
    service_name: "qtap"
    environment: "production"
```

### HTTP Protocol Configuration

```yaml
eventstore:
  - type: otel
    endpoint: "localhost:4318"    # HTTP endpoint
    protocol: http
    service_name: "qtap"
    environment: "production"
    headers:
      authorization: "Bearer ${OTEL_API_TOKEN}"
    tls:
      enabled: false
```

### Full URL Configuration

```yaml
eventstore:
  - type: otel
    endpoint: "https://otel.example.com:4318/v1/logs"
    protocol: http
    service_name: "qtap"
    environment: "production"
    headers:
      x-api-key: "${OTEL_API_KEY}"
```

### Stdout Protocol Configuration

```yaml
eventstore:
  - type: otel
    protocol: stdout
    service_name: "qtap"
    environment: "development"
    # Note: endpoint, headers, and TLS are ignored for stdout
```

## Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `endpoint` | string | `localhost:4317` (gRPC), `localhost:4318` (HTTP), N/A (stdout) | OTLP endpoint |
| `protocol` | string | `grpc` | Protocol: `grpc`, `http`, or `stdout` |
| `service_name` | string | `qtap` | Service name in telemetry data |
| `environment` | string | `production` | Environment identifier |
| `headers` | map | `{}` | Custom headers for authentication |
| `tls.enabled` | bool | `true` | Enable TLS |
| `tls.insecure_skip_verify` | bool | `false` | Skip TLS certificate verification |

## Protocol Differences

### gRPC Protocol
- Default port: 4317
- Binary protocol, typically more efficient
- Requires explicit TLS configuration
- Better for high-throughput scenarios

### HTTP Protocol
- Default port: 4318
- JSON over HTTP, easier to debug
- TLS auto-detected from URL scheme (https://)
- Better firewall/proxy compatibility
- Supports custom URL paths

### Stdout Protocol
- No endpoint required
- JSON output to stdout, ideal for debugging
- Pretty-printed by default for readability
- Ignores endpoint, headers, and TLS configuration
- Intended for testing and development only

## Event Mapping

All qtap event types are converted to OTLP log records with:

- **Timestamp**: Extracted from event timestamp field
- **Severity**: 
  - `Info`: Request, ArtifactRecord, Connection events
  - `Warn`: PIIEntity events
  - `Error`: Issue events
- **Body**: Human-readable message describing the event
- **Attributes**: All event fields converted via JSON marshaling
- **Event Type**: Added as `event.type` attribute

## Examples

For complete configuration examples, see:

- `examples/otel-grpc-eventstore.yaml` 
- `examples/otel-stdout-eventstore.yaml` 
- `examples/otel-http-eventstore.yaml`
