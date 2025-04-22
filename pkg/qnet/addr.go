package qnet

import (
	"net"
	"strconv"
)

type NetFamily string

const (
	NetFamily_Unknown NetFamily = "unknown"
	NetFamily_IPv4    NetFamily = "ipv4"
	NetFamily_IPv6    NetFamily = "ipv6"
)

func (f NetFamily) String() string {
	return string(f)
}

// ensure NetAddr fulfills net.Addr interface
var _ net.Addr = NetAddr{}

type NetAddr struct {
	Family NetFamily `json:"family,omitempty"`
	IP     net.IP    `json:"ip,omitempty"`
	Port   uint16    `json:"port,omitempty"`
}

// Network returns the network type (e.g., "ipv4" or "ipv6")
func (na NetAddr) Network() string {
	return string(na.Family)
}

// String returns a string representation of the address
func (na NetAddr) String() string {
	switch na.Family {
	case NetFamily_IPv4, NetFamily_IPv6:
		return net.JoinHostPort(na.IP.String(), strconv.Itoa(int(na.Port)))
	default:
		return "unknown"
	}
}

func NetAddrFromTCPAddr(addr *net.TCPAddr) NetAddr {
	a := NetAddr{
		Port: uint16(addr.Port),
	}

	if ip4 := addr.IP.To4(); ip4 != nil {
		a.Family = NetFamily_IPv4
	} else {
		a.Family = NetFamily_IPv6
	}

	a.IP = addr.IP

	return a
}

func (na NetAddr) ToBytes() [16]byte {
	var result [16]byte
	switch na.Family {
	case NetFamily_IPv4:
		copy(result[:], na.IP.To4())
	case NetFamily_IPv6:
		copy(result[:], na.IP.To16())
	}
	return result
}

func (na NetAddr) Equal(other NetAddr) bool {
	return na.Family == other.Family && na.IP.Equal(other.IP) && na.Port == other.Port
}

func (na NetAddr) Empty() bool {
	return na.Equal(NetAddr{})
}

func (na NetAddr) ControlValues() map[string]any {
	return map[string]any{
		"ip":   na.IP,
		"port": int(na.Port),
	}
}
