package binutils

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/cespare/xxhash/v2"
)

const (
	// hashSampleSize is the number of bytes to sample from start/mid/end.
	hashSampleSize = 32 * 1024
)

// computeBinaryHash generates a cache key using xxHash (fastest non-crypto hash).
// It samples file content + file size.
func computeBinaryHash(f *os.File) (string, error) {
	stat, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}
	size := stat.Size()

	// xxhash.New() creates a digest that implements hash.Hash64
	digest := xxhash.New()

	// 1. Mix Size into the hash (Fundamental identity characteristic)
	// We write it as raw binary data.
	if err := binary.Write(digest, binary.LittleEndian, size); err != nil {
		return "", err
	}

	buf := make([]byte, hashSampleSize)

	// Helper to safely read and write chunks
	sampleAt := func(offset int64) error {
		n, err := f.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return err
		}
		if n > 0 {
			_, err := digest.Write(buf[:n])
			if err != nil {
				return err
			}
		}
		return nil
	}

	// 2. Sample Start
	// Always read the start (contains headers, magic numbers, build IDs)
	if err := sampleAt(0); err != nil {
		return "", fmt.Errorf("reading start: %w", err)
	}

	// 3. Sample Middle
	// Only if the file is large enough to avoid overlapping with start/end.
	// (size > 3 samples) ensures distinct start, mid, and end blocks.
	if size > hashSampleSize*3 {
		midOffset := (size - int64(hashSampleSize)) / 2
		if err := sampleAt(midOffset); err != nil {
			return "", fmt.Errorf("reading middle: %w", err)
		}
	}

	// 4. Sample End
	// Only if the file is larger than one sample block.
	if size > hashSampleSize {
		endOffset := size - int64(hashSampleSize)
		if err := sampleAt(endOffset); err != nil {
			return "", fmt.Errorf("reading end: %w", err)
		}
	}

	// Return as hex string (16 chars for 64-bit hash)
	return fmt.Sprintf("%016x", digest.Sum64()), nil
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
