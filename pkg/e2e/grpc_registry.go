package e2e

import "fmt"

var GRPCRegistry *GRPCImageRegistry

func init() {
	GRPCRegistry = NewGRPCImageRegistry()
	RegisterGRPCGo()
	RegisterGRPCJava()
	RegisterGRPCPython()
	RegisterGRPCNodeJS()
	RegisterGRPCRuby()
	RegisterGRPCPHP()
}

// GRPCImageCapabilities defines what a gRPC Docker image supports
type GRPCImageCapabilities struct {
	Image    GRPCRequestImage
	Language Language
	Version  string
	OS       string
}

// GRPCImageRegistry manages all available gRPC images
type GRPCImageRegistry struct {
	images map[string]*GRPCImageCapabilities
}

// NewGRPCImageRegistry creates a new gRPC registry
func NewGRPCImageRegistry() *GRPCImageRegistry {
	return &GRPCImageRegistry{
		images: make(map[string]*GRPCImageCapabilities),
	}
}

// Register adds an image with its capabilities
func (r *GRPCImageRegistry) Register(cap *GRPCImageCapabilities) {
	key := fmt.Sprintf("%s-%s-%s", cap.Language, cap.Version, cap.OS)
	r.images[key] = cap
}

// Lookup finds an image based on requirements
func (r *GRPCImageRegistry) Lookup(language Language, version, os string) (*GRPCImageCapabilities, bool) {
	key := fmt.Sprintf("%s-%s-%s", language, version, os)
	cap, ok := r.images[key]
	return cap, ok
}

func RegisterGRPCGo() {
	GRPCRegistry.Register(&GRPCImageCapabilities{
		Image:    GRPCRequestGo1_25_1_Alpine,
		Language: Go,
		Version:  "1.25.1",
		OS:       "alpine",
	})
}

func RegisterGRPCJava() {
	GRPCRegistry.Register(&GRPCImageCapabilities{
		Image:    GRPCRequestJava21_Alpine,
		Language: Java,
		Version:  "21",
		OS:       "alpine",
	})
}

func RegisterGRPCPython() {
	GRPCRegistry.Register(&GRPCImageCapabilities{
		Image:    GRPCRequestPython3_12_0_Alpine,
		Language: Python,
		Version:  "3.12.0",
		OS:       "alpine",
	})
}

func RegisterGRPCNodeJS() {
	GRPCRegistry.Register(&GRPCImageCapabilities{
		Image:    GRPCRequestNodeJS22_16_0_Alpine,
		Language: NodeJS,
		Version:  "22.16.0",
		OS:       "alpine",
	})
}

func RegisterGRPCRuby() {
	GRPCRegistry.Register(&GRPCImageCapabilities{
		Image:    GRPCRequestRuby3_4_5_Alpine,
		Language: Ruby,
		Version:  "3.4.5",
		OS:       "alpine",
	})
}

func RegisterGRPCPHP() {
	GRPCRegistry.Register(&GRPCImageCapabilities{
		Image:    GRPCRequestPHP8_3_Alpine,
		Language: PHP,
		Version:  "8.3",
		OS:       "alpine",
	})
}
