package plugins

import (
	"context"
	"io"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/connmeta"
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
	BodyStatusContinueAndBuffer      BodyStatus = 2
)

var NewHttpPlugin func(config map[string]any) HttpPlugin

type HttpPlugin interface {
	Init(logger *zap.Logger, config yaml.Node)
	NewInstance(PluginContext, *services.ServiceRegistry) HttpPluginInstance
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

	Context() context.Context
	Meta() Meta
}

// Meta provides top-level-connection metadata extended with PluginContext-level metadata
type Meta interface {
	connmeta.Service
	RequestID() string

	ReadBytes() int64
	WriteBytes() int64
	SetReadBytes(int64)
	SetWriteBytes(int64)
}

type meta struct {
	connmeta.Service
	requestID  string
	readBytes  int64
	writeBytes int64
}

func (m *meta) RequestID() string {
	return m.requestID
}

func (m *meta) ReadBytes() int64 {
	return m.readBytes
}

func (m *meta) WriteBytes() int64 {
	return m.writeBytes
}

func (m *meta) SetReadBytes(bytes int64) {
	m.readBytes = bytes
}

func (m *meta) SetWriteBytes(bytes int64) {
	m.writeBytes = bytes
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

// PluginAccessor is a type that can access the plugin registry
type PluginAccessor interface {
	// Get retrieves a plugin by type
	Get(pluginType PluginType) HttpPlugin
}
