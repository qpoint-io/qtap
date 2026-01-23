package wrapper

import (
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// PanicCatcher is a wrapper struct that implements Plugin interface
// and provides panic recovery and logging. It conditionally implements
// HttpPlugin and RedisPlugin based on what the wrapped plugin supports.
type PanicCatcher struct {
	logger *zap.Logger
	p      plugins.Plugin
	config yaml.Node
}

func Catch(toCatch plugins.Plugin) plugins.Plugin {
	return &PanicCatcher{
		p: toCatch,
	}
}

func (s *PanicCatcher) Init(logger *zap.Logger, config yaml.Node) {
	s.logger = logger
	s.config = config
	s.p.Init(logger, config)
}

// NewHttpInstance implements the HttpPlugin interface
func (s *PanicCatcher) NewHttpInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	httpP, ok := s.p.(plugins.HttpPlugin)
	if !ok {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in NewHttpInstance",
				zap.Any("panic", r),
			)
		}
	}()

	i := httpP.NewHttpInstance(ctx, svcs)
	if i == nil {
		return nil
	}
	return NewSafeHttpFilterInstance(s.logger, i)
}

// Destroy implements the Plugin interface
func (s *PanicCatcher) Destroy() {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in Destroy",
				zap.Any("panic", r),
			)
		}
	}()
	if s.p != nil {
		s.p.Destroy()
	}
}

// NewRedisInstance implements the RedisPlugin interface
func (s *PanicCatcher) NewRedisInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.RedisPluginInstance {
	redisP, ok := s.p.(plugins.RedisPlugin)
	if !ok {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in NewRedisInstance",
				zap.Any("panic", r),
			)
		}
	}()

	i := redisP.NewRedisInstance(ctx, svcs)
	if i == nil {
		return nil
	}
	return NewSafeRedisFilterInstance(s.logger, i)
}

// SafeHttpFilterInstance is a wrapper struct that implements HttpFilterInstance interface
// and provides panic recovery and logging
type SafeHttpFilterInstance struct {
	instance plugins.HttpPluginInstance
	logger   *zap.Logger
}

// NewSafeHttpFilterInstance creates a new SafeHttpFilterInstance
func NewSafeHttpFilterInstance(logger *zap.Logger, instance plugins.HttpPluginInstance) *SafeHttpFilterInstance {
	return &SafeHttpFilterInstance{
		logger:   logger,
		instance: instance,
	}
}

// RequestHeaders implements the HttpFilterInstance interface
func (s *SafeHttpFilterInstance) RequestHeaders(requestHeaders plugins.Headers, endOfStream bool) (status plugins.HeadersStatus) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in RequestHeaders",
				zap.Any("panic", r),
			)
			status = plugins.HeadersStatusStopIteration
		}
	}()

	return s.instance.RequestHeaders(requestHeaders, endOfStream)
}

// RequestBody implements the HttpFilterInstance interface
func (s *SafeHttpFilterInstance) RequestBody(frame plugins.BodyBuffer, endOfStream bool) (status plugins.BodyStatus) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in RequestBody",
				zap.Any("panic", r),
			)
			status = plugins.BodyStatusStopIterationAndBuffer
		}
	}()

	return s.instance.RequestBody(frame, endOfStream)
}

// ResponseHeaders implements the HttpFilterInstance interface
func (s *SafeHttpFilterInstance) ResponseHeaders(responseHeaders plugins.Headers, endOfStream bool) (status plugins.HeadersStatus) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in ResponseHeaders",
				zap.Any("panic", r),
			)
			status = plugins.HeadersStatusStopIteration
		}
	}()

	return s.instance.ResponseHeaders(responseHeaders, endOfStream)
}

// ResponseBody implements the HttpFilterInstance interface
func (s *SafeHttpFilterInstance) ResponseBody(frame plugins.BodyBuffer, endOfStream bool) (status plugins.BodyStatus) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in ResponseBody",
				zap.Any("panic", r),
			)
			status = plugins.BodyStatusStopIterationAndBuffer
		}
	}()

	return s.instance.ResponseBody(frame, endOfStream)
}

// Destroy implements the HttpFilterInstance interface
func (s *SafeHttpFilterInstance) Destroy() {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in Destroy (HttpFilterInstance)",
				zap.Any("panic", r),
			)
		}
	}()

	s.instance.Destroy()
}

// SafeRedisFilterInstance is a wrapper struct that implements RedisPluginInstance interface
// and provides panic recovery and logging
type SafeRedisFilterInstance struct {
	instance plugins.RedisPluginInstance
	logger   *zap.Logger
}

// NewSafeRedisFilterInstance creates a new SafeRedisFilterInstance
func NewSafeRedisFilterInstance(logger *zap.Logger, instance plugins.RedisPluginInstance) *SafeRedisFilterInstance {
	return &SafeRedisFilterInstance{
		logger:   logger,
		instance: instance,
	}
}

// OnRedisCommand implements the RedisPluginInstance interface
func (s *SafeRedisFilterInstance) OnRedisCommand(cmd *plugins.RedisCommand) (status plugins.RedisStatus) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in OnRedisCommand",
				zap.Any("panic", r),
			)
			status = plugins.RedisStatusStopIteration
		}
	}()

	return s.instance.OnRedisCommand(cmd)
}

// OnRedisResult implements the RedisPluginInstance interface
func (s *SafeRedisFilterInstance) OnRedisResult(res *plugins.RedisResult) (status plugins.RedisStatus) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in OnRedisResult",
				zap.Any("panic", r),
			)
			status = plugins.RedisStatusStopIteration
		}
	}()

	return s.instance.OnRedisResult(res)
}

// Destroy implements the RedisPluginInstance interface
func (s *SafeRedisFilterInstance) Destroy() {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in Destroy (RedisFilterInstance)",
				zap.Any("panic", r),
			)
		}
	}()

	s.instance.Destroy()
}

func (s *PanicCatcher) PluginType() plugins.PluginType {
	return s.p.PluginType()
}
