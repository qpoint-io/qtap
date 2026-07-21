package warehouse

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
)

func TestFactoryInitRejectsEmptyResolvedTokenWithoutRequest(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(server.Close)

	factory := &Factory{}
	err := factory.Init(t.Context(), warehouseConfig(server.URL, ""))
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("Init() error = %v, want token error", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("requests = %d, want 0", got)
	}
}

func TestFactoryInitRejectsHTTPByDefault(t *testing.T) {
	factory := &Factory{}
	err := factory.Init(t.Context(), warehouseConfig("http://warehouse.example", "token"))
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("Init() error = %v, want HTTPS error", err)
	}
}

func TestPutRegistersTaskBeforeReturning(t *testing.T) {
	release := make(chan struct{})
	factory, store, _ := newWarehouseTestStore(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))

	store.Put(t.Context(), testArtifact("registered"))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := factory.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown() error = %v, want context deadline exceeded", err)
	}

	close(release)
	shutdownWarehouse(t, factory)
}

func TestShutdownWaitsForAcceptedPut(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	factory, store, _ := newWarehouseTestStore(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	}))

	store.Put(t.Context(), testArtifact("wait"))
	<-started
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- factory.Shutdown(t.Context())
	}()

	select {
	case err := <-shutdownDone:
		t.Fatalf("Shutdown() returned before upload completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestConcurrentPutAndShutdown(t *testing.T) {
	factory, store, _ := newWarehouseTestStore(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	start := make(chan struct{})
	var puts sync.WaitGroup
	for range 100 {
		puts.Add(1)
		go func() {
			defer puts.Done()
			<-start
			store.Put(t.Context(), testArtifact("concurrent"))
		}()
	}
	shutdownDone := make(chan error, 1)
	go func() {
		<-start
		shutdownDone <- factory.Shutdown(t.Context())
	}()

	close(start)
	puts.Wait()
	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	shutdownWarehouse(t, factory)
}

func TestClosedFactoryForwardsPutToReplacement(t *testing.T) {
	oldFactory, oldStore, oldRecords := newWarehouseTestStore(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	var replacementRequests atomic.Int64
	newFactory, _, _ := newWarehouseTestStore(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		replacementRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	oldFactory.Next(newFactory)
	shutdownWarehouse(t, oldFactory)

	oldStore.Put(t.Context(), testArtifact("forwarded"))
	shutdownWarehouse(t, newFactory)

	if got := replacementRequests.Load(); got != 1 {
		t.Fatalf("replacement factory requests = %d, want 1", got)
	}
	if got := oldRecords.Len(); got != 1 {
		t.Fatalf("records = %d, want 1", got)
	}
}

func TestHeadFailureDoesNotUploadOrEmitRecord(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var puts atomic.Int64
			factory, store, records := newWarehouseTestStore(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut {
					puts.Add(1)
				}
				w.WriteHeader(status)
			}))

			store.Put(t.Context(), testArtifact("head failure"))
			err := shutdownWarehouseError(t, factory)
			if err == nil || !strings.Contains(err.Error(), http.StatusText(status)) {
				t.Fatalf("Shutdown() error = %v, want status %s", err, http.StatusText(status))
			}
			if got := puts.Load(); got != 0 {
				t.Fatalf("PUT requests = %d, want 0", got)
			}
			if got := records.Len(); got != 0 {
				t.Fatalf("records = %d, want 0", got)
			}
		})
	}
}

func TestPutFailureDoesNotEmitRecord(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			factory, store, records := newWarehouseTestStore(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.WriteHeader(status)
			}))

			store.Put(t.Context(), testArtifact("put failure"))
			err := shutdownWarehouseError(t, factory)
			if err == nil || !strings.Contains(err.Error(), http.StatusText(status)) {
				t.Fatalf("Shutdown() error = %v, want status %s", err, http.StatusText(status))
			}
			if got := records.Len(); got != 0 {
				t.Fatalf("records = %d, want 0", got)
			}
		})
	}
}

func TestSuccessfulUploadEmitsExactlyOneRecord(t *testing.T) {
	var heads atomic.Int64
	var puts atomic.Int64
	factory, store, records := newWarehouseTestStore(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			heads.Add(1)
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			puts.Add(1)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))

	store.Put(t.Context(), testArtifact("success"))
	shutdownWarehouse(t, factory)

	if got := heads.Load(); got != 1 {
		t.Fatalf("HEAD requests = %d, want 1", got)
	}
	if got := puts.Load(); got != 1 {
		t.Fatalf("PUT requests = %d, want 1", got)
	}
	if got := records.Len(); got != 1 {
		t.Fatalf("records = %d, want 1", got)
	}
}

type recordingEventStore struct {
	eventstore.BaseEventStore
	mu      sync.Mutex
	records []*eventstore.ArtifactRecord
}

func (s *recordingEventStore) Save(_ context.Context, item any) {
	record, ok := item.(*eventstore.ArtifactRecord)
	if !ok {
		return
	}
	s.mu.Lock()
	s.records = append(s.records, record)
	s.mu.Unlock()
}

func (s *recordingEventStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

func newWarehouseTestStore(t *testing.T, handler http.Handler) (*Factory, *ObjectStore, *recordingEventStore) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	factory := NewFactory(WithHTTPClient(server.Client()))
	if err := factory.Init(t.Context(), warehouseConfig(server.URL, "token")); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	records := &recordingEventStore{}
	store := &ObjectStore{
		put:        factory.put,
		eventStore: records,
	}
	store.SetLogger(zap.NewNop())
	return factory, store, records
}

func testArtifact(data string) *eventstore.Artifact {
	return &eventstore.Artifact{
		Type:        eventstore.ArtifactType_RequestBody,
		Data:        []byte(data),
		ContentType: "text/plain",
	}
}

func shutdownWarehouse(t *testing.T, factory *Factory) {
	t.Helper()
	if err := shutdownWarehouseError(t, factory); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func shutdownWarehouseError(t *testing.T, factory *Factory) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	return factory.Shutdown(ctx)
}

func warehouseConfig(endpoint, token string) config.ServiceObjectStore {
	return config.ServiceObjectStore{
		Type: config.ObjectStoreType_QPOINT,
		ObjectStoreConfig: config.ObjectStoreConfig{
			ObjectStoreQPointWarehouseConfig: config.ObjectStoreQPointWarehouseConfig{
				URL: endpoint,
				Token: config.ValueSource{
					Type:  config.ValueSourceType_TEXT,
					Value: token,
				},
			},
		},
	}
}
