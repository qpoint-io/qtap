package observerstest

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/metadata"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
	"gopkg.in/yaml.v3"
)

func MarshalJSON(t *testing.T, v any) string {
	b, err := json.Marshal(v)
	require.NoErrorf(t, err, "failed to marshal json")
	return string(b)
}

func MarshalYAML(t *testing.T, v any) *yaml.Node {
	var node yaml.Node
	err := node.Encode(v)
	require.NoErrorf(t, err, "failed to marshal yaml")
	return &node
}

type Clock struct {
	now time.Time
}

func (c *Clock) Now() time.Time {
	return c.now
}

func (c *Clock) Set(t time.Time) {
	c.now = t
}

func (c *Clock) Add(d time.Duration) {
	c.now = c.now.Add(d)
}

var DefaultTime = time.Date(2024, 10, 22, 0, 0, 0, 0, time.UTC)

func NewClock(t *time.Time) *Clock {
	if t == nil {
		tt := DefaultTime
		t = &tt
	}
	return &Clock{now: *t}
}

type Logger struct {
	T      *testing.T
	Logs   *observer.ObservedLogs
	Logger *zap.Logger
}

func (tl *Logger) Sync() {
	_ = tl.Logger.Sync()
}

func (tl *Logger) Get(msg string) []map[string]any {
	tl.Sync()
	var entries []map[string]any
	for _, m := range tl.Logs.FilterMessage(msg).AllUntimed() {
		entries = append(entries, m.ContextMap())
	}
	return entries
}

func (tl *Logger) Assert(msg string, expected []map[string]any) {
	tl.Sync()
	tl.T.Helper()
	msgs := tl.Get(msg)
	require.NotNil(tl.T, msgs)
	require.Lenf(tl.T, msgs, len(expected), "asserting log message: %v", msg)
	for i, fields := range expected {
		assert.Subsetf(tl.T, msgs[i], fields, "asserting log message fields [%d]: %v", i, msg)
	}
}

func NewLogger(t *testing.T) *Logger {
	t.Helper()
	var logs *observer.ObservedLogs
	ll := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core {
		var obsCore zapcore.Core
		obsCore, logs = observer.New(zapcore.DebugLevel)
		return zapcore.NewTee(c, obsCore)
	})))

	return &Logger{
		T:      t,
		Logs:   logs,
		Logger: ll,
	}
}

func Headers(kv map[string]string) *plugins.HttpHeaderMap {
	h := plugins.NewHeaders(http.Header{})
	for k, v := range kv {
		h.Set(k, v)
	}
	return h
}

type FilterContext struct {
	T         *testing.T
	VReqBody  []byte
	VResBody  []byte
	VMetadata map[string]any
	VTags     map[string]string
	VContext  context.Context
}

// plugins.HttpPluginInstance interface implementation
// this is the client side of the connection that filters
// can use to interact with the connection
func (c *FilterContext) GetRequestBodyBuffer() plugins.BodyBuffer {
	return Buffer(c.VReqBody)
}

func (c *FilterContext) GetResponseBodyBuffer() plugins.BodyBuffer {
	return Buffer(c.VResBody)
}

// Metadata returns connection specific metadata in a map[string]any.
func (c *FilterContext) Metadata() map[string]plugins.MetadataValue {
	m := make(map[string]plugins.MetadataValue, len(c.VMetadata))
	for k, v := range c.VMetadata {
		m[k] = &metadata.MetadataValue{Value: v}
	}
	return m
}

// GetMetadata returns a key value of type any, if the key exists.
func (c *FilterContext) GetMetadata(key string) plugins.MetadataValue {
	if value, ok := c.VMetadata[key]; ok {
		return &metadata.MetadataValue{Value: value}
	}

	// if the key doesn't exist, return an empty value
	// this is to avoid nil pointers
	// an OK() method is provided to check if the value is set
	return &metadata.MetadataValue{}
}

func (c *FilterContext) Tags() tags.List {
	return tags.FromValues(c.VTags)
}

func (c *FilterContext) Context() context.Context {
	if c.VContext != nil {
		return c.VContext
	}
	return context.TODO()
}

func Buffer[T string | []byte](data T) *synq.LinkedBuffer {
	return synq.NewLinkedBuffer(1024*1024*2, []byte(data))
}
