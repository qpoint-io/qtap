package plugins

import (
	"time"

	"github.com/qpoint-io/qtap/pkg/services"
)

// RedisStatus indicates how plugin iteration should proceed
type RedisStatus int

const (
	RedisStatusContinue      RedisStatus = 0
	RedisStatusStopIteration RedisStatus = 1
)

// RedisCommand represents a Redis command received from the client
type RedisCommand struct {
	Name      string   // e.g., "GET", "SET"
	Args      []string // e.g., ["mykey"]
	Raw       []byte   // The raw wire bytes
	Timestamp time.Time
}

// RedisResult represents a Redis result received from the server
type RedisResult struct {
	Type    string // e.g., "Integer", "BulkString"
	Value   any
	IsError bool
}

// RedisPluginInstance handles Redis traffic for a single connection
type RedisPluginInstance interface {
	PluginInstance
	OnRedisCommand(cmd *RedisCommand) RedisStatus
	OnRedisResult(res *RedisResult) RedisStatus
}

// RedisPlugin is the capability interface for plugins that handle Redis traffic
type RedisPlugin interface {
	Plugin // Embeds base
	NewRedisInstance(PluginContext, *services.ServiceRegistry) RedisPluginInstance
}
