package javassl

/*

	The loader contains system-wide tooling for ssl introspection.
	It is used by all process-specific agents.

*/

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	_ "embed"
)

//go:embed dist/qtap-loader.jar
var loaderJar []byte // attaches to a java process and runs agents within the jvm of the target process

//go:embed dist/custom-jre.tar.gz
var customJre []byte // custom JRE bundle with attach libraries

type loader struct {
	dir       string
	mu        sync.Mutex
	installed bool
	ctx       context.Context
}

func newLoader(ctx context.Context, dir string) *loader {
	return &loader{
		dir: dir,
		ctx: ctx,
	}
}

func (l *loader) Install(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.installed {
		return nil
	}

	ctx, span := tracer.WithRemoteCancel(ctx, l.ctx, "javassl.installLoader")
	defer span.End()

	// ensure the host run directory exists
	if err := os.MkdirAll(l.dir, 0755); err != nil {
		return fmt.Errorf("creating host qtap directory: %w", err)
	}

	// extract the custom JRE
	if err := extractCustomJRE(ctx, l.dir); err != nil {
		return fmt.Errorf("extracting custom JRE: %w", err)
	}

	// write the loader jar file to the host run directory
	if err := os.WriteFile(filepath.Join(l.dir, "qtap-loader.jar"), loaderJar, 0644); err != nil {
		return fmt.Errorf("writing loader jar file: %w", err)
	}

	l.installed = true
	return nil
}

func (l *loader) JavaHome() string {
	return filepath.Join(l.dir, "custom-jre")
}

func (l *loader) JarPath() string {
	return filepath.Join(l.dir, "qtap-loader.jar")
}

func (l *loader) Uninstall(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "javassl.uninstallLoader") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.installed {
		if err := os.RemoveAll(l.dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		l.installed = false
	}

	return nil
}

func extractCustomJRE(ctx context.Context, dir string) error {
	ctx, span := tracer.Start(ctx, "javassl.extractLoaderCustomJRE") //nolint:ineffassign,wastedassign,staticcheck
	defer span.End()

	// clear the directory if it exists
	if err := os.RemoveAll(filepath.Join(dir, "custom-jre")); err != nil {
		return fmt.Errorf("removing custom JRE directory: %w", err)
	}

	// create a gzip reader
	gzReader, err := gzip.NewReader(bytes.NewReader(customJre))
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gzReader.Close()

	// create a tar reader
	tarReader := tar.NewReader(gzReader)

	// extract the tar archive
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar header: %w", err)
		}

		// construct the target path
		targetPath := filepath.Join(dir, header.Name)

		// ensure the parent directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", filepath.Dir(targetPath), err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// create directory
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("creating directory %s: %w", targetPath, err)
			}
		case tar.TypeReg:
			// create regular file
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("creating file %s: %w", targetPath, err)
			}

			if _, err := io.Copy(file, tarReader); err != nil {
				if err := file.Close(); err != nil {
					return fmt.Errorf("closing file %s: %w", targetPath, err)
				}
				return fmt.Errorf("writing file %s: %w", targetPath, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("closing file %s: %w", targetPath, err)
			}
		case tar.TypeSymlink:
			// create symbolic link
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("creating symlink %s -> %s: %w", targetPath, header.Linkname, err)
			}
		}
	}

	return nil
}
