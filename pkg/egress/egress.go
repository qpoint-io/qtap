//go:build linux

package egress

// TLSOkStrategy represents the strategy for marking traffic as OK for TLS termination
type TLSOkStrategy int

const (
	TLSOkStrategyOnCertInject TLSOkStrategy = iota // mark traffic as OK for TLS termination when certificate is injected
	TLSOkStrategyOnCertRead                        // mark traffic as OK for TLS termination when certificate is read
)

// String returns the string representation of the TLSOkStrategy
func (s TLSOkStrategy) String() string {
	switch s {
	case TLSOkStrategyOnCertInject:
		return "on-cert-inject"
	case TLSOkStrategyOnCertRead:
		return "on-cert-read"
	default:
		return "unknown"
	}
}

// StrategyFromString converts a string to a TLSOkStrategy
func TLSOkStrategyFromString(s string) TLSOkStrategy {
	switch s {
	case "on-cert-read":
		return TLSOkStrategyOnCertRead
	default:
		return TLSOkStrategyOnCertInject
	}
}
