package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateCmd_ValidConfig(t *testing.T) {
	content := []byte(`stacks:
  default:
    plugins:
      - type: example
`)
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tmp, content, 0644); err != nil {
		t.Fatal(err)
	}

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"validate", tmp})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}

	if out := buf.String(); out != "Config is valid.\n" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestValidateCmd_InvalidYAML(t *testing.T) {
	content := []byte(`{{{invalid yaml`)
	tmp := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tmp, content, 0644); err != nil {
		t.Fatal(err)
	}

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"validate", tmp})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestValidateCmd_MissingFile(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"validate", "/nonexistent/config.yaml"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestValidateCmd_NoArgs(t *testing.T) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"validate"})

	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
}
