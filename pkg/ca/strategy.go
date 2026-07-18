package ca

// InjectStrategy represents the strategy for injecting CA certificates
type InjectStrategy int

const (
	InjectStrategyInline InjectStrategy = iota // inject the custom CA cert into the existing file
	InjectStrategyEbpf                         // use eBPF to present a virtual file without modifying the existing files
	InjectStrategyManual                       // don't provide any injection, the operator is responsible
)

// String returns the string representation of the InjectStrategy
func (s InjectStrategy) String() string {
	switch s {
	case InjectStrategyInline:
		return "inline"
	case InjectStrategyEbpf:
		return "ebpf"
	case InjectStrategyManual:
		return "manual"
	default:
		return "unknown"
	}
}

// FromString converts a string to an InjectStrategy
func StrategyFromString(s string) InjectStrategy {
	switch s {
	case "ebpf":
		return InjectStrategyEbpf
	case "manual":
		return InjectStrategyManual
	default:
		return InjectStrategyInline
	}
}
