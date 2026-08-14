package mistral

import (
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/conversations"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/providers"
)

const ProviderName = "mistral"

type Provider struct {
}

func (o *Provider) Name() string {
	return ProviderName
}

func (o *Provider) Hosts() []providers.StringMatcher {
	return []providers.StringMatcher{
		// https://docs.mistral.ai/getting-started/quickstart/
		providers.StringExact("api.mistral.ai"),
	}
}

func (o *Provider) RequestHeaders() []providers.HeaderMatcher {
	return nil
}

func (o *Provider) ResponseHeaders() []providers.HeaderMatcher {
	return nil
}

func (o *Provider) Paths() []providers.StringMatcher {
	return nil
}

func (o *Provider) Handle(
	path string,
	reqHeaders plugins.Headers, reqBody []byte,
	resHeaders plugins.Headers, resBody []byte,
) ([]*conversations.Completion, error) {
	return nil, nil
}
