package plugins

import (
	"testing"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"gopkg.in/yaml.v3"
)

// mockHttpPlugin only implements HttpPlugin
type mockHttpPlugin struct {
	pluginType PluginType
}

func (m *mockHttpPlugin) Init(logger *zap.Logger, config yaml.Node) {}
func (m *mockHttpPlugin) Destroy()                                  {}
func (m *mockHttpPlugin) PluginType() PluginType                    { return m.pluginType }
func (m *mockHttpPlugin) NewHttpInstance(ctx PluginContext, svcs *services.ServiceRegistry) HttpPluginInstance {
	return &mockHttpInstance{}
}

type mockHttpInstance struct{}

func (m *mockHttpInstance) RequestHeaders(headers Headers, endOfStream bool) HeadersStatus {
	return HeadersStatusContinue
}
func (m *mockHttpInstance) RequestBody(body BodyBuffer, endOfStream bool) BodyStatus {
	return BodyStatusContinue
}
func (m *mockHttpInstance) ResponseHeaders(headers Headers, endOfStream bool) HeadersStatus {
	return HeadersStatusContinue
}
func (m *mockHttpInstance) ResponseBody(body BodyBuffer, endOfStream bool) BodyStatus {
	return BodyStatusContinue
}
func (m *mockHttpInstance) Destroy() {}

// mockRedisPlugin only implements RedisPlugin
type mockRedisPlugin struct {
	pluginType PluginType
}

func (m *mockRedisPlugin) Init(logger *zap.Logger, config yaml.Node) {}
func (m *mockRedisPlugin) Destroy()                                  {}
func (m *mockRedisPlugin) PluginType() PluginType                    { return m.pluginType }
func (m *mockRedisPlugin) NewRedisInstance(ctx PluginContext, svcs *services.ServiceRegistry) RedisPluginInstance {
	return &mockRedisInstance{}
}

type mockRedisInstance struct{}

func (m *mockRedisInstance) OnRedisCommand(cmd *RedisCommand) RedisStatus {
	return RedisStatusContinue
}
func (m *mockRedisInstance) OnRedisResult(res *RedisResult) RedisStatus {
	return RedisStatusContinue
}
func (m *mockRedisInstance) Destroy() {}

// mockDualPlugin implements both HttpPlugin and RedisPlugin
type mockDualPlugin struct {
	pluginType PluginType
}

func (m *mockDualPlugin) Init(logger *zap.Logger, config yaml.Node) {}
func (m *mockDualPlugin) Destroy()                                  {}
func (m *mockDualPlugin) PluginType() PluginType                    { return m.pluginType }
func (m *mockDualPlugin) NewHttpInstance(ctx PluginContext, svcs *services.ServiceRegistry) HttpPluginInstance {
	return &mockHttpInstance{}
}
func (m *mockDualPlugin) NewRedisInstance(ctx PluginContext, svcs *services.ServiceRegistry) RedisPluginInstance {
	return &mockRedisInstance{}
}

func TestStackDeployment_NewStack(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("HTTP connection gets only HttpPlugin instances", func(t *testing.T) {
		deployment := &StackDeployment{
			logger: logger,
			plugins: []Plugin{
				&mockHttpPlugin{pluginType: "http_only"},
				&mockRedisPlugin{pluginType: "redis_only"},
				&mockDualPlugin{pluginType: "dual"},
			},
		}

		instances := deployment.NewStack(ConnectionType_HTTP, nil, nil)

		// Should get 2 instances: http_only and dual (both implement HttpPlugin)
		require.Len(t, instances, 2)
		for _, inst := range instances {
			_, ok := inst.(HttpPluginInstance)
			assert.True(t, ok, "expected HttpPluginInstance")
		}
	})

	t.Run("Redis connection gets only RedisPlugin instances", func(t *testing.T) {
		deployment := &StackDeployment{
			logger: logger,
			plugins: []Plugin{
				&mockHttpPlugin{pluginType: "http_only"},
				&mockRedisPlugin{pluginType: "redis_only"},
				&mockDualPlugin{pluginType: "dual"},
			},
		}

		instances := deployment.NewStack(ConnectionType_REDIS, nil, nil)

		// Should get 2 instances: redis_only and dual (both implement RedisPlugin)
		require.Len(t, instances, 2)
		for _, inst := range instances {
			_, ok := inst.(RedisPluginInstance)
			assert.True(t, ok, "expected RedisPluginInstance")
		}
	})

	t.Run("GRPC connection treated like HTTP", func(t *testing.T) {
		deployment := &StackDeployment{
			logger: logger,
			plugins: []Plugin{
				&mockHttpPlugin{pluginType: "http_only"},
			},
		}

		instances := deployment.NewStack(ConnectionType_GRPC, nil, nil)

		// gRPC should use HTTP plugins
		require.Len(t, instances, 1)
		for _, inst := range instances {
			_, ok := inst.(HttpPluginInstance)
			assert.True(t, ok, "expected HttpPluginInstance")
		}
	})

	t.Run("empty plugins returns empty instances", func(t *testing.T) {
		deployment := &StackDeployment{
			logger:  logger,
			plugins: []Plugin{},
		}

		httpInstances := deployment.NewStack(ConnectionType_HTTP, nil, nil)
		redisInstances := deployment.NewStack(ConnectionType_REDIS, nil, nil)

		assert.Empty(t, httpInstances)
		assert.Empty(t, redisInstances)
	})

	t.Run("unknown connection type returns empty instances", func(t *testing.T) {
		deployment := &StackDeployment{
			logger: logger,
			plugins: []Plugin{
				&mockHttpPlugin{pluginType: "http_only"},
				&mockRedisPlugin{pluginType: "redis_only"},
			},
		}

		instances := deployment.NewStack(ConnectionType_UNKNOWN, nil, nil)

		assert.Empty(t, instances)
	})
}
