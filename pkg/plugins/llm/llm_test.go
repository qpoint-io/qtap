package llm

import (
	"testing"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/plugintest"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/connmeta"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gopkg.in/yaml.v3"
)

func TestLLM(t *testing.T) {
	tests := []struct {
		name           string
		config         *LLMConfig
		executeRequest func(plugins.HttpPluginInstance)
		test           func(*testing.T, *plugintest.Context, *plugintest.Logger)
	}{
		{
			name: "non LLM request",
			executeRequest: func(instance plugins.HttpPluginInstance) {
				instance.RequestHeaders(plugintest.Headers(map[string]string{
					":authority": "test.com",
				}), true)
				instance.RequestBody(nil, true)
				instance.ResponseHeaders(nil, true)
				instance.ResponseBody(nil, true)
			},
			test: func(t *testing.T, ctx *plugintest.Context, tl *plugintest.Logger) {
				_, ok := ctx.Meta().Tags().Get(TagLLMProvider)
				assert.False(t, ok)
			},
		},
		{
			name: "host detection",
			executeRequest: func(instance plugins.HttpPluginInstance) {
				instance.RequestHeaders(plugintest.Headers(map[string]string{
					":authority": "loc1-aiplatform.googleapis.com",
				}), true)
				instance.RequestBody(nil, true)
				instance.ResponseHeaders(nil, true)
				instance.ResponseBody(nil, true)
			},
			test: func(t *testing.T, ctx *plugintest.Context, tl *plugintest.Logger) {
				values, ok := ctx.Meta().Tags().Get(TagLLMProvider)
				assert.True(t, ok)
				assert.Equal(t, []string{"google"}, values)
			},
		},
		{
			name: "response header detection",
			executeRequest: func(instance plugins.HttpPluginInstance) {
				instance.RequestHeaders(plugintest.Headers(nil), true)
				instance.RequestBody(nil, true)
				instance.ResponseHeaders(plugintest.Headers(map[string]string{
					"anthropic-organization-id": "test",
				}), true)
				instance.ResponseBody(nil, true)
			},
			test: func(t *testing.T, ctx *plugintest.Context, tl *plugintest.Logger) {
				values, ok := ctx.Meta().Tags().Get(TagLLMProvider)
				assert.True(t, ok)
				assert.Equal(t, []string{"anthropic"}, values)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			tl := plugintest.NewLogger(t)
			defer tl.Sync()

			svcFactoryRegistry := services.NewFactoryRegistry()
			conn := connection.NewConnection(t.Context(), tl.Logger, &connection.OpenEvent{}, connection.WithServiceFactoryRegistry(svcFactoryRegistry))
			svcFactoryRegistry.Register(services.StaticFactory(connmeta.Type, plugintest.ConnmetaSvc(t, t.Context(), conn)), "")

			ctx := &plugintest.Context{
				T:     t,
				VMeta: plugintest.NewMeta(t, conn),
			}

			mockOS := objectstore.NewMockObjectStore(ctrl)
			mockOS.EXPECT().ServiceType().AnyTimes().Return(objectstore.TypeObjectStore)
			svcFactoryRegistry.Register(services.StaticFactory(objectstore.TypeObjectStore, mockOS), "")

			factory := &Factory{logger: tl.Logger}
			factory.Init(tl.Logger, yaml.Node{})
			instance := factory.NewHttpInstance(ctx, conn.ServiceRegistry())
			require.NotNil(t, instance)

			tt.executeRequest(instance)
			instance.Destroy()
			factory.Destroy()
			tl.Sync()
			if tt.test != nil {
				tt.test(t, ctx, tl)
			}
		})
	}
}
