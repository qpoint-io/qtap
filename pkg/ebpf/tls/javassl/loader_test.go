package javassl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoaderUninstallPreservesUnownedDirectory(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "operator-owned")
	require.NoError(t, os.WriteFile(marker, []byte("keep"), 0600))

	loader := newLoader(t.Context(), dir)
	require.NoError(t, loader.Uninstall(t.Context()))
	require.FileExists(t, marker)
}
