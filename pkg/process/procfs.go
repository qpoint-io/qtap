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
		return nil, fmt.Errorf("hierarchy ID: %d", cgroup.HierarchyID)
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
	UID      uint
	Username string
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
		return &ProcessUser{UID: uint(stat.Uid)}, fmt.Errorf("failed to look up user info: %w", err)
	}

	return &ProcessUser{
		UID:      uint(stat.Uid),
		Username: u.Username,
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

// readProcStat reads and parses /proc/PID/stat file
func readProcStat(pid int) ([]string, error) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read stat: %w", err)
	}

	// The comm field (process name) can contain spaces and parentheses
	// Find the last closing parenthesis to correctly parse the fields
	statStr := string(data)
	lastParen := strings.LastIndex(statStr, ")")
	if lastParen == -1 {
		return nil, errors.New("invalid stat format: no closing parenthesis found")
	}

	// Split the fields after the closing parenthesis
	afterParen := strings.TrimSpace(statStr[lastParen+1:])
	fields := strings.Fields(afterParen)

	// Prepend the pid and comm fields
	firstParen := strings.Index(statStr, "(")
	if firstParen == -1 {
		return nil, errors.New("invalid stat format: no opening parenthesis found")
	}

	pidStr := strings.TrimSpace(statStr[:firstParen])
	comm := statStr[firstParen+1 : lastParen]

	result := []string{pidStr, comm}
	result = append(result, fields...)

	return result, nil
}

// getControllingTerminal returns the controlling terminal device number for a process
func getControllingTerminal(pid int) (int, error) {
	fields, err := readProcStat(pid)
	if err != nil {
		return 0, err
	}

	// tty_nr is the 7th field after the closing parenthesis (index 8 in our array)
	if len(fields) < 9 {
		return 0, errors.New("stat file has insufficient fields")
	}

	ttyNr, err := strconv.Atoi(fields[8])
	if err != nil {
		return 0, fmt.Errorf("failed to parse tty_nr: %w", err)
	}

	return ttyNr, nil
}

// getPPID returns the parent process ID
func getPPID(pid int) (int, error) {
	fields, err := readProcStat(pid)
	if err != nil {
		return 0, err
	}

	// ppid is the 4th field (index 3 in our array)
	if len(fields) < 4 {
		return 0, errors.New("stat file has insufficient fields")
	}

	ppid, err := strconv.Atoi(fields[3])
	if err != nil {
		return 0, fmt.Errorf("failed to parse ppid: %w", err)
	}

	return ppid, nil
}

// isTerminalFD checks if /proc/PID/fd/0 points to a terminal device
func isTerminalFD(pid int) (bool, error) {
	fdPath := fmt.Sprintf("/proc/%d/fd/0", pid)
	target, err := os.Readlink(fdPath)
	if err != nil {
		// If we can't read the fd, it might be due to permissions
		if os.IsPermission(err) {
			return false, nil
		}
		return false, err
	}

	// Check if it points to a terminal device
	return strings.HasPrefix(target, "/dev/pts/") || strings.HasPrefix(target, "/dev/tty"), nil
}

// Known interactive shells
var interactiveShells = map[string]bool{
	"bash": true,
	"zsh":  true,
	"sh":   true,
	"fish": true,
	"ksh":  true,
	"tcsh": true,
	"csh":  true,
}

// Known terminal emulators
var terminalEmulators = map[string]bool{
	"gnome-terminal": true,
	"konsole":        true,
	"xterm":          true,
	"terminal":       true,
	"iTerm":          true,
	"iTerm2":         true,
	"ghostty":        true,
}

// getProcessName returns the name of a process
func getProcessName(pid int) (string, error) {
	fields, err := readProcStat(pid)
	if err != nil {
		return "", err
	}

	// Process name is the second field (index 1)
	if len(fields) < 2 {
		return "", errors.New("stat file has insufficient fields")
	}

	return fields[1], nil
}

// isInteractiveShell checks if a process name is a known interactive shell
func isInteractiveShell(processName string) bool {
	// Extract just the binary name if it's a full path
	baseName := filepath.Base(processName)
	return interactiveShells[baseName]
}

// isTerminalEmulator checks if a process name is a known terminal emulator
func isTerminalEmulator(processName string) bool {
	// Extract just the binary name if it's a full path
	baseName := filepath.Base(processName)
	return terminalEmulators[baseName]
}

// isSSHSession checks if a process name matches SSH session pattern
func isSSHSession(processName string) bool {
	// SSH sessions typically have pattern: "sshd: username@pts/"
	return strings.HasPrefix(processName, "sshd:") && strings.Contains(processName, "@pts/")
}

// walkParentChain walks up the parent process tree and checks each process
func walkParentChain(pid int, maxDepth int) (bool, error) {
	currentPID := pid
	depth := 0

	for depth < maxDepth && currentPID > 1 {
		// Get process name
		processName, err := getProcessName(currentPID)
		if err != nil {
			// If we can't read the process, it might have exited
			// Continue with parent if we can get it
			if ppid, ppidErr := getPPID(currentPID); ppidErr == nil {
				currentPID = ppid
				depth++
				continue
			}
			return false, err
		}

		// Check if this process indicates human interaction
		if isInteractiveShell(processName) || isTerminalEmulator(processName) || isSSHSession(processName) {
			return true, nil
		}

		// Get parent PID
		ppid, err := getPPID(currentPID)
		if err != nil {
			return false, err
		}

		// Check if we've reached init
		if ppid <= 1 {
			break
		}

		currentPID = ppid
		depth++
	}

	return false, nil
}

// isUserShell determines if a process was initiated by a human user
func isUserShell(pid int) (bool, error) {
	// Check if process exists
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); os.IsNotExist(err) {
		return false, fmt.Errorf("process %d not found", pid)
	}

	// Check for controlling terminal
	ttyNr, err := getControllingTerminal(pid)
	if err != nil {
		// If we can't read the stat file due to permissions, assume not human-initiated
		if os.IsPermission(err) {
			return false, nil
		}
		return false, err
	}

	// No controlling terminal means likely not human-initiated
	if ttyNr == 0 {
		return false, nil
	}

	// Also check if fd/0 points to a terminal (as an additional check)
	isTerminal, err := isTerminalFD(pid)
	if err != nil && !os.IsPermission(err) {
		// Only fail on non-permission errors
		return false, err
	}

	// Check the parent process chain (limit depth to prevent infinite loops)
	hasInteractiveParent, err := walkParentChain(pid, 50)
	if err != nil {
		return false, err
	}

	// A process is considered human-initiated if:
	// - It has a controlling terminal (ttyNr > 0) OR fd/0 is a terminal AND
	// - Its parent chain includes an interactive shell, SSH session, or terminal emulator
	return (ttyNr > 0 || isTerminal) && hasInteractiveParent, nil
}

// IsKernelProcess returns true if the process is a kernel process.
func IsKernelProcess(pid int) (bool, error) {
	// Read the status file directly from /proc
	statusContent, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return false, fmt.Errorf("failed to read status: %w", err)
	}

	// Look for VmSize in the status file
	lines := strings.SplitSeq(string(statusContent), "\n")
	for line := range lines {
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
