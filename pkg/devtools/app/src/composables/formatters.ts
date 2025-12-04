/**
 * Formatting utilities for DevTools UI
 */

/**
 * Format an ISO timestamp to a human-readable format like "Nov 17, 8:40 AM"
 * Uses browser's local time zone
 */
export function formatTimestamp(timestamp: string): string {
  try {
    const date = new Date(timestamp)
    return date.toLocaleString('en-US', {
      month: 'short',
      day: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
      hour12: true
    })
  } catch {
    return '-'
  }
}

/**
 * Format bytes to human-readable size like "1.2 KB"
 */
export function formatBytes(bytes?: number): string {
  if (!bytes) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return Math.round(bytes / Math.pow(k, i)) + ' ' + sizes[i]
}

/**
 * Format milliseconds to human-readable duration like "45 ms" or "2.34 s"
 */
export function formatDuration(ms?: number): string {
  if (!ms) return '-'
  if (ms < 1000) return `${ms} ms`
  return `${(ms / 1000).toFixed(2)} s`
}

