package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/responses"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/conversations"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/providers"
)

const ProviderName = "openai"

type Provider struct {
	Now func() time.Time
}

func (o *Provider) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o *Provider) Name() string {
	return ProviderName
}

func (o *Provider) Hosts() []providers.StringMatcher {
	return []providers.StringMatcher{
		providers.StringExact("api.openai.com"),
		providers.StringExact("eu.openai.com"),
	}
}

func (o *Provider) RequestHeaders() []providers.HeaderMatcher {
	return nil
}

func (o *Provider) ResponseHeaders() []providers.HeaderMatcher {
	return []providers.HeaderMatcher{
		{Name: "openai-organization"},
		{Name: "openai-version"},
	}
}

func (o *Provider) Paths() []providers.StringMatcher {
	return []providers.StringMatcher{
		// https://platform.openai.com/docs/api-reference/chat/create
		providers.StringExact("/v1/chat/completions"),
		// https://platform.openai.com/docs/api-reference/completions/create
		// TODO: support legacy endpoint
		providers.StringExact("/v1/completions"),
	}
}

func (o *Provider) Handle(
	path string,
	reqHeaders plugins.Headers, reqBody []byte,
	resHeaders plugins.Headers, resBody []byte,
) ([]*conversations.Completion, error) {
	switch path {
	case "/v1/chat/completions":
		return o.handleChatCompletions(reqHeaders, reqBody, resHeaders, resBody)
	case "/v1/responses":
		return o.handleResponses(reqHeaders, reqBody, resHeaders, resBody)
	}

	return nil, nil
}

func (o *Provider) handleChatCompletions(
	reqHeaders plugins.Headers, reqBody []byte,
	resHeaders plugins.Headers, resBody []byte,
) ([]*conversations.Completion, error) {
	// request
	var req openai.ChatCompletionNewParams
	if err := json.Unmarshal(reqBody, &req); err != nil {
		return nil, fmt.Errorf("unmarshalling request body: %w", err)
	}

	if req.Model == "" || len(req.Messages) == 0 {
		return nil, errors.New("missing model or messages")
	}

	messages := make([]*conversations.Message, len(req.Messages))
	for i, msg := range req.Messages {
		var m conversations.Message
		if role := msg.GetRole(); role != nil {
			m.Role = string(*role)
		}

		// TODO: support different content types.
		if content, ok := msg.GetContent().AsAny().(*string); ok {
			m.Content = []conversations.MessageContent{{Data: *content}}
		}
		messages[i] = &m
	}

	reqComp := &conversations.Completion{
		Provider:  ProviderName,
		Model:     req.Model,
		Messages:  messages,
		Timestamp: o.now(),
	}

	// response
	var (
		choices []openai.ChatCompletionChoice
		err     error
	)

	var contentType string
	if ct, ok := resHeaders.Get("content-type"); ok {
		contentType = strings.ToLower(ct.String())
	}

	switch {
	case strings.HasPrefix(contentType, "text/event-stream"):
		choices, err = parseSSE(resBody)

	case strings.HasPrefix(contentType, "application/json") || strings.HasSuffix(contentType, "+json"):
		var comp openai.ChatCompletion
		err = json.Unmarshal(resBody, &comp)
		if err == nil {
			choices = comp.Choices
		}

	default:
		err = fmt.Errorf("unsupported content type: %s", contentType)
	}
	if err != nil {
		return []*conversations.Completion{reqComp}, err
	}

	// assemble choices into completions
	if len(choices) == 0 {
		return []*conversations.Completion{reqComp}, nil
	}

	if len(choices) == 1 {
		msg := choices[0].Message
		if msg.Refusal == "" {
			reqComp.Messages = append(reqComp.Messages, &conversations.Message{
				Role:    string(msg.Role),
				Content: []conversations.MessageContent{{Data: msg.Content}},
			})
		}
		return []*conversations.Completion{reqComp}, nil
	}

	// if n > 1, store request separately and create a completion for each choice.
	completions := []*conversations.Completion{reqComp}

	for _, choice := range choices {
		if choice.Message.Refusal != "" {
			continue
		}

		messages := make([]*conversations.Message, 0, len(reqComp.Messages)+1)
		messages = append(messages, reqComp.Messages...)
		messages = append(messages, &conversations.Message{
			Role:    string(choice.Message.Role),
			Content: []conversations.MessageContent{{Data: choice.Message.Content}},
		})

		completions = append(completions, &conversations.Completion{
			Provider:  reqComp.Provider,
			Model:     reqComp.Model,
			Timestamp: reqComp.Timestamp,
			Messages:  messages,
		})
	}

	return completions, nil
}

func parseSSE(body []byte) ([]openai.ChatCompletionChoice, error) {
	stream := ssestream.NewStream[openai.ChatCompletionChunk](ssestream.NewDecoder(&http.Response{
		Body: io.NopCloser(bytes.NewReader(body)),
	}), nil)

	var acc openai.ChatCompletionAccumulator
	for stream.Next() {
		chunk := stream.Current()
		if !acc.AddChunk(chunk) {
			return nil, errors.New("failed to parse SSE event")
		}
	}
	if stream.Err() != nil {
		return nil, stream.Err()
	}
	return acc.Choices, nil
}

func (o *Provider) handleResponses(
	reqHeaders plugins.Headers, reqBody []byte,
	resHeaders plugins.Headers, resBody []byte,
) ([]*conversations.Completion, error) {
	var req responses.ResponseNewParams
	if err := json.Unmarshal(reqBody, &req); err != nil {
		return nil, fmt.Errorf("unmarshalling request body: %w", err)
	}

	if req.Model == "" {
		return nil, errors.New("missing model field")
	}

	var messages []*conversations.Message

	// request
	// req.Input is a union so only one will be set.
	switch {
	case req.Input.OfString.Valid():
		messages = []*conversations.Message{
			{
				Role:    "user",
				Content: []conversations.MessageContent{{Data: req.Input.OfString.Value}},
			},
		}

	case len(req.Input.OfInputItemList) > 0:
		for _, item := range req.Input.OfInputItemList {
			var content conversations.MessageContent

			// set the type
			if t := item.GetType(); t != nil {
				content.Type = *t
			}
			// store the data as JSON.
			j, err := json.Marshal(item)
			if err != nil {
				continue
			}
			content.Data = string(j)

			var role string
			if r := item.GetRole(); r != nil {
				role = *r
			}

			messages = append(messages, &conversations.Message{
				Role:    role,
				Content: []conversations.MessageContent{content},
			})
		}

	default:
		return nil, errors.New("missing or invalid input field")
	}

	reqComp := &conversations.Completion{
		Provider:  ProviderName,
		Model:     string(req.Model),
		Messages:  messages,
		Timestamp: o.now(),
	}
	if req.PreviousResponseID.Valid() {
		reqComp.ProviderParentID = req.PreviousResponseID.Value
	}

	// response
	var res responses.Response
	if err := json.Unmarshal(resBody, &res); err != nil {
		return []*conversations.Completion{reqComp}, fmt.Errorf("unmarshalling response body: %w", err)
	}

	// Check response status
	if res.Status != responses.ResponseStatusCompleted {
		reqComp.Error = string(res.Error.Code)
		// For incomplete responses, return the request completion
		return []*conversations.Completion{reqComp}, nil
	}

	// Store the provider-specific ID.
	reqComp.ProviderID = res.ID

	for _, item := range res.Output {
		var content conversations.MessageContent

		// set the type
		content.Type = item.Type
		// store the data as JSON.
		j, err := json.Marshal(item.AsAny())
		if err != nil {
			continue
		}
		content.Data = string(j)

		reqComp.Messages = append(reqComp.Messages, &conversations.Message{
			Role:    string(item.Role),
			Content: []conversations.MessageContent{content},
		})
	}

	return []*conversations.Completion{reqComp}, nil
}
