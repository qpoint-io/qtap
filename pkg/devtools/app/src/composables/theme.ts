import { useDark, useToggle } from '@vueuse/core'
import { computed } from 'vue'

/**
 * Composable for managing application theme (light/dark mode)
 * Uses @vueuse/core for persistent storage in localStorage
 */
export function useTheme() {
  // useDark automatically:
  // - Reads initial preference from localStorage (key: 'vueuse-color-scheme')
  // - Falls back to system preference if no stored value
  // - Adds/removes 'dark' class on <html> element
  const isDark = useDark({
    // CSS selector for applying dark class
    selector: 'html',
    // The attribute to use (class or data-* attribute)
    attribute: 'class',
    // The class name to add when dark mode is active
    valueDark: 'dark',
    // The class name to add when light mode is active (empty = no class)
    valueLight: '',
    // localStorage key for persistence
    storageKey: 'devtools-theme',
    // Storage type
    storage: localStorage,
  })

  // Create a toggle function
  const toggleTheme = useToggle(isDark)

  // Computed property for current theme name
  const currentTheme = computed(() => isDark.value ? 'dark' : 'light')

  // Method to set theme explicitly
  const setTheme = (theme: 'light' | 'dark' | 'auto') => {
    if (theme === 'auto') {
      // Set to system preference
      const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
      isDark.value = prefersDark
    } else {
      isDark.value = theme === 'dark'
    }
  }

  return {
    isDark,
    currentTheme,
    toggleTheme,
    setTheme
  }
}