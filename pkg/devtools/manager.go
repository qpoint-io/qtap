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
	"slices"
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
	mu                 sync.RWMutex
	clients            map[chan<- *Event]map[string]struct{}
}

func NewManager(opts ...ManagerOpt) *Manager {
	m := &Manager{
		logger:  zap.L(),
		clients: make(map[chan<- *Event]map[string]struct{}),
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
	FilterTopics        []string `json:"filter_topics,omitempty"`
	SkipProcessSnapshot bool     `json:"skip_process_snapshot,omitempty"`
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

	// init SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	writeEvent(ll, w, NewEvent("system.connected", map[string]any{
		"topics": req.FilterTopics,
	}))
	ll.Debug("client connected",
		zap.Any("topics", req.FilterTopics),
		zap.Int("total_connected_clients", m.clientCount()+1),
	)

	// subscribe to events
	events := m.SubscribeClient(r.Context(), req.FilterTopics, !req.SkipProcessSnapshot)
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
		case e := <-events:
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
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.clients) == 0 {
		return
	}

	topLevelTopic := event.TopLevelTopic()
	for client, topics := range m.clients {
		// skip if client is not subscribed to the topic
		// `system` topic events will be broadcast to all clients regardless of filtering
		if topLevelTopic != "" && topLevelTopic != "system" && len(topics) > 0 {
			if _, ok := topics[topLevelTopic]; !ok {
				continue
			}
		}

		select {
		case client <- event:
		default:
			m.logger.Warn("client buffer is full, dropping event", zap.String("topic", event.Topic))
			continue
		}
	}
}

func (m *Manager) sendProcessSnapshot(ctx context.Context, ch chan<- *Event) {
	if m.processSnapshotter == nil {
		return
	}

	m.processSnapshotter.SnapshotProcesses(func(pid int, p *process.Process) bool {
		select {
		case <-ctx.Done():
			return false
		case ch <- NewEvent("process.started", marshalProcess(p)):
		default:
			m.logger.Warn("client buffer is full, stopping process snapshot")
			return false
		}
		return true
	})
}

func (m *Manager) SubscribeClient(ctx context.Context, topics []string, sendProcessSnapshot bool) <-chan *Event {
	topicsMap := make(map[string]struct{}, len(topics))
	for _, topic := range topics {
		topicsMap[topic] = struct{}{}
	}

	ch := make(chan *Event, 100)

	m.mu.Lock()
	m.clients[ch] = topicsMap
	m.mu.Unlock()

	// send a snapshot of the current processes if subscribed
	if sendProcessSnapshot && (len(topics) == 0 || slices.Contains(topics, "process")) {
		go m.sendProcessSnapshot(ctx, ch)
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

func (m *Manager) ProcessStarted(p *process.Process) error {
	m.broadcast(NewEvent("process.started", marshalProcess(p)))
	return nil
}

func (m *Manager) ProcessReplaced(p *process.Process) error {
	m.broadcast(NewEvent("process.replaced", marshalProcess(p)))
	return nil
}

func (m *Manager) ProcessStopped(p *process.Process) error {
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
