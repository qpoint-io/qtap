package gobin

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"regexp"
)

var ErrNotGoExecutable = errors.New("not a Go executable")
var ErrUnsupportedGoVersion = errors.New("unsupported Go version")

// minGoVersion defines the minimum instrumentable Go version. If the target binary was
// compiled using an older Go version, it will be treated as a non-Go program.
const minGoVersion = "1.14"

// The build info blob left by the linker is identified by
// a 16-byte header, consisting of buildInfoMagic (14 bytes),
// the binary's pointer size (1 byte),
// and whether the binary is big endian (1 byte).
var buildInfoMagic = []byte("\xff Go buildinf:")

// SupportedGoVersion checks if the given Go version string is equal or greater than the
// minimum supported version.
func SupportedGoVersion(version string) bool {
	extracted := extractGoVersion(version)
	if extracted == "" {
		return false
	}
	return compareVersions(extracted, minGoVersion) >= 0
}

func extractGoVersion(version string) string {
	re := regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)
	match := re.FindStringSubmatch(version)
	if match == nil {
		return ""
	}

	return match[0]
}

func GetGoDetails(f *elf.File) (string, error) {
	data, err2 := getBuildInfoBlob(f)
	if err2 != nil {
		return "", err2
	}

	// Decode the blob.
	// The first 14 bytes are buildInfoMagic.
	// The next two bytes indicate pointer size in bytes (4 or 8) and endianness
	// (0 for little, 1 for big).
	// Two virtual addresses to Go strings follow that: runtime.buildVersion,
	// and runtime.modinfo.
	// On 32-bit platforms, the last 8 bytes are unused.
	// If the endianness has the 2 bit set, then the pointers are zero
	// and the 32-byte header is followed by varint-prefixed string data
	// for the two string values we care about.
	ptrSize := int(data[14])
	var vers string
	if data[15]&2 != 0 {
		vers, _ = decodeString(data[32:])
	} else {
		bigEndian := data[15] != 0
		var bo binary.ByteOrder
		if bigEndian {
			bo = binary.BigEndian
		} else {
			bo = binary.LittleEndian
		}
		var readPtr func([]byte) uint64
		if ptrSize == 4 {
			readPtr = func(b []byte) uint64 { return uint64(bo.Uint32(b)) }
		} else {
			readPtr = bo.Uint64
		}
		vers = readString(f, ptrSize, readPtr, readPtr(data[16:]))
	}
	if vers == "" {
		return "", ErrNotGoExecutable
	}
	vers = extractGoVersion(vers)
	if vers == "" {
		return "", ErrNotGoExecutable
	}

	return vers, nil
}

// getBuildInfoBlob reads the first 64kB of text to find the build info blob.
func getBuildInfoBlob(f *elf.File) ([]byte, error) {
	text := dataStart(f)
	data, err := readData(f, text, 64*1024)
	if err != nil {
		return nil, err
	}
	const (
		buildInfoAlign = 16
		buildInfoSize  = 32
	)
	for {
		i := bytes.Index(data, buildInfoMagic)
		if i < 0 || len(data)-i < buildInfoSize {
			return nil, ErrNotGoExecutable
		}
		if i%buildInfoAlign == 0 && len(data)-i >= buildInfoSize {
			data = data[i:]
			break
		}
		data = data[(i+buildInfoAlign-1)&^buildInfoAlign:]
	}
	return data, nil
}

func dataStart(f *elf.File) uint64 {
	for _, s := range f.Sections {
		if s.Name == ".go.buildinfo" {
			return s.Addr
		}
	}
	for _, p := range f.Progs {
		if p.Type == elf.PT_LOAD && p.Flags&(elf.PF_X|elf.PF_W) == elf.PF_W {
			return p.Vaddr
		}
	}
	return 0
}

func readData(f *elf.File, addr, size uint64) ([]byte, error) {
	for _, prog := range f.Progs {
		if prog.Vaddr <= addr && addr <= prog.Vaddr+prog.Filesz-1 {
			n := min(prog.Vaddr+prog.Filesz-addr, size)
			data := make([]byte, n)
			_, err := prog.ReadAt(data, int64(addr-prog.Vaddr))
			if err != nil {
				return nil, err
			}
			return data, nil
		}
	}
	return nil, errors.New("address not mapped")
}

// readString returns the string at address addr in the executable x.
func readString(f *elf.File, ptrSize int, readPtr func([]byte) uint64, addr uint64) string {
	hdr, err := readData(f, addr, uint64(2*ptrSize))
	if err != nil || len(hdr) < 2*ptrSize {
		return ""
	}
	dataAddr := readPtr(hdr)
	dataLen := readPtr(hdr[ptrSize:])
	data, err := readData(f, dataAddr, dataLen)
	if err != nil || uint64(len(data)) < dataLen {
		return ""
	}
	return string(data)
}

func decodeString(data []byte) (s string, rest []byte) {
	u, n := binary.Uvarint(data)
	if n <= 0 || u >= uint64(len(data)-n) {
		return "", nil
	}
	return string(data[n : uint64(n)+u]), data[uint64(n)+u:]
}
