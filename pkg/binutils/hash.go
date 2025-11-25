package binutils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

const (
	// hashSampleSize is the number of bytes to sample from different parts of the file
	// for fast hashing. We sample from start, middle, and end.
	hashSampleSize = 32 * 1024 // 32KB per sample
)

// computeBinaryHash computes a fast hash of a binary file.
// It samples bytes from different parts of the file rather than hashing the entire file,
// combined with file size for uniqueness.
//
// This approach is fast while still being effective at detecting different binaries.
// For a more thorough hash, use computeFullHash.
func computeBinaryHash(f *os.File) (string, error) {
	stat, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}

	size := stat.Size()
	h := sha256.New()

	// Include file size in hash
	fmt.Fprintf(h, "size:%d;", size)

	// Sample from start
	buf := make([]byte, hashSampleSize)
	n, err := f.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("reading start: %w", err)
	}
	h.Write(buf[:n])

	// Sample from middle (if file is large enough)
	if size > hashSampleSize*2 {
		midOffset := (size - int64(hashSampleSize)) / 2
		n, err = f.ReadAt(buf, midOffset)
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("reading middle: %w", err)
		}
		h.Write(buf[:n])
	}

	// Sample from end (if file is large enough)
	if size > hashSampleSize {
		endOffset := size - int64(hashSampleSize)
		if endOffset < 0 {
			endOffset = 0
		}
		n, err = f.ReadAt(buf, endOffset)
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("reading end: %w", err)
		}
		h.Write(buf[:n])
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// ComputeFullHash computes a full SHA256 hash of the file.
// This is slower but provides a cryptographically strong hash.
func ComputeFullHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

