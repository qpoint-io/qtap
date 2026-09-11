package container

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDockerCallbacks(t *testing.T) {
	for _, key := range []string{"DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "DOCKER_API_VERSION"} {
		t.Setenv(key, "")
	}
	for _, mode := range []string{"all", "none", "started only"} {
		t.Run(mode, func(t *testing.T) {
			const id = "123456789012abcdef"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/_ping"):
					w.Header().Set("API-Version", "1.55")
				case strings.HasSuffix(r.URL.Path, "/info"):
					fmt.Fprint(w, `{}`)
				case strings.HasSuffix(r.URL.Path, "/containers/json"):
					fmt.Fprintf(w, `[{"Id":%q}]`, id)
				case strings.HasSuffix(r.URL.Path, "/containers/"+id+"/json"):
					fmt.Fprint(w, `{"Name":"/web","Config":{"Image":"nginx","Labels":{"io.kubernetes.pod.name":"web","io.kubernetes.pod.namespace":"apps"}}}`)
				case strings.HasSuffix(r.URL.Path, "/events"):
					<-r.Context().Done()
				default:
					t.Errorf("unexpected Docker request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			var manager *Manager
			var got []string
			record := func(event string) func(*Container, string) {
				return func(c *Container, runtime string) {
					assert.Equal(t, "docker", runtime)
					assert.Equal(t, id, c.ID)
					// Re-enter lookup to verify callbacks run outside the cache lock.
					if event == "stopped" {
						assert.Nil(t, manager.GetByID(c.ID))
					} else {
						assert.Same(t, c, manager.GetByID(c.ID))
					}
					got = append(got, event)
				}
			}
			var callbacks Callbacks
			var want []string
			if mode != "none" {
				callbacks.Started = record("started")
				want = []string{"started"}
			}
			if mode == "all" {
				callbacks.Stopped = record("stopped")
				callbacks.Restarted = record("restarted")
				want = []string{"started", "restarted", "stopped"}
			}
			missingSocket := filepath.Join(t.TempDir(), "missing.sock")
			manager = NewManager(zap.NewNop(), server.URL, missingSocket, missingSocket, callbacks)
			require.Len(t, manager.accessors, 1)
			d := manager.accessors[0].(*docker)
			defer d.Close()
			require.NoError(t, manager.Start(ctx))
			initial := manager.GetByID(id)
			require.NotNil(t, initial)
			assert.False(t, initial.GetStartTime().IsZero())
			assert.Equal(t, "web", initial.TidyName())
			assert.Equal(t, "apps", initial.Pod().Namespace)

			d.handleContainerEvent(ctx, id)
			updated := manager.GetByID(id[:12])
			require.NotNil(t, updated)
			assert.NotSame(t, initial, updated)
			assert.Equal(t, "web", updated.Pod().Name)
			d.handleContainerRestart(ctx, id)
			d.handleContainerStop(ctx, id)
			d.handleContainerStop(ctx, id)
			d.handleContainerRestart(ctx, id)
			assert.Nil(t, manager.GetByID(id))
			d.handleContainerEvent(ctx, id)
			require.NotNil(t, manager.GetByID(id))
			if mode != "none" {
				want = append(want, "started")
			}
			assert.Equal(t, want, got)
		})
	}
}

func TestContainerdCallbacks(t *testing.T) {
	for _, mode := range []string{"all", "none", "started only"} {
		t.Run(mode, func(t *testing.T) {
			const id = "123456789012abcdef"
			c := &Containerd{logger: zap.NewNop(), cache: make(map[string]*Container)}
			var got []string
			record := func(event string) func(*Container, string) {
				return func(cr *Container, runtime string) {
					assert.Equal(t, "containerd", runtime)
					assert.Equal(t, id, cr.ID)
					if event == "stopped" {
						assert.Nil(t, c.GetByID(cr.ID))
					} else {
						assert.Same(t, cr, c.GetByID(cr.ID))
					}
					got = append(got, event)
				}
			}
			var want []string
			if mode != "none" {
				c.callbacks.Started = record("started")
				want = []string{"started"}
			}
			if mode == "all" {
				c.callbacks.Stopped = record("stopped")
				c.callbacks.Restarted = record("restarted")
				want = []string{"started", "stopped"}
			}

			initial := &Container{ID: id, Labels: map[string]string{ContainerLabelKeyPodName: "web"}}
			c.addContainer(initial)
			assert.Same(t, initial, c.GetByID(id))
			assert.False(t, initial.GetStartTime().IsZero())
			assert.Equal(t, "web", c.GetByID(id).Pod().Name)
			updated := &Container{ID: id, RootPID: 42, Labels: initial.Labels}
			c.addContainer(updated)
			assert.Same(t, updated, c.GetByID(id[:12]))
			assert.Equal(t, "web", c.GetByID(id).Pod().Name)
			c.addContainer(&Container{ID: "sandbox", Labels: map[string]string{"io.cri-containerd.kind": "sandbox"}})
			assert.Nil(t, c.GetByID("sandbox"))
			c.processContainerDelete(id)
			c.processContainerDelete(id)
			assert.Nil(t, c.GetByID(id))
			c.addContainer(&Container{ID: id})
			require.NotNil(t, c.GetByID(id))
			if mode != "none" {
				want = append(want, "started")
			}
			assert.Equal(t, want, got)
		})
	}
}
