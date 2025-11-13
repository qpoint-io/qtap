package wrapper

import (
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// PanicCatcher is a wrapper struct that implements HttpFilter interface
// and provides panic recovery and logging
type PanicCatcher struct {
	logger *zap.Logger
	p      plugins.HttpPlugin
	config yaml.Node
}

func Catch(toCatch plugins.HttpPlugin) plugins.HttpPlugin {
	return &PanicCatcher{
		p: toCatch,
	}
}

func (s *PanicCatcher) Init(logger *zap.Logger, config yaml.Node) {
	s.logger = logger
	s.config = config
	s.p.Init(logger, config)
}

// NewInstance implements the HttpFilter interface
func (s *PanicCatcher) NewInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in NewInstance",
				zap.Any("panic", r),
			)
		}
	}()

	return NewSafeHttpFilterInstance(s.logger, s.p.NewInstance(ctx, svcs))
}

// Destroy implements the HttpFilter interface
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
		}
	}()

	return s.instance.ResponseBody(frame, endOfStream)
}

// Destroy implements the HttpFilterInstance interface
func (s *SafeHttpFilterInstance) Destroy() {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Panic in Destroy",
				zap.Any("panic", r),
			)
		}
	}()

	s.instance.Destroy()
}

func (s *PanicCatcher) PluginType() plugins.PluginType {
	return s.p.PluginType()
}
