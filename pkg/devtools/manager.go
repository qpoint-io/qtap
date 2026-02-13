package devtools

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins/httpcapture"
	"github.com/qpoint-io/qtap/pkg/process"
	"go.uber.org/zap"
)

//go:embed app/dist
var appFS embed.FS

type Manager struct {
	logger             *zap.Logger
	eventStore         *EventStoreFactory
	objectStore        *ObjectStoreFactory
	processSnapshotter ProcessSnapshotter
	connectionCache    *ConnectionCache
	mu                 sync.RWMutex
	clients            map[chan<- *Event]*ClientSubscription
}

// ClientSubscription holds subscription information for a connected client
type ClientSubscription struct {
	TopicFilters map[string]*EventFilter // topic -> compiled filter (nil = "*" = match all)
}

func NewManager(opts ...ManagerOpt) *Manager {
	m := &Manager{
		logger:          zap.L(),
		clients:         make(map[chan<- *Event]*ClientSubscription),
		connectionCache: NewConnectionCache(DefaultCacheMaxSize),
	}
	m.eventStore = &EventStoreFactory{broadcast: m.broadcast}
	m.objectStore = &ObjectStoreFactory{broadcast: m.broadcast}
	for _, opt := range opts {
		opt(m)
	}
	m.eventStore.logger = m.logger
	m.objectStore.logger = m.logger
	return m
}

type ManagerOpt func(*Manager)

func WithLogger(logger *zap.Logger) ManagerOpt {
	return func(m *Manager) {
		m.logger = logger
	}
}

func WithProcessSnapshotter(ps ProcessSnapshotter) ManagerOpt {
	return func(m *Manager) {
		m.processSnapshotter = ps
	}
}

/*
	HTTP server
*/

func (m *Manager) RegisterRoutes(mux *http.ServeMux, prefix string) error {
	dist, err := fs.Sub(appFS, "app/dist")
	if err != nil {
		return fmt.Errorf("reading embedded app files: %w", err)
	}

	registerRoute := func(path string, handler http.Handler) {
		mux.Handle(prefix+path, http.StripPrefix(prefix, handler))
	}

	registerRoute("/", http.FileServerFS(dist))
	registerRoute("/api/events", http.HandlerFunc(m.routeAPIEvents))
	return nil
}

type APIEventsRequest struct {
	Topics              map[string]string `json:"topics,omitempty"` // topic -> filter ("*" = all)
	SkipProcessSnapshot bool              `json:"skip_process_snapshot,omitempty"`
}

func (m *Manager) routeAPIEvents(w http.ResponseWriter, r *http.Request) {
	ll := m.logger.With(
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("remote_addr", r.RemoteAddr),
	)

	// Set CORS headers for development (allow all origins for devtools)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// Handle preflight OPTIONS request
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req APIEventsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if !errors.Is(err, io.EOF) {
			// an empty body is ok
			httpError(ll, w, fmt.Errorf("reading request body: %w", err), http.StatusBadRequest)
			return
		}
	}

	// Apply defaults if no topics specified
	if len(req.Topics) == 0 {
		req.Topics = map[string]string{
			"process":    "*",
			"connection": "*",
			"http":       "*",
		}
	}

	// Parse filters for each topic
	topicFilters := make(map[string]*EventFilter)
	for topic, filterExpr := range req.Topics {
		if filterExpr == "*" {
			topicFilters[topic] = nil // nil means match all
		} else {
			filter, err := ParseFilter(filterExpr)
			if err != nil {
				httpError(ll, w, fmt.Errorf("parsing filter for topic %s: %w", topic, err), http.StatusBadRequest)
				return
			}
			topicFilters[topic] = filter
		}
	}

	// init SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	writeEvent(ll, w, NewEvent("system.connected", map[string]any{
		"topics": req.Topics,
	}))
	ll.Debug("client connected",
		zap.Any("topics", req.Topics),
		zap.Int("total_connected_clients", m.clientCount()+1),
	)

	// subscribe to events
	events := m.SubscribeClient(r.Context(), topicFilters, !req.SkipProcessSnapshot)
	for {
		select {
		case <-r.Context().Done():
			if c := context.Cause(r.Context()); c != nil && c.Error() == "server shutdown" {
				// server shutdown
				writeEvent(ll, w, NewEvent("system.disconnected", map[string]any{
					"cause": "server shutdown",
				}))
			} else {
				// client disconnected or http error
				ll.Debug("request context done",
					zap.String("cause", context.Cause(r.Context()).Error()),
					zap.Int("total_connected_clients", m.clientCount()),
				)
			}
			return
		case e, ok := <-events:
			if !ok {
				// events channel closed (unsubscribed); stop streaming
				return
			}
			writeEvent(ll, w, e)
		}
	}
}

func (m *Manager) clientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}

func httpError(ll *zap.Logger, w http.ResponseWriter, err error, status int) {
	ll.Error("http error",
		zap.Int("status", status),
		zap.Error(err),
	)
	http.Error(w, "err: "+err.Error(), status)
}

func writeSSE[T string | []byte](w http.ResponseWriter, id string, event string, data T) {
	if id != "" {
		fmt.Fprintf(w, "id: %s\n", id)
	}
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", data)

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

type Event struct {
	Topic     string
	Timestamp time.Time
	Data      any
}

func (e *Event) TopLevelTopic() string {
	parts := strings.SplitN(e.Topic, ".", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return e.Topic
}

func writeEvent(ll *zap.Logger, w http.ResponseWriter, event *Event) {
	j, err := json.Marshal(map[string]any{
		"ts":   event.Timestamp.Format(time.RFC3339Nano),
		"data": event.Data,
	})
	if err != nil {
		ll.Error("encoding event data", zap.Error(err))
		writeSSE(w, "", "error", fmt.Sprintf("encoding %s event: %v", event.Topic, err))
		return
	}

	writeSSE(w, "", event.Topic, j)
}

func (m *Manager) broadcast(event *Event) {
	// Update connection cache for cross-entity filtering
	m.updateConnectionCache(event)

	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.clients) == 0 {
		return
	}

	topLevelTopic := event.TopLevelTopic()

	// Map event topic to subscription topic ("request" -> "http")
	subscriptionTopic := topLevelTopic
	if topLevelTopic == "request" {
		subscriptionTopic = "http"
	}

	for client, sub := range m.clients {
		// System events always pass through
		if topLevelTopic == "system" {
			select {
			case client <- event:
			default:
			}
			continue
		}

		// Check if client is subscribed to this topic
		filter, subscribed := sub.TopicFilters[subscriptionTopic]
		if !subscribed {
			continue
		}

		// Apply filter if present (nil = match all)
		if filter != nil && !filter.Matches(event, m.connectionCache) {
			continue
		}

		select {
		case client <- event:
		default:
			m.logger.Warn("client buffer is full, dropping event", zap.String("topic", event.Topic))
			continue
		}
	}
}

// updateConnectionCache maintains the connection cache for cross-entity filtering
func (m *Manager) updateConnectionCache(event *Event) {
	// Only process connection events
	if !strings.HasPrefix(event.Topic, "connection.") {
		return
	}

	data, ok := event.Data.(map[string]any)
	if !ok {
		return
	}

	// Connection data is nested under "data" key
	innerData, ok := data["data"]
	if !ok {
		return
	}

	// Handle both struct and map types
	var connId string
	var connData map[string]any

	switch d := innerData.(type) {
	case map[string]any:
		connData = d
		if meta, ok := d["meta"].(map[string]any); ok {
			connId, _ = meta["connectionId"].(string)
		} else {
			connId, _ = d["connectionId"].(string)
		}
	default:
		// For struct types, we need to extract via JSON marshaling
		// This handles *eventstore.Connection
		jsonBytes, err := json.Marshal(innerData)
		if err != nil {
			return
		}
		if err := json.Unmarshal(jsonBytes, &connData); err != nil {
			return
		}
		if meta, ok := connData["meta"].(map[string]any); ok {
			connId, _ = meta["connectionId"].(string)
		} else {
			connId, _ = connData["connectionId"].(string)
		}
	}

	if connId == "" {
		return
	}

	switch event.Topic {
	case "connection.opened", "connection.updated":
		m.connectionCache.Set(connId, connData)
	case "connection.closed":
		// Remove from cache - connection is done, no more requests expected
		m.connectionCache.Delete(connId)
	}
}

func (m *Manager) sendProcessSnapshot(ctx context.Context, ch chan<- *Event, filter *EventFilter) {
	if m.processSnapshotter == nil {
		return
	}

	m.processSnapshotter.SnapshotProcesses(func(pid int, p *process.Process) bool {
		event := NewEvent("process.started", marshalProcess(p))

		// Apply filter if present (no cache needed for process events)
		if filter != nil && !filter.Matches(event, nil) {
			return true // continue to next process
		}

		select {
		case <-ctx.Done():
			return false
		case ch <- event:
		default:
			m.logger.Warn("client buffer is full, stopping process snapshot")
			return false
		}
		return true
	})
}

func (m *Manager) SubscribeClient(ctx context.Context, topicFilters map[string]*EventFilter, sendProcessSnapshot bool) <-chan *Event {
	ch := make(chan *Event, 100)

	m.mu.Lock()
	m.clients[ch] = &ClientSubscription{
		TopicFilters: topicFilters,
	}
	m.mu.Unlock()

	// send a snapshot of the current processes if subscribed to process topic
	if sendProcessSnapshot {
		if filter, ok := topicFilters["process"]; ok {
			go m.sendProcessSnapshot(ctx, ch, filter)
		}
	}

	// unsubscribe on ctx done
	go func() {
		<-ctx.Done()

		m.mu.Lock()
		delete(m.clients, ch)
		m.mu.Unlock()
		close(ch)
	}()

	return ch
}

func NewEvent(topic string, data any) *Event {
	return &Event{
		Timestamp: time.Now(),
		Topic:     topic,
		Data:      data,
	}
}

/*
	Process observer
*/

type ProcessSnapshotter interface {
	SnapshotProcesses(fn func(pid int, p *process.Process) bool)
}

func (m *Manager) ProcessStarted(ctx context.Context, p *process.Process) error {
	m.broadcast(NewEvent("process.started", marshalProcess(p)))
	return nil
}

func (m *Manager) ProcessReplaced(ctx context.Context, p *process.Process) error {
	m.broadcast(NewEvent("process.replaced", marshalProcess(p)))
	return nil
}

func (m *Manager) ProcessStopped(ctx context.Context, p *process.Process) error {
	values := map[string]any{
		"pid":       p.Pid,
		"createdAt": p.CreatedAt().Format(time.RFC3339Nano),
		"closedAt":  p.ClosedAt().Format(time.RFC3339Nano),
	}
	m.broadcast(NewEvent("process.stopped", values))
	return nil
}

func marshalProcess(p *process.Process) map[string]any {
	values := p.ControlValues()
	values["pid"] = p.Pid
	values["createdAt"] = p.CreatedAt().Format(time.RFC3339Nano)
	if closed := p.ClosedAt(); closed != nil {
		values["closedAt"] = closed.Format(time.RFC3339Nano)
	}

	if container, _ := p.Container(); container != nil {
		values["container"] = container.ControlValues()
	}

	if pod, _ := p.Pod(); pod != nil {
		values["pod"] = pod.ControlValues()
	}

	return values
}

/*
	Factories
*/

func (m *Manager) PluginFactory() *PluginFactory {
	return &PluginFactory{
		logger:      m.logger,
		httpCapture: &httpcapture.Factory{},
	}
}

func (m *Manager) EventStoreFactory() *EventStoreFactory {
	return m.eventStore
}

func (m *Manager) ObjectStoreFactory() *ObjectStoreFactory {
	return m.objectStore
}
