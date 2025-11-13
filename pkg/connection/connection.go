package connection

import (
	"context"
	"errors"
	"maps"
	"net"
	"strings"
	"sync"
	"time"

	"fmt"
	"strconv"

	"github.com/qpoint-io/qtap/pkg/dns"
	"github.com/qpoint-io/qtap/pkg/labels"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/qnet"
	servicespkg "github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/tags"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"github.com/qpoint-io/qtap/pkg/tlsutils"
	"github.com/rs/xid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var tracer = telemetry.Tracer()

type services interface {
	finalizeConnection(conn *Connection)
	createStreamer(conn *Connection) StreamProcessor
}

type ControlManager interface {
	Control(conn *Connection)
	Delete(conn *Connection) error
}

type ErrStreamUnrecoverable error

type Connection struct {
	mu     sync.Mutex
	logger *zap.Logger
	// connecting reporting system
	report

	// lifecycle management
	cancel    context.CancelFunc
	startOnce sync.Once

	// dependencies
	services           services
	svcFactoryRegistry *servicespkg.FactoryRegistry
	svcRegistry        *servicespkg.ServiceRegistry

	controlManager  ControlManager
	streamProcessor StreamProcessor
	dnsRecord       *dns.Record

	// connection properties
	id string

	// held indicates that another claimant is holding the close condition for the connection
	held bool

	// keys
	cookie     Cookie
	connPIDKey ConnPIDKey

	Protocol Protocol

	// TLS
	IsTLS          bool
	TLSClientHello *tlsutils.ClientHello

	// eventQueue handles events coming from the socket reader
	eventQueue          *synq.Queue
	OpenEvent           *OpenEvent
	CloseEvent          *CloseEvent
	OriginalDestination *qnet.NetAddr
	HandlerType         HandlerType

	// internal domain
	domain     string
	domainIsIP bool

	// internal process
	process *process.Process

	// skipping stream processing
	skipStreamProcessing       bool
	skipStreamProcessingReason string

	// tags & labels
	tags   tags.List
	labels labels.Set
}

type ConnOpt func(c *Connection)

func WithProcess(process *process.Process) ConnOpt {
	return func(c *Connection) {
		c.setProcess(process)
	}
}

func WithDNSRecord(dnsRecord *dns.Record) ConnOpt {
	return func(c *Connection) {
		c.dnsRecord = dnsRecord
	}
}

func WithServices(services services) ConnOpt {
	return func(c *Connection) {
		c.services = services
	}
}

func WithTags(t tags.List) ConnOpt {
	return func(c *Connection) {
		if t == nil {
			return
		}

		if c.tags == nil {
			c.tags = t.Clone()
		}

		c.tags.Merge(t)
	}
}

func WithControlManager(controlManager ControlManager) ConnOpt {
	return func(c *Connection) {
		c.controlManager = controlManager
	}
}

func WithServiceFactoryRegistry(fr *servicespkg.FactoryRegistry) ConnOpt {
	return func(c *Connection) {
		c.svcFactoryRegistry = fr
	}
}

func NewConnection(ctx context.Context, logger *zap.Logger, openEvent *OpenEvent, opts ...ConnOpt) *Connection {
	ctx, cancel := context.WithCancel(ctx)
	ctx, span := tracer.Start(ctx, "Connection")

	handlerType := HandlerType_RAW
	if openEvent.IsRedirected {
		handlerType = HandlerType_REDIRECTED
	}

	id := xid.New().String()
	span.SetAttributes(
		attribute.String("connection.id", id),
		attribute.Int64("connection.cookie", int64(openEvent.Cookie)),
	)

	t := tags.New()
	// TODO: is this what we actually want here?
	// if openEvent.Source == Client {
	// 	t.Add("ip", openEvent.Local.IP.String())
	// } else {
	// 	t.Add("ip", openEvent.Remote.IP.String())
	// }
	t.Add("ip", openEvent.Local.IP.String())

	logger = logger.With(zap.String("conn_id", id), zap.Any("cookie", openEvent.Cookie))
	c := &Connection{
		report: report{
			ctx: ctx,
		},
		cancel:      cancel,
		logger:      logger,
		id:          id,
		cookie:      openEvent.Cookie,
		connPIDKey:  openEvent.ConnPIDKey,
		held:        openEvent.IsRedirected,
		OpenEvent:   openEvent,
		eventQueue:  synq.NewQueue(ctx),
		HandlerType: handlerType,
		tags:        t,
		labels:      labels.New(),
	}

	// apply options
	for _, opt := range opts {
		opt(c)
	}

	c.createServiceRegistry()
	return c
}

func (c *Connection) createServiceRegistry() {
	if c.svcFactoryRegistry == nil {
		c.svcFactoryRegistry = servicespkg.NewFactoryRegistry() // an empty registry that will return "not found" errors
	}

	c.svcRegistry = servicespkg.NewServiceRegistry(c.svcFactoryRegistry)
	c.svcRegistry.SetConfigurator(func(ctx context.Context, service servicespkg.Service) (servicespkg.Service, error) {
		// apply adapters
		if l, ok := service.(servicespkg.LoggerAdapter); ok {
			l.SetLogger(c.logger.With(zap.Stringer("service", service.ServiceType())))
		}

		if ca, ok := service.(ConnectionAdapter); ok {
			ca.SetConnection(c)
		}

		if es, ok := service.(eventstore.EventStore); ok {
			// if this is an event store, wrap it with the meta injector
			service = &EventStoreMetaInjector{
				Conn:       c,
				EventStore: es,
			}
		}

		// return the service
		return service, nil
	})
}

func (c *Connection) ServiceRegistry() *servicespkg.ServiceRegistry {
	return c.svcRegistry
}

func (c *Connection) SetProcess(process *process.Process) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.setProcess(process)
}

func (c *Connection) Process() *process.Process {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.process
}

// setProcess sets the process and adds tags to the connection.
// Note that this is not thread safe.
func (c *Connection) setProcess(process *process.Process) {
	c.process = process
	if process == nil {
		return
	}

	// add tags
	if c.tags == nil {
		c.tags = tags.New()
	}

	c.tags.Merge(c.process.Tags())

	// TODO: the tags below should be added to the processes
	// tag list and merged (see above).
	c.tags.Add("bin", c.process.Binary)
	c.tags.Add("strategy", c.process.Strategy.String())
	if hostname, _ := c.process.Hostname(); hostname != "" {
		if c.process.PodID != "" {
			c.tags.Add("pod", hostname)

			parts := strings.Split(hostname, "-")
			if len(parts) > 0 {
				c.tags.Add("app", parts[0])
			}
		} else {
			c.tags.Add("host", hostname)
		}
	}

	// add labels
	userShell, err := c.process.UserShell()
	if err != nil {
		c.logger.Warn("failed to process user shellindication check", zap.Error(err))
	} else if userShell {
		c.labels.Add("user-shell")
	}
}

// Open initializes the connection monitoring
func (c *Connection) Open() {
	c.startOnce.Do(func() {
		c.logger.Debug("opening connection")

		// report metrics
		connOpenTotal.WithLabelValues(c.OpenEvent.Remote.IP.String(), strconv.Itoa(int(c.OpenEvent.Remote.Port)), c.Direction()).Inc()
		connActiveTotal.WithLabelValues(c.OpenEvent.Remote.IP.String(), strconv.Itoa(int(c.OpenEvent.Remote.Port)), c.Direction()).Inc()

		// Check that the process was redirected if this processes connections
		// are intended to be forwarded/proxied.
		if c.process != nil {
			if (c.process.Strategy == process.StrategyForward || c.process.Strategy == process.StrategyProxy) && !c.OpenEvent.IsRedirected {
				c.logger.Warn("process is configured to forward/proxy but connection was not redirected",
					zap.String("process", c.process.Exe))
			}
			c.logger = c.logger.With(zap.String("exe", c.process.Exe))
		}

		// Start monitoring
		go c.watch()
	})
}

func (c *Connection) setupReporters() {
	// start all configured reporter services
	//
	// NOTE(kamal): this is kind of hacky, but we need to do this because:
	// 	1. whether a connection should be reported or not is determined by the connection manager
	//  2. services are created on-demand if requested from the registry
	for _, key := range c.svcRegistry.AvailableServicesForType(servicespkg.ServiceType("reporter")) {
		// getting the reporter service is enough - it will start itself on creation
		_, err := servicespkg.GetService[servicespkg.Service](c.ctx, c.svcRegistry, key.Type, key.ID)
		if err != nil {
			c.logger.Error("failed to get reporter service", zap.Error(err))
			continue
		}
	}
}

func (c *Connection) ID() string {
	return c.id
}

func (c *Connection) CreatedAt() time.Time {
	return c.report.openTime
}

func (c *Connection) ClosedAt() *time.Time {
	if c.report.closeTime.IsZero() {
		return nil
	}

	return &c.report.closeTime
}

func (c *Connection) watch() {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("panic in connection event loop", zap.Any("panic", r))
		}
	}()

	if c.controlManager != nil {
		// evaluate control rules following the open event
		c.controlManager.Control(c)
	}

	for {
		event, hasMore := c.eventQueue.Next()
		if !hasMore {
			break
		}
		c.processEvent(event)

		if c.controlManager != nil {
			go c.controlManager.Control(c)
		}
	}
}

func (c *Connection) Close() {
	defer c.cancel()

	// report metrics
	connCloseTotal.WithLabelValues(c.OpenEvent.Remote.IP.String(), strconv.Itoa(int(c.OpenEvent.Remote.Port)), c.Direction()).Inc()
	connActiveTotal.WithLabelValues(c.OpenEvent.Remote.IP.String(), strconv.Itoa(int(c.OpenEvent.Remote.Port)), c.Direction()).Dec()
	connDuration.WithLabelValues(c.OpenEvent.Remote.IP.String(), strconv.Itoa(int(c.OpenEvent.Remote.Port))).Observe(float64(c.report.closeTime.Sub(c.report.openTime).Milliseconds()))
	connBytesSentTotal.WithLabelValues(c.OpenEvent.Remote.IP.String(), strconv.Itoa(int(c.OpenEvent.Remote.Port)), c.Direction()).Add(float64(c.CloseEvent.WrBytes))
	connBytesRecvTotal.WithLabelValues(c.OpenEvent.Remote.IP.String(), strconv.Itoa(int(c.OpenEvent.Remote.Port)), c.Direction()).Add(float64(c.CloseEvent.RdBytes))

	span := trace.SpanFromContext(c.ctx)
	defer span.End()

	c.logger.Debug("closing connection")

	// removes itself from the pool of connections
	c.services.finalizeConnection(c)

	// process any remaining events in the queue (this is blocking)
	if err := c.eventQueue.Drain(3 * time.Second); err != nil {
		c.logger.Warn("failed to drain event queue", zap.Error(err))
	}

	// close the event queue
	if err := c.eventQueue.Close(); err != nil {
		c.logger.Error("error closing pid queue", zap.Error(err))
	}

	// close the stream processor
	if c.streamProcessor != nil {
		c.streamProcessor.Close()
	}

	if c.controlManager != nil {
		if err := c.controlManager.Delete(c); err != nil {
			c.logger.Warn("error deleting connection from control", zap.Error(err))
		}
	}

	// close all connection services
	// this will also close the reporter service, sending a final report to the event store
	if err := c.svcRegistry.Close(); err != nil {
		c.logger.Error("error closing service registry", zap.Error(err))
	}
}

func (c *Connection) SetDomain(input string) {
	if input == "" {
		return
	}

	if !c.domainIsIP && len(c.domain) > 0 {
		return
	}

	// parse the domain or IP
	domain, _, domainIsIP := parseHostString(input)

	// if the domain is empty, return
	if domain == "" {
		return
	}

	// don't allow an IP to replace a domain
	if domainIsIP && !c.domainIsIP {
		return
	}

	// don't allow the same domain to be set twice
	if strings.EqualFold(domain, c.domain) {
		return
	}

	// set the domain
	c.domain = domain
	c.domainIsIP = domainIsIP

	// add to logger
	c.logger = c.logger.With(zap.String("domain", domain))
}

func (c *Connection) Domain() string {
	// if domain is already set (and NOT an IP), return it
	if c.domain != "" && !c.domainIsIP {
		return c.domain
	}

	// identify the destination address
	var dstAddr qnet.NetAddr

	if c.OpenEvent != nil {
		// client vs server
		switch c.OpenEvent.Source {
		case Client:
			dstAddr = c.OpenEvent.Remote
		case Server:
			dstAddr = c.OpenEvent.Local
		}
	}

	if c.dnsRecord != nil {
		// set domain from the record
		c.domain = c.dnsRecord.Domain
		c.domainIsIP = false
	}

	// if we still don't have a domain, set it to the destination IP
	if c.domain == "" {
		// if we have an original destination, use that
		if c.OriginalDestination != nil {
			c.domain = c.OriginalDestination.IP.String()
		} else {
			c.domain = dstAddr.IP.String()
		}
		c.domainIsIP = true
	}

	// add to logger. use an object marshaler func for lazy evaluation
	c.logger = c.logger.With(zap.Inline(zapcore.ObjectMarshalerFunc(func(enc zapcore.ObjectEncoder) error {
		enc.AddString("domain", c.domain)
		return nil
	})))

	// return from the cache
	return c.domain
}

func (c *Connection) Direction() string {
	if c.OpenEvent == nil {
		return ""
	}

	// client vs server
	switch c.OpenEvent.Source {
	case Client:
		if c.Destination().IP.IsPrivate() {
			return "egress-internal"
		} else {
			return "egress-external"
		}
	default:
		return "ingress"
	}
}

func (c *Connection) Proto() string {
	return string(c.Protocol)
}

// Destination returns the original destination address of the connection
func (c *Connection) Destination() qnet.NetAddr {
	if c.OriginalDestination != nil {
		return *c.OriginalDestination
	}

	if c.OpenEvent != nil {
		return c.OpenEvent.Remote
	}

	return qnet.NetAddr{}
}

func (c *Connection) Logger() *zap.Logger {
	return c.logger
}

func (c *Connection) Tags() tags.List {
	return c.tags
}

func (c *Connection) Labels() labels.Set {
	return c.labels
}

func (c *Connection) Context() context.Context {
	if c.ctx == nil {
		return context.Background()
	}

	return c.ctx
}

func (c *Connection) Cookie() Cookie {
	return c.cookie
}

// ControlValues returns the values that are used to evaluate the control rules
// NOTE: please make sure to only use value types that are supported by rulekit.
func (c *Connection) ControlValues() map[string]any {
	var (
		src = map[string]any{}
		dst = map[string]any{}
	)

	v := map[string]any{
		"protocol": c.Proto(),

		"src": src,
		"dst": dst,
	}

	if d := c.Direction(); d != "" {
		v["direction"] = d
	}

	tags := c.Tags()
	if tags != nil {
		v["tags"] = tags.List()
	}

	if c.OpenEvent != nil {
		maps.Copy(src, c.OpenEvent.Local.ControlValues())

		if c.OpenEvent.SocketType != SocketType_UNKNOWN {
			v["type"] = string(c.OpenEvent.SocketType)
		}
	}

	if t := c.TLSClientHello; t != nil {
		v["tls"] = t.ControlValues()
	}

	// src
	if p := c.Process(); p != nil {
		src["process"] = p.ControlValues()

		if container, _ := p.Container(); container != nil && container.ID != "" {
			src["container"] = container.ControlValues()
			if pod, _ := p.Pod(); pod != nil && pod.Name != "" {
				src["pod"] = pod.ControlValues()
			}
		}
	}

	// dst
	if d := c.Destination(); !d.Empty() {
		maps.Copy(dst, d.ControlValues())
	}

	if h := c.Domain(); h != "" && !c.domainIsIP {
		dst["domain"] = h
	}

	return v
}

func (m *Manager) shouldReport(conn *Connection) (bool, error) {
	if conn.HandlerType == HandlerType_FORWARDING {
		return false, errors.New("forwarding connection detected")
	}

	// if this is DNS, ensure we're wanted
	if m.config != nil && conn.Protocol == Protocol_DNS && !m.config.Tap.AuditIncludeDNS {
		return false, errors.New("DNS audit log disabled")
	}

	return true, nil
}

func parseHostString(input string) (string, string, bool) {
	// Remove any leading/trailing whitespace
	input = strings.TrimSpace(input)

	var host string
	var port string

	// Check if the input contains a port
	_host, _port, err := net.SplitHostPort(input)
	if err == nil {
		// Validate port
		if validPort, err := validatePort(_port); err == nil {
			port = validPort
			host = _host
		} else {
			// Invalid port, but host might still be valid
			host = _host
		}
	} else {
		// If there's no port, the entire input is the host
		host = input
	}

	// Check if the host is a valid IP address
	if ip := net.ParseIP(host); ip != nil {
		return host, port, true
	}

	// Validate domain name
	if isValidDomain(host) {
		return host, port, false
	}

	// Invalid host
	return "", "", false
}

// validatePort checks if the port string is valid and returns it if it is
func validatePort(port string) (string, error) {
	// Convert port to integer
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return "", err
	}

	// Check if port is in valid range (1-65535)
	if portNum < 1 || portNum > 65535 {
		return "", fmt.Errorf("port %d out of range", portNum)
	}

	return port, nil
}

// isValidDomain checks if the string could be a valid domain name
func isValidDomain(domain string) bool {
	// Empty domains are invalid
	if domain == "" {
		return false
	}

	// Max length of a domain name is 253 characters
	if len(domain) > 253 {
		return false
	}

	// Split domain into labels
	labels := strings.Split(domain, ".")

	// Domain must have at least one label
	if len(labels) == 0 {
		return false
	}

	// Check each label
	for _, label := range labels {
		// Label length must be between 1 and 63 characters
		if len(label) == 0 || len(label) > 63 {
			return false
		}

		// First and last character must be alphanumeric
		if !isAlphanumeric(rune(label[0])) || !isAlphanumeric(rune(label[len(label)-1])) {
			return false
		}

		// Check each character in the label
		for _, ch := range label {
			if !isValidDomainChar(ch) {
				return false
			}
		}
	}

	return true
}

// isValidDomainChar checks if a character is valid in a domain name
func isValidDomainChar(ch rune) bool {
	return isAlphanumeric(ch) || ch == '-'
}

// isAlphanumeric checks if a character is a letter or digit
func isAlphanumeric(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

type ConnectionAdapter interface {
	SetConnection(*Connection)
}

// EventStoreMetaInjector implements the event store interface and adds connection metadata to events
// before they are submitted to the event store.
type EventStoreMetaInjector struct {
	Conn       *Connection
	EventStore eventstore.EventStore
}

func (e *EventStoreMetaInjector) Save(ctx context.Context, item any) {
	if item != nil && e.Conn != nil {
		if c, ok := item.(connidable); ok {
			c.SetConnectionID(e.Conn.ID())
		}

		if t, ok := item.(taggable); ok {
			t.AddTags(e.Conn.Tags().List()...)
		}
	}

	e.EventStore.Save(ctx, item)
}

func (e *EventStoreMetaInjector) ServiceType() servicespkg.ServiceType {
	return eventstore.TypeEventStore
}

type connidable interface {
	SetConnectionID(id string)
}

type taggable interface {
	AddTags(tag ...string)
}
