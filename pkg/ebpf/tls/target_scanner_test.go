package tls

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qpoint-io/qtap/pkg/binutils"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
)

func Test_targetScanner(t *testing.T) {
	ctx := t.Context()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	opensslProbe := NewMockProbe(ctrl)
	opensslProbe.EXPECT().Name().AnyTimes().Return("openssl")
	gnutlsProbe := NewMockProbe(ctrl)
	gnutlsProbe.EXPECT().Name().AnyTimes().Return("gnutls")
	scanner := NewTargetScanner(zaptest.NewLogger(t), []Probe{opensslProbe, gnutlsProbe})

	f1 := createTestElf(t, "")
	fakeMtime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	require.NoError(t, os.Chtimes(f1, time.Unix(fakeMtime, 0), time.Unix(fakeMtime, 0)))

	var scanRes *ScanResult

	t.Run("Scan", func(t *testing.T) {
		opensslProbe.EXPECT().Scan(gomock.Any(), gomock.Cond(func(target *ExeElfScannable) bool {
			return target.Path == f1
		})).Return(&testProbeScanResult{name: "openssl", detected: false}, nil)
		gnutlsProbe.EXPECT().Scan(gomock.Any(), gomock.Cond(func(target *ExeElfScannable) bool {
			return target.Path == f1
		})).Return(&testProbeScanResult{name: "gnutls", detected: true}, nil)

		var err error
		scanRes, err = scanner.Scan(ctx, &ExeScannable{Path: f1})
		require.NoError(t, err)
		require.Equal(t, &ScanResult{
			Hash:  "v1:5fc052f589b5b2bf",
			Mtime: fakeMtime,
			ProbeResults: map[string]ProbeScanResult{
				"openssl": &testProbeScanResult{name: "openssl", detected: false},
				"gnutls":  &testProbeScanResult{name: "gnutls", detected: true},
			},
		}, scanRes)

		t.Run("Cache", func(t *testing.T) {
			cached, ok := scanner.cache.Load(scanRes.Hash)
			require.True(t, ok)
			require.Equal(t, scanRes, cached)

			// scanning again should hit the cache
			// NOTE: we should not change the value of the original `scanRes` variable
			// here at it is used in the Attach tests.
			cacheScanRes, err := scanner.Scan(ctx, &ExeScannable{Path: f1})
			require.NoError(t, err)
			require.Equal(t, cacheScanRes, cached)

			// changing the mtime should invalidate the cache
			require.NoError(t, os.Chtimes(f1, time.Unix(fakeMtime+1, 0), time.Unix(fakeMtime+1, 0)))
			opensslProbe.EXPECT().Scan(gomock.Any(), gomock.Cond(func(target *ExeElfScannable) bool {
				return target.Path == f1
			})).Return(&testProbeScanResult{name: "openssl", detected: false}, nil)
			gnutlsProbe.EXPECT().Scan(gomock.Any(), gomock.Cond(func(target *ExeElfScannable) bool {
				return target.Path == f1
			})).Return(&testProbeScanResult{name: "gnutls", detected: false}, nil)

			cacheScanRes, err = scanner.Scan(ctx, &ExeScannable{Path: f1})
			require.NoError(t, err)
			require.NotEqual(t, cacheScanRes, cached)
			require.Equal(t, cacheScanRes.Hash, cached.Hash)
			require.NotEqual(t, cacheScanRes.Mtime, cached.Mtime)
		})
	})

	t.Run("Attach", func(t *testing.T) {
		gnutlsCloser := &testCloser{}
		gnutlsProbe.EXPECT().Attach(gomock.Any(),
			gomock.Cond(func(a *ExeLinkAttachable) bool {
				return a.Path == f1
			}),
			&testProbeScanResult{name: "gnutls", detected: true},
		).DoAndReturn(func(ctx context.Context, target *ExeLinkAttachable, result ProbeScanResult) (io.Closer, error) {
			require.Equal(t, f1, target.Path)
			require.Equal(t, 1, target.PID)
			require.NotNil(t, target.Exe)
			return gnutlsCloser, nil
		})

		closer, err := scanner.Attach(ctx, &ExeAttachable{
			PID:  1,
			Path: f1,
			Root: "/proc/1/root",
		}, scanRes)
		require.NoError(t, err)
		require.NoError(t, closer.Close())
		require.Equal(t, 1, gnutlsCloser.closes)

		t.Run("probe_error", func(t *testing.T) {
			gnutlsProbe.EXPECT().Attach(gomock.Any(),
				gomock.Cond(func(a *ExeLinkAttachable) bool {
					return a.Path == f1
				}),
				&testProbeScanResult{name: "gnutls", detected: true},
			).Return(nil, errors.New("probe error"))

			closer, err := scanner.Attach(ctx, &ExeAttachable{
				PID:  1,
				Path: f1,
				Root: "/proc/1/root",
			}, scanRes)
			require.EqualError(t, err, "tls[gnutls]: probe error")
			require.Nil(t, closer)
			require.Equal(t, 1, gnutlsCloser.closes)
		})
	})
}

func Test_targetScanner_Container(t *testing.T) {
	ctx := t.Context()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	opensslProbe := NewMockProbe(ctrl)
	opensslProbe.EXPECT().Name().AnyTimes().Return("openssl")
	opensslProbe.EXPECT().SharedLibraries().AnyTimes().Return("libssl.so")
	gnutlsProbe := NewMockProbe(ctrl)
	gnutlsProbe.EXPECT().Name().AnyTimes().Return("gnutls")
	gnutlsProbe.EXPECT().SharedLibraries().AnyTimes().Return("libgnutls.so")
	scanner := NewTargetScanner(zaptest.NewLogger(t), []Probe{opensslProbe, gnutlsProbe})

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "usr/lib"), 0755))
	libOpenssl1 := filepath.Join(root, "usr/lib/libssl.so.1")
	libOpenssl2 := filepath.Join(root, "usr/lib/libssl.so")
	libGnutls := filepath.Join(root, "usr/lib/libgnutls.so")
	libGnutlsDupe := filepath.Join(root, "usr/lib/libgnutls.so.1")

	createTestElf(t, libOpenssl1)
	createTestElf(t, libOpenssl2)
	createTestElf(t, libGnutls)
	createTestElf(t, libGnutlsDupe)
	// append data to the files to make them different.
	appendToFile(t, libOpenssl1, []byte("1"))
	appendToFile(t, libOpenssl2, []byte("2"))
	appendToFile(t, libGnutls, []byte("3"))
	appendToFile(t, libGnutlsDupe, []byte("3")) // identical to libGnutls

	var containerScanRes *ContainerScanResult
	t.Run("Scan", func(t *testing.T) {
		opensslProbe.EXPECT().ScanLibrary(gomock.Any(), gomock.Cond(func(ef *binutils.Elf) bool {
			return ef.Path() == libOpenssl1
		})).Return(&testProbeScanResult{name: "openssl1", detected: false}, nil)
		opensslProbe.EXPECT().ScanLibrary(gomock.Any(), gomock.Cond(func(ef *binutils.Elf) bool {
			return ef.Path() == libOpenssl2
		})).Return(&testProbeScanResult{name: "openssl2", detected: true}, nil)

		gnutlsProbe.EXPECT().ScanLibrary(gomock.Any(), gomock.Cond(func(ef *binutils.Elf) bool {
			// these files are identical so only one should be scanned and the result should be cached.
			return ef.Path() == libGnutls || ef.Path() == libGnutlsDupe
		})).Return(&testProbeScanResult{name: "gnutls", detected: true}, nil)

		var err error
		containerScanRes, err = scanner.ScanContainer(ctx, "container-id", root)
		require.NoError(t, err)
		require.Equal(t, &ContainerScanResult{
			SharedLibraries: map[string]map[string]ProbeScanResult{
				"openssl": {
					libOpenssl1: &testProbeScanResult{name: "openssl1", detected: false},
					libOpenssl2: &testProbeScanResult{name: "openssl2", detected: true},
				},
				"gnutls": {
					libGnutls:     &testProbeScanResult{name: "gnutls", detected: true},
					libGnutlsDupe: &testProbeScanResult{name: "gnutls", detected: true},
				},
			},
		}, containerScanRes)
	})

	t.Run("Attach", func(t *testing.T) {
		opensslCloser := &testCloser{}
		opensslProbe.EXPECT().AttachLibrary(gomock.Any(),
			gomock.Cond(func(ex *ExeLibraryAttachable) bool {
				return ex.Path == libOpenssl2
			}),
			&testProbeScanResult{name: "openssl2", detected: true},
		).Return(opensslCloser, nil)

		gnutlsCloser := &testCloser{}
		gnutlsProbe.EXPECT().AttachLibrary(gomock.Any(),
			gomock.Cond(func(ex *ExeLibraryAttachable) bool {
				return ex.Path == libGnutls
			}),
			&testProbeScanResult{name: "gnutls", detected: true},
		).Return(gnutlsCloser, nil)
		gnutlsProbe.EXPECT().AttachLibrary(gomock.Any(),
			gomock.Cond(func(ex *ExeLibraryAttachable) bool {
				return ex.Path == libGnutlsDupe
			}),
			&testProbeScanResult{name: "gnutls", detected: true},
		).Return(gnutlsCloser, nil)

		closer, err := scanner.AttachContainer(ctx, containerScanRes)
		require.NoError(t, err)
		require.NoError(t, closer.Close())
		require.Equal(t, 1, opensslCloser.closes)
		require.Equal(t, 2, gnutlsCloser.closes)
	})
}

// createTestElf creates a minimal valid ELF64 binary with symbols for testing.
// If path is empty, a temporary file will be created.
// Returns the path to the created file.
func createTestElf(t *testing.T, path string) string {
	t.Helper()
	if path == "" {
		path = t.TempDir() + "/test.elf"
	}

	// Build a minimal ELF64 with:
	// - ELF header
	// - Section headers: null, .symtab, .strtab, .shstrtab
	// - Symbol table with test symbols
	// - String tables

	// String tables
	shstrtab := []byte("\x00.symtab\x00.strtab\x00.shstrtab\x00")
	strtab := []byte("\x00test_symbol\x00SSL_read\x00SSL_write\x00hello_world\x00")

	// Symbol table entries (Sym64: 24 bytes each)
	// Entry 0: null symbol (required)
	// Entry 1-4: test symbols
	symtab := make([]byte, 5*24) // 5 symbols * 24 bytes

	// Symbol 1: test_symbol at index 1 in strtab
	binary.LittleEndian.PutUint32(symtab[24:28], 1)      // name offset
	symtab[28] = 0x12                                    // info: STB_GLOBAL | STT_FUNC
	symtab[29] = 0                                       // other
	binary.LittleEndian.PutUint16(symtab[30:32], 1)      // shndx
	binary.LittleEndian.PutUint64(symtab[32:40], 0x1000) // value
	binary.LittleEndian.PutUint64(symtab[40:48], 0x100)  // size

	// Symbol 2: SSL_read at index 13 in strtab
	binary.LittleEndian.PutUint32(symtab[48:52], 13)
	symtab[52] = 0x12
	symtab[53] = 0
	binary.LittleEndian.PutUint16(symtab[54:56], 1)
	binary.LittleEndian.PutUint64(symtab[56:64], 0x2000)
	binary.LittleEndian.PutUint64(symtab[64:72], 0x50)

	// Symbol 3: SSL_write at index 22 in strtab
	binary.LittleEndian.PutUint32(symtab[72:76], 22)
	symtab[76] = 0x12
	symtab[77] = 0
	binary.LittleEndian.PutUint16(symtab[78:80], 1)
	binary.LittleEndian.PutUint64(symtab[80:88], 0x3000)
	binary.LittleEndian.PutUint64(symtab[88:96], 0x60)

	// Symbol 4: hello_world at index 32 in strtab
	binary.LittleEndian.PutUint32(symtab[96:100], 32)
	symtab[100] = 0x12
	symtab[101] = 0
	binary.LittleEndian.PutUint16(symtab[102:104], 1)
	binary.LittleEndian.PutUint64(symtab[104:112], 0x4000)
	binary.LittleEndian.PutUint64(symtab[112:120], 0x70)

	// Calculate offsets
	ehdrSize := uint64(64)   // ELF64 header
	shdrSize := uint64(64)   // Section header size
	numSections := uint64(4) // null, .symtab, .strtab, .shstrtab

	// Section data follows header
	symtabOff := ehdrSize
	strtabOff := symtabOff + uint64(len(symtab))
	shstrtabOff := strtabOff + uint64(len(strtab))
	shdrOff := shstrtabOff + uint64(len(shstrtab))

	// Build ELF header (64 bytes)
	ehdr := make([]byte, 64)
	// e_ident
	copy(ehdr[0:4], []byte{0x7f, 'E', 'L', 'F'}) // magic
	ehdr[4] = 2                                  // ELFCLASS64
	ehdr[5] = 1                                  // ELFDATA2LSB (little endian)
	ehdr[6] = 1                                  // EV_CURRENT
	ehdr[7] = 0                                  // ELFOSABI_NONE
	// e_type
	binary.LittleEndian.PutUint16(ehdr[16:18], 2) // ET_EXEC
	// e_machine
	binary.LittleEndian.PutUint16(ehdr[18:20], 62) // EM_X86_64
	// e_version
	binary.LittleEndian.PutUint32(ehdr[20:24], 1) // EV_CURRENT
	// e_entry
	binary.LittleEndian.PutUint64(ehdr[24:32], 0x1000)
	// e_phoff (no program headers)
	binary.LittleEndian.PutUint64(ehdr[32:40], 0)
	// e_shoff (section headers offset)
	binary.LittleEndian.PutUint64(ehdr[40:48], shdrOff)
	// e_flags
	binary.LittleEndian.PutUint32(ehdr[48:52], 0)
	// e_ehsize
	binary.LittleEndian.PutUint16(ehdr[52:54], 64)
	// e_phentsize
	binary.LittleEndian.PutUint16(ehdr[54:56], 56)
	// e_phnum
	binary.LittleEndian.PutUint16(ehdr[56:58], 0)
	// e_shentsize
	binary.LittleEndian.PutUint16(ehdr[58:60], 64)
	// e_shnum
	binary.LittleEndian.PutUint16(ehdr[60:62], uint16(numSections))
	// e_shstrndx
	binary.LittleEndian.PutUint16(ehdr[62:64], 3) // .shstrtab is section 3

	// Build section headers (64 bytes each)
	shdrs := make([]byte, numSections*shdrSize)

	// Section 0: null (all zeros - already done)

	// Section 1: .symtab
	shdr1 := shdrs[64:128]
	binary.LittleEndian.PutUint32(shdr1[0:4], 1)                     // sh_name offset in shstrtab
	binary.LittleEndian.PutUint32(shdr1[4:8], 2)                     // sh_type = SHT_SYMTAB
	binary.LittleEndian.PutUint64(shdr1[8:16], 0)                    // sh_flags
	binary.LittleEndian.PutUint64(shdr1[16:24], 0)                   // sh_addr
	binary.LittleEndian.PutUint64(shdr1[24:32], symtabOff)           // sh_offset
	binary.LittleEndian.PutUint64(shdr1[32:40], uint64(len(symtab))) // sh_size
	binary.LittleEndian.PutUint32(shdr1[40:44], 2)                   // sh_link = .strtab index
	binary.LittleEndian.PutUint32(shdr1[44:48], 1)                   // sh_info = first non-local symbol
	binary.LittleEndian.PutUint64(shdr1[48:56], 8)                   // sh_addralign
	binary.LittleEndian.PutUint64(shdr1[56:64], 24)                  // sh_entsize = sizeof(Elf64_Sym)

	// Section 2: .strtab
	shdr2 := shdrs[128:192]
	binary.LittleEndian.PutUint32(shdr2[0:4], 9)                     // sh_name offset in shstrtab
	binary.LittleEndian.PutUint32(shdr2[4:8], 3)                     // sh_type = SHT_STRTAB
	binary.LittleEndian.PutUint64(shdr2[8:16], 0)                    // sh_flags
	binary.LittleEndian.PutUint64(shdr2[16:24], 0)                   // sh_addr
	binary.LittleEndian.PutUint64(shdr2[24:32], strtabOff)           // sh_offset
	binary.LittleEndian.PutUint64(shdr2[32:40], uint64(len(strtab))) // sh_size
	binary.LittleEndian.PutUint32(shdr2[40:44], 0)                   // sh_link
	binary.LittleEndian.PutUint32(shdr2[44:48], 0)                   // sh_info
	binary.LittleEndian.PutUint64(shdr2[48:56], 1)                   // sh_addralign
	binary.LittleEndian.PutUint64(shdr2[56:64], 0)                   // sh_entsize

	// Section 3: .shstrtab
	shdr3 := shdrs[192:256]
	binary.LittleEndian.PutUint32(shdr3[0:4], 17)                      // sh_name offset in shstrtab
	binary.LittleEndian.PutUint32(shdr3[4:8], 3)                       // sh_type = SHT_STRTAB
	binary.LittleEndian.PutUint64(shdr3[8:16], 0)                      // sh_flags
	binary.LittleEndian.PutUint64(shdr3[16:24], 0)                     // sh_addr
	binary.LittleEndian.PutUint64(shdr3[24:32], shstrtabOff)           // sh_offset
	binary.LittleEndian.PutUint64(shdr3[32:40], uint64(len(shstrtab))) // sh_size
	binary.LittleEndian.PutUint32(shdr3[40:44], 0)                     // sh_link
	binary.LittleEndian.PutUint32(shdr3[44:48], 0)                     // sh_info
	binary.LittleEndian.PutUint64(shdr3[48:56], 1)                     // sh_addralign
	binary.LittleEndian.PutUint64(shdr3[56:64], 0)                     // sh_entsize

	// Write the ELF file
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create test ELF: %v", err)
	}
	defer f.Close()

	if _, err := f.Write(ehdr); err != nil {
		t.Fatalf("failed to write ELF header: %v", err)
	}
	if _, err := f.Write(symtab); err != nil {
		t.Fatalf("failed to write symtab: %v", err)
	}
	if _, err := f.Write(strtab); err != nil {
		t.Fatalf("failed to write strtab: %v", err)
	}
	if _, err := f.Write(shstrtab); err != nil {
		t.Fatalf("failed to write shstrtab: %v", err)
	}
	if _, err := f.Write(shdrs); err != nil {
		t.Fatalf("failed to write section headers: %v", err)
	}

	return path
}

func appendToFile(t *testing.T, path string, data []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		t.Fatalf("failed to write data: %v", err)
	}
}
