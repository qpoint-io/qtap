package llm

import (
	"context"
	"encoding/json"

	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/conversations"
	"github.com/qpoint-io/qtap/pkg/plugins/llm/providers"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/services/objectstore"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const TagLLMProvider = "llm_provider"

type instance struct {
	logger *zap.Logger

	ctx           context.Context
	conn          plugins.PluginContext
	objectstore   objectstore.ObjectStore
	conversations *conversations.Tracker

	provider   providers.Provider
	bufferBody bool

	reqHeaders plugins.Headers
	resHeaders plugins.Headers
}

func (i *instance) setProvider(provider providers.Provider) {
	i.provider = provider
	i.conn.Meta().Tags().Add(TagLLMProvider, provider.Name())
}

func (i *instance) RequestHeaders(headers plugins.Headers, endOfStream bool) plugins.HeadersStatus {
	i.reqHeaders = headers

	if i.provider == nil && headers != nil {
		authority, _ := headers.Get(":authority")
		if provider := i.providerFromHost(authority.String()); provider != nil {
			i.logger.Debug("detected LLM provider via host", zap.String("provider", provider.Name()))
			i.setProvider(provider)
		}
	}

	if i.provider == nil && headers != nil {
		if provider := i.providerFromReqHeaders(headers); provider != nil {
			i.logger.Debug("detected LLM provider via request headers", zap.String("provider", provider.Name()))
			i.setProvider(provider)
		}
	}

	if i.provider != nil {
		// inspect path for conversation api detection
		if path, ok := headers.Get(":path"); ok {
			for _, p := range i.provider.Paths() {
				if p(path.String()) {
					i.logger.Debug("detected LLM conversation via path", zap.String("path", path.String()))
					i.bufferBody = true
					break
				}
			}
		}
	}

	return plugins.HeadersStatusContinue
}

func (i *instance) RequestBody(frame plugins.BodyBuffer, endOfStream bool) plugins.BodyStatus {
	if i.provider == nil || !i.bufferBody {
		return plugins.BodyStatusContinue
	}

	return plugins.BodyStatusContinueAndBuffer
}

func (i *instance) ResponseHeaders(headers plugins.Headers, endOfStream bool) plugins.HeadersStatus {
	i.resHeaders = headers

	if i.provider == nil && headers != nil {
		if provider := i.providerFromResHeaders(headers); provider != nil {
			i.logger.Debug("detected LLM provider via response headers", zap.String("provider", provider.Name()))
			i.setProvider(provider)
		}
	}

	return plugins.HeadersStatusContinue
}

func (i *instance) ResponseBody(frame plugins.BodyBuffer, endOfStream bool) plugins.BodyStatus {
	if i.provider == nil || !i.bufferBody {
		return plugins.BodyStatusContinue
	}

	return plugins.BodyStatusContinueAndBuffer
}

func (i *instance) Destroy() {
	_, span := tracer.Start(i.ctx, "Destroy")
	defer func() {
		span.End()
		// end parent filterInstance span
		defer trace.SpanFromContext(i.ctx).End()
	}()

	if i.provider == nil {
		return
	}

	req := i.conn.GetRequestBodyBuffer().Copy()
	res := i.conn.GetResponseBodyBuffer().Copy()
	var path string
	if p, ok := i.reqHeaders.Get(":path"); ok {
		path = p.String()
	}
	completions, err := i.provider.Handle(path, i.reqHeaders, req, i.resHeaders, res)
	if err != nil {
		i.logger.Error("error handling request/response", zap.Error(err))
		return
	}

	for _, completion := range completions {
		if completion := i.conversations.TrackCompletion(*completion); completion != nil {
			i.saveCompletion(completion)
		}
	}
}

func (i *instance) providerFromHost(host string) providers.Provider {
	for _, provider := range Providers {
		for _, h := range provider.Hosts() {
			if h(host) {
				return provider
			}
		}
	}

	return nil
}

func (i *instance) providerFromReqHeaders(headers plugins.Headers) providers.Provider {
	for _, provider := range Providers {
		for _, h := range provider.RequestHeaders() {
			if value, ok := headers.Get(h.Name); ok {
				if h.MatchValue(value.String()) {
					return provider
				}
			}
		}
	}

	return nil
}

func (i *instance) providerFromResHeaders(headers plugins.Headers) providers.Provider {
	for _, provider := range Providers {
		for _, h := range provider.ResponseHeaders() {
			if value, ok := headers.Get(h.Name); ok {
				if h.MatchValue(value.String()) {
					return provider
				}
			}
		}
	}

	return nil
}

func (i *instance) saveCompletion(completion *conversations.Completion) {
	i.logger.Debug("saving completion", zap.Any("completion", completion))
	conversation := toConversationArtifact(completion)
	conversationBytes, err := json.Marshal(conversation)
	if err != nil {
		i.logger.Error("error marshalling conversation", zap.Error(err))
		return
	}

	meta := i.conn.Meta()
	artifact := &eventstore.Artifact{
		Type:        eventstore.ArtifactType_LLMConversation,
		Data:        conversationBytes,
		ContentType: "application/json",
		Summary: map[string]any{
			"conversation_id":        conversation.ID,
			"conversation_parent_id": conversation.ParentID,
			"provider":               conversation.Provider,
			"model":                  conversation.Model,
		},
	}
	artifact.SetRequestID(meta.RequestID())
	i.objectstore.Put(i.ctx, artifact)
}

// ConversationArtifact is an artifact representing a conversation for storage in the object store.
type ConversationArtifact struct {
	ID       string                         `json:"id"`
	ParentID string                         `json:"parent_id"`
	Provider string                         `json:"provider"`
	Model    string                         `json:"model"`
	Messages []*ConversationArtifactMessage `json:"messages,omitempty"`

	// Version describes the schema version of this object.
	Version int `json:"version"`
}

type ConversationArtifactMessage struct {
	Role    string                               `json:"role,omitempty"`
	Content []ConversationArtifactMessageContent `json:"content,omitempty"`
}

type ConversationArtifactMessageContent struct {
	Type string `json:"type,omitempty"`
	Data string `json:"data,omitempty"`
}

func toConversationArtifact(completion *conversations.Completion) *ConversationArtifact {
	artifact := &ConversationArtifact{
		ID:       completion.ID,
		ParentID: completion.ParentID,
		Provider: completion.Provider,
		Model:    completion.Model,
		Version:  1,
	}
	msgs := make([]*ConversationArtifactMessage, 0, len(completion.Messages))
	for _, message := range completion.Messages {
		content := make([]ConversationArtifactMessageContent, 0, len(message.Content))
		for _, c := range message.Content {
			content = append(content, ConversationArtifactMessageContent{
				Type: c.Type,
				Data: c.Data,
			})
		}

		msgs = append(msgs, &ConversationArtifactMessage{
			Role:    message.Role,
			Content: content,
		})
	}
	artifact.Messages = msgs
	return artifact
}
