package binutils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeBinaryHash(t *testing.T) {
	// Create a temp file with some content
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "testfile")

	// Test with small file (less than hashSampleSize)
	smallContent := []byte("hello world test content")
	err := os.WriteFile(tmpFile, smallContent, 0644)
	require.NoError(t, err)

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer f.Close()

	hash, err := computeBinaryHash(f)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.Equal(t, "c3143f737fad1b17", hash)

	// Test consistency - same file should produce same hash
	f2, _ := os.Open(tmpFile)
	defer f2.Close()
	hash2, _ := computeBinaryHash(f2)
	assert.Equal(t, hash, hash2)
}

func TestComputeBinaryHashLargeFile(t *testing.T) {
	// Create a large file (> 2*hashSampleSize)
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "largefile")

	// 100KB file
	largeContent := make([]byte, 100*1024)
	for i := range largeContent {
		largeContent[i] = byte(i % 256)
	}
	err := os.WriteFile(tmpFile, largeContent, 0644)
	require.NoError(t, err)

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer f.Close()

	hash, err := computeBinaryHash(f)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}

func TestComputeBinaryHashDifferentFiles(t *testing.T) {
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "file1")
	file2 := filepath.Join(tmpDir, "file2")

	err := os.WriteFile(file1, []byte("content one"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(file2, []byte("content two"), 0644)
	require.NoError(t, err)

	f1, _ := os.Open(file1)
	defer f1.Close()
	f2, _ := os.Open(file2)
	defer f2.Close()

	hash1, _ := computeBinaryHash(f1)
	hash2, _ := computeBinaryHash(f2)

	assert.NotEqual(t, hash1, hash2)
}

func TestComputeFullHash(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "testfile")

	content := []byte("test content for full hash")
	err := os.WriteFile(tmpFile, content, 0644)
	require.NoError(t, err)

	hash, err := ComputeFullHash(tmpFile)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.Len(t, hash, 64)

	// Test consistency
	hash2, _ := ComputeFullHash(tmpFile)
	assert.Equal(t, hash, hash2)
}

func TestComputeFullHashNonExistentFile(t *testing.T) {
	_, err := ComputeFullHash("/nonexistent/file/path")
	assert.Error(t, err)
}

func TestComputeFullHashDifferentFiles(t *testing.T) {
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "file1")
	file2 := filepath.Join(tmpDir, "file2")

	err := os.WriteFile(file1, []byte("content one"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(file2, []byte("content two"), 0644)
	require.NoError(t, err)

	hash1, _ := ComputeFullHash(file1)
	hash2, _ := ComputeFullHash(file2)

	assert.NotEqual(t, hash1, hash2)
}
