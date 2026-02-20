package tools

import (
	"slices"
	"strings"
)

func MimeCategory(contentType string) string {
	// Default value if content type is empty or undefined
	if contentType == "" {
		return "other"
	}

	// Truncate to base content type without any parameters
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = contentType[:idx]
	}

	// Check for various content types
	switch {
	case isApp(contentType):
		return "app"
	case isCss(contentType):
		return "css"
	case isJs(contentType):
		return "js"
	case isFont(contentType):
		return "font"
	case isImage(contentType):
		return "image"
	case isMedia(contentType):
		return "media"
	default:
		return "other"
	}
}

func isApp(contentType string) bool {
	appTypes := []string{"text/html", "application/json", "application/grpc", "text/xml", "application/xml", "text/plain"}
	return slices.Contains(appTypes, contentType)
}

func isCss(contentType string) bool {
	return contentType == "text/css"
}

func isJs(contentType string) bool {
	return contentType == "text/javascript"
}

func isFont(contentType string) bool {
	return strings.HasPrefix(contentType, "font")
}

func isImage(contentType string) bool {
	return strings.HasPrefix(contentType, "image")
}

func isMedia(contentType string) bool {
	return strings.HasPrefix(contentType, "audio") || strings.HasPrefix(contentType, "video")
}
