package connection

import (
	"context"
	"maps"
	"net"
	"strings"
	"sync"
	"time"

	"fmt"
	"strconv"

	"github.com/qpoint-io/qtap/pkg/dns"
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

const (
	WarmupTimeout = 10 * time.Second
)

var tracer = telemetry.Tracer()

type services interface {
	generateAuditLog(conn *Connection)
	finalizeConnection(conn *Connection)
	createStreamer(conn *Connection) StreamProcessor
}

type ControlManager interface {
	Control(conn *Connection)
	Delete(conn *Connection) error
}

type ErrStreamUnrecoverable error

type Connection struct {
	logger *zap.Logger
	// connecting reporting system
	report

	// lifecycle management
	cancel    context.CancelFunc
	startOnce sync.Once

	// dependencies
	services        services
	serviceRegistry *servicespkg.ServiceRegistry
	eventStore      eventstore.EventStore
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

	// tags
	tags tags.List

	// auditCount is the number of times a connection audit log as been reported
	auditCount uint32
}

type ConnOpt func(c *Connection)

func WithProcess(process *process.Process) ConnOpt {
	return func(c *Connection) {
		if process == nil {
			return
		}
		c.process = process

		// add tags
		if c.tags != nil && c.process != nil {
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
		}
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

func WithServiceRegistry(serviceRegistry *servicespkg.ServiceRegistry) ConnOpt {
	return func(c *Connection) {
		c.serviceRegistry = serviceRegistry
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
	}

	// apply options
	for _, opt := range opts {
		opt(c)
	}

	if c.serviceRegistry != nil {
		factory := c.serviceRegistry.Get(eventstore.TypeEventStore)
		if factory != nil {
			instance, err := factory.Create(ctx)
			if err != nil {
				c.logger.Error("failed to create event store", zap.Error(err))
			}
			if l, ok := instance.(servicespkg.LoggerAdapter); ok {
				l.SetLogger(c.logger)
			}
			if es, ok := instance.(eventstore.EventStore); ok {
				c.eventStore = es
			} else {
				c.logger.DPanic("event store factory returned non-eventstore instance")
			}
		}
	}

	return c
}

// Open initializes the connection monitoring
func (c *Connection) Open() {
	c.startOnce.Do(func() {
		c.logger.Debug("opening connection")

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
		go c.warmup()
	})
}

func (c *Connection) ID() string {
	return c.id
}

func (c *Connection) warmup() {
	// Start goroutine to wait for timeout
	select {
	case <-c.ctx.Done():
		return
	case <-time.After(WarmupTimeout):
		c.services.generateAuditLog(c)
	}
}

func (c *Connection) watch() {
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

	// generate an audit log
	c.services.generateAuditLog(c)

	// log connection report
	// if the connection is expected this will be a debug level log
	// otherwise it will be a warning or an error
	c.logConnectionReport()
}

func (c *Connection) SetDomain(input string) {
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

func (c *Connection) ProcessMeta() map[string]any {
	p := c.process
	if p == nil {
		return nil
	}

	m := map[string]any{
		"conn_id":      c.id,
		"pid":          p.Pid,
		"exe":          p.Exe,
		"bin":          p.Binary,
		"container_id": p.ContainerID,
		"pod_id":       p.PodID,
	}

	if c := p.Container; c != nil {
		setIfNotEmpty(m, "container_name", c.Name)
		setIfNotEmpty(m, "container_image", c.Image)
	}
	if p := p.Pod; p != nil {
		setIfNotEmpty(m, "pod_name", p.Name)
		setIfNotEmpty(m, "pod_namespace", p.Namespace)
	}

	return m
}

// setIfNotEmpty sets a value in the map only if the string is not empty
func setIfNotEmpty(m map[string]any, key, value string) {
	if value != "" && value != "<nil>" {
		m[key] = value
	}
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
// NOTE: values types not supported by the rule engine are ignored (see top-level comment in pkg/rule/rule.go)
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
	if c.process != nil {
		src["process"] = c.process.ControlValues()

		if container := c.process.Container; container != nil && container.ID != "" {
			src["container"] = container.ControlValues()
			if pod := c.process.Pod; pod != nil && pod.Name != "" {
				src["pod"] = pod.ControlValues()
			}
		}
	}

	// dst
	if d := c.Destination(); !d.Empty() {
		maps.Copy(dst, d.ControlValues())
	}

	if h := c.Domain(); h != "" && h != "<nil>" && !c.domainIsIP {
		dst["domain"] = h
	}

	return v
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
