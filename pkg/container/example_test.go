package container_test

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/qpoint-io/qtap/pkg/container"
	"go.uber.org/zap"
)

// This example needs access to a running container runtime. Without an Output
// directive, go test compiles it without connecting to runtime services.
func ExampleNewManager() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := container.NewManager(zap.NewNop(), "", "", "", container.Callbacks{})
	if err := manager.Start(ctx); err != nil {
		fmt.Println("start discovery:", err)
		return
	}
	c := manager.GetByID("123456789012") // Replace with a full or 12-character container ID.
	if c == nil {
		fmt.Println("container not discovered")
		return
	}
	fmt.Println(c.ID, c.TidyName(), c.Image)
}

func ExampleNewManager_startedCallback() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	callbacks := container.Callbacks{
		Started: func(c *container.Container, runtime string) {
			fmt.Println("discovered", runtime, c.ID)
		},
	}
	manager := container.NewManager(zap.NewNop(), "", "", "", callbacks)
	if err := manager.Start(ctx); err != nil {
		fmt.Println("start discovery:", err)
	}
	// In a long-running application, keep ctx alive while discovery is needed.
}

func TestNoQTapDependencies(t *testing.T) {
	const packagePath = "github.com/qpoint-io/qtap/pkg/container"
	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "-f", "{{.ImportPath}}", packagePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list production dependencies: %v\n%s", err, output)
	}
	for dependency := range strings.FieldsSeq(string(output)) {
		if strings.HasPrefix(dependency, "github.com/qpoint-io/qtap/") && dependency != packagePath {
			t.Errorf("container discovery depends on another qtap package: %s", dependency)
		}
	}
}
