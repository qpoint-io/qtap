package heartbeat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestVariants pins the two heartbeat flavors' distinguishing traits: request
// path, success status code, and JSON payload shape.
func TestVariants(t *testing.T) {
	sysInfo := map[string]map[string]string{
		"kernel": {"release": "6.1.0"},
		"system": {"architecture": "x86_64"},
	}

	if Deploy.Path != "/deploy/ping" || Deploy.OKStatus != http.StatusOK {
		t.Fatalf("deploy variant: got path=%q status=%d", Deploy.Path, Deploy.OKStatus)
	}
	if Pulse.Path != "/api/v1/heartbeat" || Pulse.OKStatus != http.StatusCreated {
		t.Fatalf("pulse variant: got path=%q status=%d", Pulse.Path, Pulse.OKStatus)
	}

	deploy := marshal(t, Deploy.Build("host1", "inst1", StatusReady, sysInfo))
	if deploy["name"] != "host1" || deploy["instanceId"] != "inst1" || deploy["sysInfo"] == nil {
		t.Fatalf("deploy payload missing fields: %v", deploy)
	}

	pulse := marshal(t, Pulse.Build("host1", "inst1", StatusReady, sysInfo))
	if pulse["hostname"] != "host1" || pulse["kernelRelease"] != "6.1.0" || pulse["architecture"] != "x86_64" {
		t.Fatalf("pulse payload missing fields: %v", pulse)
	}

	// missing sysInfo keys must degrade to empty strings, not panic
	pulseEmpty := marshal(t, Pulse.Build("h", "i", StatusStarting, map[string]map[string]string{}))
	if pulseEmpty["kernelRelease"] != "" || pulseEmpty["architecture"] != "" {
		t.Fatalf("pulse payload should tolerate missing sysInfo: %v", pulseEmpty)
	}
}

func TestNewRejectsEmptyTokenWithoutRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	for _, token := range []string{"", " \t\n"} {
		if _, err := New(zap.NewNop(), server.URL, token, time.Hour, Deploy); err == nil {
			t.Fatalf("New() token %q: expected error", token)
		}
	}

	if got := requests.Load(); got != 0 {
		t.Fatalf("requests = %d, want 0", got)
	}
}

func TestStartReturnsInitialRequestFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	hb, err := New(zap.NewNop(), server.URL, "token", time.Hour, Deploy)
	if err != nil {
		t.Fatal(err)
	}
	if err := hb.Start(); err == nil {
		t.Fatal("Start() expected non-success status error")
	}
}

func TestStartRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	client := server.Client()
	client.Timeout = 25 * time.Millisecond
	hb, err := NewWithClient(zap.NewNop(), server.URL, "token", time.Hour, Deploy, client)
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	if err := hb.Start(); err == nil {
		t.Fatal("Start() expected timeout error")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Start() took %s, want bounded request", elapsed)
	}
}

func TestStartStopAreIdempotent(t *testing.T) {
	var starting atomic.Int32
	var stopped atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		switch body.Status {
		case string(StatusStarting):
			starting.Add(1)
		case string(StatusStopped):
			stopped.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hb, err := New(zap.NewNop(), server.URL, "token", time.Hour, Deploy)
	if err != nil {
		t.Fatal(err)
	}
	if err := hb.Start(); err != nil {
		t.Fatal(err)
	}
	if err := hb.Start(); err != nil {
		t.Fatal(err)
	}
	if err := hb.StopContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := hb.StopContext(t.Context()); err != nil {
		t.Fatal(err)
	}

	if got := starting.Load(); got != 1 {
		t.Fatalf("starting requests = %d, want 1", got)
	}
	if got := stopped.Load(); got != 1 {
		t.Fatalf("stopped requests = %d, want 1", got)
	}
}

func TestStopContextBoundsFinalRequest(t *testing.T) {
	finalStarted := make(chan struct{})
	releaseFinal := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Status == string(StatusStopped) {
			close(finalStarted)
			<-releaseFinal
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	defer close(releaseFinal)

	client := server.Client()
	client.Timeout = time.Second
	hb, err := NewWithClient(zap.NewNop(), server.URL, "token", time.Hour, Deploy, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := hb.Start(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := hb.StopContext(ctx); err == nil {
		t.Fatal("StopContext() expected deadline error")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("StopContext() took %s, want bounded request", elapsed)
	}
	select {
	case <-finalStarted:
	case <-time.After(time.Second):
		t.Fatal("final heartbeat was not requested")
	}
}

func marshal(t *testing.T, v any) map[string]any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
