package eventstore

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/qpoint-io/qtap/pkg/qnet"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/tlsutils"
)

const (
	TypeEventStore services.ServiceType = "eventstore"
)

// EventStore defines the interface for event storage services
type EventStore interface {
	services.Service
	Save(ctx context.Context, item any)
}

// BaseEventStore provides common functionality for EventStore implementations
type BaseEventStore struct {
	Registry services.RegistryAccessor
}

// ServiceType returns the service type
func (b *BaseEventStore) ServiceType() services.ServiceType {
	return TypeEventStore
}

func (b *BaseEventStore) SetRegistry(registry services.RegistryAccessor) {
	b.Registry = registry
}

type meta struct {
	ConnectionID string `json:"connectionId,omitzero"`
	EndpointId   string `json:"endpointId,omitzero"`
	RequestId    string `json:"requestId,omitzero"`
}

func (m *meta) SetConnectionID(id string) {
	m.ConnectionID = id
}

func (m *meta) SetEndpointID(id string) {
	m.EndpointId = id
}

func (m *meta) SetRequestID(id string) {
	m.RequestId = id
}

type tags struct {
	Tags []string `json:"tags,omitempty"`
}

func (t *tags) AddTags(tag ...string) {
	t.Tags = append(t.Tags, tag...)
}

type Request struct {
	meta
	tags

	Timestamp   time.Time `json:"timestamp"`
	Direction   string    `json:"direction"`
	Url         string    `json:"url"`
	URLPath     string    `json:"path"`
	Method      string    `json:"method"`
	Status      int       `json:"status"`
	Duration    int64     `json:"duration"`
	ContentType string    `json:"contentType"`
	Category    string    `json:"category"`
	Agent       string    `json:"agent"`

	WrBytes int64 `json:"bytesSent"`
	RdBytes int64 `json:"bytesReceived"`

	RequestAuthToken
}

type RequestAuthToken struct {
	AuthTokenMask string `json:"authTokenMask"`
	// AuthTokenHash is a SHA-256 hash of the auth token. The length is 32 bytes (64 characters) enforced by ClickHouse.
	AuthTokenHash   string `json:"authTokenHash"`
	AuthTokenSource string `json:"authTokenSource"`
	AuthTokenType   string `json:"authTokenType"`
}

type Issue struct {
	meta
	tags

	Timestamp time.Time `json:"timestamp"`
	Direction string    `json:"direction"`
	Error     string    `json:"error"`
	URL       string    `json:"url"`
	URLPath   string    `json:"path"`
	Method    string    `json:"method"`
	Status    int       `json:"status"`

	TriggerConditions []IssueTriggerCondition `json:"triggerConditions,omitempty"`
	TriggerReasons    []string                `json:"triggerReasons,omitempty"`
}

type PluginName string

const (
	PluginNameDetectErrors PluginName = "detect_errors"
)

type IssueTriggerCondition struct {
	Plugin    PluginName
	Condition string
}

func (c *IssueTriggerCondition) String() string {
	return fmt.Sprintf("%s:%s", c.Plugin, c.Condition)
}

func (c *IssueTriggerCondition) MarshalText() ([]byte, error) {
	return []byte(c.String()), nil
}

func (c *IssueTriggerCondition) UnmarshalText(text []byte) error {
	parts := strings.SplitN(string(text), ":", 2)
	if len(parts) < 2 {
		return fmt.Errorf("invalid trigger condition: %s, expected format: <plugin>:<condition>", string(text))
	}
	c.Plugin = PluginName(parts[0])
	c.Condition = parts[1]
	return nil
}

type PIIEntity struct {
	meta
	tags

	Timestamp    time.Time `json:"timestamp"`
	EntityType   string    `json:"entityType"`
	Score        float32   `json:"score"`
	EntitySource string    `json:"entitySource"`
	FieldPath    string    `json:"fieldPath"`
	// ValueHash is a SHA-256 hash of the value. The length is 32 bytes (64 characters) enforced by ClickHouse.
	ValueHash string `json:"valueHash"`
}

type ArtifactType string

var (
	ArtifactType_RequestBody     ArtifactType = "req-body"
	ArtifactType_RequestHeaders  ArtifactType = "req-headers"
	ArtifactType_ResponseBody    ArtifactType = "res-body"
	ArtifactType_ResponseHeaders ArtifactType = "res-headers"
	ArtifactType_DLPMatches      ArtifactType = "dlp_matches"
	ArtifactType_SensitiveData   ArtifactType = "sensitive_data"
)

func (a ArtifactType) String() string {
	return string(a)
}

// Artifact contains a payload which is sent to Warehouse.
type Artifact struct {
	digest string `json:"-"`

	meta

	Type        ArtifactType `json:"type"`
	Data        []byte       `json:"data"`
	ContentType string       `json:"contentType"`
}

// Digest computes the SHA-1 hash of binary data from a byte slice
func (a *Artifact) Digest() string {
	if a.digest != "" {
		return a.digest
	}

	// Compute SHA-1 hash
	hasher := sha1.New()
	hasher.Write(a.Data)
	hashBytes := hasher.Sum(nil)

	// Convert bytes to hex string
	hashHex := hex.EncodeToString(hashBytes)

	// Return the hash as a hex string
	a.digest = hashHex
	return hashHex
}

func (a *Artifact) Record(url string) *ArtifactRecord {
	return &ArtifactRecord{
		meta:      a.meta,
		Type:      a.Type,
		Timestamp: time.Now(),
		Digest:    a.Digest(),
		URL:       url,
	}
}

// ArtifactRecord is a record of an Artifact which is sent to Pulse.
type ArtifactRecord struct {
	meta

	Timestamp time.Time    `json:"timestamp"`
	Type      ArtifactType `json:"type"`
	Digest    string       `json:"digest"`
	URL       string       `json:"url"`
}

type Direction string

const (
	Direction_Ingress        Direction = "ingress"
	Direction_Egress         Direction = "egress"
	Direction_EgressInternal Direction = "egress-internal"
	Direction_EgressExternal Direction = "egress-external"
)

type SocketProtocol string

const (
	SocketProtocol_TCP  SocketProtocol = "tcp"
	SocketProtocol_UDP  SocketProtocol = "udp"
	SocketProtocol_RAW  SocketProtocol = "raw"
	SocketProtocol_ICMP SocketProtocol = "icmp"
)

type L7Protocol string

const (
	L7Protocol_HTTP1 L7Protocol = "http1"
	L7Protocol_HTTP2 L7Protocol = "http2"
	L7Protocol_DNS   L7Protocol = "dns"
	L7Protocol_GRPC  L7Protocol = "grpc"
	L7Protocol_OTHER L7Protocol = "other"
)

type Connection struct {
	meta `json:"meta,omitempty"`

	Tags           map[string][]string `json:"tags,omitzero"`
	Finalized      bool                `json:"finalized,omitempty"`
	Timestamp      time.Time           `json:"timestamp,omitempty"`
	Direction      Direction           `json:"direction,omitempty"`
	VendorID       string              `json:"vendorId,omitempty"`
	Part           uint32              `json:"part,omitempty"`
	SocketProtocol SocketProtocol      `json:"socketProtocol,omitempty"`
	L7Protocol     L7Protocol          `json:"l7Protocol,omitempty"`
	TLSVersion     tlsutils.TLSVersion `json:"tlsVersion,omitempty"`
	System         *ConnectionSystem   `json:"system,omitempty"`
	Source         ConnectionEndpoint  `json:"source,omitempty"`
	Destination    ConnectionEndpoint  `json:"destination,omitempty"`
	BytesReceived  uint64              `json:"bytesReceived,omitempty"`
	BytesSent      uint64              `json:"bytesSent,omitempty"`
}

type ConnectionSystem struct {
	Hostname      string `json:"hostname,omitempty"`
	Agent         string `json:"agent,omitempty"`
	AgentInstance string `json:"agentInstance,omitempty"`
}

type ConnectionEndpoint interface {
	isConnectionEndpoint()
}

type ConnectionEndpointLocal struct {
	Address   qnet.NetAddr `json:"address,omitempty"`
	Hostname  string       `json:"hostname,omitempty"`
	Exe       string       `json:"exe,omitempty"`
	User      string       `json:"user,omitempty"`
	Container *Container   `json:"container,omitempty"`
}

type ConnectionEndpointRemote struct {
	Address qnet.NetAddr `json:"address,omitempty"`
}

func (c *ConnectionEndpointLocal) isConnectionEndpoint()  {}
func (c *ConnectionEndpointRemote) isConnectionEndpoint() {}

type Container struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Image string `json:"image,omitempty"`
	Pod   *Pod   `json:"pod,omitempty"`
}

type Pod struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}
