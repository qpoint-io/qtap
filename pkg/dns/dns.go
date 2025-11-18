package dns

import (
	"net"
	"syscall"

	"github.com/qpoint-io/qtap/pkg/qnet"
)

// DNS record entry
type Record struct {
	SaFamily uint16   // Address family
	Addr     [16]byte // ipv4 or ipv6 address raw bytes
	Domain   string   // the domain name
}

func NewRecord(saFamily uint16, addr [16]byte, domain string) *Record {
	return &Record{
		SaFamily: saFamily,
		Addr:     addr,
		Domain:   domain,
	}
}

func (r Record) AddrSize() uint16 {
	switch r.SaFamily {
	case syscall.AF_INET:
		return 4
	case syscall.AF_INET6:
		return 16
	default:
		return 0
	}
}

func (r Record) IP() net.IP {
	return net.IP(r.Addr[:r.AddrSize()])
}

func (r Record) IpString() string {
	return qnet.IPString(r.IP())
}
