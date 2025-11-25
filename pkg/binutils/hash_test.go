package binutils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComputeBinaryHash(t *testing.T) {
	// Create a temp file with some content
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "testfile")

	// Test with small file (less than hashSampleSize)
	smallContent := []byte("hello world test content")
	if err := os.WriteFile(tmpFile, smallContent, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	f, err := os.Open(tmpFile)
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	hash, err := computeBinaryHash(f)
	if err != nil {
		t.Errorf("computeBinaryHash() error = %v, want nil", err)
	}
	if hash == "" {
		t.Error("computeBinaryHash() returned empty hash")
	}
	if len(hash) != 64 { // SHA256 hex is 64 chars
		t.Errorf("computeBinaryHash() hash length = %d, want 64", len(hash))
	}

	// Test consistency - same file should produce same hash
	f2, _ := os.Open(tmpFile)
	defer f2.Close()
	hash2, _ := computeBinaryHash(f2)
	if hash != hash2 {
		t.Errorf("computeBinaryHash() not consistent: got %s then %s", hash, hash2)
	}
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
	if err := os.WriteFile(tmpFile, largeContent, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	f, err := os.Open(tmpFile)
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer f.Close()

	hash, err := computeBinaryHash(f)
	if err != nil {
		t.Errorf("computeBinaryHash() error = %v, want nil", err)
	}
	if hash == "" {
		t.Error("computeBinaryHash() returned empty hash")
	}
}

func TestComputeBinaryHashDifferentFiles(t *testing.T) {
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "file1")
	file2 := filepath.Join(tmpDir, "file2")

	if err := os.WriteFile(file1, []byte("content one"), 0644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("content two"), 0644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	f1, _ := os.Open(file1)
	defer f1.Close()
	f2, _ := os.Open(file2)
	defer f2.Close()

	hash1, _ := computeBinaryHash(f1)
	hash2, _ := computeBinaryHash(f2)

	if hash1 == hash2 {
		t.Error("computeBinaryHash() should produce different hashes for different files")
	}
}

func TestComputeFullHash(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "testfile")

	content := []byte("test content for full hash")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	hash, err := ComputeFullHash(tmpFile)
	if err != nil {
		t.Errorf("ComputeFullHash() error = %v, want nil", err)
	}
	if hash == "" {
		t.Error("ComputeFullHash() returned empty hash")
	}
	if len(hash) != 64 {
		t.Errorf("ComputeFullHash() hash length = %d, want 64", len(hash))
	}

	// Test consistency
	hash2, _ := ComputeFullHash(tmpFile)
	if hash != hash2 {
		t.Errorf("ComputeFullHash() not consistent: got %s then %s", hash, hash2)
	}
}

func TestComputeFullHashNonExistentFile(t *testing.T) {
	_, err := ComputeFullHash("/nonexistent/file/path")
	if err == nil {
		t.Error("ComputeFullHash() should error on non-existent file")
	}
}

func TestComputeFullHashDifferentFiles(t *testing.T) {
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "file1")
	file2 := filepath.Join(tmpDir, "file2")

	if err := os.WriteFile(file1, []byte("content one"), 0644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("content two"), 0644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	hash1, _ := ComputeFullHash(file1)
	hash2, _ := ComputeFullHash(file2)

	if hash1 == hash2 {
		t.Error("ComputeFullHash() should produce different hashes for different files")
	}
}

