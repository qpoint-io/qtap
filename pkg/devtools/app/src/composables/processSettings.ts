import { ref, watch } from 'vue'

const STORAGE_KEY = 'devtools_processes_show_only_with_connections'

// Singleton state to share across all components using this composable
const showOnlyWithConnections = ref(
  localStorage.getItem(STORAGE_KEY) !== 'false' // Default: true
)

// Persist changes to localStorage
watch(showOnlyWithConnections, (value) => {
  localStorage.setItem(STORAGE_KEY, String(value))
})

export function useProcessSettings() {
  return {
    showOnlyWithConnections,
  }
}
