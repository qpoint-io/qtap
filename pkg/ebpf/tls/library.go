package tls

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/qpoint-io/rulekit/set"
	"go.opentelemetry.io/otel/attribute"
)

var commonLibDirs = []string{"/lib", "/lib64", "/usr/lib", "/usr/lib64", "/usr/local/lib", "/nix/store", "/opt", "/snap"}

// SharedLibrary represents a shared library that a probe can attach to.
type SharedLibrary struct {
	// Name corresponds to the library name prefix.
	Name string

	// Paths is a list of unique filesystem paths to the library.
	Paths []string
}

func FindSharedLibrary(ctx context.Context, root string, libNamePrefix string) ([]string, error) {
	res, err := FindSharedLibraries(ctx, root, []string{libNamePrefix})
	if err != nil {
		return nil, err
	}
	if len(res) == 1 {
		return res[0].Paths, nil
	}
	return nil, fs.ErrNotExist
}

// FindSharedLibraries searches for shared libraries in a filesystem root.
func FindSharedLibraries(ctx context.Context, root string, libNamePrefixes []string) ([]*SharedLibrary, error) {
	ctx, span := tracer.Start(ctx, "FindSharedLibraries") //nolint:ineffassign,wastedassign,staticcheck
	span.SetAttributes(
		attribute.String("root", root),
		attribute.StringSlice("prefixes", libNamePrefixes),
	)
	defer span.End()

	var (
		matches = make(map[string]*SharedLibrary)
		// matchedInodes tracks the inodes of the files that have been matched.
		// This is used to deduplicate matches.
		matchedInodes = set.NewSet[uint64]()
	)

	// scan for matches
	scan := func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to get file info for %s: %w", path, err)
		}

		base := filepath.Base(path)
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("failed to get syscall.Stat_t for %s", path)
		}
		// Use a combination of inode and device ID as a unique identifier
		id := stat.Ino + uint64(stat.Dev)<<32

		if matchedInodes.Contains(id) {
			// we've already matched this inode, move on.
			return nil
		}

		for _, libNamePrefix := range libNamePrefixes {
			if strings.HasPrefix(base, libNamePrefix) {
				// mark inode as matched
				matchedInodes.Add(id)

				// we've found a match, add it to the matches map.
				if matches[libNamePrefix] == nil {
					matches[libNamePrefix] = &SharedLibrary{
						Name: libNamePrefix,
					}
				}
				matches[libNamePrefix].Paths = append(matches[libNamePrefix].Paths, path)
			}
		}

		return nil
	}

	// scan the common lib directories
	for _, libDir := range commonLibDirs {
		// absolute path to lib dir
		absLibDir := filepath.Join(root, libDir)

		// walk directories and scan
		if err := filepath.WalkDir(absLibDir, scan); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
	}

	// return any matches
	return slices.Collect(maps.Values(matches)), nil
}
