//go:build linux

package kernel

import (
	"fmt"

	"golang.org/x/sys/unix"
)

type VersionInfo struct {
	Kernel int
	Major  int
	Minor  int
	Flavor string
}

func (v VersionInfo) String() string {
	return fmt.Sprintf("%d.%d.%d%s", v.Kernel, v.Major, v.Minor, v.Flavor)
}

func CurrentVersion() (VersionInfo, error) {
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		return VersionInfo{}, err
	}

	return ParseRelease(unix.ByteSliceToString(uts.Release[:]))
}

func ParseRelease(release string) (VersionInfo, error) {
	var (
		version VersionInfo
		partial string
		parsed  int
	)

	parsed, _ = fmt.Sscanf(release, "%d.%d%s", &version.Kernel, &version.Major, &partial)
	if parsed < 2 {
		return VersionInfo{}, fmt.Errorf("parse kernel version %q", release)
	}

	parsed, _ = fmt.Sscanf(partial, ".%d%s", &version.Minor, &version.Flavor)
	if parsed < 1 {
		version.Flavor = partial
	}

	return version, nil
}

func CompareVersion(a, b VersionInfo) int {
	if a.Kernel < b.Kernel {
		return -1
	}
	if a.Kernel > b.Kernel {
		return 1
	}

	if a.Major < b.Major {
		return -1
	}
	if a.Major > b.Major {
		return 1
	}

	if a.Minor < b.Minor {
		return -1
	}
	if a.Minor > b.Minor {
		return 1
	}

	return 0
}

func CheckVersion(kernel, major, minor int) (bool, error) {
	current, err := CurrentVersion()
	if err != nil {
		return false, err
	}

	minimum := VersionInfo{Kernel: kernel, Major: major, Minor: minor}
	return CompareVersion(current, minimum) >= 0, nil
}
