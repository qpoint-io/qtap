# QPoint DevTools

## Features

- **Three Main Panels**

  - **Connections**: Monitor TCP/UDP network connections with real-time status, direction, and metadata
  - **Requests**: Inspect HTTP/HTTPS transactions with full request/response details
  - **Processes**: Track Linux processes with container and Kubernetes pod information

- **Real-Time Data Streaming**

  - Server-Sent Events (SSE) integration for live backend data
  - Automatic reconnection with infinite retries
  - Independent pause/resume controls per panel
  - SessionStorage persistence for data retention across page reloads

- **Advanced Filtering & Search**

  - Multi-filter support with AND logic
  - Dynamic filter value extraction from live data
  - Full operator support (is, is not, contains, starts with, ends with, etc.)
  - Panel-specific filterable keys for each data type

- **Chrome DevTools-Inspired UX**

  - Familiar look, feel, and interaction patterns
  - Split panel views for detailed inspection (table + details)
  - Top-anchored scroll with frozen mode for streaming data
  - URL-based state management (shareable and bookmarkable views)

- **Dark/Light Theme Support**

  - Complete DevTools color palette with light and dark variants
  - Persistent theme preference with system preference fallback
  - All components fully support both themes

- **Rich Detail Views**
  - HTTP request/response headers, preview, and body inspection
  - Connection metadata including TLS detection, endpoints, and system info
  - Process details with container, pod, and user information
  - Color-coded status badges and direction indicators

## Technology Stack

- **Framework**: Vue 3 with TypeScript (Composition API)
- **State Management**: Pinia (data) + VueUse URL parameters (UI state)
- **Utilities**: @vueuse/core v13.9.0 (URL state management, theme management)
- **Styling**: Tailwind CSS with custom DevTools color scheme (light + dark mode)
- **Build Tool**: Vite
- **Component Structure**: Template-first, script-last

## Getting Started

### Prerequisites

- Node.js `^20.19.0` or `>=22.12.0`
- npm

### Installation

```sh
npm install
```

### Development

Start the development server with hot-reload:

```sh
npm run dev
```

The application will be available at `http://localhost:5173` (or the port Vite assigns).

### Building for Production

Type-check, compile, and minify for production:

```sh
npm run build
```

The built files will be in the `dist/` directory.

### Type Checking

Run TypeScript type checking:

```sh
npm run type-check
```

## Project Structure

```
src/
├── App.vue                    # Main app with tab navigation
├── main.ts                    # Vue app entry point
├── assets/
│   └── main.css               # Tailwind imports
├── components/
│   ├── icons/                 # Icon components
│   ├── pages/                 # Main panel components
│   │   ├── Connections.vue   # Network connections panel
│   │   ├── Processes.vue    # Process monitoring panel
│   │   ├── Requests.vue      # HTTP transactions panel
│   │   └── Welcome.vue       # Welcome/landing page
│   └── ux/                    # Reusable UX components
│       ├── Button.vue
│       ├── DirectionIndicator.vue
│       ├── StatusBadge.vue
│       ├── ThemeToggle.vue
│       └── Toolbar.vue
├── composables/               # Reusable composition functions
│   ├── config.ts             # Runtime configuration
│   ├── events.ts             # SSE connection and event routing
│   ├── formatters.ts         # Formatting utilities
│   ├── requests.ts           # Request filtering logic
│   ├── connections.ts        # Connection filtering logic
│   ├── processes.ts          # Process filtering logic
│   ├── theme.ts              # Theme management
│   ├── urlParams.ts          # URL parameter helpers
│   ├── storage.ts            # SessionStorage utilities
│   └── persistedBuffer.ts    # Persisted buffer pattern
├── stores/                    # Pinia stores
│   ├── http.ts               # HTTP transaction store
│   ├── connections.ts        # Connection store
│   ├── processes.ts          # Process store
│   └── filter.ts             # Filter types
└── mocks/                     # Mock data for testing
    └── requests.ts
```

## Development Guidelines

### IDE Setup

**Recommended**: [VS Code](https://code.visualstudio.com/) + [Vue (Official)](https://marketplace.visualstudio.com/items?itemName=Vue.volar) (disable Vetur)

**Browser Extensions**:

- Chromium-based browsers: [Vue.js devtools](https://chromewebstore.google.com/detail/vuejs-devtools/nhdogjmejiglipccpnnnanhbledajbpd)
- Firefox: [Vue.js devtools](https://addons.mozilla.org/en-US/firefox/addon/vue-js-devtools/)

### Key Design Principles

1. **Chrome DevTools UX**: All design decisions should reference Chrome DevTools for familiarity
2. **Dark Mode Support**: Every component MUST include `dark:` variants for all colors
3. **Template-First Structure**: Components use template-first, script-last structure
4. **URL-Based UI State**: Navigation and selection state managed via URL parameters
5. **Persisted Buffers**: Data buffers use sessionStorage with size-based limits

### Architecture Patterns

The project follows several key architectural patterns:

- **Independent Store Pattern**: Separate Pinia stores for each data type with independent pause states
- **Persisted Buffer Pattern**: Automatic FIFO management with sessionStorage persistence
- **Composable-Based Filtering**: Reusable filtering logic via composables
- **URL State Management**: UI state (tabs, selections) managed via URL parameters for shareability

For detailed implementation patterns, architecture decisions, and data models, see [docs/overview.md](docs/overview.md).

## Documentation

Comprehensive documentation including architecture decisions, implementation patterns, data models, and development guidelines is available in [docs/overview.md](docs/overview.md).
