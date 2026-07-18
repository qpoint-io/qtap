package gobin

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hashicorp/go-version"
	"go.uber.org/zap"
)

var (
	// ErrStructNotFound indicates the requested struct path was not found
	ErrStructNotFound = errors.New("struct not found")

	// ErrPropertyNotFound indicates the requested property was not found
	ErrPropertyNotFound = errors.New("property not found")

	// ErrNoValidOffset indicates no valid offset was found for the given version
	ErrNoValidOffset = errors.New("no valid offset found for version")
)

var (
	//go:embed offsets.json
	embeddedOffsets    []byte
	precomputedOffsets offsetsFile
)

func init() {
	err := json.Unmarshal(embeddedOffsets, &precomputedOffsets)
	if err != nil {
		panic(fmt.Sprintf("failed to unmarshal embedded offsets: %s", err))
	}
}

type offsetsFile struct {
	Data map[string]map[string]structSpan `json:"data"`
}

type structSpan struct {
	Versions struct {
		Oldest string `json:"oldest"`
		Newest string `json:"newest"`
	} `json:"versions"`
	Offsets []struct {
		Offset uint32 `json:"offset"`
		Since  string `json:"since"`
	} `json:"offsets"`
}

// GetOffset returns the appropriate offset for a struct's property at a specific Go version.
// It returns the most recent offset that's not newer than the requested version.
func GetOffset(structPath, property, version string) (uint32, error) {
	structData, ok := precomputedOffsets.Data[structPath]
	if !ok {
		return 0, ErrStructNotFound
	}

	propData, ok := structData[property]
	if !ok {
		return 0, ErrPropertyNotFound
	}

	// Check if version is too old
	if compareVersions(version, propData.Versions.Oldest) < 0 {
		return 0, ErrNoValidOffset
	}

	// If version is newer than our newest, warn and use the newest version
	if compareVersions(version, propData.Versions.Newest) > 0 {
		zap.L().Warn("requested Go version exceeds supported range, using newest supported version",
			zap.String("struct_path", structPath),
			zap.String("property", property),
			zap.String("requested_version", version),
			zap.String("newest_supported_version", propData.Versions.Newest))
		version = propData.Versions.Newest
	}

	// Find the most recent applicable offset
	var lastValidOffset uint32
	found := false

	for _, o := range propData.Offsets {
		if compareVersions(version, o.Since) >= 0 {
			lastValidOffset = o.Offset
			found = true
		} else {
			break
		}
	}

	if !found {
		return 0, ErrNoValidOffset
	}

	return lastValidOffset, nil
}

// compareVersions compares two version strings (format: "1.2.3")
// Returns:
//
//	-1 if v1 < v2
//	0 if v1 == v2
//	1 if v1 > v2
func compareVersions(ver, minVersion string) int {
	v, err := version.NewVersion(ver)
	if err != nil {
		return -1
	}
	minV, err := version.NewVersion(minVersion)
	if err != nil {
		return 1
	}
	return v.Compare(minV)
}
