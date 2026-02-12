package accesslogs

import "strings"

// truncateQuery trims and truncates a query string for display
func truncateQuery(query string, maxLen int) string {
	query = strings.TrimSpace(query)
	query = strings.ReplaceAll(query, "\n", " ")
	query = strings.ReplaceAll(query, "\t", " ")
	if maxLen < 3 {
		maxLen = 3
	}
	if len(query) > maxLen {
		return query[:maxLen-3] + "..."
	}
	return query
}

// padRight pads a string to the given width with spaces
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// extractQueryType extracts the SQL command type from a query
func extractQueryType(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "UNKNOWN"
	}

	// Find first word
	parts := strings.Fields(query)
	if len(parts) == 0 {
		return "UNKNOWN"
	}

	return strings.ToUpper(parts[0])
}
