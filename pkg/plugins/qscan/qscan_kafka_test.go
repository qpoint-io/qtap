package qscan

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newKafkaInstance(t *testing.T, factory *Factory, os *mockObjectStore) *kafkaFilterInstance {
	t.Helper()
	return &kafkaFilterInstance{
		logger: zap.NewNop(),
		ctx: &mockPluginContext{
			meta: &mockMeta{requestID: "kafka-req-001", endpoint: "ep-kafka"},
		},
		objectstore: os,
		config:      factory.config,
		factory:     factory,
	}
}

func TestKafkaCommand_ProduceWithMessages(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newKafkaInstance(t, factory, os)

	status := inst.OnKafkaCommand(&plugins.KafkaCommand{
		Operation: "Produce",
		Topics:    []string{"orders"},
		Messages: []plugins.KafkaMessage{
			{Topic: "orders", Partition: 0, Key: "order-1", Value: `{"item":"widget"}`},
			{Topic: "orders", Partition: 1, Key: "order-2", Value: `{"item":"gadget"}`},
		},
		Timestamp: time.Now(),
	})
	assert.Equal(t, plugins.KafkaStatusContinue, status)

	inst.OnKafkaResult(&plugins.KafkaResult{
		IsError: false,
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))

	assert.Equal(t, "db", txn.TransactionType)
	assert.Equal(t, "kafka", txn.DBType)
	assert.Equal(t, "Produce orders", txn.Query)
	assert.Equal(t, "kafka-req-001", txn.RequestID)
	assert.Nil(t, txn.Error)

	require.Len(t, txn.ResultSet, 2)
	assert.Equal(t, "orders", txn.ResultSet[0]["topic"])
	assert.Equal(t, "order-1", txn.ResultSet[0]["key"])
	assert.Equal(t, `{"item":"widget"}`, txn.ResultSet[0]["value"].(string)) //nolint:testifylint // comparing string values, not JSON
}

func TestKafkaCommand_FetchWithResponseMessages(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newKafkaInstance(t, factory, os)

	inst.OnKafkaCommand(&plugins.KafkaCommand{
		Operation: "Fetch",
		Topics:    []string{"events"},
	})
	inst.OnKafkaResult(&plugins.KafkaResult{
		Messages: []plugins.KafkaMessage{
			{Topic: "events", Partition: 0, Key: "evt-1", Value: "payload1"},
			{Topic: "events", Partition: 0, Key: "evt-2", Value: "payload2"},
		},
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))

	assert.Equal(t, "Fetch events", txn.Query)
	require.Len(t, txn.ResultSet, 2)
	assert.Equal(t, "evt-1", txn.ResultSet[0]["key"])
	assert.Equal(t, "evt-2", txn.ResultSet[1]["key"])
}

func TestKafkaCommand_CombinesCommandAndResultMessages(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newKafkaInstance(t, factory, os)

	inst.OnKafkaCommand(&plugins.KafkaCommand{
		Operation: "Produce",
		Topics:    []string{"topic1"},
		Messages: []plugins.KafkaMessage{
			{Topic: "topic1", Key: "cmd-msg"},
		},
	})
	inst.OnKafkaResult(&plugins.KafkaResult{
		Messages: []plugins.KafkaMessage{
			{Topic: "topic1", Key: "res-msg"},
		},
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))

	require.Len(t, txn.ResultSet, 2)
	assert.Equal(t, "cmd-msg", txn.ResultSet[0]["key"])
	assert.Equal(t, "res-msg", txn.ResultSet[1]["key"])
}

func TestKafkaCommand_ErrorResult(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newKafkaInstance(t, factory, os)

	inst.OnKafkaCommand(&plugins.KafkaCommand{
		Operation: "Produce",
		Topics:    []string{"topic1"},
	})
	inst.OnKafkaResult(&plugins.KafkaResult{
		IsError:      true,
		ErrorCode:    3,
		ErrorMessage: "UNKNOWN_TOPIC_OR_PARTITION",
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))

	require.NotNil(t, txn.Error)
	assert.Equal(t, 3, txn.Error.Code)
	assert.Equal(t, "UNKNOWN_TOPIC_OR_PARTITION", txn.Error.Message)
}

func TestKafkaCommand_WithGroupID(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newKafkaInstance(t, factory, os)

	inst.OnKafkaCommand(&plugins.KafkaCommand{
		Operation: "Fetch",
		Topics:    []string{"events"},
		GroupID:   "my-consumer-group",
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))

	assert.Equal(t, "Fetch events group=my-consumer-group", txn.Query)
}

func TestKafkaCommand_NotSampled(t *testing.T) {
	factory := newTestFactory(t)
	factory.sampler = NewSampler(factory.sampler.cache, 0, 0)

	os := &mockObjectStore{}
	inst := newKafkaInstance(t, factory, os)

	inst.OnKafkaCommand(&plugins.KafkaCommand{
		Operation: "Produce",
		Topics:    []string{"topic1"},
	})
	inst.Destroy()

	assert.Empty(t, os.artifacts)
}

func TestKafkaCommand_ArtifactMetadata(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newKafkaInstance(t, factory, os)

	inst.OnKafkaCommand(&plugins.KafkaCommand{
		Operation: "Fetch",
		Topics:    []string{"topic1"},
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	a := os.artifacts[0]
	assert.Equal(t, eventstore.ArtifactType_QscanRequest, a.Type)
	assert.Equal(t, "application/json", a.ContentType)
}

func TestKafkaCommand_NoResult(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newKafkaInstance(t, factory, os)

	inst.OnKafkaCommand(&plugins.KafkaCommand{
		Operation: "ApiVersions",
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))

	assert.Equal(t, "ApiVersions", txn.Query)
	assert.Nil(t, txn.ResultSet)
	assert.Nil(t, txn.Error)
}
