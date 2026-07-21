package buildinfo

import "testing"

func TestMetadataDefaults(t *testing.T) {
	got := Metadata()
	if got.Version != "dev" || got.Commit != "unknown" || got.Ref != "unknown" || got.BuildTime != "unknown" {
		t.Fatalf("Metadata() defaults = %+v", got)
	}
	if got.Source != "https://github.com/qpoint-io/qtap" {
		t.Fatalf("Metadata().Source = %q", got.Source)
	}
	if got.License != "AGPL-3.0-only" {
		t.Fatalf("Metadata().License = %q", got.License)
	}
	if got.OS == "" || got.Architecture == "" {
		t.Fatalf("Metadata() platform = %q/%q", got.OS, got.Architecture)
	}
}

func TestMetadataInjectedValues(t *testing.T) {
	oldVersion, oldCommit, oldRef, oldBuildTime := version, commit, ref, buildTime
	t.Cleanup(func() {
		version, commit, ref, buildTime = oldVersion, oldCommit, oldRef, oldBuildTime
	})

	version = "v1.2.3"
	commit = "0123456789012345678901234567890123456789"
	ref = "refs/tags/v1.2.3"
	buildTime = "2026-07-21T12:34:56Z"

	got := Metadata()
	if got.Version != version || got.Commit != commit || got.Ref != ref || got.BuildTime != buildTime {
		t.Fatalf("Metadata() = %+v", got)
	}
	if Branch() != ref {
		t.Fatalf("Branch() = %q, want %q", Branch(), ref)
	}
}
