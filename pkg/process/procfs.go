package process

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Cgroup models one line from /proc/[pid]/cgroup. Each Cgroup struct describes the placement of a PID inside a
// specific control hierarchy. The kernel has two cgroup APIs, v1 and v2. v1 has one hierarchy per available resource
// controller, while v2 has one unified hierarchy shared by all controllers. Regardless of v1 or v2, all hierarchies
// contain all running processes, so the question answerable with a Cgroup struct is 'where is this process in
// this hierarchy' (where==what path on the specific cgroupfs). By prefixing this path with the mount point of
// *this specific* hierarchy, you can locate the relevant pseudo-files needed to read/set the data for this PID
// in this hierarchy
//
// Also see http://man7.org/linux/man-pages/man7/cgroups.7.html
type Cgroup struct {
	// HierarchyID that can be matched to a named hierarchy using /proc/cgroups. Cgroups V2 only has one
	// hierarchy, so HierarchyID is always 0. For cgroups v1 this is a unique ID number
	HierarchyID int
	// Controllers using this hierarchy of processes. Controllers are also known as subsystems. For
	// Cgroups V2 this may be empty, as all active controllers use the same hierarchy
	Controllers []string
	// Path of this control group, relative to the mount point of the cgroupfs representing this specific
	// hierarchy
	Path string
}

// parseCgroupString parses each line of the /proc/[pid]/cgroup file
// Line format is hierarchyID:[controller1,controller2]:path.
func parseCgroupString(cgroupStr string) (*Cgroup, error) {
	var err error

	fields := strings.SplitN(cgroupStr, ":", 3)
	if len(fields) < 3 {
		return nil, fmt.Errorf("3+ fields required, found %d fields in cgroup string: %s", len(fields), cgroupStr)
	}

	cgroup := &Cgroup{
		Path:        fields[2],
		Controllers: nil,
	}
	cgroup.HierarchyID, err = strconv.Atoi(fields[0])
	if err != nil {
		return nil, fmt.Errorf("hierarchy ID: %q", cgroup.HierarchyID)
	}
	if fields[1] != "" {
		ssNames := strings.Split(fields[1], ",")
		cgroup.Controllers = append(cgroup.Controllers, ssNames...)
	}
	return cgroup, nil
}

// parseCgroups reads each line of the /proc/[pid]/cgroup file.
func parseCgroups(data []byte) ([]Cgroup, error) {
	var cgroups []Cgroup
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		mountString := scanner.Text()
		parsedMounts, err := parseCgroupString(mountString)
		if err != nil {
			return nil, err
		}
		cgroups = append(cgroups, *parsedMounts)
	}

	err := scanner.Err()
	return cgroups, err
}

// ProcessUser contains information about the user running a process
type ProcessUser struct {
	UID      string
	Username string
	Name     string
}

// GetProcessUser retrieves user information for a given process ID using the /proc filesystem
func GetProcessUser(pid int) (*ProcessUser, error) {
	// Construct path to process directory
	procPath := filepath.Join("/proc", strconv.Itoa(pid))

	// Check if process exists
	if _, err := os.Stat(procPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("process %d not found", pid)
	}

	// Get file info for the process directory
	info, err := os.Stat(procPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat process directory: %w", err)
	}

	// Get system-specific file info to access UID
	stat := info.Sys().(*syscall.Stat_t)
	if stat == nil {
		return nil, errors.New("failed to get detailed file info")
	}

	// Convert UID to string
	uid := strconv.FormatUint(uint64(stat.Uid), 10)

	// Look up user info from UID
	u, err := user.LookupId(uid)
	if err != nil {
		return nil, fmt.Errorf("failed to look up user info: %w", err)
	}

	return &ProcessUser{
		UID:      uid,
		Username: u.Username,
		Name:     u.Name,
	}, nil
}

// AllProcs returns a list of all currently available processes.
func AllProcs(path string) ([]int, error) {
	d, err := os.Open(path)
	if err != nil {
		return []int{}, err
	}
	defer d.Close()

	names, err := d.Readdirnames(-1)
	if err != nil {
		return []int{}, fmt.Errorf("reading file: %v: %w", names, err)
	}

	p := []int{}
	for _, n := range names {
		pid, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			continue
		}
		p = append(p, int(pid))
	}

	return p, nil
}

// Cgroups reads from /proc/<pid>/cgroups and returns a []*Cgroup struct locating this PID in each process
// control hierarchy running on this system. On every system (v1 and v2), all hierarchies contain all processes,
// so the len of the returned struct is equal to the number of active hierarchies on this system.
func Cgroups(pid int) ([]Cgroup, error) {
	data, err := os.ReadFile(path.Join("/proc", strconv.FormatInt(int64(pid), 10), "cgroup"))
	if err != nil {
		return nil, err
	}
	return parseCgroups(data)
}

// CmdLine returns the command line of a process.
func CmdLine(pid int) ([]string, error) {
	data, err := os.ReadFile(path.Join("/proc", strconv.FormatInt(int64(pid), 10), "cmdline"))
	if err != nil {
		return nil, err
	}

	if len(data) < 1 {
		return []string{}, nil
	}

	return strings.Split(string(bytes.TrimRight(data, "\x00")), "\x00"), nil
}

// Environ reads process environments from `/proc/<pid>/environ`.
func Environ(pid int) ([]string, error) {
	environments := make([]string, 0)

	data, err := os.ReadFile(path.Join("/proc", strconv.FormatInt(int64(pid), 10), "environ"))
	if err != nil {
		return environments, err
	}

	environments = strings.Split(string(data), "\000")
	if len(environments) > 0 {
		environments = environments[:len(environments)-1]
	}

	return environments, nil
}

// Executable returns the absolute path to the executable of the process.
func Executable(pid int) (string, error) {
	exe, err := os.Readlink(path.Join("/proc", strconv.FormatInt(int64(pid), 10), "exe"))
	if os.IsNotExist(err) {
		return "", nil
	}

	return exe, err
}

// IsKernelProcess returns true if the process is a kernel process.
func IsKernelProcess(pid int) (bool, error) {
	// Read the status file directly from /proc
	statusContent, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return false, fmt.Errorf("failed to read status: %w", err)
	}

	// Look for VmSize in the status file
	lines := strings.Split(string(statusContent), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "VmSize:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				vmSize, err := strconv.ParseUint(fields[1], 10, 64)
				if err != nil {
					return false, fmt.Errorf("failed to parse VmSize: %w", err)
				}
				return vmSize == 0, nil
			}
		}
	}

	// If VmSize is not found, it's likely a kernel process
	return true, nil
}
