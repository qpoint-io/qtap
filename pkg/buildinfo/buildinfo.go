package buildinfo

import "runtime"

const (
	sourceURL = "https://github.com/qpoint-io/qtap"
	license   = "AGPL-3.0-only"
)

// this is set by the build process
var (
	version   string
	commit    string
	ref       string
	buildTime string
)

// Info is the stable, machine-readable release metadata contract.
type Info struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	Ref          string `json:"ref"`
	BuildTime    string `json:"build_time"`
	Source       string `json:"source"`
	License      string `json:"license"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

func Version() string {
	if version == "" {
		return "dev"
	}
	return version
}

func Commit() string {
	if commit == "" {
		return "unknown"
	}
	return commit
}

func Ref() string {
	if ref == "" {
		return "unknown"
	}
	return ref
}

// Branch is retained for callers that previously displayed this value. Release
// builds now report the full Git ref rather than an abbreviated branch name.
func Branch() string {
	return Ref()
}

func BuildTime() string {
	if buildTime == "" {
		return "unknown"
	}
	return buildTime
}

func Source() string {
	return sourceURL
}

func License() string {
	return license
}

func Metadata() Info {
	return Info{
		Version:      Version(),
		Commit:       Commit(),
		Ref:          Ref(),
		BuildTime:    BuildTime(),
		Source:       Source(),
		License:      License(),
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
	}
}
