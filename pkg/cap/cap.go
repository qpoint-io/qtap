package cap

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// readFile and statfs are exported for testing
var (
	readFile = os.ReadFile
	statfs   = syscall.Statfs
)

var (
	ErrCgroupsV2NotEnabled = errors.New("cgroups v2 is not enabled")
	ErrModernKernel        = errors.New("kernel is not modern (5.10 or later required)")
	ErrBpfProbeWriteUser   = errors.New("kernel is in lockdown mode, blocking bpf_probe_write_user functionality")
)

// the magic number for cgroup v2 defined as 0x63677270 in the Linux kernel
const CGROUP2_SUPER_MAGIC = 0x63677270

// Capability represents a Linux capability.
type Capability int

const (
	CAP_IS_MODERN_KERNEL     Capability = iota // is the kernel at least 5.10
	CAP_BPF_PROBE_WRITE_USER                   // allows writing to user memory from eBPF programs.
	CAP_CGROUPS_V2                             // indicates if cgroups v2 is enabled
)

func (c Capability) String() string {
	switch c {
	case CAP_IS_MODERN_KERNEL:
		return "is_modern_kernel"
	case CAP_BPF_PROBE_WRITE_USER:
		return "bpf_probe_write_user"
	case CAP_CGROUPS_V2:
		return "cgroups_v2"
	default:
		return fmt.Sprintf("unknown capability: %d", c)
	}
}

func CanBpfProbeWriteUser() error {
	// read the contents of the lockdown file
	content, err := readFile("/sys/kernel/security/lockdown")
	if err != nil {
		return fmt.Errorf("failed to read lockdown file: %w", err)
	}

	// convert content to string and trim whitespace
	lockdownStatus := strings.TrimSpace(string(content))

	// check if the lockdown status contains [none]
	if strings.Contains(lockdownStatus, "[none]") {
		return nil
	}

	// if [none] is not present, bpf_probe_write_user is likely not allowed
	return ErrBpfProbeWriteUser
}

func IsModernKernel() error {
	// Read the kernel version from /proc/sys/kernel/osrelease
	kernelVersion, err := readFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return fmt.Errorf("failed to read kernel version: %w", err)
	}

	verParseFn := func(err error) error {
		return fmt.Errorf("failed to parse kernel version: %w", err)
	}

	// Parse the kernel version string
	version := strings.TrimSpace(string(kernelVersion))
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return verParseFn(fmt.Errorf("version (%s) incorrect semantic versioning", version))
	}

	// Convert major and minor version to integers
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return verParseFn(fmt.Errorf("major version (%s) not an integer", parts[0]))
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return verParseFn(fmt.Errorf("minor version (%s) not an integer", parts[1]))
	}

	// Check if the kernel version is 5.10 or later
	if major > 5 || (major == 5 && minor >= 10) {
		return nil
	}

	return ErrModernKernel
}

func HasCgroupsV2() error {
	var s syscall.Statfs_t
	if err := statfs("/sys/fs/cgroup", &s); err != nil {
		return fmt.Errorf("failed to statfs /sys/fs/cgroup: %w", err)
	}

	if s.Type == CGROUP2_SUPER_MAGIC {
		return nil
	}

	return ErrCgroupsV2NotEnabled
}
