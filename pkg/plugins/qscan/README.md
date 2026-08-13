# Qscan Plugin

The Qscan plugin detects sensitive entities (PII) in HTTP requests and responses using the Qscan service.

## Configuration

### Basic Settings

- `cache_ttl`: How long to keep URLs in the cache (default: 24h)
- `cache_size`: Maximum number of URLs to track (default: 4096)
- `sample_baseline`: Always sample first N requests per URL (default: 100)
- `sample_rate`: Sample rate after baseline (default: 0.1 or 10%)

### Entity Monitoring

- `monitors`: List of entity types to monitor
  - `type`: Entity type (e.g., "CREDIT_CARD", "EMAIL_ADDRESS")
  - `record_value`: Whether to record the actual sensitive value

### Document Recording

- `record_document`: Save detected entities as a single JSON document; if false, nothing is saved (default: false)

## Storage Modes

### No Storage (Default)

When `record_document: false` (default), the plugin:
- Detects entities but does not save anything to the event store
- Only performs detection for monitoring/logging purposes

### Document Mode

When `record_document: true`, the plugin saves:
- A single JSON document containing all detected entities for the request
- Sensitive values are included in the document only if their respective `record_value` flag is true

## Example Configuration

```yaml
cache_ttl: 24h
cache_size: 4096
sample_baseline: 100
sample_rate: 0.1
record_document: true  # Use new document mode
monitors:
  - type: CREDIT_CARD
    record_value: true   # Include actual CC numbers in document
  - type: EMAIL_ADDRESS
    record_value: false  # Only include hashed values
  - type: SSN
    record_value: false
```

## Document Structure

When using `record_document: true`, the saved JSON document has this structure:

```json
{
  "timestamp": "2023-01-01T12:00:00Z",
  "request_id": "req-123",
  "endpoint_id": "endpoint-456",
  "request_method": "POST",
  "request_path": "/api/payment",
  "entities": [
    {
      "entity_type": "CREDIT_CARD",
      "score": 0.95,
      "entity_source": "request_body",
      "field_path": "$.payment.card_number",
      "value_hash": "abcd1234...",
      "value": "4111-1111-1111-1111"
    },
    {
      "entity_type": "EMAIL_ADDRESS",
      "score": 0.87,
      "entity_source": "request_body",
      "field_path": "$.contact.email",
      "value_hash": "efgh5678..."
    }
  ]
}
```

The `value` field is only included on entities where `record_value: true` is configured.
