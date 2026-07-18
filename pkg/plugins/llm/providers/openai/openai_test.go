package openai

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared/constant"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/conversations"
	"github.com/stretchr/testify/require"
)

func TestCompletion(t *testing.T) {
	reqBody := `{"model":"gpt-4.1-nano-2025-04-14","messages":[{"role":"user","content":"Say a random number"}]}`
	resBody := `{"choices":[{"index":0,"message":{"role":"assistant","content":"9"}}],"model":"gpt-4.1-nano-2025-04-14"}`

	provider := &Provider{
		Now: func() time.Time { return time.Unix(0, 0) },
	}

	resHeaders := http.Header{}
	resHeaders.Set("content-type", "application/json")

	completions, err := provider.Handle("/v1/chat/completions", plugins.NewHeaders(nil), []byte(reqBody), plugins.NewHeaders(resHeaders), []byte(resBody))
	require.NoError(t, err)
	require.Equal(t, []*conversations.Completion{
		{
			Provider:  "openai",
			Model:     "gpt-4.1-nano-2025-04-14",
			Timestamp: time.Unix(0, 0),
			Messages: []*conversations.Message{
				conversations.NewTextMessage("user", "Say a random number"),
				conversations.NewTextMessage("assistant", "9"),
			},
		},
	}, completions)
}

func TestCompletion_multipleChoices(t *testing.T) {
	reqBody := `{"model":"gpt-4.1-nano-2025-04-14","n":2,"messages":[{"role":"user","content":"Say a random number"}]}`
	resBody := `{"choices":[
		{"index":0,"message":{"role":"assistant","content":"9"}},
		{"index":1,"message":{"role":"assistant","content":"42"}}
	]}`

	resHeaders := http.Header{}
	resHeaders.Set("content-type", "application/json")

	provider := &Provider{
		Now: func() time.Time { return time.Unix(0, 0) },
	}

	completions, err := provider.Handle("/v1/chat/completions", plugins.NewHeaders(nil), []byte(reqBody), plugins.NewHeaders(resHeaders), []byte(resBody))
	require.NoError(t, err)
	require.Equal(t, []*conversations.Completion{
		{
			Provider:  "openai",
			Model:     "gpt-4.1-nano-2025-04-14",
			Timestamp: time.Unix(0, 0),
			Messages: []*conversations.Message{
				conversations.NewTextMessage("user", "Say a random number"),
			},
		},
		{
			Provider:  "openai",
			Model:     "gpt-4.1-nano-2025-04-14",
			Timestamp: time.Unix(0, 0),
			Messages: []*conversations.Message{
				conversations.NewTextMessage("user", "Say a random number"),
				conversations.NewTextMessage("assistant", "9"),
			},
		},
		{
			Provider:  "openai",
			Model:     "gpt-4.1-nano-2025-04-14",
			Timestamp: time.Unix(0, 0),
			Messages: []*conversations.Message{
				conversations.NewTextMessage("user", "Say a random number"),
				conversations.NewTextMessage("assistant", "42"),
			},
		},
	}, completions)
}

func TestSSE(t *testing.T) {
	headers := http.Header{}
	headers.Set("content-type", "text/event-stream; charset=utf-8")

	reqBody := `{"model":"gpt-4.1-nano-2025-04-14","stream":true,"messages":[{"role":"user","content":"Hello!"}]}`

	resBody := `data: {"object":"chat.completion.chunk","model":"gpt-4.1-nano-2025-04-14","choices":[{"index":0,"delta":{"role":"assistant","content":"","refusal":null}}]}

data: {"object":"chat.completion.chunk","model":"gpt-4.1-nano-2025-04-14","choices":[{"index":0,"delta":{"content":"Hello"}}]}

data: {"object":"chat.completion.chunk","model":"gpt-4.1-nano-2025-04-14","choices":[{"index":0,"delta":{"content":"!"}}]}

data: {"object":"chat.completion.chunk","model":"gpt-4.1-nano-2025-04-14","choices":[{"index":0,"delta":{"content":" How"}}]}

data: {"object":"chat.completion.chunk","model":"gpt-4.1-nano-2025-04-14","choices":[{"index":0,"delta":{"content":" can"}}]}

data: {"object":"chat.completion.chunk","model":"gpt-4.1-nano-2025-04-14","choices":[{"index":0,"delta":{"content":" I"}}]}

data: {"object":"chat.completion.chunk","model":"gpt-4.1-nano-2025-04-14","choices":[{"index":0,"delta":{"content":" assist"}}]}

data: {"object":"chat.completion.chunk","model":"gpt-4.1-nano-2025-04-14","choices":[{"index":0,"delta":{"content":" you"}}]}

data: {"object":"chat.completion.chunk","model":"gpt-4.1-nano-2025-04-14","choices":[{"index":0,"delta":{"content":" today"}}]}

data: {"object":"chat.completion.chunk","model":"gpt-4.1-nano-2025-04-14","choices":[{"index":0,"delta":{"content":"?"}}]}

data: {"object":"chat.completion.chunk","model":"gpt-4.1-nano-2025-04-14","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

	t.Run("parseSSE", func(t *testing.T) {
		parsedBody, err := parseSSE([]byte(resBody))
		require.NoError(t, err)
		require.Equal(t, []openai.ChatCompletionChoice{
			{
				Message: openai.ChatCompletionMessage{
					Role:    constant.Assistant("assistant"),
					Content: "Hello! How can I assist you today?",
				},
				FinishReason: "stop",
			},
		}, parsedBody)
	})

	t.Run("handle", func(t *testing.T) {
		provider := &Provider{
			Now: func() time.Time { return time.Unix(0, 0) },
		}
		completions, err := provider.Handle("/v1/chat/completions", plugins.NewHeaders(nil), []byte(reqBody), plugins.NewHeaders(headers), []byte(resBody))
		require.NoError(t, err)
		require.Equal(t, []*conversations.Completion{
			{
				Provider:  "openai",
				Model:     "gpt-4.1-nano-2025-04-14",
				Timestamp: time.Unix(0, 0),
				Messages: []*conversations.Message{
					conversations.NewTextMessage("user", "Hello!"),
					conversations.NewTextMessage("assistant", "Hello! How can I assist you today?"),
				},
			},
		}, completions)
	})
}

func TestResponses(t *testing.T) {
	reqBody := `{"previous_response_id":"resp_1","input":[{"role":"user","type":"message","content":[{"type":"input_text","text":"Say a random number"}]}],"model":"gpt-4.1"}`
	resBody := `
{
  "id": "resp_2",
  "object": "response",
  "status": "completed",
  "model": "gpt-4.1",
  "output": [
    {
      "id": "msg_1",
      "type": "message",
      "status": "completed",
      "content": [
        {
          "type": "output_text",
          "text": "Under a shimmering silver moon"
        }
      ],
      "role": "assistant"
    }
  ]
}`

	var res responses.Response
	if err := json.Unmarshal([]byte(resBody), &res); err != nil {
		t.Fatalf("unmarshalling response body: %v", err)
	}

	provider := &Provider{
		Now: func() time.Time { return time.Unix(0, 0) },
	}

	resHeaders := http.Header{}
	resHeaders.Set("content-type", "application/json")

	completions, err := provider.Handle("/v1/responses", plugins.NewHeaders(nil), []byte(reqBody), plugins.NewHeaders(resHeaders), []byte(resBody))
	require.NoError(t, err)
	require.Equal(t, []*conversations.Completion{
		{
			ProviderID:       "resp_2",
			ProviderParentID: "resp_1",
			Provider:         "openai",
			Model:            "gpt-4.1",
			Timestamp:        time.Unix(0, 0),
			Messages: []*conversations.Message{
				{
					Role: "user",
					Content: []conversations.MessageContent{{
						Type: "message",
						Data: `{"content":[{"text":"Say a random number","type":"input_text"}],"role":"user","type":"message"}`,
					}},
				},
				{
					Role: "assistant",
					Content: []conversations.MessageContent{{
						Type: "message",
						Data: `{"id":"msg_1","content":[{"annotations":null,"text":"Under a shimmering silver moon","type":"output_text","logprobs":null,"refusal":""}],"role":"assistant","status":"completed","type":"message"}`,
					}},
				},
			},
		},
	}, completions)
}
