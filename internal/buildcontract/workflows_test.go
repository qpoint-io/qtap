package buildcontract_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestE2EWorkflowHasSingleOwner(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	workflowsDir := filepath.Join(filepath.Dir(filename), "..", "..", ".github", "workflows")
	entries, err := os.ReadDir(workflowsDir)
	require.NoError(t, err)

	var owners []string
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if entry.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(workflowsDir, entry.Name()))
		require.NoError(t, err)
		if strings.Contains(string(content), "make e2e") {
			owners = append(owners, entry.Name())
		}
	}

	require.Len(t, owners, 1, "expected one e2e workflow owner, found %v", owners)
}
