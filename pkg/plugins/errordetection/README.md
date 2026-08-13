# QPoint Error Detection Plugin

The error detection plugin detects issues and errors and reports based on a configurable ruleset and will upload artifacts to Qpoint's Warehouse and Pulse for tracking and observability.

### Configuration options:

#### DetectErrorConfig

- `batchPeriodMS`: uint32 - The duration in milliseconds for batching error detection.
- `pulseEndpoint`: string - The endpoint for sending pulse signals.
- `pulseToken`: string - The token required for authenticating pulse signals.
- `warehouseEndpoint`: string - The endpoint for storing error data in a warehouse.
- `warehouseToken`: string - The token required for authenticating with the warehouse.
- `rules`: array of Rule - The list of rules to apply for error detection.

#### Rule

- `name`: string - The name of the rule.
- `triggerStatusCodes`: array of string - HTTP status codes that trigger the rule.
- `triggerEmptyBody`: bool - Specifies whether an empty response body triggers the rule.
- `triggerDuration`: time.Duration - The duration after which the rule is triggered.
- `triggerContains`: string - Substring that triggers the rule if found in the response.
- `recordReqHeaders`: bool - Specifies whether to record request headers.
- `recordReqBody`: bool - Specifies whether to record request body.
- `recordResHeaders`: bool - Specifies whether to record response headers.
- `recordResBody`: bool - Specifies whether to record response body.
- `onlyCategories`: array of string - Categories to which the rule applies.
- `onlyUrls`: array of string - URLs to which the rule applies exclusively.
- `excludeUrls`: array of string - URLs to exclude from rule application.
- `withTags`: array of string - Tags associated with the rule.
- `reportAsIssue`: bool - Specifies whether to report the rule violation as an issue.
- `continue`: bool - Specifies whether to continue processing after rule violation.

### Example config:

```json
{
    "batchPeriodMS": 5000,
    "pulseEndpoint": "https://pulse.qpoint.io",
    "pulseToken": "abc123",
    "warehouseEndpoint": "https://warehouse.qpoint.io",
    "warehouseToken": "xyz789",
    "rules": [
        {
        "name": "Rule 1",
        "triggerStatusCodes": ["400", "500"],
        "triggerEmptyBody": true,
        "triggerDuration": "10s",
        "triggerContains": "error",
        "onlyCategories": ["authentication", "database"],
        "onlyUrls": ["https://example.com/api/login", "https://example.com/api/db"],
        "excludeUrls": ["https://example.com/api/debug"],
        "withTags": ["high_priority", "urgent"],
        "reportAsIssue": true,
        "continue": false,
        "recordReqHeaders": true,
        "recordReqBody": true,
        "recordResHeaders": true,
        "recordResBody": true
        },
        {
        "name": "Rule 2",
        "triggerStatusCodes": ["200"],
        "triggerEmptyBody": false,
        "triggerDuration": "5s",
        "triggerContains": "success",
        "onlyCategories": ["authorization"],
        "onlyUrls": ["https://example.com/api/auth"],
        "withTags": ["low_priority"],
        "reportAsIssue": false,
        "continue": true,
        "recordReqHeaders": false,
        "recordReqBody": false,
        "recordResHeaders": false,
        "recordResBody": false
        }
    ]
}

```
