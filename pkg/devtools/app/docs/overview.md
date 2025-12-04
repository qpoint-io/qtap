# DevTools UI - Project Overview

## Project Goals

This is an embedded DevTools UI for a Linux agent that monitors network traffic, processes, and containers. The primary goal is to create a **Chrome DevTools-inspired interface** that provides a familiar, professional experience for developers debugging and monitoring HTTP transactions, connections, and processes.

**Critical Design Principle:** This UI must emulate the look, feel, and interaction patterns of Chrome DevTools. Familiarity is key to adoption.

## Technology Stack

- **Framework:** Vue 3 with TypeScript (Composition API)
- **State Management:** Pinia (data) + VueUse URL parameters (UI state)
- **Utilities:** @vueuse/core v13.9.0 (URL state management, theme management)
- **Styling:** Tailwind CSS with custom DevTools color scheme (light + dark mode)
- **Build Tool:** Vite
- **Component Structure:** Template-first, script-last (user preference)
- **Embedding:** Built as a single-page application (SPA) embedded in the Go binary
- **Theme System:** Class-based dark mode with persistent localStorage storage

## Current Implementation Status

### ✅ Phase 1 - Completed
- App structure with three-tab navigation (Connections, Requests, Processes)
- **Requests Panel** fully implemented with:
  - Full-width table view when no request selected
  - Split view (1/3 table, 2/3 details) when request selected
  - Direction indicators with color coding
  - Request/response detail tabs (Headers, Preview, Response)
  - Top-anchored scroll with frozen mode (new items at top, "New Items" pill when scrolled)
- Chrome DevTools light theme color scheme
- Modular architecture with separated concerns

### ✅ Phase 2 - Completed
- **Connections Panel** fully implemented with:
  - Full-width table view showing Timestamp, Direction, Source, Destination, Status, Socket, L7, Process, Duration
  - Split view (1/3 table, 2/3 details) when connection selected
  - Narrow table shows only Timestamp, Direction, Source, Destination when details open
  - Details panel with sections: Connection Info, Source, Destination, System, TLS Detection, Tags
  - Color-coded status badges (green for open, gray for closed)
  - Top-anchored scroll with frozen mode (see Requests.vue for behavior details)
  - URL-based selection via `connection_id` parameter
- **Processes Panel** fully implemented with:
  - Full-width table view showing Timestamp, Status, PID, Binary, Path, User, Container, Pod, Duration
  - Split view (1/4 table, 3/4 details) when process selected
  - Narrow table shows only Timestamp, Status, PID, Binary, Path when details open
  - Details panel with sections: Process Info, User, Container, Pod (with formatted JSON labels)
  - Color-coded status badges (green for running, gray for exited)
  - Top-anchored scroll with frozen mode (see Requests.vue for behavior details)
  - URL-based selection via `process_id` parameter
- **StatusBadge Component** created as reusable UX component
- **App.vue** updated to clear selection parameters on tab switch

### ✅ Phase 3 - Completed
- ✅ Persisted buffer pattern with sessionStorage and size-based limits for all data types
- ✅ Separated types and mocks into dedicated directories
- ✅ URL-based state management for tab navigation and request selection
- ✅ Real SSE endpoint integration for live backend data
- ✅ Connection lifecycle handling (connect, disconnect, auto-reconnect)
- ✅ Full type definitions for HTTP transactions, connections, and processes
- ✅ Event handlers for all lifecycle events (connection opened/updated/closed, process started/stopped)
- ✅ Store actions and getters for all data types
- ✅ Automatic restoration of buffer state on page reload

### ✅ Phase 4 - Completed
- ✅ **Filtering and Toolbar** implemented for all panels (Requests, Connections, Processes):
  - Composable-based filtering pattern (`useRequests()`, `useConnections()`, `useProcesses()`)
  - Dynamic filter value extraction from live data
  - Full operator support (is, is not, contains, starts with, ends with, etc.)
  - Multi-filter support with AND logic
  - Reusable Toolbar component with filter management UI
  - Panel-specific filterable keys for each data type
- ✅ **Pause functionality** for event streaming:
  - Global pause state in Pinia store
  - Events composable respects pause state
  - UI updates frozen while paused, resumes on unpause
  - Integrated with Toolbar component across all panels

### ✅ Phase 5 - Completed
- ✅ **Dark/Light Theme Support** implemented across entire application:
  - Class-based dark mode using Tailwind's `darkMode: 'class'`
  - Complete DevTools color palette with light and dark variants
  - `useTheme()` composable for theme state management (`composables/theme.ts`)
  - `ThemeToggle` component in app header for manual theme switching
  - Persistent theme preference stored in localStorage
  - All components updated with `dark:` variants for backgrounds, text, borders, and hover states
  - HTTP method colors support both light and dark modes
  - Custom status colors optimized for visibility in both themes
  - Falls back to system preference if no stored theme

## Architecture Decisions

### Independent Store Pattern
**Decision:** Separate stores for each data type (HTTP, Connections, Processes) with independent pause states and persisted buffer management.

**Rationale:**
- Each store manages its own data lifecycle independently
- Individual pause control per data type
- Size-based buffer limits (bytes) for predictable memory usage
- SessionStorage persistence for data retention across page reloads
- Clear separation of concerns

**Persisted Buffer Pattern:**
Each store uses the `usePersistedBuffer` composable for automatic FIFO management with persistence:
```typescript
// At the top of the store file
import { usePersistedBuffer } from '@/composables/persistedBuffer'

const bufferManager = usePersistedBuffer<HttpTransaction>({
  storageKey: 'devtools_http_buffer',
  maxBytes: 5 * 1024 * 1024, // 5 MiB
})

// In store actions
addRequest(transaction: HttpTransaction) {
  bufferManager.addAndPersist(this.requestsBuffer, transaction)
}

restoreFromStorage() {
  const restored = bufferManager.restore()
  if (restored) {
    this.requestsBuffer = restored
  }
}
```

### Type Co-location with Stores
**Decision:** Define types directly in store files alongside their associated Pinia stores.

**Benefits:**
- Types co-located with related state management code
- Single import for both store and types
- Clear ownership of data structures
- Simpler import paths and fewer files to navigate

### URL-Based State Management
**Decision:** Use URL query parameters for UI state (tab selection, request selection) via VueUse's `useUrlSearchParams`.

**Rationale:**
- UI state is shareable and bookmark-able
- Browser back/forward navigation works automatically
- Deep linking to specific views (e.g., specific request details)
- Clear separation: Store = data, URL = UI state
- Simpler store focused only on data management

**Implementation:**
```typescript
// Using VueUse's useUrlSearchParams
const params = useUrlSearchParams('history')

// Read from URL
const activeTab = computed(() => {
  const tab = params.tab as string
  return validTabs.includes(tab) ? tab : 'requests'  // Default: 'requests'
})

// Write to URL
params.tab = 'connections'
params.request_id = 'abc123'

// Remove from URL
delete params.request_id
```

**When to Use Store vs URL:**

| State Type | Storage | Example | Rationale |
|------------|---------|---------|-----------|
| **UI Navigation** | URL | Active tab, selected request | Should be shareable/bookmarkable |
| **Data Buffers** | Store | HTTP transactions, connections | Backend data, not UI state |
| **Transient UI** | Component ref | Detail tab (headers/preview/response) | Internal component state only |
| **User Preferences** | localStorage (future) | Theme, column visibility | Persists across sessions |

### Composables Architecture
**Decision:** Extract configuration and SSE logic into dedicated composables.

**Rationale:**
- Separation of concerns: configuration, event handling, and UI are decoupled
- Reusability: composables can be used across multiple components
- Testability: easier to test SSE logic in isolation
- Maintainability: clear boundaries for different concerns

**Composables:**

**`useConfig()`** - Runtime configuration management
- Returns SSE endpoint URL
- Dev mode: reads from `VITE_SSE_ENDPOINT` environment variable
- Production: uses current host with static path

**`useEvents()`** - SSE connection and event routing
- Connects to backend SSE endpoint via `@vueuse/core`'s `useEventSource`
- Automatic reconnection with infinite retries (2s delay)
- Parses incoming events and routes to appropriate stores
- Handles base64-encoded HTTP transaction data
- Respects individual pause states from each store (HTTP, Connections, Processes)
- Returns connection status, error, and close function

**`useRequests()`** - Request filtering and toolbar state
- Wraps the store's requestsBuffer with filtering logic
- Manages filter state (array of Filter objects)
- Exposes pause state from store as reactive ref
- Provides filterable keys and dynamic value extraction
- Returns filtered requests, pause state, filters, and helper functions

**Formatting utilities** (`composables/formatters.ts`) - Reusable formatters
- `formatTimestamp()` - Formats ISO timestamps to human-readable format ("Nov 17, 8:40 AM")
- `formatBytes()` - Formats bytes to human-readable sizes ("1.2 KB", "345 B")
- `formatDuration()` - Formats milliseconds to durations ("45 ms", "2.34 s")
- Used across all three panels (Requests, Connections, Processes) for consistent formatting

### Filtering and Toolbar Pattern

**Pattern Overview:**
The filtering pattern uses composables to provide a clean separation between data (store), filtering logic (composable), and UI (component). This pattern can be replicated for Connections and Processes panels.

**Architecture:**
```
Store (paused state + data buffer)
    ↓
Composable (filtering logic + state management)
    ↓
Component (UI rendering + Toolbar integration)
```

**Implementation Steps:**

**1. Store Setup (Independent Pause State)**
```typescript
// stores/http.ts (example - same pattern for connections.ts, processes.ts)
state: () => ({
  requestsBuffer: [] as HttpTransaction[],
  maxBufferSize: 100,
  paused: false,  // Independent pause state per store
})
```

**2. Events Composable (Respect Individual Pause States)**
```typescript
// composables/events.ts
// Routes events to appropriate store based on type
switch (eventType) {
  case 'request.http_transaction':
    if (httpStore.paused) return  // Check HTTP store pause state
    // ... process and add to httpStore
    
  case 'connection.opened':
    if (connectionsStore.paused) return  // Check connections store pause state
    // ... process and add to connectionsStore
    
  case 'process.started':
    if (processesStore.paused) return  // Check processes store pause state
    // ... process and add to processesStore
}
```

**3. Data Composable Pattern**
```typescript
// composables/requests.ts (example)
import { computed, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { useHttpStore } from '@/stores/http'
import type { Filter } from '@/stores/filter'

export function useRequests() {
  const store = useHttpStore()
  
  // Get reactive pause state from store
  const { paused } = storeToRefs(store)
  
  // Local filter state
  const filters = ref<Filter[]>([])
  
  // Filtered data computed property
  const filtered = computed(() => {
    let result = store.requestsBuffer
    
    // Apply each filter (AND logic)
    filters.value.forEach(filter => {
      result = result.filter(item => {
        // Extract field value based on filter.key
        let fieldValue = extractFieldValue(item, filter.key)
        if (!fieldValue) return false
        
        // Apply operator (is, contains, starts with, etc.)
        return applyOperator(fieldValue, filter.operator, filter.value)
      })
    })
    
    return result
  })
  
  // Define filterable keys
  const filterableKeys = ['method', 'status', 'endpoint', ...]
  
  // Extract unique values for filter suggestions
  const getFilterValues = (key: string): string[] => {
    const values = new Set<string>()
    store.requestsBuffer.forEach(item => {
      const value = extractFieldValue(item, key)
      if (value) values.add(value)
    })
    return Array.from(values).sort()
  }
  
  return {
    requests: filtered,
    isPaused: paused,
    filters,
    filterableKeys,
    getFilterValues
  }
}
```

**4. Component Integration**
```vue
<!-- components/pages/Requests.vue -->
<template>
  <div>
    <!-- Toolbar with filter and pause controls -->
    <Toolbar
      :filters="filterableKeys"
      :values-cb="getFilterValues"
      v-model:filter="filters"
      v-model:pause="isPaused"
    />
    
    <!-- Data table iterating over filtered results -->
    <div v-for="item in requests" :key="item.id">
      <!-- ... -->
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRequests } from '@/composables/requests'

// Destructure everything from composable
const { requests, isPaused, filters, filterableKeys, getFilterValues } = useRequests()
</script>
```

**Filter Type Definition:**
```typescript
// stores/filter.ts (types only, no store)
export type Operator = 
  | 'is' 
  | 'is not' 
  | 'contains' 
  | 'does not contain' 
  | 'starts with' 
  | 'does not start with' 
  | 'ends with' 
  | 'does not end with'

export interface Filter {
  key: string      // e.g., 'method', 'status', 'endpoint'
  operator: Operator
  value: string    // Filter value to match against
}
```

**Key Features:**
- **Reactive filtering:** Computed property automatically updates when filters or data changes
- **Case-insensitive matching:** All comparisons use `.toLowerCase()`
- **Multi-filter support:** Multiple filters applied with AND logic
- **Dynamic suggestions:** `getFilterValues()` extracts real values from current buffer
- **Independent pause state:** Each store has its own pause state, accessed via `storeToRefs()`
- **Field extraction:** Switch statement maps filter keys to data structure paths
- **Operator support:** All 8 operators fully implemented

**Implementation Status:**
- ✅ **Requests Panel:** Fully implemented with 7 filterable keys (method, status, endpoint, path, direction, process, type)
- ✅ **Connections Panel:** Fully implemented with 8 filterable keys (direction, source, destination, status, socketProtocol, l7Protocol, process, user)
- ✅ **Processes Panel:** Fully implemented with 8 filterable keys (binary, path, user, status, container, containerImage, pod, podNamespace)

## Architecture & File Structure

```
internal/devtools/app/src/
├── App.vue                          # Main app with tab navigation
├── main.ts                          # Vue app entry point
├── assets/
│   └── main.css                     # Tailwind imports
├── components/
│   ├── icons/
│   │   ├── IngressIcon.vue          # Cloud with down arrow
│   │   ├── EgressIcon.vue           # Cloud with up arrow
│   │   ├── FilterIcon.vue           # Filter icon for toolbar
│   │   ├── PlayIcon.vue             # Play icon for unpause
│   │   ├── PauseIcon.vue            # Pause icon for pause
│   │   ├── DownTrayIcon.vue         # Download icon for export
│   │   ├── CheckIcon.vue            # Check/tick icon
│   │   ├── ClearIcon.vue            # Clear/trash icon
│   │   └── SleeperIcon.vue          # Sleeper icon
│   ├── pages/
│   │   ├── Connections.vue          # ✅ Fully implemented with auto-scroll, filtering, and toolbar
│   │   ├── Processes.vue            # ✅ Fully implemented with auto-scroll, filtering, and toolbar
│   │   ├── Requests.vue             # ✅ Fully implemented with auto-scroll, filtering, and toolbar
│   │   └── Welcome.vue              # Welcome/landing page
│   └── ux/
│       ├── DirectionIndicator.vue   # Reusable direction indicator
│       ├── StatusBadge.vue          # ✅ Color-coded status badges
│       ├── StatusPill.vue           # ✅ Status pill for connection state
│       ├── Button.vue               # ✅ Reusable button component
│       ├── ThemeToggle.vue          # ✅ Light/dark theme toggle (sun/moon icon)
│       └── Toolbar.vue              # ✅ Filter and pause controls
├── composables/
│   ├── config.ts                    # ✅ Runtime configuration (SSE endpoint)
│   ├── events.ts                    # ✅ SSE connection and event routing
│   ├── formatters.ts                # ✅ Formatting utilities (timestamp, bytes, duration)
│   ├── requests.ts                  # ✅ Request filtering and toolbar state
│   ├── connections.ts               # ✅ Connection filtering and toolbar state
│   ├── processes.ts                 # ✅ Process filtering and toolbar state
│   ├── theme.ts                     # ✅ Dark/light theme management (useTheme composable)
│   ├── urlParams.ts                 # ✅ URL parameter management
│   ├── storage.ts                   # ✅ SessionStorage helpers
│   └── persistedBuffer.ts           # ✅ Persisted buffer utilities
├── mocks/
│   └── requests.ts                  # ✅ Mock HTTP transactions for testing
└── stores/
    ├── http.ts                      # ✅ HTTP types + store (requests buffer, pause, actions)
    ├── connections.ts               # ✅ Connection types + store (connections buffer, pause, actions)
    ├── processes.ts                 # ✅ Process types + store (processes buffer, pause, actions)
    └── filter.ts                    # ✅ Filter types (no store, types only)
```

## Data Models

All type definitions are located in their respective store files (`stores/http.ts`, `stores/connections.ts`, `stores/processes.ts`, `stores/filter.ts`).

### HTTP Transactions

Defined in `stores/http.ts`. The backend provides `HttpTransaction` objects with this schema:

```typescript
interface HttpTransaction {
  metadata: Metadata           // Process and connection info
  request: Request            // HTTP request details
  response: Response          // HTTP response details
  transaction_time: string    // ISO timestamp
  duration_ms?: number        // Request duration
  direction?: string          // 'ingress' | 'egress-internal' | 'egress-external'
}

interface Metadata {
  process_id?: string
  process_exe?: string         // e.g., "/usr/bin/nginx"
  container_name?: string
  container_image?: string
  pod_name?: string            // Kubernetes pod
  pod_namespace?: string
  bytes_sent?: number
  bytes_received?: number
  request_id?: string          // Unique identifier
  connection_id?: string
  endpoint_id?: string
}

interface Request {
  method: string               // GET, POST, PUT, DELETE, etc.
  url: string                  // Full URL
  scheme?: string              // http/https
  path?: string                // URL path
  authority?: string           // Domain
  protocol?: string            // HTTP/1.1, HTTP/2
  request_id?: string
  user_agent?: string
  content_type?: string
  headers?: Record<string, string>
  body?: string                // JSON stringified
}

interface Response {
  status: number               // HTTP status code
  content_type?: string
  headers?: Record<string, string>
  body?: string
}
```

### Connections

Defined in `stores/connections.ts`. The backend provides `Connection` objects representing TCP/UDP network connections:

```typescript
interface Connection {
  meta: ConnectionMeta            // Connection identifiers
  tags: Array<{ key: string; value: string }>  // Metadata tags
  createdAt: string               // ISO 8601 timestamp
  direction: 'ingress' | 'egress-internal' | 'egress-external'
  status: 'open' | 'closed'
  duration?: number               // Connection duration in ms (when closed)
  part: number                    // Connection part/segment number
  socketProtocol: string          // "tcp", "udp"
  l7Protocol: string              // "http", "https", "other"
  system: SystemInfo              // System/agent information
  source: ConnectionEndpoint      // Source endpoint details
  destination: ConnectionEndpoint // Destination endpoint details
}

interface ConnectionMeta {
  connectionId: string
  endpointId: string
  tlsProbeTypesDetected?: string[]  // e.g., ["openssl", "gotls"]
}

interface SystemInfo {
  hostname: string
  agent: string                   // e.g., "tap"
  agentInstance: string
}

interface ConnectionEndpoint {
  address: NetworkAddress
  hostname?: string               // Only on source
  exe?: string                    // Only on source (executable path)
  user?: string                   // Only on source
}

interface NetworkAddress {
  family: 'ipv4' | 'ipv6'
  ip: string
  port: number
}
```

### Processes

Defined in `stores/processes.ts`. The backend provides `Process` objects representing monitored Linux processes:

```typescript
interface Process {
  binary: string                  // Binary name
  container?: ProcessContainer    // Container details (if containerized)
  pod?: ProcessPod                // Kubernetes pod details (if in K8s)
  createdAt: string               // ISO 8601 timestamp
  hostname: string
  path: string                    // Full path to executable
  pid: number                     // Process ID
  user: ProcessUser               // User running the process
  status: 'running' | 'exited'
  duration?: number               // Process lifetime in ms (when exited)
}

interface ProcessContainer {
  id: string
  image: string
  name: string
  labels?: Record<string, any>
}

interface ProcessPod {
  name: string
  namespace: string
  labels?: Record<string, any>
}

interface ProcessUser {
  id: number
  name: string
}
```

## Key Design Patterns

### 1. Direction Indicators

**Values from backend:**
- `ingress` - incoming traffic (blue, icon on left)
- `egress-external` - outgoing external traffic (purple, icon on right)
- `egress-internal` - outgoing internal traffic (purple, icon on right)

**Display normalization:**
- `ingress` → Blue with `IngressIcon` on left: `[↓] ingress`
- `egress-*` → Purple with `EgressIcon` on right: `egress [↑]`

**Component:** `DirectionIndicator.vue` with optional `show-label` prop

### 2. Split Panel Behavior (Requests)

**No selection (default):**
- Table uses full width
- Shows all columns: Direction, Endpoint, Path, Status, Method, Type, Process, Size, Time

**Request selected:**
- Table shrinks to 1/4 width, shows only: Direction, Endpoint, Path
- Detail panel takes 3/4 width on right
- Close button (X) in detail header to deselect

### 2a. Top-Anchored Scroll Behavior (All Panels)

**Smart frozen mode for streaming data:**
- **Default (Live Mode):** New items appear at top, view stays at top showing latest
- **User scrolls down:** Enters frozen mode, scroll position preserved as new items arrive
- **New items while frozen:** "New [Items]" pill button appears at top
- **Click pill or scroll to top:** Exits frozen mode, returns to live view at top
- **Item selected:** Enters frozen mode (keeps selected item visible)

**Implementation (see Requests.vue for reference):**
- Watches first item ID for new items (`requests.value[0]?.request.request_id`)
- Tracks scroll position via `@scroll` event handler  
- Uses `frozenMode`, `newUnseenItems`, and `isScrollable` state refs
- Preserves scroll position by adjusting `scrollTop` when DOM updates in frozen mode

### 3. Chrome DevTools Color Scheme & Dark Mode

**⚠️ CRITICAL:** All components MUST support both light and dark themes using Tailwind's `dark:` prefix.

The application uses a class-based dark mode system that toggles a `dark` class on the `<html>` element. The color palette includes both light (default) and dark variants for all colors.

**Tailwind Configuration** (`tailwind.config.js`):

```javascript
darkMode: 'class', // REQUIRED: Enables class-based dark mode

colors: {
  devtools: {
    bg: {
      // Light mode (default)
      primary: '#ffffff',
      secondary: '#f3f3f3',
      hover: '#f9f9f9',
      selected: '#e8f0fe',
      // Dark mode variants
      'dark-primary': '#202124',
      'dark-secondary': '#292a2d',
      'dark-hover': '#35363a',
      'dark-selected': '#2a3f5f',
    },
    border: {
      light: '#e0e0e0',
      dark: '#d4d4d4',
      'dark-light': '#3c4043',
      'dark-dark': '#494c50',
    },
    text: {
      primary: '#202124',
      secondary: '#5f6368',
      tertiary: '#80868b',
      'dark-primary': '#e8eaed',
      'dark-secondary': '#9aa0a6',
      'dark-tertiary': '#71757a',
    },
    accent: {
      blue: '#1a73e8',
      blueHover: '#1557b0',
      'dark-blue': '#8ab4f8',
      'dark-blueHover': '#669df6',
    },
    status: {
      success: '#188038',
      error: '#d93025',
      warning: '#e37400',
      info: '#1967d2',
      'dark-success': '#34a853',
      'dark-error': '#ea4335',
      'dark-warning': '#fbbc04',
      'dark-info': '#4285f4',
    },
  },
  method: {
    get: { light: '#1e40af', dark: '#60a5fa' },
    post: { light: '#166534', dark: '#4ade80' },
    put: { light: '#9a3412', dark: '#fb923c' },
    delete: { light: '#991b1b', dark: '#f87171' },
    patch: { light: '#6b21a8', dark: '#c084fc' },
  },
}
```

**How to Implement Dark Mode in Components:**

**1. Using Tailwind Classes (Most Common Pattern):**

Every color-related class MUST include a `dark:` variant:

```vue
<template>
  <!-- Background colors -->
  <div class="bg-dt-bg-primary dark:bg-dt-bg-dark-primary">
    
    <!-- Text colors -->
    <span class="text-dt-text-primary dark:text-dt-text-dark-primary">
      Text content
    </span>
    
    <!-- Border colors -->
    <div class="border border-dt-border-light dark:border-dt-border-dark-light">
      Bordered content
    </div>
    
    <!-- Hover states -->
    <button class="hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover">
      Button
    </button>
    
    <!-- Multiple color properties -->
    <div class="bg-white dark:bg-dt-bg-dark-primary 
                text-dt-text-secondary dark:text-dt-text-dark-secondary 
                border-dt-border-light dark:border-dt-border-dark-light">
      Multi-property example
    </div>
  </div>
</template>
```

**2. Using Theme State Programmatically:**

When you need to conditionally render content or load different assets based on theme:

```vue
<template>
  <div>
    <!-- Conditional rendering -->
    <img v-if="isDark" :src="logoWhite" />
    <img v-else :src="logoBlack" />
    
    <!-- Conditional classes -->
    <div :class="isDark ? 'some-dark-class' : 'some-light-class'">
      Content
    </div>
  </div>
</template>

<script setup lang="ts">
import { useTheme } from '@/composables/theme'

// Access theme state
const { isDark } = useTheme()
</script>
```

**3. Custom CSS with Dark Mode (When Needed):**

For complex styling that can't be done with utility classes:

```vue
<style>
/* Light mode (default) */
.my-component {
  background: #ffffff;
}

/* Dark mode */
.dark .my-component {
  background: #202124;
}
</style>
```

**Theme Management Composable** (`composables/theme.ts`):

```typescript
import { useTheme } from '@/composables/theme'

const { 
  isDark,           // Reactive boolean: true when dark mode active
  toggleTheme,      // Function to toggle between light/dark
  setTheme,         // Function to set theme explicitly: 'light' | 'dark' | 'auto'
  currentTheme      // Computed string: 'light' or 'dark'
} = useTheme()
```

**Key Implementation Details:**

- **Persistence:** Theme preference is stored in `localStorage` with key `devtools-theme`
- **Toggle Mechanism:** The `ThemeToggle` component (already implemented) in the top-right of the app bar
- **DOM Implementation:** Dark mode works by adding/removing `dark` class on `<html>` element
- **Auto Detection:** Falls back to system preference if no stored value exists
- **Real-time Updates:** All components react automatically when theme changes

**Checklist for New Components:**

When creating or modifying components:

- [ ] Add `dark:` variant to EVERY background color class
- [ ] Add `dark:` variant to EVERY text color class
- [ ] Add `dark:` variant to EVERY border color class
- [ ] Add `dark:` variant to hover/focus/active states
- [ ] Test component in both light and dark modes
- [ ] Use custom DevTools colors (not arbitrary Tailwind colors)
- [ ] Import `useTheme()` if programmatic access to theme state is needed

### 4. HTTP Method Color Coding

All HTTP method colors support both light and dark modes:

- **GET:** Blue - `text-method-get-light dark:text-method-get-dark`
- **POST:** Green - `text-method-post-light dark:text-method-post-dark`
- **PUT:** Orange - `text-method-put-light dark:text-method-put-dark`
- **DELETE:** Red - `text-method-delete-light dark:text-method-delete-dark`
- **PATCH:** Purple - `text-method-patch-light dark:text-method-patch-dark`

**Usage Example:**

```vue
<span class="text-method-get-light dark:text-method-get-dark">GET</span>
```

## Component Guidelines

### Template-First Structure
**User preference:** Always structure Vue components as:
```vue
<template>
  <!-- UI here -->
</template>

<script setup lang="ts">
// Logic here
</script>
```

### Tab Navigation Pattern
Active tabs use:
- Blue text color
- Blue underline (0.5px height, positioned absolutely at bottom)
- Transition effects for smooth UX

### Table Styling
- Font size: `text-xs` (12px) for headers, `text-[11px]` for data
- Monospace font for technical data (paths, URLs)
- Truncate with ellipsis for long content
- Borders: `border-dt-border-light`
- Row hover: `hover:bg-dt-bg-hover`
- Selected row: `bg-dt-bg-selected`

## State Management

### Pinia Stores (Independent per Data Type)

The application uses three independent Pinia stores, each managing its own data type with individual pause state and persisted buffer management.

**Stores:**
- `useHttpStore` in `stores/http.ts` - HTTP transaction data
- `useConnectionsStore` in `stores/connections.ts` - Network connection data
- `useProcessesStore` in `stores/processes.ts` - Process monitoring data

**Store Pattern (each store follows this structure):**

**Buffer Manager (outside store definition):**
```typescript
import { usePersistedBuffer } from '@/composables/persistedBuffer'

const bufferManager = usePersistedBuffer<HttpTransaction>({
  storageKey: 'devtools_http_buffer',  // Unique sessionStorage key
  maxBytes: 5 * 1024 * 1024,            // 5 MiB size limit
})
```

**State:**
```typescript
{
  requestsBuffer: HttpTransaction[]    // Reactive array with automatic persistence
  paused: false                        // Independent pause state per store
  filters: Filter[]                    // Active filters for this store
}
```

**Actions:**
```typescript
// HTTP Store (stores/http.ts)
restoreFromStorage()                            // Restore buffer from sessionStorage on init
addRequest(transaction: HttpTransaction)        // Add new request (FIFO, auto-persist)
updateRequest(id: string, transaction)          // Replace existing request by ID (auto-persist)
removeRequest(id: string)                       // Remove request by ID (auto-persist)
clearRequests()                                 // Empty buffer and storage
getRequestById(id: string)                      // Find request by ID (getter)

// Connections Store (stores/connections.ts)
restoreFromStorage()                            // Restore buffer from sessionStorage on init
addConnection(connection: Connection)           // Add new connection (FIFO, auto-persist)
updateConnection(id: string, connection)        // Replace existing connection by ID (auto-persist)
removeConnection(id: string)                    // Remove connection by ID (auto-persist)
clearConnections()                              // Empty buffer and storage
getConnectionById(id: string)                   // Find connection by connectionId (getter)

// Processes Store (stores/processes.ts)
restoreFromStorage()                            // Restore buffer from sessionStorage on init
addProcess(process: Process)                    // Add new process (FIFO, auto-persist)
updateProcess(pid: number, process)             // Replace existing process by PID (auto-persist)
removeProcess(pid: number)                      // Remove process by PID (auto-persist)
clearProcesses()                                // Empty buffer and storage
getProcessByPid(pid: number)                    // Find process by PID (getter)
```

**Key Features:**
- **SessionStorage persistence:** Data survives page reloads (cleared on tab close)
- **Size-based limits:** Buffer limits based on bytes (5 MiB default), not item counts
- **Automatic FIFO management:** When size limit exceeded, oldest items are removed
- **Independent pause states:** Each panel (Requests/Connections/Processes) can be paused independently
- **Reactive arrays:** Automatic Vue reactivity with persistence hooks
- **Restoration on init:** Call `restoreFromStorage()` in `App.vue` `onMounted` to restore persisted data
- **Types defined directly in store files** for co-location

**Creating a New Store with Persisted Buffer:**

When creating a new store, follow this pattern:

```typescript
// stores/newStore.ts
import { defineStore } from 'pinia'
import type { Filter } from './filter'
import { usePersistedBuffer } from '@/composables/persistedBuffer'

// 1. Create buffer manager OUTSIDE store definition
const bufferManager = usePersistedBuffer<YourDataType>({
  storageKey: 'devtools_yourdata_buffer',  // Must be unique
  maxBytes: 5 * 1024 * 1024,               // Adjust size as needed
})

// 2. Define your data types here
export interface YourDataType {
  // ... your type definition
}

// 3. Define the store
export const useYourStore = defineStore('yourStore', {
  state: () => ({
    dataBuffer: [] as YourDataType[],
    paused: false,
    filters: [] as Filter[],
  }),
  
  getters: {
    getItemById: (state) => (id: string) => {
      return state.dataBuffer.find((item) => item.id === id)
    },
  },
  
  actions: {
    // REQUIRED: Restore from sessionStorage on mount
    restoreFromStorage() {
      const restored = bufferManager.restore()
      if (restored) {
        this.dataBuffer = restored
      }
    },

    // Add with automatic persistence
    addItem(item: YourDataType) {
      bufferManager.addAndPersist(this.dataBuffer, item)
    },

    // Update with automatic persistence
    updateItem(id: string, item: YourDataType) {
      const index = this.dataBuffer.findIndex((i) => i.id === id)
      if (index !== -1) {
        this.dataBuffer[index] = item
        bufferManager.updateAndPersist(this.dataBuffer)
      }
    },

    // Remove with automatic persistence
    removeItem(id: string) {
      bufferManager.removeAndPersist(
        this.dataBuffer,
        (item) => item.id === id
      )
    },

    // Clear buffer and storage
    clearItems() {
      bufferManager.clearAll(this.dataBuffer)
    },
  },
})
```

**Don't forget to restore in `App.vue`:**
```typescript
// App.vue
import { useYourStore } from '@/stores/yourStore'

const yourStore = useYourStore()
onMounted(() => {
  yourStore.restoreFromStorage()
})
```

### URL State (UI Navigation)

**Library:** `@vueuse/core` v13.9.0

**Query Parameters:**
- `?tab=connections|requests|processes` - Active panel (default: `requests`)
- `?request_id={id}` - Selected request in Requests panel (default: none)
- `?connection_id={id}` - Selected connection in Connections panel (default: none)
- `?process_id={pid}` - Selected process in Processes panel (default: none)

**Usage Pattern:**
```typescript
// In App.vue or any component
import { useUrlSearchParams } from '@vueuse/core'

const params = useUrlSearchParams('history')

// Read reactive values
const activeTab = computed(() => params.tab || 'requests')
const selectedRequestId = computed(() => params.request_id)

// Update URL (triggers browser history)
params.tab = 'connections'
params.request_id = 'abc123'

// Remove parameter
delete params.request_id
```

**Benefits:**
- Shareable URLs for specific views
- Browser back/forward navigation
- Deep linking support
- Clearer separation of concerns

## SSE Integration & Mock Data

### Real SSE Connection

**Location:** `composables/events.ts` exports `useEvents()`

**SSE Connection Features:**
- Connects to backend SSE endpoint on mount
- Automatic reconnection with infinite retries (2s delay)
- Handles multiple event types (see below)
- Routes incoming events to appropriate store actions
- Parses incoming event data and transforms to UI types
- Logs connection status and errors to console

**Supported Event Types:**
- `system.connected` - Backend connection established
- `request.http_transaction` - HTTP request/response transaction (base64-encoded)
- `connection.opened` - New TCP/UDP connection opened
- `connection.updated` - Connection metadata updated
- `connection.closed` - Connection closed (includes duration)
- `process.started` - New process started
- `process.stopped` - Process exited (includes duration)

**Configuration:**
- `composables/config.ts` manages SSE endpoint configuration
- Dev mode: Override with `VITE_SSE_ENDPOINT` environment variable
- Production: Uses current host at `/devtools/api/events`

### Mock Data (Testing Only)

**Location:** `mocks/requests.ts` exports `mockHttpTransactions`

**Mock Data Characteristics:**
- 10 diverse HTTP transactions covering various scenarios
- Mix of ingress (5) and egress (5) traffic
- Various HTTP methods (GET, POST, PUT, DELETE)
- Different status codes (200, 201, 204, 404, 500)
- Multiple processes (/usr/bin/nginx, /usr/bin/node, /usr/local/bin/python3, etc.)
- Container and Kubernetes metadata
- Realistic headers, bodies, and timing data
- Duration range: 3ms to 5002ms

## Important Implementation Details

### URL Parsing Helpers
```typescript
getPathFromUrl(url: string)       // Extract path: "/api/users"
getEndpointFromUrl(url: string)   // Extract domain: "api.example.com"
```

### Formatting Helpers

Centralized formatting utilities in `composables/formatters.ts`:

```typescript
formatTimestamp(timestamp: string)  // "Nov 17, 8:40 AM" (browser local time)
formatBytes(bytes?: number)         // "1.2 KB", "345 B"
formatDuration(ms?: number)         // "45 ms", "2.34 s"
```

Component-specific helpers (Requests.vue):
```typescript
formatBody(body?: string)           // Pretty-print JSON, handle non-JSON
```

### Status Code Classification
```typescript
getStatusClass(status: number)    // Returns appropriate color class
// 2xx → success (green)
// 3xx → info (blue)
// 4xx → warning (orange)
// 5xx → error (red)
```

## Development Guidelines

### Never Compile/Build
⚠️ **Critical:** The user has explicitly stated "DONT EVER COMPILE THE CODE" - they build in a specific environment. Only make source code changes.

### Styling Conventions
- Use Tailwind utility classes
- Follow Chrome DevTools visual patterns
- Maintain consistent spacing (px-2, py-1.5 for table cells)
- Use semantic color names from the devtools palette

### Component Reusability
- Extract repeated UI patterns into components (like `DirectionIndicator`)
- Place page-level components in `components/pages/`
- Place reusable UI components in `components/ux/`
- Place icons in `components/icons/`



## Quick Start for Next Session

1. **Understand the Chrome DevTools UX paradigm** - All design decisions should reference Chrome DevTools
2. **⚠️ ALWAYS implement dark mode support** - Every component MUST include `dark:` variants for all colors
3. **Review panel implementations** - Reference implementations showing all patterns:
   - `components/pages/Requests.vue` - HTTP transactions panel with filtering and toolbar (canonical scroll behavior)
   - `components/pages/Connections.vue` - Network connections panel
   - `components/pages/Processes.vue` - Process monitoring panel
   - `components/pages/Welcome.vue` - Welcome/landing page
   - All use split panel, top-anchored scroll with frozen mode, reactive updates, URL-based selection, dark mode support
4. **Check stores/** - Three independent stores with types and persisted buffer management:
   - `stores/http.ts` - HTTP transaction types + store (uses persistedBuffer)
   - `stores/connections.ts` - Connection types + store (uses persistedBuffer)
   - `stores/processes.ts` - Process types + store (uses persistedBuffer)
   - `stores/filter.ts` - Filter types (no store)
   - All stores use `usePersistedBuffer()` for automatic sessionStorage persistence
5. **Review App.vue** - URL-based tab navigation with selection cleanup on tab switch, theme toggle
6. **Review composables:**
   - `composables/events.ts` - SSE connection, event routing to multiple stores
   - `composables/config.ts` - Runtime configuration management
   - `composables/formatters.ts` - ✨ Reusable formatting utilities (timestamp, bytes, duration)
   - `composables/requests.ts` - Filtering pattern for HTTP requests
   - `composables/connections.ts` - Filtering pattern for network connections
   - `composables/processes.ts` - Filtering pattern for processes
   - `composables/theme.ts` - ✨ Theme management (useTheme composable)
   - `composables/persistedBuffer.ts` - ✨ Persisted buffer pattern with sessionStorage
   - `composables/storage.ts` - SessionStorage utilities (size calculation, save/load)
   - `composables/urlParams.ts` - URL parameter helpers
7. **Check mocks/requests.ts** - Sample data structure for testing
8. **Look at reusable components:**
   - `components/ux/DirectionIndicator.vue` - Direction badges with color coding
   - `components/ux/StatusBadge.vue` - Status badges for connections and processes
   - `components/ux/StatusPill.vue` - Status pill for connection state
   - `components/ux/ThemeToggle.vue` - ✨ Light/dark theme toggle button
   - `components/ux/Button.vue` - Reusable button component
   - `components/ux/Toolbar.vue` - Filter and pause controls
9. **Reference the Tailwind config** - DevTools color scheme (light + dark) is defined there
10. **Understand state management patterns:**
    - Persisted Buffer Pattern - See section for creating stores with sessionStorage persistence
    - URL State Management - See section for when to use URL vs Store
    - Filtering and Toolbar Pattern - See section for replicating in other panels
    - Theme Management - See section for dark mode implementation

## Key Questions to Ask

Before implementing new features:
- Does this match Chrome DevTools' approach?
- **Have I added `dark:` variants to ALL color classes?**
- **Does the component look good in both light AND dark modes?**
- **If creating/modifying a store, am I using the `usePersistedBuffer()` pattern?**
- **Have I added `restoreFromStorage()` call in `App.vue` for new stores?**
- Is the component reusable enough to extract?
- Are we using the established DevTools color scheme (not arbitrary Tailwind colors)?
- Does the layout follow the split-panel pattern?
- Is the mock data realistic and diverse?

---

**Last Updated:** Documentation updated to reflect persisted buffer pattern using `usePersistedBuffer()` composable with sessionStorage and size-based limits (5 MiB default). All stores now automatically persist data across page reloads with FIFO management based on byte size rather than item counts. Comprehensive dark/light theme implementation guide included. Phase 4 fully completed - Filtering and toolbar functionality implemented for all three panels (Requests, Connections, Processes). Created composables for each panel (`useRequests()`, `useConnections()`, `useProcesses()`) with filtering logic, dynamic filter value extraction, and full operator support. Each store has independent pause state with integration in events composable. Reusable Toolbar component integrated across all panels with panel-specific filterable keys for each data type. Dark mode support implemented across all components using Tailwind's class-based dark mode with `useTheme()` composable for theme management.

