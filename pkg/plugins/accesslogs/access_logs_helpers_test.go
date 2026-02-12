package accesslogs

import "testing"

func TestTruncateQuery(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		maxLen int
		want   string
	}{
		{"short query unchanged", "SELECT 1", 50, "SELECT 1"},
		{"exact length", "SELECT", 6, "SELECT"},
		{"truncated with ellipsis", "SELECT * FROM users WHERE id = 1", 20, "SELECT * FROM use..."},
		{"trims whitespace", "  SELECT 1  ", 50, "SELECT 1"},
		{"replaces newlines", "SELECT\n1\nFROM\nusers", 50, "SELECT 1 FROM users"},
		{"replaces tabs", "SELECT\t1\tFROM\tusers", 50, "SELECT 1 FROM users"},
		{"maxLen less than 3 is clamped", "abcdef", 1, "..."},
		{"maxLen of 0", "abcdef", 0, "..."},
		{"maxLen negative", "abcdef", -5, "..."},
		{"maxLen exactly 3", "abcdef", 3, "..."},
		{"empty query", "", 50, ""},
		{"maxLen 4 truncates", "abcdef", 4, "a..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateQuery(tt.query, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateQuery(%q, %d) = %q, want %q", tt.query, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestExtractQueryType(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"select", "SELECT * FROM users", "SELECT"},
		{"insert", "INSERT INTO users VALUES (1)", "INSERT"},
		{"update", "update users SET name = 'foo'", "UPDATE"},
		{"delete", "delete from users", "DELETE"},
		{"create table", "CREATE TABLE foo (id int)", "CREATE"},
		{"leading whitespace", "  SELECT 1", "SELECT"},
		{"empty string", "", "UNKNOWN"},
		{"whitespace only", "   ", "UNKNOWN"},
		{"lowercase", "select 1", "SELECT"},
		{"begin", "BEGIN", "BEGIN"},
		{"commit", "COMMIT", "COMMIT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractQueryType(tt.query)
			if got != tt.want {
				t.Errorf("extractQueryType(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		width int
		want  string
	}{
		{"pad short string", "abc", 6, "abc   "},
		{"exact width", "abc", 3, "abc"},
		{"longer than width", "abcdef", 3, "abcdef"},
		{"empty string", "", 4, "    "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := padRight(tt.s, tt.width)
			if got != tt.want {
				t.Errorf("padRight(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
			}
		})
	}
}
