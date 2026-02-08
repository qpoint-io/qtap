package wrapper

import (
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// PanicCatcher is a wrapper struct that implements Plugin interface
// and provides panic recovery and logging. It conditionally implements
// HttpPlugin, RedisPlugin, and MySQLPlugin based on what the wrapped plugin supports.
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

// NewKafkaInstance implements the KafkaPlugin interface
func (s *PanicCatcher) NewKafkaInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.KafkaPluginInstance {
	kafkaP, ok := s.p.(plugins.KafkaPlugin)
	if !ok {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in NewKafkaInstance",
				zap.Any("panic", r),
			)
		}
	}()

	i := kafkaP.NewKafkaInstance(ctx, svcs)
	if i == nil {
		return nil
	}
	return NewSafeKafkaFilterInstance(s.logger, i)
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

// NewMySQLInstance implements the MySQLPlugin interface
func (s *PanicCatcher) NewMySQLInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.MySQLPluginInstance {
	mysqlP, ok := s.p.(plugins.MySQLPlugin)
	if !ok {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in NewMySQLInstance",
				zap.Any("panic", r),
			)
		}
	}()

	i := mysqlP.NewMySQLInstance(ctx, svcs)
	if i == nil {
		return nil
	}
	return NewSafeMySQLFilterInstance(s.logger, i)
}

// NewKafkaInstance implements the KafkaPlugin interface
func (s *PanicCatcher) NewKafkaInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.KafkaPluginInstance {
	kafkaP, ok := s.p.(plugins.KafkaPlugin)
	if !ok {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in NewKafkaInstance",
				zap.Any("panic", r),
			)
		}
	}()

	i := kafkaP.NewKafkaInstance(ctx, svcs)
	if i == nil {
		return nil
	}
	return NewSafeKafkaFilterInstance(s.logger, i)
}

// SafeMySQLFilterInstance is a wrapper struct that implements MySQLPluginInstance interface
// and provides panic recovery and logging
type SafeMySQLFilterInstance struct {
	instance plugins.MySQLPluginInstance
	logger   *zap.Logger
}

// NewSafeMySQLFilterInstance creates a new SafeMySQLFilterInstance
func NewSafeMySQLFilterInstance(logger *zap.Logger, instance plugins.MySQLPluginInstance) *SafeMySQLFilterInstance {
	return &SafeMySQLFilterInstance{
		logger:   logger,
		instance: instance,
	}
}

// SafeKafkaFilterInstance is a wrapper struct that implements KafkaPluginInstance interface
// and provides panic recovery and logging
type SafeKafkaFilterInstance struct {
	instance plugins.KafkaPluginInstance
	logger   *zap.Logger
}

// NewSafeKafkaFilterInstance creates a new SafeKafkaFilterInstance
func NewSafeKafkaFilterInstance(logger *zap.Logger, instance plugins.KafkaPluginInstance) *SafeKafkaFilterInstance {
	return &SafeKafkaFilterInstance{
		logger:   logger,
		instance: instance,
	}
}

// OnMySQLCommand implements the MySQLPluginInstance interface
func (s *SafeMySQLFilterInstance) OnMySQLCommand(cmd *plugins.MySQLCommand) (status plugins.MySQLStatus) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in OnMySQLCommand",
				zap.Any("panic", r),
			)
			status = plugins.MySQLStatusStopIteration
		}
	}()

	return s.instance.OnMySQLCommand(cmd)
}

// OnMySQLResult implements the MySQLPluginInstance interface
func (s *SafeMySQLFilterInstance) OnMySQLResult(res *plugins.MySQLResult) (status plugins.MySQLStatus) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in OnMySQLResult",
				zap.Any("panic", r),
			)
			status = plugins.MySQLStatusStopIteration
		}
	}()

	return s.instance.OnMySQLResult(res)
}

// Destroy implements the MySQLPluginInstance interface
func (s *SafeMySQLFilterInstance) Destroy() {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in Destroy (MySQLFilterInstance)",
				zap.Any("panic", r),
			)
		}
	}()

	s.instance.Destroy()
}

// OnKafkaCommand implements the KafkaPluginInstance interface
func (s *SafeKafkaFilterInstance) OnKafkaCommand(cmd *plugins.KafkaCommand) (status plugins.KafkaStatus) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in OnKafkaCommand",
				zap.Any("panic", r),
			)
			status = plugins.KafkaStatusStopIteration
		}
	}()

	return s.instance.OnKafkaCommand(cmd)
}

// OnKafkaResult implements the KafkaPluginInstance interface
func (s *SafeKafkaFilterInstance) OnKafkaResult(res *plugins.KafkaResult) (status plugins.KafkaStatus) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in OnKafkaResult",
				zap.Any("panic", r),
			)
			status = plugins.KafkaStatusStopIteration
		}
	}()

	return s.instance.OnKafkaResult(res)
}

// Destroy implements the KafkaPluginInstance interface
func (s *SafeKafkaFilterInstance) Destroy() {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in Destroy (KafkaFilterInstance)",
				zap.Any("panic", r),
			)
		}
	}()

	s.instance.Destroy()
}

func (s *PanicCatcher) PluginType() plugins.PluginType {
	return s.p.PluginType()
}
