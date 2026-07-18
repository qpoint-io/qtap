package llm

import (
	"github.com/qpoint-io/qtap/pkg/plugins/llm/providers"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/providers/anthropic"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/providers/google"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/providers/mistral"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/providers/openai"
)

var Providers = []providers.Provider{
	&openai.Provider{},
	&anthropic.Provider{},
	&google.Provider{},
	&mistral.Provider{},
}
