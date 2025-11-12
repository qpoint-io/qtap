package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kamaln7/resolvable"
	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/tags"
	"go.uber.org/zap"
)

var (
	podRe       *regexp.Regexp
	containerRe *regexp.Regexp

	ErrProcessReplaced = errors.New("process replaced")
	ErrUnknownProcess  = errors.New("unknown process")
)

func init() {
	// pod ID regular expression
	podRe = regexp.MustCompile(`pod([0-9a-f]{8}[-_][0-9a-f]{4}[-_][0-9a-f]{4}[-_][0-9a-f]{4}[-_][0-9a-f]{12}\b)`)

	// container ID regular expression
	containerRe = regexp.MustCompile(`(\b[0-9a-f]{64}\b)`)
}

// unknown process error
type UnknownProcessError struct {
	Message string
}

// process does not exist with pid
func (e UnknownProcessError) Error() string {
	return e.Message
}

type Process struct {
	Pid            int
	PidExe         string // PidExe is the path to the /proc process symlink
	PodID          string // TODO: remove
	Cgroup         string
	ContainerID    string
	RootID         uint64
	Binary         string
	Exe            string
	ExeFilename    string // ExeFilename is the path to the file that was called by the syscall. It can be empty.
	Args           []string
	Root           string
	Env            map[string]string
	Strategy       QpointStrategy
	PredatesQpoint bool
	User           resolvable.V[*ProcessUser]
	UserShell      resolvable.V[bool]
	ExitCode       int

	// TLSProbeTypesDetected are the the probes that have scanned the process binary and found matching hooks.
	TLSProbeTypesDetected []string

	Container resolvable.V[*Container]
	Pod       resolvable.V[*Pod]

	// internal
	logger     *zap.Logger
	hostname   string
	filter     uint8
	elf        *binutils.Elf
	exited     atomic.Bool
	tlsOk      bool
	startTime  time.Time
	closeTime  *time.Time
	mu         sync.Mutex
	scanMu     sync.Mutex
	scanWg     sync.WaitGroup
	scanCtx    context.Context
	scanCancel context.CancelFunc
	tags       tags.List
	envTags    []config.Tag
	closers    []io.Closer

	// notifier is called when parts of the process change
	// that are required to be updated by the eventer for
	// other systems to handle how they load the process
	//
	// eg. setting tlsOk in eBPF map so the egress forwarder
	// knows to forward connections from this process
	notifier func() error
}

func NewProcess(pid int, exeFilename string, logger *zap.Logger) *Process {
	p := &Process{
		Pid:         pid,
		PidExe:      fmt.Sprintf("/proc/%d/exe", pid),
		ExeFilename: exeFilename,
		startTime:   time.Now(),
		Container:   resolvable.Static[*Container](nil).WithBackgroundContext(),
		Pod:         resolvable.Static[*Pod](nil).WithBackgroundContext(),
		logger:      logger,
	}
	p.User = resolvable.New(func(context.Context) (*ProcessUser, error) {
		return GetProcessUser(p.Pid)
	}, resolvable.WithRetry()).WithBackgroundContext()

	p.UserShell = resolvable.New(func(context.Context) (bool, error) {
		// check if the process was created by a user shell
		u, err := isUserShell(p.Pid)
		if err != nil {
			return false, err
		}

		return u, nil
	}, resolvable.WithRetry()).WithBackgroundContext()
	return p
}

func AllProcesses(logger *zap.Logger) ([]*Process, error) {
	ps, err := AllProcs("/proc")
	if err != nil {
		return nil, fmt.Errorf("reading /proc: %w", err)
	}

	procs := make([]*Process, 0, len(ps))

	for _, p := range ps {
		// Check if it's a kernel process
		isKernel, err := IsKernelProcess(p)
		if err != nil {
			// Log the error and continue with the next process
			fmt.Printf("Error checking process %d: %v\n", p, err)
			continue
		}
		if isKernel {
			continue
		}

		procs = append(procs, NewProcess(int(p), "", logger))
	}

	return procs, nil
}

func (p *Process) Discover(mountPoint string, envMask *synq.Map[string, bool]) error {
	// extract the executable
	exe, err := Executable(p.Pid)
	if err != nil {
		return fmt.Errorf("extracting executable: %w", err)
	}
	p.Exe = exe

	// apply process filters
	p.filter = applyFilters(p)

	// set binary
	p.Binary = filepath.Base(p.Exe)

	// set the root path
	if p.Root == "" {
		p.Root = filepath.Join(mountPoint, strconv.Itoa(p.Pid), "root")
	}

	// determine cgroups
	if p.Cgroup == "" {
		cgroups, err := Cgroups(p.Pid)
		if err != nil {
			return fmt.Errorf("extracting cgroup information: %w", err)
		}
		if len(cgroups) > 0 {
			p.Cgroup = cgroups[0].Path
		}
	}

	if p.ContainerID == "" {
		// split the cgroups in hierarchy
		namespaces := strings.Split(p.Cgroup, "/")

		// iterate over the namespaces from the bottom up
		// (this is necessary because of nested hierarchies like DnD/KinD etc)
		for i := len(namespaces) - 1; i >= 0; i-- {
			// current namespace
			namespace := namespaces[i]

			// check for container ID
			if p.ContainerID == "" {
				if match := containerRe.FindStringSubmatch(namespace); match != nil {
					p.ContainerID = match[1][:12]
				}
			}

			// check for Pod ID
			if p.PodID == "" {
				if match := podRe.FindStringSubmatch(namespace); match != nil {
					p.PodID = strings.ReplaceAll(match[1], "_", "-")
				}
			}
		}

		// set the default container ID
		if p.ContainerID == "" {
			p.ContainerID = "root"
		}
	}

	// get the root ID
	if p.RootID == 0 {
		rootID, err := p.getRootID()
		if err != nil {
			return fmt.Errorf("getting root ID: %w", err)
		}
		p.RootID = rootID
	}

	// discover env vars that are masked
	if len(p.Env) == 0 && envMask != nil {
		env, err := Environ(p.Pid)
		if err != nil {
			return fmt.Errorf("failed to get environment variables: %w", err)
		}

		// initialize the env map
		p.Env = make(map[string]string)

		// iterate over the environment variables
		for _, envVar := range env {
			// split the environment variable into key and value
			parts := strings.SplitN(envVar, "=", 2)
			if len(parts) == 2 {
				// see if the mask has the key
				if _, ok := envMask.Load(parts[0]); ok {
					p.Env[parts[0]] = parts[1]
				}
			}
		}
	}

	// set the qpoint strategy
	// ALWAYS CHECK because the exe filter could change
	strategy, err := QpointStrategyFromString(p.Env[QpointStrategyEnvVar], p)
	if err != nil {
		// always fallback to observe
		strategy = StrategyObserve
	}
	p.Strategy = strategy

	// notify the eventer that the process has changed
	if p.notifier != nil {
		if err := p.notifier(); err != nil {
			return fmt.Errorf("calling eventer notifier: %w", err)
		}
	}

	return nil
}

func (p *Process) CacheKey() string {
	return p.ContainerID + "-" + p.Exe
}

func (p *Process) SetUser(uid uint, user string) {
	p.User = resolvable.Static(&ProcessUser{UID: uid, Username: user}).WithBackgroundContext()
}

func (p *Process) Hostname() (string, error) {
	if p.hostname == "" {
		// read th hostname within the container
		content, err := os.ReadFile(path.Join(p.Root, "/etc/hostname"))
		if err != nil {
			return "", fmt.Errorf("failed to read hostname file: %w", err)
		}

		// extract the content
		p.hostname = strings.TrimSpace(string(content))
	}

	// return from cache
	return p.hostname, nil
}

func (p *Process) SetHostname(hostname string) {
	p.hostname = hostname
}

func (p *Process) TlsOk() bool {
	return p.tlsOk
}

func (p *Process) SetTlsOk(tlsOk bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// if the value is the same, don't update
	if p.tlsOk == tlsOk {
		return nil
	}

	// set the tls ok
	p.tlsOk = tlsOk

	// notify the eventer that the process has changed
	if p.notifier != nil {
		if err := p.notifier(); err != nil {
			return fmt.Errorf("calling eventer notifier: %w", err)
		}
	}

	return nil
}

func (p *Process) RootFS() string {
	if c, err := p.Container(); err == nil && c != nil {
		return path.Join("/proc/1/root", c.RootFS)
	}
	return fmt.Sprintf("/proc/%d/root", p.Pid)
}

func (p *Process) FindSharedLibrary(libNamePrefix string) ([]string, error) {
	// there might be multiple matches
	var matches []string

	// we only want unique objects, not the symlinks
	var uniqueMatches []string

	// scan for matches
	scan := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasPrefix(filepath.Base(path), libNamePrefix) {
			matches = append(matches, path)
		}
		return nil
	}

	// scan the common lib directories
	for _, libDir := range []string{"/lib", "/usr/lib", "/usr/local/lib", "/nix/store"} {
		// absolute path to lib dir
		absLibDir := filepath.Join(p.Root, libDir)

		// walk directories and scan
		if err := filepath.Walk(absLibDir, scan); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("scanning for shared library: %w", err)
		}
	}

	// hold unique file identifiers
	unique := make(map[uint64]string)

	for _, path := range matches {
		fi, err := os.Stat(path)
		if err != nil {
			return nil, err
		}

		stat, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return nil, fmt.Errorf("failed to get syscall.Stat_t for %s", path)
		}

		// Use a combination of inode and device ID as a unique identifier
		id := stat.Ino + uint64(stat.Dev)<<32
		if _, exists := unique[id]; !exists {
			unique[id] = path
			uniqueMatches = append(uniqueMatches, path)
		}
	}

	// return any matches
	return uniqueMatches, nil
}

// Close closes the elf file
func (p *Process) Close() error {
	// mark the process as exited
	p.exited.Store(true)

	// close the closeables
	for _, closer := range p.closers {
		if err := closer.Close(); err != nil {
			return fmt.Errorf("closing closeable: %w", err)
		}
	}

	if p.notifier != nil {
		if err := p.notifier(); err != nil {
			return fmt.Errorf("calling eventer notifier: %w", err)
		}
	}

	return nil
}

func (p *Process) Exited() bool {
	return p.exited.Load()
}

// Elf returns the elf file
func (p *Process) Elf() (*binutils.Elf, error) {
	if p.elf == nil {
		var err error
		p.elf, err = binutils.NewElf(p.PidExe, "/", false)
		if err != nil {
			return nil, fmt.Errorf("failed to create elf: %w", err)
		}
	}

	return p.elf, nil
}

func (p *Process) CloseElf() error {
	if p.elf != nil {
		if err := p.elf.Close(); err != nil {
			return fmt.Errorf("closing elf: %w", err)
		}

		// unset the elf
		p.elf = nil
	}

	return nil
}

func (p *Process) Lock() {
	p.mu.Lock()
}

func (p *Process) Unlock() {
	p.mu.Unlock()
}

// StartScan cancels any in-progress scan, waits for it to finish, then returns a new context for the current scan.
// The caller MUST defer FinishScan() to signal completion.
func (p *Process) StartScan(ctx context.Context) (context.Context, error) {
	p.scanMu.Lock()
	defer p.scanMu.Unlock()

	// Cancel any existing scan
	if p.scanCancel != nil {
		p.scanCancel()
	}

	// Wait for the previous scan to fully exit
	// Note: FinishScan() only calls Done(), which doesn't need the lock,
	// so the cancelled goroutine can complete even though we hold the lock
	p.scanWg.Wait()

	// Now start the new scan
	p.scanWg.Add(1)
	p.scanCtx, p.scanCancel = context.WithCancel(ctx)
	return p.scanCtx, nil
}

// FinishScan signals that the current scan has completed.
// Must be called (typically via defer) after StartScan.
func (p *Process) FinishScan() {
	p.scanWg.Done()
}

// CancelScan cancels any in-progress scan without waiting
func (p *Process) CancelScan() {
	p.scanMu.Lock()
	defer p.scanMu.Unlock()

	if p.scanCancel != nil {
		p.scanCancel()
		p.scanCancel = nil
		p.scanCtx = nil
	}
}

func (p *Process) Tags() tags.List {
	if p.tags == nil {
		// initialize the tag list
		p.tags = tags.New()

		// discover the tags
		if err := p.discoverTags(); err != nil {
			p.logger.Error("failed to discover tags", zap.Error(err))
		}
	}

	// return a clone of the tag list
	return p.tags.Clone()
}

func (p *Process) discoverTags() error {
	// look for any custom tags in the QPOINT_TAGS environment variable
	if v, ok := p.Env[QpointTagsEnvVar]; ok {
		ts := strings.Split(v, ",")
		for _, t := range ts {
			if err := p.tags.AddString(t); err != nil {
				return fmt.Errorf("adding tag from QPOINT_TAGS environment variable: %w", err)
			}
		}
	}

	// check the environment for tags
	for _, t := range p.envTags {
		switch t.Source {
		case "env":
			if v, ok := p.Env[t.Location]; ok {
				p.tags.Add(t.Key, v)
			}
		case "k8s.label":
			if pod, _ := p.Pod(); pod != nil {
				for k, v := range pod.Labels {
					if k == t.Location {
						p.tags.Add(t.Key, v)
					}
				}
			}
		case "k8s.annotation":
			if pod, _ := p.Pod(); pod != nil {
				for k, v := range pod.Annotations {
					if k == t.Location {
						p.tags.Add(t.Key, v)
					}
				}
			}
		case "container.label":
			if container, _ := p.Container(); container != nil {
				for k, v := range container.Labels {
					if k == t.Location {
						p.tags.Add(t.Key, v)
					}
				}
			}
		}
	}

	return nil
}

// getRootID returns the unique identifier of the process' root filesystem
func (p *Process) getRootID() (uint64, error) {
	rootInfo, err := os.Stat(p.Root)
	if err != nil {
		return 0, fmt.Errorf("failed to stat %s: %w", p.Root, err)
	}

	stat, ok := rootInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("failed to get syscall.Stat_t for %s", p.Root)
	}

	// use uint64 for both Ino and Dev to ensure we capture the full range
	ino := uint64(stat.Ino)
	dev := uint64(stat.Dev)

	// combine device and inode for a unique identifier
	return (dev << 32) | (ino & 0xFFFFFFFF), nil
}

func (p *Process) checkProcessError(err error) (string, bool) {
	if p.Exited() {
		return "Process exited", true
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "fs.ErrNotExist", true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return "os.ErrProcessDone", true
	}
	if errors.Is(err, os.ErrPermission) {
		return "os.ErrPermission", true
	}
	if errors.Is(err, syscall.ESRCH) {
		return "syscall.ESRCH", true
	}
	if strings.Contains(err.Error(), "no such process") {
		return "no such process (string match)", true
	}
	if strings.Contains(err.Error(), "no such file or directory") {
		return "no such file or directory (string match)", true
	}
	return "", false
}

func (p *Process) AddCloser(c ...io.Closer) {
	p.closers = append(p.closers, c...)
}

func (p *Process) ControlValues() map[string]any {
	v := map[string]any{
		"path":   p.Exe,
		"binary": p.Binary,
	}

	if h, err := p.Hostname(); err == nil && h != "" {
		v["hostname"] = h
	}

	// user
	user := map[string]any{}
	if u, _ := p.User(); u != nil {
		user["id"] = u.UID
		user["name"] = u.Username
	}
	v["user"] = user

	// envs
	if len(p.Env) > 0 {
		env := make(map[string]any, len(p.Env))
		for k, v := range p.Env {
			env[k] = v
		}
		v["env"] = env
	}

	return v
}

func (p *Process) SetNotifier(n func() error) {
	p.notifier = n
}

func (p *Process) FullCmd() string {
	return strings.TrimSpace(p.Exe + " " + strings.Join(p.Args, " "))
}

func (p *Process) Filter() uint8 {
	return p.filter
}

func (p *Process) IsFiltered(flag ...config.FilterLevel) bool {
	if p.Exe == "" {
		return false
	}

	for _, f := range flag {
		if p.filter&f.Resolve() != 0 {
			return true
		}
	}

	return false
}

func (p *Process) AddDetectedTLSProbeType(t string) {
	if p.TLSProbeTypesDetected == nil {
		p.TLSProbeTypesDetected = make([]string, 0, 1)
	}

	p.TLSProbeTypesDetected = append(p.TLSProbeTypesDetected, t)
}

func (p *Process) CreatedAt() time.Time {
	return p.startTime
}

func (p *Process) ClosedAt() *time.Time {
	return p.closeTime
}
