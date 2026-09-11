//go:build linux

package main

import (
	"strings"
	"testing"

	"github.com/qpoint-io/qtap/pkg/container"
)

func TestDescribe(t *testing.T) {
	for _, tt := range []struct {
		name      string
		kind      string
		container container.Container
		want      []string
	}{
		{
			name: "populated and escaped",
			kind: "started",
			container: container.Container{
				ID: "abcdef123456", Name: "/web\nworker", Image: "nginx:latest",
				ImageDigest: "sha256:abc", RootPID: 42, RootFS: "/run/container/rootfs",
				Labels: map[string]string{
					container.ContainerLabelKeyPodName:      "web",
					container.ContainerLabelKeyPodNamespace: "apps",
					container.ContainerLabelKeyPodUID:       "pod-123",
					"note":                                  "line1\nline2",
				},
			},
			want: []string{
				`started runtime="docker" id="abcdef123456"`,
				`name:      "web\nworker"`, `image:     "nginx:latest"`,
				`digest:    "sha256:abc"`, `root_pid:  42`, `rootfs:    "/run/container/rootfs"`,
				`pod:       "web"`, `namespace: "apps"`, `pod_uid:   "pod-123"`,
				`"note":"line1\nline2"`,
			},
		},
		{
			name: "missing metadata",
			kind: "stopped",
			want: []string{
				`stopped runtime="docker" id=unavailable`,
				"name:      unavailable", "image:     unavailable", "digest:    unavailable",
				"root_pid:  unavailable", "rootfs:    unavailable", "pod:       unavailable",
				"namespace: unavailable", "pod_uid:   unavailable", "labels:    unavailable",
			},
		},
		{
			name:      "restart",
			kind:      "restarted",
			container: container.Container{ID: "abcdef123456"},
			want:      []string{`restarted runtime="docker" id="abcdef123456"`},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := describe(tt.kind, "docker", &tt.container)
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q in:\n%s", want, out)
				}
			}
			if lines := strings.Count(out, "\n"); lines != 10 {
				t.Errorf("want 10 lines, got %d:\n%s", lines, out)
			}
		})
	}
}
