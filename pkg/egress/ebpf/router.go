package ebpf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/cap"
	"go.uber.org/zap"
)

// Root cgroup path
const CGROUP_PATH = "/sys/fs/cgroup"

type Router struct {
	logger *zap.Logger
	objs   *tap.TapObjects
	links  []link.Link
}

func NewRouter(logger *zap.Logger, objs *tap.TapObjects) (*Router, error) {
	// check if cgroups v2 is enabled
	if err := cap.HasCgroupsV2(); err != nil {
		if errors.Is(err, cap.ErrCgroupsV2NotEnabled) {
			logger.Warn("cgroups v2 is not enabled, skipping egress controller")
			return nil, err
		}
		return nil, fmt.Errorf("failed to check if cgroups v2 is enabled: %w", err)
	}

	// ensure the cgroup path exists
	if _, err := os.Stat(CGROUP_PATH); os.IsNotExist(err) {
		return nil, fmt.Errorf("cgroup path does not exist: %s", CGROUP_PATH)
	}

	return &Router{
		logger: logger,
		objs:   objs,
		links:  []link.Link{},
	}, nil
}

func (a *Router) Start() error {
	// ensure the cgroup path exists
	if _, err := os.Stat(CGROUP_PATH); os.IsNotExist(err) {
		return fmt.Errorf("cgroup path does not exist: %s", CGROUP_PATH)
	}

	// attach ipv4 connect probe
	connect4Link, err := link.AttachCgroup(link.CgroupOptions{
		Path:    CGROUP_PATH,
		Attach:  ebpf.AttachCGroupInet4Connect,
		Program: a.objs.CgConnect4,
	})
	if err != nil {
		return fmt.Errorf("attaching CgConnect4 program to cgroup: %w", err)
	}
	a.links = append(a.links, connect4Link)

	// attach ipv6 connect probe
	connect6Link, err := link.AttachCgroup(link.CgroupOptions{
		Path:    CGROUP_PATH,
		Attach:  ebpf.AttachCGroupInet6Connect,
		Program: a.objs.CgConnect6,
	})
	if err != nil {
		return fmt.Errorf("attaching CgConnect6 program to cgroup: %w", err)
	}
	a.links = append(a.links, connect6Link)

	// attach sockops probe
	sockopsLink, err := link.AttachCgroup(link.CgroupOptions{
		Path:    CGROUP_PATH,
		Attach:  ebpf.AttachCGroupSockOps,
		Program: a.objs.CgSockOps,
	})
	if err != nil {
		return fmt.Errorf("attaching CgSockOps program to cgroup: %w", err)
	}
	a.links = append(a.links, sockopsLink)

	// attach sockopt probe
	sockoptLink, err := link.AttachCgroup(link.CgroupOptions{
		Path:    CGROUP_PATH,
		Attach:  ebpf.AttachCGroupGetsockopt,
		Program: a.objs.CgSockOpt,
	})
	if err != nil {
		return fmt.Errorf("attaching CgSockOpt program to cgroup: %w", err)
	}
	a.links = append(a.links, sockoptLink)

	return nil
}

func (a *Router) Stop() error {
	// close the links
	for _, link := range a.links {
		if err := link.Close(); err != nil {
			return fmt.Errorf("closing link: %w", err)
		}
	}

	return nil
}

// SetMgmtAddrs configures the listen addresses in the eBPF map
func (a *Router) SetMgmtAddrs(ipv4 net.IP, ipv6 net.IP, port int) error {
	type forwardAddrs struct {
		Ipv4 uint32
		Ipv6 [4]uint32
		Port uint32
	}

	// init the listen addrs for the ebpf map
	addrs := forwardAddrs{
		Port: uint32(binary.BigEndian.Uint16([]byte{byte(port), byte(port >> 8)})),
	}

	// set the ipv4 addr
	if ipv4 != nil {
		ipv4Bytes := ipv4.To4()
		if ipv4Bytes != nil {
			addrs.Ipv4 = binary.NativeEndian.Uint32(ipv4Bytes)
		}
	}

	// set the ipv6 addr
	if ipv6 != nil {
		ipv6Bytes := ipv6.To16()
		if ipv6Bytes != nil {
			for i := range addrs.Ipv6 {
				addrs.Ipv6[i] = binary.NativeEndian.Uint32(ipv6Bytes[i*4 : (i+1)*4])
			}
		}
	}

	// Set in the eBPF map
	if err := a.objs.MgmtAddrs.Put(uint32(0), addrs); err != nil {
		return fmt.Errorf("setting listener addrs: %w", err)
	}

	return nil
}
