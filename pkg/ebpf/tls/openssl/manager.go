package openssl

import (
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/qpoint-io/qtap/pkg/ebpf/common"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
	"go.uber.org/zap"
)

// openssl symbols
var opensslSymbols = []string{
	"SSL_read",
	"SSL_write",
	"SSL_read_ex",
	"SSL_write_ex",
}

var symbolScan []binutils.SymbolSearch

func init() {
	// init symbol scan
	symbolScan = make([]binutils.SymbolSearch, len(opensslSymbols))
	for i, symbol := range opensslSymbols {
		symbolScan[i] = binutils.SymbolSearch{
			Name:          symbol,
			MatchStrategy: binutils.MatchStrategyExact,
		}
	}
}

// cache for openssl
type ScanResult struct {
	ContainsLibSSL bool
	Symbols        []elf.Symbol
}

type OpenSSLManager struct {
	// logger
	logger *zap.Logger

	// probe creator function
	probeFn func() []*common.Uprobe

	// map of containers by containerID
	containers *synq.Map[string, *Container]

	// targets by pid
	targets *synq.Map[int, *OpenSSLTarget]

	// scan cache
	cache *synq.TTLCache[string, *ScanResult]

	// target versions
	targetVersions *synq.Map[int, string]

	// embed a default process observer
	process.DefaultObserver
}

func NewOpenSSLManager(logger *zap.Logger, probeFn func() []*common.Uprobe) *OpenSSLManager {
	return &OpenSSLManager{
		logger:         logger,
		probeFn:        probeFn,
		containers:     synq.NewMap[string, *Container](),
		targets:        synq.NewMap[int, *OpenSSLTarget](),
		cache:          synq.NewTTLCache[string, *ScanResult](5*time.Minute, 5*time.Minute),
		targetVersions: synq.NewMap[int, string](),
	}
}

func (m *OpenSSLManager) Start() error {
	metrics.NewSystemGaugeFunc("containers_tracked", "The number of containers currently being tracked",
		func() float64 {
			return float64(m.containers.Len())
		},
	)
	metrics.NewSystemGaugeFunc("targets_tracked", "The number of targets currently being tracked",
		func() float64 {
			return float64(m.targets.Len())
		},
	)

	return nil
}

func (m *OpenSSLManager) Stop() (err error) {
	// stop the TTL cache cleanup goroutine
	m.cache.Stop()

	// iterate through all of the containers and cleanup
	m.containers.Iter(func(_ string, c *Container) bool {
		if err = c.Cleanup(); err != nil {
			err = fmt.Errorf("cleaning container: %w", err)
			return false
		}
		return true
	})
	if err != nil {
		return err
	}

	// iterate through all of the targets and cleanup
	m.targets.Iter(func(_ int, t *OpenSSLTarget) bool {
		if err = t.Stop(); err != nil {
			err = fmt.Errorf("cleaning target: %w", err)
			return false
		}
		return true
	})

	return err
}

func (m *OpenSSLManager) ProcessStarted(p *process.Process) error {
	// get the cache key
	cacheKey := p.CacheKey()

	// do we already have a target for this process?
	if version, exists := m.targetVersions.Load(p.Pid); exists && version == cacheKey {
		return nil
	}

	// fetch the container
	container, exists := m.containers.Load(p.ContainerID)

	// init container
	if !exists {
		container = NewContainer(m.logger, m.probeFn)
		m.containers.Store(p.ContainerID, container)

		// initialize the container
		go func() {
			if err := container.Init(p); err != nil {
				m.logger.Error("initializing container", zap.Error(err))
			}
		}()
	}

	// increment the container process count
	container.AddProcess(p.Pid)

	// if the container has OpenSSL libraries, add as detected
	if container.HasOpenSSL() {
		p.AddDetectedTLSProbeType("openssl")
	}

	// determine if this process has statically linked libssl
	staticOpenSSL := false
	var err error

	// fetch the cache
	cache, cacheExists := m.cache.Load(cacheKey)
	if cacheExists {
		// renew the expiration
		m.cache.Renew(cacheKey)

		// if the cache doesn't contain libssl, return early
		if !cache.ContainsLibSSL {
			return nil
		} else {
			staticOpenSSL = true
		}
	}

	if !staticOpenSSL {
		// detect if this process has statically linked libssl
		staticOpenSSL, err = m.detectStaticallyLinkedLibssl(p)
		if err != nil {
			return fmt.Errorf("detecting statically linked libssl: %w", err)
		}
	}

	// if this process has statically linked libssl, add it to the targets map
	if staticOpenSSL {
		// create a cache entry
		if !cacheExists {
			cache = &ScanResult{
				ContainsLibSSL: true,
			}

			// set the cache
			m.cache.Store(cacheKey, cache)
		}

		ef, err := p.Elf()
		if err != nil {
			return fmt.Errorf("failed to get elf: %w", err)
		}

		// create a target
		target := NewOpenSSLTarget(m.logger, p.Exe, p.ContainerID, p.PidExe, ef, TargetTypeStatic, m.probeFn(), cache)

		// start the target
		if err := target.Start(); err != nil {
			return fmt.Errorf("starting openssl target: %w", err)
		}

		// did the cache key change? (happens when a process is replaced)
		if cacheKey != p.CacheKey() {
			// stop the target
			if err := target.Stop(); err != nil {
				return fmt.Errorf("stopping openssl target after process replaced: %w", err)
			}

			// if we created the cache entry, it's likely invalid
			if !cacheExists {
				m.cache.Delete(cacheKey)
			}

			return process.ErrProcessReplaced
		}

		// add the target to the targets map
		m.targets.Store(p.Pid, target)

		// set the target version
		m.targetVersions.Store(p.Pid, cacheKey)

		// register that the process has been detected to contain openssl probe endpoints
		p.AddDetectedTLSProbeType("openssl")

		// debug
		m.logger.Debug("OpenSSL static symbols detected",
			zap.String("exe", p.Exe),
			zap.String("container_id", p.ContainerID),
			zap.Int("pid", p.Pid),
		)
	} else {
		// set the cache
		m.cache.Store(cacheKey, &ScanResult{
			ContainsLibSSL: false,
		})
	}

	return nil
}

func (m *OpenSSLManager) ProcessReplaced(p *process.Process) error {
	// do we have a target version for this process?
	version, versionExists := m.targetVersions.Load(p.Pid)

	// nothing to do if we don't have a target version
	if !versionExists {
		return nil
	}

	// if the version we installed matches the new cache key, nothing to do
	if version == p.CacheKey() {
		return nil
	}

	// stop the target
	if target, exists := m.targets.Load(p.Pid); exists {
		if err := target.Stop(); err != nil {
			return fmt.Errorf("stopping openssl target: %w", err)
		}

		// remove the target
		m.targets.Delete(p.Pid)
	}

	// delete the target version
	m.targetVersions.Delete(p.Pid)

	return nil
}

func (m *OpenSSLManager) ProcessStopped(p *process.Process) error {
	// fetch the container
	container, exists := m.containers.Load(p.ContainerID)

	// we don't have a corresponding container for some reason
	if !exists {
		return nil
	}

	// decrement the process count
	container.RemoveProcess(p.Pid)

	// are we empty?
	if container.IsEmpty() {
		// unlink probes, etc
		if err := container.Cleanup(); err != nil {
			return err
		}

		// remove the container
		m.containers.Delete(p.ContainerID)
	}

	// do we have a target?
	target, exists := m.targets.Load(p.Pid)
	if exists {
		// stop the target
		if err := target.Stop(); err != nil {
			return fmt.Errorf("stopping openssl target: %w", err)
		}

		// remove the target
		m.targets.Delete(p.Pid)
	}

	// remove the target version
	m.targetVersions.Delete(p.Pid)

	return nil
}

func (m *OpenSSLManager) detectStaticallyLinkedLibssl(proc *process.Process) (bool, error) {
	// get the pid of this running process
	if os.Getpid() == proc.Pid {
		return false, nil
	}

	// ignore if this is a kernel process
	if proc.Exe == "" {
		return false, nil
	}

	// get the elf file
	e, err := proc.Elf()
	if err != nil {
		return false, fmt.Errorf("failed to get elf: %w", err)
	}

	// find the symbols
	matches, err := e.SearchSymbols(symbolScan, elf.SHT_SYMTAB)
	if err != nil && !errors.Is(err, binutils.ErrNoSymbols) {
		m.logger.Debug("failed to search for SSL symbols",
			zap.String("exe", proc.Exe),
			zap.Error(err),
		)
	}

	// if no matches, return false
	if len(matches) == 0 {
		return false, nil
	}

	// we need to detect if any of the matches are actually embedded in the binary
	for _, match := range matches {
		if match.Value != 0 && match.Size != 0 && match.Library == "" {
			return true, nil
		}
	}

	return false, nil
}
