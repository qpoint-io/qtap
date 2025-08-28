package dns

import (
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/telemetry/metrics"
	"go.uber.org/zap"
)

type key struct {
	Addr      [16]byte
	Container string
}

type DNSManager struct {
	// zap logger
	logger *zap.Logger

	// process manager
	processManager *process.Manager

	// records cache
	records *synq.TTLCache[key, *Record]
}

func NewDNSManager(logger *zap.Logger, processManager *process.Manager) *DNSManager {
	d := &DNSManager{
		logger:         logger,
		processManager: processManager,
		records:        synq.NewTTLCache[key, *Record](5*time.Minute, 5*time.Minute),
	}

	return d
}

func (m *DNSManager) Start() error {
	metrics.NewGaugeFunc("records_cached", "The number of DNS records currently held in the cache",
		func() float64 {
			return float64(m.records.Len())
		},
	)

	return nil
}

func (m *DNSManager) Get(addr [16]byte, containerID string) *Record {
	// create a key
	k := key{
		Addr:      addr,
		Container: containerID,
	}

	// fetch the record if it exists
	record, _ := m.records.Load(k)
	return record
}

func (m *DNSManager) Set(record *Record, pid int) {
	// find the process
	proc := m.processManager.Get(pid)

	// nothing to do if we don't have a process
	if proc == nil {
		return
	}

	// set by container
	m.set(record, proc.ContainerID)
}

func (m *DNSManager) Lookup(addr [16]byte, containerID string) (*Record, error) {
	// convert addr to ip
	ip := net.IP(addr[:])

	// grab an IP string
	ipString := ip.String()

	// lookup domains
	domains, err := net.LookupAddr(ipString)

	// bubble error if exists
	if err != nil {
		return nil, fmt.Errorf("resolving domain from ip: %w", err)
	}

	// ensure we have at least one domain
	if len(domains) == 0 {
		return nil, nil
	}

	// determine the address family
	var saFamily uint16

	if ip.To4() != nil {
		saFamily = syscall.AF_INET // IPv4
	} else if len(ip) == net.IPv6len {
		saFamily = syscall.AF_INET6 // IPv6
	}

	// create a record from the first domain in the list
	record := Record{
		SaFamily: saFamily,
		Addr:     addr,
		Domain:   domains[0],
	}

	// persist on the container for the next time
	m.set(&record, containerID)

	return &record, nil
}

func (m *DNSManager) Stop() error {
	// stop the expirable
	m.records.Stop()

	return nil
}

func (m *DNSManager) set(record *Record, containerID string) {
	// create a key
	k := key{
		Addr:      record.Addr,
		Container: containerID,
	}

	// does this exist?
	_, exists := m.records.Load(k)

	// set if it doesn't exist, otherwise renew the timeout lease
	if !exists {
		m.records.Store(k, record)
	} else {
		m.records.Renew(k)
	}
}
