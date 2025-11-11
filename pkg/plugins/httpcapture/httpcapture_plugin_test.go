package httpcapture

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/plugintest"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/rulekitsvc"
	"github.com/qpoint-io/rulekit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"gopkg.in/yaml.v3"
)

func TestHttpCaptureConfigParsing(t *testing.T) {
	tests := []struct {
		name           string
		configYAML     string
		expectedLevel  CaptureLevel
		expectedFormat OutputFormat
		expectedRules  int
	}{
		{
			name: "Default config",
			configYAML: `
---
`,
			expectedLevel:  CaptureLevelNone,
			expectedFormat: OutputFormatJSON,
			expectedRules:  0,
		},
		{
			name: "Basic config with level and format",
			configYAML: `
level: summary
format: text
`,
			expectedLevel:  CaptureLevelSummary,
			expectedFormat: OutputFormatText,
			expectedRules:  0,
		},
		{
			name: "Config with rules",
			configYAML: `
level: none
format: json
rules:
  - name: "Capture API requests"
    expr: "request.path startsWith '/api/'"
    level: full
  - name: "Capture error responses"
    expr: "response.status >= 400"
    level: headers
    format: text
`,
			expectedLevel:  CaptureLevelNone,
			expectedFormat: OutputFormatJSON,
			expectedRules:  2,
		},
		{
			name: "Config with headers capture level",
			configYAML: `
level: headers
format: json
`,
			expectedLevel:  CaptureLevelHeaders,
			expectedFormat: OutputFormatJSON,
			expectedRules:  0,
		},
		{
			name: "Config with full capture level",
			configYAML: `
level: full
format: text
`,
			expectedLevel:  CaptureLevelFull,
			expectedFormat: OutputFormatText,
			expectedRules:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Parse the YAML config
			var node yaml.Node
			err := yaml.Unmarshal([]byte(tc.configYAML), &node)
			require.NoError(t, err)

			// Create a factory and initialize it with the config
			factory := &Factory{}
			logger, _ := zap.NewDevelopment()
			factory.Init(logger, node)

			// Verify the config was parsed correctly
			assert.Equal(t, tc.expectedLevel, factory.config.Level)
			assert.Equal(t, tc.expectedFormat, factory.config.Format)
			assert.Len(t, factory.config.Rules, tc.expectedRules)
		})
	}
}

func TestCaptureLevelConstants(t *testing.T) {
	tests := []struct {
		level CaptureLevel
		value string
		valid bool
	}{
		{CaptureLevelNone, "none", true},
		{CaptureLevelSummary, "summary", true},
		{CaptureLevelHeaders, "headers", true},
		{CaptureLevelFull, "full", true},
		{CaptureLevel("invalid"), "invalid", false},
	}

	for _, tc := range tests {
		t.Run(string(tc.level), func(t *testing.T) {
			// Verify that the string value matches the expected value
			assert.Equal(t, tc.value, string(tc.level))

			// Verify the capture level is one of the valid constants
			is_valid := tc.level == CaptureLevelNone ||
				tc.level == CaptureLevelSummary ||
				tc.level == CaptureLevelHeaders ||
				tc.level == CaptureLevelFull

			assert.Equal(t, tc.valid, is_valid)
		})
	}
}

func TestOutputFormatConstants(t *testing.T) {
	tests := []struct {
		format OutputFormat
		value  string
		valid  bool
	}{
		{OutputFormatJSON, "json", true},
		{OutputFormatText, "text", true},
		{OutputFormat("invalid"), "invalid", false},
	}

	for _, tc := range tests {
		t.Run(string(tc.format), func(t *testing.T) {
			// Verify that the string value matches the expected value
			assert.Equal(t, tc.value, string(tc.format))

			// Verify the output format is one of the valid constants
			is_valid := tc.format == OutputFormatJSON || tc.format == OutputFormatText

			assert.Equal(t, tc.valid, is_valid)
		})
	}
}

func TestHttpCapturePlugin(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx := t.Context()
	conn := connection.NewConnection(ctx, logger, &connection.OpenEvent{})

	// factory
	pluginConf := &HttpCaptureConfig{
		Level:  CaptureLevelNone,
		Format: OutputFormatJSON,
		Rules: []LogRule{
			{
				Name:  "happy path",
				Expr:  "success_response() && pi() != 5",
				Level: CaptureLevelFull,
			},
		},
	}
	var pluginConfYaml yaml.Node
	err := pluginConfYaml.Encode(pluginConf)
	require.NoError(t, err)

	factory := &Factory{}
	factory.Init(logger, pluginConfYaml)

	// dependencies
	rulekit := &rulekitsvc.Factory{
		Macros: map[string]rulekit.Rule{
			"success_response": rulekit.MustParse("http.res.status >= 200 && http.res.status < 300"),
			"pi": rulekit.RuleFunc(func(ctx *rulekit.Ctx) rulekit.Result {
				return rulekit.Result{Value: float64(math.Pi)}
			}),
		},
	}

	factories := services.NewFactoryRegistry()
	svcs := services.NewServiceRegistry(factories)

	rulekitSvc, err := rulekit.Create(ctx, svcs)
	require.NoError(t, err)
	rulekitSvc.(plugins.ConnectionAdapter).SetConnection(conn)
	factories.Register(services.StaticFactory(rulekitsvc.TypeRulekit, rulekitSvc), "")

	connMetaSvc := plugintest.ConnmetaSvc(t, ctx, conn)
	// setup
	t.Run("match", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockES := eventstore.NewMockEventStore(ctrl)
		ctx := &plugintest.Context{
			T:        t,
			VReqBody: []byte("in"),
			VResBody: []byte("out"),
			VMeta: &plugintest.Meta{
				Service: connMetaSvc,
			},
		}

		mockES.EXPECT().ServiceType().AnyTimes().Return(eventstore.TypeEventStore)
		mockES.EXPECT().Save(gomock.Any(), gomock.All(
			gomock.Cond(func(a *eventstore.Artifact) bool {
				return a.Type == eventstore.ArtifactType_HTTPTransaction && a.ContentType == "application/json"
			}),
			HttpTransactionMatcher(func(tx *HttpTransaction) bool {
				return string(tx.Request.Body) == "in" &&
					string(tx.Response.Body) == "out" &&
					tx.Response.Status == 200
			}),
		)).Return()

		// simulate http tx
		factories.Register(services.StaticFactory(eventstore.TypeEventStore, mockES), "")
		plugin := factory.NewInstance(ctx, svcs)
		plugin.RequestHeaders(nil, true)
		plugin.RequestBody(nil, true)
		// response status 200 should trigger
		plugin.ResponseHeaders(plugintest.Headers(map[string]string{":status": "200"}), true)
		plugin.ResponseBody(nil, true)

		plugin.Destroy()
	})

	t.Run("no match", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mockES := eventstore.NewMockEventStore(ctrl)
		ctx := &plugintest.Context{
			T: t,
		}
		mockES.EXPECT().ServiceType().AnyTimes().Return(eventstore.TypeEventStore)

		// simulate http tx
		factories.Register(services.StaticFactory(eventstore.TypeEventStore, mockES), "")
		plugin := factory.NewInstance(ctx, svcs)
		plugin.RequestHeaders(nil, true)
		plugin.RequestBody(nil, true)
		// response status 403 should NOT trigger
		plugin.ResponseHeaders(plugintest.Headers(map[string]string{":status": "403"}), true)
		plugin.ResponseBody(nil, true)

		plugin.Destroy()
	})
}

func HttpTransactionMatcher(fn func(tx *HttpTransaction) bool) gomock.Matcher {
	return gomock.Cond(func(a *eventstore.Artifact) bool {
		var tx HttpTransaction
		err := json.Unmarshal(a.Data, &tx)
		if err != nil {
			panic("artifact is not an HttpTransaction")
		}
		return fn(&tx)
	})
}
