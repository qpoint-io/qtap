import { useUrlSearchParams } from '@vueuse/core'

/**
 * Shared URL params composable
 * 
 * Returns the same reactive params instance across all components
 * to ensure proper reactivity when navigating between tabs.
 */

// Create a singleton instance
const params = useUrlSearchParams('history', {
  writeMode: 'push',
})

export function useUrlParams() {
  return params
}

