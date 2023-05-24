package proxy

// network proxy
type Forwarder interface {
	Start() error
	Forward(to string) error
	Stop() error
}
