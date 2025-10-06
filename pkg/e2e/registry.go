package e2e

import "fmt"

var Registry *ImageRegistry

func init() {
	Registry = NewImageRegistry()
	RegisterPython()
	RegisterRuby()
	RegisterPHP()
}

// ClientCapabilities defines what a specific HTTP client can do
type ClientCapabilities struct {
	Name          string
	HTTPProtocols []HTTPProtocol
	// Features     map[string]bool   // TODO(Jon): maybe a future feature? "async", "streaming", "compression", etc.
}

// ImageCapabilities defines what a Docker image supports
type ImageCapabilities struct {
	Image    HTTPRequestImage
	Language Language
	Version  string
	OS       string
	Clients  map[string]*ClientCapabilities
}

// ImageRegistry manages all available images
type ImageRegistry struct {
	images map[string]*ImageCapabilities
}

// NewImageRegistry creates a new registry
func NewImageRegistry() *ImageRegistry {
	return &ImageRegistry{
		images: make(map[string]*ImageCapabilities),
	}
}

// Register adds an image with its capabilities
func (r *ImageRegistry) Register(cap *ImageCapabilities) {
	key := fmt.Sprintf("%s-%s-%s", cap.Language, cap.Version, cap.OS)
	r.images[key] = cap
}

// Lookup finds an image based on requirements
func (r *ImageRegistry) Lookup(language Language, version, os string) (*ImageCapabilities, bool) {
	key := fmt.Sprintf("%s-%s-%s", language, version, os)
	cap, ok := r.images[key]
	return cap, ok
}

// ListAvailable prints all registered images and their capabilities
func (r *ImageRegistry) ListAvailable() {
	for key, cap := range r.images {
		fmt.Printf("%s:\n", key)
		for clientName, client := range cap.Clients {
			fmt.Printf("  %s: HTTP %v\n", clientName, client.HTTPProtocols)
		}
	}
}
