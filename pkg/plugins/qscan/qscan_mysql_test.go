package qscan

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/tags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockObjectStore captures artifacts for testing
type mockObjectStore struct {
	artifacts []*eventstore.Artifact
}

func (m *mockObjectStore) Put(_ context.Context, a *eventstore.Artifact) {
	m.artifacts = append(m.artifacts, a)
}

func (m *mockObjectStore) ServiceType() services.ServiceType { return "objectstore" }

// mockPluginContext provides a minimal PluginContext for testing
type mockPluginContext struct {
	meta *mockMeta
}

func (c *mockPluginContext) GetRequestBodyBuffer() plugins.BodyBuffer  { return nil }
func (c *mockPluginContext) GetResponseBodyBuffer() plugins.BodyBuffer { return nil }
func (c *mockPluginContext) Context() context.Context                  { return context.Background() }
func (c *mockPluginContext) Meta() plugins.Meta                        { return c.meta }

type mockMeta struct {
	requestID  string
	endpoint   string
	readBytes  int64
	writeBytes int64
}

func (m *mockMeta) RequestID() string                 { return m.requestID }
func (m *mockMeta) Endpoint() string                  { return m.endpoint }
func (m *mockMeta) Direction() string                 { return "egress" }
func (m *mockMeta) ConnectionID() string              { return "conn-001" }
func (m *mockMeta) Tags() tags.List                   { return nil }
func (m *mockMeta) Protocol() string                  { return "mysql" }
func (m *mockMeta) Process() *process.Process         { return nil }
func (m *mockMeta) ServiceType() services.ServiceType { return "connmeta" }
func (m *mockMeta) ReadBytes() int64                  { return m.readBytes }
func (m *mockMeta) WriteBytes() int64                 { return m.writeBytes }
func (m *mockMeta) SetReadBytes(b int64)              { m.readBytes = b }
func (m *mockMeta) SetWriteBytes(b int64)             { m.writeBytes = b }

func newTestFactory(t *testing.T) *Factory {
	t.Helper()
	cache := expirable.NewLRU[string, uint32](4096, nil, time.Hour)
	return &Factory{
		logger: zap.NewNop(),
		config: &QscanConfig{
			SampleBaseline: 100,
			SampleRate:     0.1,
		},
		sampler: NewSampler(cache, 100, 0.1),
	}
}

func newMySQLInstance(t *testing.T, factory *Factory, os *mockObjectStore) *mysqlFilterInstance {
	t.Helper()
	return &mysqlFilterInstance{
		logger: zap.NewNop(),
		ctx: &mockPluginContext{
			meta: &mockMeta{requestID: "test-req-001", endpoint: "ep-001"},
		},
		objectstore: os,
		config:      factory.config,
		factory:     factory,
	}
}

func TestMySQLCommand_ResultSetZipped(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newMySQLInstance(t, factory, os)

	status := inst.OnMySQLCommand(&plugins.MySQLCommand{
		Type:      comQuery,
		Query:     "SELECT name, email FROM customers WHERE id = 1",
		Timestamp: time.Now(),
	})
	assert.Equal(t, plugins.MySQLStatusContinue, status)

	inst.OnMySQLResult(&plugins.MySQLResult{
		Type:    "ResultSet",
		Columns: []string{"name", "email"},
		Rows: [][]string{
			{"John Smith", "john@example.com"},
			{"Jane Doe", "jane@example.com"},
		},
	})

	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))

	assert.Equal(t, "db", txn.TransactionType)
	assert.Equal(t, "mysql", txn.DBType)
	assert.Equal(t, "SELECT name, email FROM customers WHERE id = 1", txn.Query)
	assert.Equal(t, "test-req-001", txn.RequestID)
	assert.Nil(t, txn.Error)
	assert.False(t, txn.Truncated)

	require.Len(t, txn.ResultSet, 2)
	assert.Equal(t, "John Smith", txn.ResultSet[0]["name"])
	assert.Equal(t, "john@example.com", txn.ResultSet[0]["email"])
	assert.Equal(t, "Jane Doe", txn.ResultSet[1]["name"])
	assert.Equal(t, "jane@example.com", txn.ResultSet[1]["email"])
}

func TestMySQLCommand_OKResult(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newMySQLInstance(t, factory, os)

	inst.OnMySQLCommand(&plugins.MySQLCommand{
		Type:  comQuery,
		Query: "INSERT INTO users (name) VALUES ('Alice')",
	})
	inst.OnMySQLResult(&plugins.MySQLResult{
		Type:         "OK",
		AffectedRows: 1,
		LastInsertID: 42,
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))

	require.Len(t, txn.ResultSet, 1)
	// JSON unmarshals numbers as float64 into map[string]any
	assert.EqualValues(t, 1, txn.ResultSet[0]["affected_rows"])
	assert.EqualValues(t, 42, txn.ResultSet[0]["last_insert_id"])
	assert.Nil(t, txn.Error)
}

func TestMySQLCommand_ErrorResult(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newMySQLInstance(t, factory, os)

	inst.OnMySQLCommand(&plugins.MySQLCommand{
		Type:  comQuery,
		Query: "SELECT * FROM nonexistent",
	})
	inst.OnMySQLResult(&plugins.MySQLResult{
		Type:         "Error",
		ErrorCode:    1146,
		ErrorMessage: "Table 'nonexistent' doesn't exist",
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))

	require.NotNil(t, txn.Error)
	assert.Equal(t, 1146, txn.Error.Code)
	assert.Equal(t, "Table 'nonexistent' doesn't exist", txn.Error.Message)
	assert.Nil(t, txn.ResultSet)
}

func TestMySQLCommand_TruncatedResult(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newMySQLInstance(t, factory, os)

	inst.OnMySQLCommand(&plugins.MySQLCommand{
		Type:  comQuery,
		Query: "SELECT * FROM large_table",
	})
	inst.OnMySQLResult(&plugins.MySQLResult{
		Type:      "ResultSet",
		Columns:   []string{"id"},
		Rows:      [][]string{{"1"}, {"2"}},
		RowCount:  10000,
		Truncated: true,
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))

	assert.True(t, txn.Truncated)
	require.Len(t, txn.ResultSet, 2)
}

func TestMySQLCommand_NonQuerySkipped(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newMySQLInstance(t, factory, os)

	// COM_PING (0x0e) should be skipped
	inst.OnMySQLCommand(&plugins.MySQLCommand{
		Type:  0x0e,
		Query: "",
	})
	inst.Destroy()

	assert.Empty(t, os.artifacts)
}

func TestMySQLCommand_EmptyQuerySkipped(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newMySQLInstance(t, factory, os)

	inst.OnMySQLCommand(&plugins.MySQLCommand{
		Type:  comQuery,
		Query: "",
	})
	inst.Destroy()

	assert.Empty(t, os.artifacts)
}

func TestMySQLCommand_StmtPrepareAccepted(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newMySQLInstance(t, factory, os)

	inst.OnMySQLCommand(&plugins.MySQLCommand{
		Type:  comStmtPrepare,
		Query: "SELECT * FROM users WHERE id = ?",
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))
	assert.Equal(t, "SELECT * FROM users WHERE id = ?", txn.Query)
}

func TestMySQLCommand_NotSampled(t *testing.T) {
	// Use baseline=0 and rate=0 so nothing is sampled
	cache := expirable.NewLRU[string, uint32](4096, nil, time.Hour)
	factory := &Factory{
		logger: zap.NewNop(),
		config: &QscanConfig{
			SampleBaseline: 0,
			SampleRate:     0,
		},
		sampler: NewSampler(cache, 0, 0),
	}
	os := &mockObjectStore{}
	inst := newMySQLInstance(t, factory, os)

	inst.OnMySQLCommand(&plugins.MySQLCommand{
		Type:  comQuery,
		Query: "SELECT 1",
	})
	inst.Destroy()

	assert.Empty(t, os.artifacts)
}

func TestMySQLCommand_SizeLimit(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newMySQLInstance(t, factory, os)

	// Create a query that will produce a >1MB JSON payload
	bigQuery := strings.Repeat("x", defaultCaptureByteLimit+1)
	inst.OnMySQLCommand(&plugins.MySQLCommand{
		Type:  comQuery,
		Query: bigQuery,
	})
	inst.Destroy()

	assert.Empty(t, os.artifacts)
}

func TestMySQLCommand_NilObjectStore(t *testing.T) {
	factory := newTestFactory(t)
	inst := &mysqlFilterInstance{
		logger: zap.NewNop(),
		ctx: &mockPluginContext{
			meta: &mockMeta{requestID: "test-req-001", endpoint: "ep-001"},
		},
		objectstore: nil,
		config:      factory.config,
		factory:     factory,
	}

	inst.OnMySQLCommand(&plugins.MySQLCommand{
		Type:  comQuery,
		Query: "SELECT 1",
	})
	inst.Destroy()
	// Should not panic, just increment error metric
}

func TestMySQLCommand_ArtifactMetadata(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newMySQLInstance(t, factory, os)

	inst.OnMySQLCommand(&plugins.MySQLCommand{
		Type:  comQuery,
		Query: "SELECT 1",
	})
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	a := os.artifacts[0]
	assert.Equal(t, eventstore.ArtifactType_QscanRequest, a.Type)
	assert.Equal(t, "application/json", a.ContentType)
}

func TestMySQLCommand_NoResult(t *testing.T) {
	factory := newTestFactory(t)
	os := &mockObjectStore{}
	inst := newMySQLInstance(t, factory, os)

	inst.OnMySQLCommand(&plugins.MySQLCommand{
		Type:  comQuery,
		Query: "SELECT 1",
	})
	// No OnMySQLResult call
	inst.Destroy()

	require.Len(t, os.artifacts, 1)
	var txn DBTransactionRequest
	require.NoError(t, json.Unmarshal(os.artifacts[0].Data, &txn))
	assert.Equal(t, "SELECT 1", txn.Query)
	assert.Nil(t, txn.ResultSet)
	assert.Nil(t, txn.Error)
}

func TestZipMySQLRows(t *testing.T) {
	tests := []struct {
		name     string
		columns  []string
		rows     [][]string
		expected []map[string]any
	}{
		{
			name:    "basic zip",
			columns: []string{"name", "age"},
			rows:    [][]string{{"Alice", "30"}, {"Bob", "25"}},
			expected: []map[string]any{
				{"name": "Alice", "age": "30"},
				{"name": "Bob", "age": "25"},
			},
		},
		{
			name:     "empty rows",
			columns:  []string{"name"},
			rows:     [][]string{},
			expected: []map[string]any{},
		},
		{
			name:    "row shorter than columns",
			columns: []string{"a", "b", "c"},
			rows:    [][]string{{"1", "2"}},
			expected: []map[string]any{
				{"a": "1", "b": "2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := zipMySQLRows(tt.columns, tt.rows)
			require.Len(t, result, len(tt.expected))
			for i, row := range result {
				for k, v := range tt.expected[i] {
					assert.Equal(t, v, row[k], "row %d, key %s", i, k)
				}
			}
		})
	}
}
