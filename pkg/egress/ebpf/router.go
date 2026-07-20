package ebpf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/qpoint-io/qtap/internal/tap"
	"github.com/qpoint-io/qtap/pkg/cap"
	"go.uber.org/zap"
)

// Root cgroup path
const CGROUP_PATH = "/sys/fs/cgroup"

type Router struct {
	mu           sync.Mutex
	logger       *zap.Logger
	objs         *tap.TapObjects
	links        []attachedLink
	attachCgroup func(link.CgroupOptions) (attachedLink, error)
	started      bool
	cleanupErr   error
}

type attachedLink interface {
	Close() error
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
		links:  []attachedLink{},
		attachCgroup: func(opts link.CgroupOptions) (attachedLink, error) {
			return link.AttachCgroup(opts)
		},
	}, nil
}

func (a *Router) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.started {
		return nil
	}
	if a.cleanupErr != nil {
		return fmt.Errorf("router cleanup failed; refusing to start: %w", a.cleanupErr)
	}
	if len(a.links) != 0 {
		return errors.New("router has links pending cleanup")
	}

	// ensure the cgroup path exists
	if _, err := os.Stat(CGROUP_PATH); os.IsNotExist(err) {
		return fmt.Errorf("cgroup path does not exist: %s", CGROUP_PATH)
	}

	attachments := []struct {
		name    string
		attach  ebpf.AttachType
		program *ebpf.Program
	}{
		{name: "CgConnect4", attach: ebpf.AttachCGroupInet4Connect, program: a.objs.CgConnect4},
		{name: "CgConnect6", attach: ebpf.AttachCGroupInet6Connect, program: a.objs.CgConnect6},
		{name: "CgSockOps", attach: ebpf.AttachCGroupSockOps, program: a.objs.CgSockOps},
		{name: "CgSockOpt", attach: ebpf.AttachCGroupGetsockopt, program: a.objs.CgSockOpt},
	}

	attached := make([]attachedLink, 0, len(attachments))
	for _, attachment := range attachments {
		attachedLink, err := a.attachCgroup(link.CgroupOptions{
			Path:    CGROUP_PATH,
			Attach:  attachment.attach,
			Program: attachment.program,
		})
		if err != nil {
			closeErrs := closeLinks(attached)
			a.links = nil
			a.cleanupErr = errors.Join(closeErrs...)
			return errors.Join(
				fmt.Errorf("attaching %s program to cgroup: %w", attachment.name, err),
				a.cleanupErr,
			)
		}
		attached = append(attached, attachedLink)
	}
	a.links = attached
	a.started = true

	return nil
}

func closeLinks(links []attachedLink) []error {
	var errs []error
	for _, attachedLink := range links {
		if err := attachedLink.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing link: %w", err))
		}
	}
	return errs
}

func (a *Router) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cleanupErr != nil {
		return a.cleanupErr
	}

	errs := closeLinks(a.links)
	a.links = nil
	a.started = false
	a.cleanupErr = errors.Join(errs...)
	return a.cleanupErr
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
