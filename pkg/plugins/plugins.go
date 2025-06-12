package plugins

import (
	"context"
	"io"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/tags"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

type PluginType string

func (p PluginType) String() string {
	return string(p)
}

type HeadersStatus int

const (
	HeadersStatusContinue      HeadersStatus = 0
	HeadersStatusStopIteration HeadersStatus = 1
)

type BodyStatus int

const (
	BodyStatusContinue               BodyStatus = 0
	BodyStatusStopIterationAndBuffer BodyStatus = 1
)

var NewHttpPlugin func(config map[string]any) HttpPlugin

type HttpPlugin interface {
	Init(logger *zap.Logger, config yaml.Node)
	NewInstance(PluginContext, ...services.Service) HttpPluginInstance
	RequiredServices() []services.ServiceType
	Destroy()
	PluginType() PluginType
}

type HttpPluginInstance interface {
	RequestHeaders(requestHeaders Headers, endOfStream bool) HeadersStatus
	RequestBody(frame BodyBuffer, endOfStream bool) BodyStatus
	ResponseHeaders(responseHeaders Headers, endOfStream bool) HeadersStatus
	ResponseBody(frame BodyBuffer, endOfStream bool) BodyStatus
	Destroy()
}

type PluginContext interface {
	GetRequestBodyBuffer() BodyBuffer
	GetResponseBodyBuffer() BodyBuffer

	// TODO(Jon): these should be "services"
	Metadata() map[string]MetadataValue
	GetMetadata(key string) MetadataValue
	Tags() tags.List
	Context() context.Context
	ControlValues() map[string]any
}

type Headers interface {
	Get(key string) (HeaderValue, bool)
	Values(key string, iter func(value HeaderValue))
	Set(key, value string)
	Remove(key string)
	All() map[string]string
}

type BodyBuffer interface {
	io.ReaderAt
	Length() int
	Slices(iter func(view []byte))
	Copy() []byte
	NewReader() io.Reader
}

type HeaderValue interface {
	String() string
	Bytes() []byte
	Equal(str string) bool
}

type MetadataValue interface {
	OK() bool
	Raw() any
	String() string
	Int64() int64
}

// PluginAccessor is a type that can access the plugin registry
type PluginAccessor interface {
	// Get retrieves a plugin by type
	Get(pluginType PluginType) HttpPlugin
}
