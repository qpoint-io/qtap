package plugins

import (
	"context"
	"net/http"
	"testing"
)

func TestSetRequestPreservesQueryInPseudoPath(t *testing.T) {
	req, err := http.NewRequest("POST", "https://example.com/anything?foo=1&bar=hello%20world", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	c := &Connection{ctx: context.Background()}
	c.SetRequest(req)

	got := req.Header.Get(":path")
	want := "/anything?foo=1&bar=hello%20world"
	if got != want {
		t.Fatalf("SetRequest() :path = %q, want %q", got, want)
	}
}
