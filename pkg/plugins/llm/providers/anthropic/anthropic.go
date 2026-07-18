package anthropic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/conversations"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/providers"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/providers/openai"
)

const ProviderName = "anthropic"

type Provider struct {
	Now func() time.Time

	openai *openai.Provider
}

func (o *Provider) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o *Provider) openAI() *openai.Provider {
	if o.openai == nil {
		o.openai = &openai.Provider{
			Now: func() time.Time { return o.now() },
		}
	}
	return o.openai
}

func (o *Provider) Name() string {
	return ProviderName
}

func (o *Provider) Hosts() []providers.StringMatcher {
	return []providers.StringMatcher{
		// https://docs.anthropic.com/en/api/overview#examples
		providers.StringExact("api.anthropic.com"),
	}
}

func (o *Provider) RequestHeaders() []providers.HeaderMatcher {
	return nil
}

func (o *Provider) ResponseHeaders() []providers.HeaderMatcher {
	return []providers.HeaderMatcher{
		// https://docs.anthropic.com/en/api/overview#response-headers
		{Name: "anthropic-organization-id"},
	}
}

func (o *Provider) Paths() []providers.StringMatcher {
	return []providers.StringMatcher{
		// https://docs.anthropic.com/en/api/messages
		providers.StringExact("/v1/messages"),

		// OpenAI API compatibility layer
		// https://docs.anthropic.com/en/api/openai-sdk
		providers.StringExact("/v1/chat/completions"),
	}
}

func (o *Provider) Handle(
	path string,
	reqHeaders plugins.Headers, reqBody []byte,
	resHeaders plugins.Headers, resBody []byte,
) ([]*conversations.Completion, error) {
	switch path {
	case "/v1/messages":
		return o.handleMessages(reqHeaders, reqBody, resHeaders, resBody)
	case "/v1/chat/completions":
		return rewriteProvider(o.openAI().Handle(path, reqHeaders, reqBody, resHeaders, resBody))
	}

	return nil, nil
}

func (o *Provider) handleMessages(
	reqHeaders plugins.Headers, reqBody []byte,
	resHeaders plugins.Headers, resBody []byte,
) ([]*conversations.Completion, error) {
	// request
	var req anthropic.MessageNewParams
	if err := json.Unmarshal(reqBody, &req); err != nil {
		return nil, fmt.Errorf("unmarshalling request body: %w", err)
	}

	if req.Model == "" || len(req.Messages) == 0 {
		return nil, errors.New("missing model or messages")
	}

	messages := make([]*conversations.Message, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = parseMessage(msg)
	}

	reqComp := &conversations.Completion{
		Provider:  ProviderName,
		Model:     string(req.Model),
		Messages:  messages,
		Timestamp: o.now(),
	}

	// response
	var (
		comp anthropic.Message
		err  error
	)

	var contentType string
	if ct, ok := resHeaders.Get("content-type"); ok {
		contentType = strings.ToLower(ct.String())
	}

	switch {
	case strings.HasPrefix(contentType, "text/event-stream"):
		comp, err = parseMessagesSSE(resBody)

	case strings.HasPrefix(contentType, "application/json") || strings.HasSuffix(contentType, "+json"):
		err = json.Unmarshal(resBody, &comp)

	default:
		err = fmt.Errorf("unsupported content type: %s", contentType)
	}
	if err != nil {
		return []*conversations.Completion{reqComp}, err
	}

	var content []conversations.MessageContent
	for _, c := range comp.Content {
		// set the type and store the data as json.
		j, err := c.ToParam().MarshalJSON()
		if err != nil {
			continue
		}

		content = append(content, conversations.MessageContent{
			Type: c.Type,
			Data: string(j),
		})
	}

	reqComp.Messages = append(reqComp.Messages, &conversations.Message{
		Role:    string(comp.Role),
		Content: content,
	})

	return []*conversations.Completion{reqComp}, nil
}

func parseMessage(msg anthropic.MessageParam) *conversations.Message {
	content := make([]conversations.MessageContent, 0, len(msg.Content))
	for _, item := range msg.Content {
		var c conversations.MessageContent
		if t := item.GetType(); t != nil {
			c.Type = *t
		}

		j, err := item.MarshalJSON()
		if err != nil {
			continue
		}
		c.Data = string(j)

		content = append(content, c)
	}

	return &conversations.Message{
		Role:    string(msg.Role),
		Content: content,
	}
}

func parseMessagesSSE(body []byte) (anthropic.Message, error) {
	stream := ssestream.NewStream[anthropic.MessageStreamEventUnion](ssestream.NewDecoder(&http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}), nil)

	var msg anthropic.Message
	for stream.Next() {
		ev := stream.Current()
		err := msg.Accumulate(ev)
		if err != nil {
			return msg, err
		}
	}
	if stream.Err() != nil {
		return msg, stream.Err()
	}

	return msg, nil
}

func rewriteProvider(completions []*conversations.Completion, err error) ([]*conversations.Completion, error) {
	for _, comp := range completions {
		comp.Provider = ProviderName
	}
	return completions, err
}
