package heartbeat

import (
	"encoding/json"
	"net/http"
	"testing"
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
