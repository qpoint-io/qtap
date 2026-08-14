package nodetls

import (
	"fmt"
	"strconv"
	"strings"
)

type nodeTlsSymaddr struct {
	TLSWrapStreamListenerOffset     int32
	StreamListenerStreamOffset      int32
	StreamBaseStreamResourceOffset  int32
	LibuvStreamWrapStreamBaseOffset int32
	LibuvStreamWrapStreamOffset     int32
	UvStreamSIoWatcherOffset        int32
	UvIoSFdOffset                   int32
}

type semVer struct {
	Major int
	Minor int
	Patch int
}

var nodeTlsSymaddrsV12_3_1 = &nodeTlsSymaddr{
	TLSWrapStreamListenerOffset:     0x0130,
	StreamListenerStreamOffset:      0x08,
	StreamBaseStreamResourceOffset:  0x00,
	LibuvStreamWrapStreamBaseOffset: 0x50,
	LibuvStreamWrapStreamOffset:     0x90,
	UvStreamSIoWatcherOffset:        0x88,
	UvIoSFdOffset:                   0x30,
}

var nodeTlsSymaddrsV12_16_2 = &nodeTlsSymaddr{
	TLSWrapStreamListenerOffset:     0x138,
	StreamListenerStreamOffset:      0x08,
	StreamBaseStreamResourceOffset:  0x00,
	LibuvStreamWrapStreamBaseOffset: 0x58,
	LibuvStreamWrapStreamOffset:     0x98,
	UvStreamSIoWatcherOffset:        0x88,
	UvIoSFdOffset:                   0x30,
}

var nodeTlsSymaddrsV13_0_0 = &nodeTlsSymaddr{
	TLSWrapStreamListenerOffset:     0x130,
	StreamListenerStreamOffset:      0x8,
	StreamBaseStreamResourceOffset:  0x00,
	LibuvStreamWrapStreamBaseOffset: 0x50,
	LibuvStreamWrapStreamOffset:     0x90,
	UvStreamSIoWatcherOffset:        0x88,
	UvIoSFdOffset:                   0x30,
}

var nodeTlsSymaddrsV13_2_0 = &nodeTlsSymaddr{
	TLSWrapStreamListenerOffset:     0x138,
	StreamListenerStreamOffset:      0x08,
	StreamBaseStreamResourceOffset:  0x00,
	LibuvStreamWrapStreamBaseOffset: 0x58,
	LibuvStreamWrapStreamOffset:     0x98,
	UvStreamSIoWatcherOffset:        0x88,
	UvIoSFdOffset:                   0x30,
}

var nodeTlsSymaddrsV13_10_1 = &nodeTlsSymaddr{
	TLSWrapStreamListenerOffset:     0x140,
	StreamListenerStreamOffset:      0x8,
	StreamBaseStreamResourceOffset:  0x00,
	LibuvStreamWrapStreamBaseOffset: 0x60,
	LibuvStreamWrapStreamOffset:     0xa0,
	UvStreamSIoWatcherOffset:        0x88,
	UvIoSFdOffset:                   0x30,
}

var nodeTlsSymaddrsV14_5_0 = &nodeTlsSymaddr{
	TLSWrapStreamListenerOffset:     0x138,
	StreamListenerStreamOffset:      0x08,
	StreamBaseStreamResourceOffset:  0x00,
	LibuvStreamWrapStreamBaseOffset: 0x58,
	LibuvStreamWrapStreamOffset:     0x98,
	UvStreamSIoWatcherOffset:        0x88,
	UvIoSFdOffset:                   0x30,
}

var nodeTlsSymaddrsV15_0_0 = &nodeTlsSymaddr{
	TLSWrapStreamListenerOffset:     0x78,
	StreamListenerStreamOffset:      0x08,
	StreamBaseStreamResourceOffset:  0x00,
	LibuvStreamWrapStreamBaseOffset: 0x58,
	LibuvStreamWrapStreamOffset:     0x98,
	UvStreamSIoWatcherOffset:        0x88,
	UvIoSFdOffset:                   0x30,
}

var nodeTlsSymaddrsV22_7_0 = &nodeTlsSymaddr{
	TLSWrapStreamListenerOffset:     0x80,
	StreamListenerStreamOffset:      0x08,
	StreamBaseStreamResourceOffset:  0x00,
	LibuvStreamWrapStreamBaseOffset: 0x60,
	LibuvStreamWrapStreamOffset:     0xa0,
	UvStreamSIoWatcherOffset:        0x88,
	UvIoSFdOffset:                   0x30,
}

var nodeTlsSymaddrsV23_0_0 = &nodeTlsSymaddr{
	TLSWrapStreamListenerOffset:     0x90,
	StreamListenerStreamOffset:      0x08,
	StreamBaseStreamResourceOffset:  0x00,
	LibuvStreamWrapStreamBaseOffset: 0x70,
	LibuvStreamWrapStreamOffset:     0xb0,
	UvStreamSIoWatcherOffset:        0x88,
	UvIoSFdOffset:                   0x30,
}

func symAddrsFromVersion(versionStr string) (*nodeTlsSymaddr, error) {
	// convert the version string to a SemVer
	ver, err := parseSemVer(versionStr)
	if err != nil {
		return nil, fmt.Errorf("invalid version string: %w", err)
	}

	// fetch the symbol offsets for the version
	kNodeVersionSymaddrs := map[semVer]*nodeTlsSymaddr{
		{Major: 12, Minor: 3, Patch: 1}:  nodeTlsSymaddrsV12_3_1,
		{Major: 12, Minor: 16, Patch: 2}: nodeTlsSymaddrsV12_16_2,
		{Major: 13, Minor: 0, Patch: 0}:  nodeTlsSymaddrsV13_0_0,
		{Major: 13, Minor: 2, Patch: 0}:  nodeTlsSymaddrsV13_2_0,
		{Major: 13, Minor: 10, Patch: 1}: nodeTlsSymaddrsV13_10_1,
		{Major: 14, Minor: 5, Patch: 0}:  nodeTlsSymaddrsV14_5_0,
		{Major: 15, Minor: 0, Patch: 0}:  nodeTlsSymaddrsV15_0_0,
		{Major: 22, Minor: 7, Patch: 0}:  nodeTlsSymaddrsV22_7_0,
		{Major: 22, Minor: 20, Patch: 0}: nodeTlsSymaddrsV23_0_0, // BaseObject list_node_ field added
		{Major: 23, Minor: 0, Patch: 0}:  nodeTlsSymaddrsV23_0_0,
	}

	// find the floor version
	floorVer, found := versionFloor(kNodeVersionSymaddrs, ver)
	if !found {
		return nil, fmt.Errorf("found no symbol offsets for version '%s'", versionStr)
	}

	// return the symbol offsets
	return kNodeVersionSymaddrs[floorVer], nil
}

func parseSemVer(version string) (semVer, error) {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return semVer{}, fmt.Errorf("invalid version format: expected 'X.Y.Z', got '%s'", version)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semVer{}, fmt.Errorf("invalid major version: %w", err)
	}

	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semVer{}, fmt.Errorf("invalid minor version: %w", err)
	}

	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semVer{}, fmt.Errorf("invalid patch version: %w", err)
	}

	return semVer{
		Major: major,
		Minor: minor,
		Patch: patch,
	}, nil
}

func versionFloor(m map[semVer]*nodeTlsSymaddr, ver semVer) (semVer, bool) {
	var floorVer semVer
	found := false

	for k := range m {
		if (k.Major < ver.Major) ||
			(k.Major == ver.Major && k.Minor < ver.Minor) ||
			(k.Major == ver.Major && k.Minor == ver.Minor && k.Patch <= ver.Patch) {
			if !found || k.Major > floorVer.Major ||
				(k.Major == floorVer.Major && k.Minor > floorVer.Minor) ||
				(k.Major == floorVer.Major && k.Minor == floorVer.Minor && k.Patch > floorVer.Patch) {
				floorVer = k
				found = true
			}
		}
	}

	return floorVer, found
}
