package container

import (
	"testing"

	containertypes "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/storage"
	"github.com/stretchr/testify/assert"
)

func TestContainerFromInspect(t *testing.T) {
	tests := []struct {
		name string
		data containertypes.InspectResponse
		want *Container
	}{
		{
			name: "all fields populated",
			data: containertypes.InspectResponse{
				Name:  "/happy-path",
				Image: "sha256:abc123",
				Config: &containertypes.Config{
					Image:  "nginx:latest",
					Labels: map[string]string{"app": "web"},
				},
				State:       &containertypes.State{Pid: 4242},
				GraphDriver: &storage.DriverData{Data: map[string]string{"MergedDir": "/var/lib/docker/overlay2/xyz/merged"}},
			},
			want: &Container{
				ID:          "cid",
				Name:        "/happy-path",
				ImageDigest: "sha256:abc123",
				Image:       "nginx:latest",
				Labels:      map[string]string{"app": "web"},
				RootPID:     4242,
				RootFS:      "/var/lib/docker/overlay2/xyz/merged",
			},
		},
		{
			// GraphDriver became a pointer (and omitempty) in moby/moby/api,
			// so a nil here must not panic.
			name: "nil GraphDriver",
			data: containertypes.InspectResponse{Name: "/no-graphdriver"},
			want: &Container{ID: "cid", Name: "/no-graphdriver"},
		},
		{
			name: "GraphDriver present but no MergedDir",
			data: containertypes.InspectResponse{
				Name:        "/no-mergeddir",
				GraphDriver: &storage.DriverData{Data: map[string]string{"UpperDir": "/upper"}},
			},
			want: &Container{ID: "cid", Name: "/no-mergeddir"},
		},
		{
			name: "nil Config and State",
			data: containertypes.InspectResponse{Name: "/bare"},
			want: &Container{ID: "cid", Name: "/bare"},
		},
		{
			// a created-but-not-started container reports Pid 0
			name: "zero pid is not recorded",
			data: containertypes.InspectResponse{
				Name:  "/not-running",
				State: &containertypes.State{Pid: 0},
			},
			want: &Container{ID: "cid", Name: "/not-running"},
		},
		{
			name: "empty response",
			data: containertypes.InspectResponse{},
			want: &Container{ID: "cid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				assert.Equal(t, tt.want, containerFromInspect("cid", tt.data))
			})
		})
	}
}
