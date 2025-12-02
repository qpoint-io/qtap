package tls

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"go.opentelemetry.io/otel/attribute"
)

// FindSharedLibrary searches for shared libraries in a filesystem root.
// root is the path to the filesystem root (e.g., "/" for host, "/proc/PID/root" for container)
// libNamePrefix is the library name prefix to search for (e.g., "libssl.so")
// Returns unique library paths (by inode).
func FindSharedLibrary(ctx context.Context, root, libNamePrefix string) ([]string, error) {
	libs, err := FindSharedLibraries(ctx, root, []string{libNamePrefix})
	if err != nil {
		return nil, err
	}
	return libs[libNamePrefix], nil
}

func FindSharedLibraries(ctx context.Context, root string, libNamePrefixes []string) (map[string][]string, error) {
	ctx, span := tracer.WithoutCancel(ctx, "FindSharedLibraries") //nolint:ineffassign,wastedassign,staticcheck
	span.SetAttributes(
		attribute.String("root", root),
		attribute.StringSlice("prefixes", libNamePrefixes),
	)
	defer span.End()

	matches := make(map[string][]string, len(libNamePrefixes))

	// scan for matches
	scan := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		for _, libNamePrefix := range libNamePrefixes {
			if strings.HasPrefix(filepath.Base(path), libNamePrefix) {
				matches[libNamePrefix] = append(matches[libNamePrefix], path)
			}
		}
		// TODO: use fi.Sys() here to check for uniqueness instead of stating twice
		return nil
	}

	// scan the common lib directories
	for _, libDir := range []string{"/lib", "/usr/lib", "/usr/local/lib", "/nix/store"} {
		// absolute path to lib dir
		absLibDir := filepath.Join(root, libDir)

		// walk directories and scan
		if err := filepath.Walk(absLibDir, scan); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("scanning for shared libraries: %w", err)
		}
	}

	// dedupe matches
	for _, libName := range libNamePrefixes {
		// hold unique file identifiers
		unique := make(map[uint64]string)
		var uniqueMatches []string
		for _, path := range matches[libName] {
			fi, err := os.Stat(path)
			if err != nil {
				return nil, err
			}

			stat, ok := fi.Sys().(*syscall.Stat_t)
			if !ok {
				return nil, fmt.Errorf("failed to get syscall.Stat_t for %s", path)
			}

			// Use a combination of inode and device ID as a unique identifier
			id := stat.Ino + uint64(stat.Dev)<<32
			if _, exists := unique[id]; !exists {
				unique[id] = path
				uniqueMatches = append(uniqueMatches, path)
			}
		}

		matches[libName] = uniqueMatches
	}

	// return any matches
	return matches, nil
}
