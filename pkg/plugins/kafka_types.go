package plugins

import (
	"time"

	"github.com/qpoint-io/qtap/pkg/services"
)

// KafkaStatus indicates how plugin iteration should proceed
type KafkaStatus int

const (
	KafkaStatusContinue      KafkaStatus = 0
	KafkaStatusStopIteration KafkaStatus = 1
)

// KafkaCommand represents a Kafka request received from the client
type KafkaCommand struct {
	ApiKey        int16    // e.g., 0=Produce, 1=Fetch, 3=Metadata, 18=ApiVersions
	ApiVersion    int16    // Protocol version
	CorrelationID int32    // Correlation ID for request/response matching
	ClientID      string   // Client identifier
	Operation     string   // Human-readable operation name (e.g., "Produce", "Fetch")
	Topics        []string // Topic names (from Produce/Fetch requests)
	GroupID       string   // Consumer group ID (from JoinGroup/SyncGroup/OffsetCommit)
	Timestamp     time.Time
}

// KafkaResult represents a Kafka response received from the server
type KafkaResult struct {
	CorrelationID int32  // Correlation ID for request/response matching
	ErrorCode     int16  // Kafka error code (0 = no error)
	ErrorMessage  string // Human-readable error message
	IsError       bool   // Whether this response contains an error
}

// KafkaPluginInstance handles Kafka traffic for a single connection
type KafkaPluginInstance interface {
	PluginInstance
	OnKafkaCommand(cmd *KafkaCommand) KafkaStatus
	OnKafkaResult(res *KafkaResult) KafkaStatus
}

// KafkaPlugin is the capability interface for plugins that handle Kafka traffic
type KafkaPlugin interface {
	Plugin // Embeds base
	NewKafkaInstance(PluginContext, *services.ServiceRegistry) KafkaPluginInstance
}
