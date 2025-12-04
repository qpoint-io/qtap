/**
 * Runtime configuration composable
 * 
 * Dev mode: Set VITE_SSE_ENDPOINT in .env to override default
 * Production: Uses current host with static path (embedded in Go binary)
 */
export function useConfig() {
  const getSSEEndpoint = (): string => {
    // Use environment variable if set (dev mode override)
    if (import.meta.env.VITE_SSE_ENDPOINT) {
      return import.meta.env.VITE_SSE_ENDPOINT as string
    }
    
    // Default: use current host with static path
    return `${window.location.protocol}//${window.location.host}/devtools/api/events`
  }

  return {
    sseEndpoint: getSSEEndpoint(),
  }
}

