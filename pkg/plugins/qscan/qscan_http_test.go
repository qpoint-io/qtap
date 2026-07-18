package qscan

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsTextBasedContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		expected    bool
	}{
		{
			name:        "empty content type defaults to text",
			contentType: "",
			expected:    true,
		},
		{
			name:        "text/plain",
			contentType: "text/plain",
			expected:    true,
		},
		{
			name:        "text/html",
			contentType: "text/html",
			expected:    true,
		},
		{
			name:        "text/css",
			contentType: "text/css",
			expected:    true,
		},
		{
			name:        "text/javascript",
			contentType: "text/javascript",
			expected:    true,
		},
		{
			name:        "text/xml",
			contentType: "text/xml",
			expected:    true,
		},
		{
			name:        "application/json",
			contentType: "application/json",
			expected:    true,
		},
		{
			name:        "application/xml",
			contentType: "application/xml",
			expected:    true,
		},
		{
			name:        "application/x-www-form-urlencoded",
			contentType: "application/x-www-form-urlencoded",
			expected:    true,
		},
		{
			name:        "application/javascript",
			contentType: "application/javascript",
			expected:    true,
		},
		{
			name:        "application/x-javascript",
			contentType: "application/x-javascript",
			expected:    true,
		},
		{
			name:        "application/ecmascript",
			contentType: "application/ecmascript",
			expected:    true,
		},
		{
			name:        "application/x-ecmascript",
			contentType: "application/x-ecmascript",
			expected:    true,
		},
		{
			name:        "application/octet-stream",
			contentType: "application/octet-stream",
			expected:    false,
		},
		{
			name:        "application/pdf",
			contentType: "application/pdf",
			expected:    false,
		},
		{
			name:        "image/jpeg",
			contentType: "image/jpeg",
			expected:    false,
		},
		{
			name:        "image/png",
			contentType: "image/png",
			expected:    false,
		},
		{
			name:        "image/gif",
			contentType: "image/gif",
			expected:    false,
		},
		{
			name:        "image/svg+xml",
			contentType: "image/svg+xml",
			expected:    false,
		},
		{
			name:        "audio/mpeg",
			contentType: "audio/mpeg",
			expected:    false,
		},
		{
			name:        "audio/wav",
			contentType: "audio/wav",
			expected:    false,
		},
		{
			name:        "video/mp4",
			contentType: "video/mp4",
			expected:    false,
		},
		{
			name:        "video/quicktime",
			contentType: "video/quicktime",
			expected:    false,
		},
		{
			name:        "text/plain with charset parameter",
			contentType: "text/plain; charset=utf-8",
			expected:    true,
		},
		{
			name:        "application/json with charset parameter",
			contentType: "application/json; charset=utf-8",
			expected:    true,
		},
		{
			name:        "image/jpeg with parameters",
			contentType: "image/jpeg; boundary=something",
			expected:    false,
		},
		{
			name:        "uppercase content type",
			contentType: "TEXT/PLAIN",
			expected:    true,
		},
		{
			name:        "mixed case content type",
			contentType: "Application/Json",
			expected:    true,
		},
		{
			name:        "uppercase binary type",
			contentType: "APPLICATION/OCTET-STREAM",
			expected:    false,
		},
		{
			name:        "content type with spaces before semicolon",
			contentType: "text/plain ; charset=utf-8",
			expected:    true,
		},
		{
			name:        "content type with trailing spaces",
			contentType: "text/plain  ",
			expected:    true,
		},
		{
			name:        "content type with leading spaces",
			contentType: "  text/plain",
			expected:    true,
		},
		{
			name:        "unknown application type defaults to text",
			contentType: "application/unknown",
			expected:    true,
		},
		{
			name:        "unknown type defaults to text",
			contentType: "custom/type",
			expected:    true,
		},
		{
			name:        "application/xml with parameters",
			contentType: "application/xml; charset=iso-8859-1",
			expected:    true,
		},
		{
			name:        "text/html with multiple parameters",
			contentType: "text/html; charset=utf-8; boundary=something",
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTextBasedContentType(tt.contentType)
			require.Equal(t, tt.expected, result, "content type %q should return %v", tt.contentType, tt.expected)
		})
	}
}
