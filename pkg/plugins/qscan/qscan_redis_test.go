package qscan

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newRedisInstance(t *testing.T, factory *Factory, os *mockObjectStore) *redisFilterInstance {
	t.Helper()
	return &redisFilterInstance{
		logger: zap.NewNop(),
		ctx: &mockPluginContext{
			meta: &mockMeta{requestID: "redis-req-001", endpoint: "ep-redis"},
		},
		objectstore: os,
		config:      factory.config,
		factory:     factory,
	}
}

func TestRedisCommand_SuccessfulGetResult(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newRedisInstance(t, factory, os)

	status := inst.OnRedisCommand(&plugins.RedisCommand{
		Name:      "GET",
		Args:      []string{"mykey"},
		Timestamp: time.Now(),
	})
	assert.Equal(t, plugins.RedisStatusContinue, status)

	inst.OnRedisResult(&plugins.RedisResult{
		Type:    "BulkString",
		Value:   "hello world",
		IsError: false,
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))

	assert.Equal(t, "db", txn.TransactionType)
	assert.Equal(t, "redis", txn.DBType)
	assert.Equal(t, "GET mykey", txn.Query)
	assert.Equal(t, "redis-req-001", txn.RequestID)
	assert.Nil(t, txn.Error)

	require.Len(t, txn.ResultSet, 1)
	assert.Equal(t, "BulkString", txn.ResultSet[0]["type"])
	assert.Equal(t, "hello world", txn.ResultSet[0]["value"])
}

func TestRedisCommand_ErrorResult(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newRedisInstance(t, factory, os)

	inst.OnRedisCommand(&plugins.RedisCommand{
		Name: "GET",
		Args: []string{"badkey"},
	})
	inst.OnRedisResult(&plugins.RedisResult{
		Type:    "Error",
		Value:   "WRONGTYPE Operation against a key holding the wrong kind of value",
		IsError: true,
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))

	require.NotNil(t, txn.Error)
	assert.Equal(t, "WRONGTYPE Operation against a key holding the wrong kind of value", txn.Error.Message)
	assert.Nil(t, txn.ResultSet)
}

func TestRedisCommand_NoArgs(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newRedisInstance(t, factory, os)

	inst.OnRedisCommand(&plugins.RedisCommand{
		Name: "PING",
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))

	assert.Equal(t, "PING", txn.Query)
}

func TestRedisCommand_MultipleArgs(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newRedisInstance(t, factory, os)

	inst.OnRedisCommand(&plugins.RedisCommand{
		Name: "SET",
		Args: []string{"mykey", "myvalue", "EX", "60"},
	})
	inst.OnRedisResult(&plugins.RedisResult{
		Type:  "SimpleString",
		Value: "OK",
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))

	assert.Equal(t, "SET mykey myvalue EX 60", txn.Query)
}

func TestRedisCommand_NotSampled(t *testing.T) {
	factory := newTestFactory(t)
	// Exhaust baseline
	for range 100 {
		factory.shouldSampleDB("GET mykey")
	}
	// Now set rate to 0 so nothing passes
	factory.sampler = NewSampler(factory.sampler.cache, 0, 0)

	os := &mockObjectStore{}
	inst := newRedisInstance(t, factory, os)

	inst.OnRedisCommand(&plugins.RedisCommand{
		Name: "GET",
		Args: []string{"mykey"},
	})
	inst.Destroy()

	assert.Empty(t, os.artifacts)
}

func TestRedisCommand_SizeLimit(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newRedisInstance(t, factory, os)

	inst.OnRedisCommand(&plugins.RedisCommand{
		Name: "GET",
		Args: []string{"key"},
	})
	inst.OnRedisResult(&plugins.RedisResult{
		Type:  "BulkString",
		Value: strings.Repeat("x", defaultCaptureByteLimit+1),
	})
	inst.Destroy()

	assert.Empty(t, os.artifacts)
}

func TestRedisCommand_ArtifactMetadata(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newRedisInstance(t, factory, os)

	inst.OnRedisCommand(&plugins.RedisCommand{
		Name: "GET",
		Args: []string{"key"},
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	a := os.artifacts[0]
	assert.Equal(t, eventstore.ArtifactType_QscanRequest, a.Type)
	assert.Equal(t, "application/json", a.ContentType)
}

func TestRedisCommand_NoResult(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newRedisInstance(t, factory, os)

	inst.OnRedisCommand(&plugins.RedisCommand{
		Name: "GET",
		Args: []string{"key"},
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))
	assert.Nil(t, txn.ResultSet)
	assert.Nil(t, txn.Error)
}
