package anthropic

import (
	"net/http"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/conversations"
	"github.com/stretchr/testify/require"
)

func TestOpenAI_Completion(t *testing.T) {
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
			Provider:  "anthropic",
			Model:     "gpt-4.1-nano-2025-04-14",
			Timestamp: time.Unix(0, 0),
			Messages: []*conversations.Message{
				conversations.NewTextMessage("user", "Say a random number"),
				conversations.NewTextMessage("assistant", "9"),
			},
		},
	}, completions)
}

func TestCompletion(t *testing.T) {
	reqBody := `{"model": "claude-sonnet-4","messages":[{"role": "user", "content": [{"type": "text", "text": "Hello, world"}]}]}`
	resBody := `{"type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"Hello! Nice to meet you. How are you doing today? Is there anything I can help you with?"}]}`

	provider := &Provider{
		Now: func() time.Time { return time.Unix(0, 0) },
	}

	resHeaders := http.Header{}
	resHeaders.Set("content-type", "application/json")

	completions, err := provider.Handle("/v1/messages", plugins.NewHeaders(nil), []byte(reqBody), plugins.NewHeaders(resHeaders), []byte(resBody))
	require.NoError(t, err)
	require.Equal(t, []*conversations.Completion{
		{
			Provider:  "anthropic",
			Model:     "claude-sonnet-4",
			Timestamp: time.Unix(0, 0),
			Messages: []*conversations.Message{
				{
					Role: "user",
					Content: []conversations.MessageContent{{
						Type: "text",
						Data: `{"text":"Hello, world","type":"text"}`,
					}},
				},
				{
					Role: "assistant",
					Content: []conversations.MessageContent{{
						Type: "text",
						Data: `{"text":"Hello! Nice to meet you. How are you doing today? Is there anything I can help you with?","type":"text"}`,
					}},
				},
			},
		},
	}, completions)
}

func TestSSE(t *testing.T) {
	headers := http.Header{}
	headers.Set("content-type", "text/event-stream; charset=utf-8")

	reqBody := `{"model": "claude-sonnet-4","messages":[{"role": "user", "content": [{"type": "text", "text": "Hello, world"}]}], "stream": true}`

	resBody := `event: message_start
data: {"type":"message_start","message":{"type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[],"stop_reason":null,"stop_sequence":null}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}     }

event: ping
data: {"type": "ping"}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}   }

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"! Nice to meet you."}   }

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" How are you doing today"}  }

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"? Is there anything I can"}          }

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" help you with?"}       }

event: content_block_stop
data: {"type":"content_block_stop","index":0             }

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":25}   }

event: message_stop
data: {"type":"message_stop"    }

`

	t.Run("parseMessagesSSE", func(t *testing.T) {
		parsedBody, err := parseMessagesSSE([]byte(resBody))
		require.NoError(t, err)
		require.Len(t, parsedBody.Content, 1)

		// zero out raw json
		parsedBody.JSON = (anthropic.Message{}).JSON
		parsedBody.Content[0].JSON = (anthropic.ContentBlockUnion{}).JSON

		require.Equal(t, anthropic.Message{
			Type:       "message",
			Role:       "assistant",
			Model:      "claude-sonnet-4-20250514",
			StopReason: "end_turn",
			Content: []anthropic.ContentBlockUnion{
				{Type: "text", Text: "Hello! Nice to meet you. How are you doing today? Is there anything I can help you with?"},
			},
			Usage: anthropic.Usage{
				OutputTokens: 25,
			},
		}, parsedBody)
	})

	t.Run("handle", func(t *testing.T) {
		provider := &Provider{
			Now: func() time.Time { return time.Unix(0, 0) },
		}
		completions, err := provider.Handle("/v1/messages", plugins.NewHeaders(nil), []byte(reqBody), plugins.NewHeaders(headers), []byte(resBody))
		require.NoError(t, err)
		require.Equal(t, []*conversations.Completion{
			{
				Provider:  "anthropic",
				Model:     "claude-sonnet-4",
				Timestamp: time.Unix(0, 0),
				Messages: []*conversations.Message{
					{
						Role: "user",
						Content: []conversations.MessageContent{{
							Type: "text",
							Data: `{"text":"Hello, world","type":"text"}`,
						}},
					},
					{
						Role: "assistant",
						Content: []conversations.MessageContent{{
							Type: "text",
							Data: `{"text":"Hello! Nice to meet you. How are you doing today? Is there anything I can help you with?","type":"text"}`,
						}},
					},
				},
			},
		}, completions)
	})
}
