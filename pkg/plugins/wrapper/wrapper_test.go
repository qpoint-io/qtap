package wrapper

import (
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"gopkg.in/yaml.v3"
)

// mockRedisPlugin is a test plugin that implements both Plugin and RedisPlugin
type mockRedisPlugin struct {
	initCalled     bool
	destroyCalled  bool
	panicOnInit    bool
	panicOnCreate  bool
	panicOnDestroy bool
}

func (m *mockRedisPlugin) Init(logger *zap.Logger, config yaml.Node) {
	m.initCalled = true
	if m.panicOnInit {
		panic("init panic")
	}
}

func (m *mockRedisPlugin) Destroy() {
	if m.panicOnDestroy {
		panic("plugin destroy panic")
	}
	m.destroyCalled = true
}

func (m *mockRedisPlugin) PluginType() plugins.PluginType {
	return "mock_redis"
}

func (m *mockRedisPlugin) NewRedisInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.RedisPluginInstance {
	if m.panicOnCreate {
		panic("create instance panic")
	}
	return &mockRedisInstance{}
}

// mockRedisInstance implements RedisPluginInstance
type mockRedisInstance struct {
	commandsReceived []*plugins.RedisCommand
	resultsReceived  []*plugins.RedisResult
	destroyCalled    bool
	panicOnCommand   bool
	panicOnResult    bool
	panicOnDestroy   bool
}

func (m *mockRedisInstance) OnRedisCommand(cmd *plugins.RedisCommand) plugins.RedisStatus {
	if m.panicOnCommand {
		panic("command panic")
	}
	m.commandsReceived = append(m.commandsReceived, cmd)
	return plugins.RedisStatusContinue
}

func (m *mockRedisInstance) OnRedisResult(res *plugins.RedisResult) plugins.RedisStatus {
	if m.panicOnResult {
		panic("result panic")
	}
	m.resultsReceived = append(m.resultsReceived, res)
	return plugins.RedisStatusContinue
}

func (m *mockRedisInstance) Destroy() {
	if m.panicOnDestroy {
		panic("destroy panic")
	}
	m.destroyCalled = true
}

// mockHttpOnlyPlugin only implements HttpPlugin, not RedisPlugin
type mockHttpOnlyPlugin struct{}

func (m *mockHttpOnlyPlugin) Init(logger *zap.Logger, config yaml.Node) {}
func (m *mockHttpOnlyPlugin) Destroy()                                  {}
func (m *mockHttpOnlyPlugin) PluginType() plugins.PluginType            { return "mock_http" }
func (m *mockHttpOnlyPlugin) NewHttpInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	return nil
}

func TestPanicCatcher_NewRedisInstance(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("returns instance for RedisPlugin", func(t *testing.T) {
		mock := &mockRedisPlugin{}
		catcher := Catch(mock)
		catcher.Init(logger, yaml.Node{})

		instance := catcher.(*PanicCatcher).NewRedisInstance(nil, nil)
		require.NotNil(t, instance)
	})

	t.Run("returns nil for non-RedisPlugin", func(t *testing.T) {
		mock := &mockHttpOnlyPlugin{}
		catcher := Catch(mock)
		catcher.Init(logger, yaml.Node{})

		instance := catcher.(*PanicCatcher).NewRedisInstance(nil, nil)
		assert.Nil(t, instance)
	})

	t.Run("recovers from panic in NewRedisInstance", func(t *testing.T) {
		mock := &mockRedisPlugin{panicOnCreate: true}
		catcher := Catch(mock)
		catcher.Init(logger, yaml.Node{})

		// Should not panic, should return nil
		instance := catcher.(*PanicCatcher).NewRedisInstance(nil, nil)
		assert.Nil(t, instance)
	})
}

func TestSafeRedisFilterInstance_OnRedisCommand(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("passes command to wrapped instance", func(t *testing.T) {
		mock := &mockRedisInstance{}
		safe := NewSafeRedisFilterInstance(logger, mock)

		cmd := &plugins.RedisCommand{
			Name:      "GET",
			Args:      []string{"mykey"},
			Timestamp: time.Now(),
		}

		status := safe.OnRedisCommand(cmd)

		assert.Equal(t, plugins.RedisStatusContinue, status)
		require.Len(t, mock.commandsReceived, 1)
		assert.Equal(t, "GET", mock.commandsReceived[0].Name)
	})

	t.Run("recovers from panic and returns StopIteration", func(t *testing.T) {
		mock := &mockRedisInstance{panicOnCommand: true}
		safe := NewSafeRedisFilterInstance(logger, mock)

		cmd := &plugins.RedisCommand{Name: "GET"}

		// Should not panic
		status := safe.OnRedisCommand(cmd)
		assert.Equal(t, plugins.RedisStatusStopIteration, status)
	})
}

func TestSafeRedisFilterInstance_OnRedisResult(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("passes result to wrapped instance", func(t *testing.T) {
		mock := &mockRedisInstance{}
		safe := NewSafeRedisFilterInstance(logger, mock)

		res := &plugins.RedisResult{
			Type:    "BulkString",
			Value:   "value",
			IsError: false,
		}

		status := safe.OnRedisResult(res)

		assert.Equal(t, plugins.RedisStatusContinue, status)
		require.Len(t, mock.resultsReceived, 1)
		assert.Equal(t, "BulkString", mock.resultsReceived[0].Type)
	})

	t.Run("recovers from panic and returns StopIteration", func(t *testing.T) {
		mock := &mockRedisInstance{panicOnResult: true}
		safe := NewSafeRedisFilterInstance(logger, mock)

		res := &plugins.RedisResult{Type: "Error", IsError: true}

		// Should not panic
		status := safe.OnRedisResult(res)
		assert.Equal(t, plugins.RedisStatusStopIteration, status)
	})
}

func TestSafeRedisFilterInstance_Destroy(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("calls destroy on wrapped instance", func(t *testing.T) {
		mock := &mockRedisInstance{}
		safe := NewSafeRedisFilterInstance(logger, mock)

		safe.Destroy()

		assert.True(t, mock.destroyCalled)
	})

	t.Run("recovers from panic in destroy", func(t *testing.T) {
		mock := &mockRedisInstance{panicOnDestroy: true}
		safe := NewSafeRedisFilterInstance(logger, mock)

		// Should not panic
		safe.Destroy()
	})
}

func TestPanicCatcher_PluginType(t *testing.T) {
	t.Run("returns wrapped plugin type", func(t *testing.T) {
		mock := &mockRedisPlugin{}
		catcher := Catch(mock)

		pluginType := catcher.(*PanicCatcher).PluginType()
		assert.Equal(t, plugins.PluginType("mock_redis"), pluginType)
	})
}

func TestPanicCatcher_Destroy(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("calls destroy on wrapped plugin", func(t *testing.T) {
		mock := &mockRedisPlugin{}
		catcher := Catch(mock)
		catcher.Init(logger, yaml.Node{})

		catcher.Destroy()

		assert.True(t, mock.destroyCalled)
	})

	t.Run("recovers from panic in destroy", func(t *testing.T) {
		mock := &mockRedisPlugin{panicOnDestroy: true}
		catcher := Catch(mock)
		catcher.Init(logger, yaml.Node{})

		// Should not panic
		catcher.Destroy()
	})
}
