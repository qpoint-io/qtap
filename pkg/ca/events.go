package ca

// CertEvent represents the different types of cert events
type CertEvent uint64

const (
	CertRead CertEvent = 1 + iota
)

type CertEventMeta struct {
	Type CertEvent
}

type CertReadEvent struct {
	Pid      int64
	FileSize uint32
	File     [256]int8
	_        [4]byte // padding
}
