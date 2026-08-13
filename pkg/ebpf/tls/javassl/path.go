package javassl

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// ValidateExecutionBasePath verifies that JavaSSL can stage and execute its
// loader from path. This catches read-only and noexec mounts before attachment.
func ValidateExecutionBasePath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("javassl execution base %q must be absolute", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("accessing javassl execution base %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("javassl execution base %q is not a directory", path)
	}

	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return fmt.Errorf("checking javassl execution base %q: %w", path, err)
	}
	if stat.Flags&unix.ST_RDONLY != 0 {
		return fmt.Errorf("javassl execution base %q is read-only; configure a writable filesystem", path)
	}
	if stat.Flags&unix.ST_NOEXEC != 0 {
		return fmt.Errorf("javassl execution base %q is not executable; configure another path with the JavaSSL path flags or environment variables", path)
	}

	probe, err := os.CreateTemp(path, ".qtap-write-check-*")
	if err != nil {
		return fmt.Errorf("javassl execution base %q is not writable: %w", path, err)
	}
	probePath := probe.Name()
	defer os.Remove(probePath)

	if err := probe.Close(); err != nil {
		return fmt.Errorf("closing javassl write check in %q: %w", path, err)
	}

	return nil
}
