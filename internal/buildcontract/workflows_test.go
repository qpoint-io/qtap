package buildcontract_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPullRequestsHaveSingleE2EWorkflowOwner(t *testing.T) {
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
		workflow := string(content)
		if strings.Contains(workflow, "pull_request:") && strings.Contains(workflow, "make e2e") {
			owners = append(owners, entry.Name())
		}
	}

	require.Equal(t, []string{"e2e.yaml"}, owners, "expected one pull-request e2e workflow owner")
}

func TestReleaseWorkflowIsSinglePublisher(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	workflowsDir := filepath.Join(filepath.Dir(filename), "..", "..", ".github", "workflows")
	entries, err := os.ReadDir(workflowsDir)
	require.NoError(t, err)

	var publishers []string
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if entry.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(workflowsDir, entry.Name()))
		require.NoError(t, err)
		workflow := string(content)
		if strings.Contains(workflow, "GAR_JSON_KEY") ||
			strings.Contains(workflow, "R2_ACCESS_KEY_ID") ||
			strings.Contains(workflow, "gh release create") {
			publishers = append(publishers, entry.Name())
		}
	}

	require.Equal(t, []string{"release.yaml"}, publishers, "expected one release publisher")
}
