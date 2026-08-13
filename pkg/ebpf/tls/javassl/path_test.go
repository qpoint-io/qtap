package javassl_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qpoint-io/qtap/pkg/ebpf/tls/javassl"
	"github.com/stretchr/testify/require"
)

func TestValidateExecutionBasePath(t *testing.T) {
	t.Run("accepts writable executable directory", func(t *testing.T) {
		require.NoError(t, javassl.ValidateExecutionBasePath(t.TempDir()))
	})

	t.Run("rejects relative directory", func(t *testing.T) {
		err := javassl.ValidateExecutionBasePath(filepath.Join("relative", "qtap"))
		require.ErrorContains(t, err, "absolute")
	})
}

func TestValidateExecutionBasePathRejectsNoExecMount(t *testing.T) {
	dir, err := os.MkdirTemp("/run", "qtap-javassl-test-") //nolint:usetesting // Must exercise the real noexec mount.
	if err != nil {
		t.Skipf("cannot create test directory under /run: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	err = javassl.ValidateExecutionBasePath(dir)
	if err == nil {
		t.Skip("/run permits execution on this host")
	}
	require.ErrorContains(t, err, "not executable")
	require.ErrorContains(t, err, "path flags")
}
