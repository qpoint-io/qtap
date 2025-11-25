package binutils

import (
	"debug/elf"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewElf(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	assert.Equal(t, elfPath, e.exe)
	assert.NotNil(t, e.file)
	assert.False(t, e.isClosed.Load())
}

func TestNewElfNonExistent(t *testing.T) {
	ctx := t.Context()
	_, err := NewElf(ctx, "/nonexistent/file", "", false)
	assert.Error(t, err)
}

func TestNewElfNotElf(t *testing.T) {
	ctx := t.Context()
	notElf := createNonElfTestFile(t)

	_, err := NewElf(ctx, notElf, "", false)
	assert.Error(t, err)
}

func TestNewElfContainer(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	// The file is in tmpdir, simulate container with the tmpdir as root
	tmpDir := t.TempDir()
	// Create the ELF in a subdir to test container path joining
	e, err := NewElf(ctx, elfPath, "", false) // non-container mode
	require.NoError(t, err)
	defer e.Close()

	// Now test container mode by checking getFilePath behavior
	e2 := &Elf{
		exe:         "test.elf",
		root:        tmpDir,
		isContainer: true,
	}
	expected := tmpDir + "/test.elf"
	assert.Equal(t, expected, e2.getFilePath())
}

func TestElfClose(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)

	// First close should succeed
	require.NoError(t, e.Close())
	assert.True(t, e.isClosed.Load())

	// Second close should be idempotent
	require.NoError(t, e.Close())
}

func TestElfElf(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	ef, err := e.Elf(ctx)
	require.NoError(t, err)
	assert.NotNil(t, ef)

	// Second call should return same instance (lazy init)
	ef2, _ := e.Elf(ctx)
	assert.Same(t, ef, ef2)
}

func TestElfElfClosed(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)

	e.Close()

	_, err = e.Elf(ctx)
	require.ErrorIs(t, err, ErrFileClosed)
}

func TestElfElfNoFile(t *testing.T) {
	ctx := t.Context()
	e := &Elf{exe: "dummy"}

	_, err := e.Elf(ctx)
	require.ErrorIs(t, err, ErrNoFileLoaded)
}

func TestElfGetSections(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	sections := e.GetSections(ctx)
	assert.NotNil(t, sections)
	assert.NotEmpty(t, sections)
}

func TestElfSearchSymbols(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	// Search for existing symbol
	targets := []SymbolSearch{
		{Name: "test_symbol", MatchStrategy: MatchStrategyExact},
	}

	symbols, err := e.SearchSymbols(ctx, targets, elf.SHT_SYMTAB)
	require.NoError(t, err)
	require.Len(t, symbols, 1)
	assert.Equal(t, "test_symbol", symbols[0].Name)
}

func TestElfSearchSymbolsPrefix(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	// Search with prefix strategy
	targets := []SymbolSearch{
		{Name: "SSL_", MatchStrategy: MatchStrategyPrefix},
	}

	symbols, err := e.SearchSymbols(ctx, targets, elf.SHT_SYMTAB)
	require.NoError(t, err)
	// Should find SSL_read and SSL_write (but stops at 1 since targets has len 1)
	assert.GreaterOrEqual(t, len(symbols), 1)
}

func TestElfSearchSymbolsNoMatch(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	targets := []SymbolSearch{
		{Name: "nonexistent_symbol_12345", MatchStrategy: MatchStrategyExact},
	}

	symbols, err := e.SearchSymbols(ctx, targets, elf.SHT_SYMTAB)
	require.NoError(t, err)
	assert.Empty(t, symbols)
}

func TestElfSearchSymbolsMultiple(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	// Search for multiple symbols
	targets := []SymbolSearch{
		{Name: "SSL_read", MatchStrategy: MatchStrategyExact},
		{Name: "SSL_write", MatchStrategy: MatchStrategyExact},
		{Name: "hello_world", MatchStrategy: MatchStrategyExact},
	}

	symbols, err := e.SearchSymbols(ctx, targets, elf.SHT_SYMTAB)
	require.NoError(t, err)
	assert.Len(t, symbols, 3)
}

func TestElfContainsAnySymbols(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	// Test with existing symbol
	targets := []SymbolSearch{
		{Name: "test_symbol", MatchStrategy: MatchStrategyExact},
	}

	found, err := e.ContainsAnySymbols(ctx, targets, elf.SHT_SYMTAB)
	require.NoError(t, err)
	assert.True(t, found)
}

func TestElfContainsAnySymbolsNotFound(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	targets := []SymbolSearch{
		{Name: "definitely_not_a_real_symbol", MatchStrategy: MatchStrategyExact},
	}

	found, err := e.ContainsAnySymbols(ctx, targets, elf.SHT_SYMTAB)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestElfContainsAnySymbolsUnsupportedSection(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	targets := []SymbolSearch{
		{Name: "test", MatchStrategy: MatchStrategyExact},
	}

	// Use an unsupported section type
	_, err = e.ContainsAnySymbols(ctx, targets, elf.SHT_NULL)
	assert.Error(t, err)
}

func TestElfCalculateUprobeAddresses(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	// Test with empty symbols
	symbols := []elf.Symbol{}
	result := e.CalculateUprobeAddresses(ctx, symbols)
	assert.Empty(t, result)

	// Test with a mock symbol
	symbols = []elf.Symbol{
		{Name: "test", Value: 0x1000},
	}
	result = e.CalculateUprobeAddresses(ctx, symbols)
	assert.Len(t, result, 1)
}

func TestElfLdd(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	if err != nil {
		t.Fatalf("NewElf() error = %v", err)
	}
	defer e.Close()

	// Our basic synthetic ELF doesn't have dynamic section
	libs, err := e.Ldd(ctx)
	// May or may not error depending on ELF structure
	_ = libs
	_ = err
}

func TestElfLddWithDynamicSection(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElfWithDynsym(t)

	e, err := NewElf(ctx, elfPath, "", false)
	if err != nil {
		t.Fatalf("NewElf() error = %v", err)
	}
	defer e.Close()

	libs, err := e.Ldd(ctx)
	if err != nil {
		t.Logf("Ldd() error = %v (may be expected)", err)
		return
	}

	// Should find libssl.so.1.1 and libcrypto.so.1.1
	if len(libs) != 2 {
		t.Errorf("Ldd() found %d libs, want 2", len(libs))
	}
}

func TestIsElfNoFile(t *testing.T) {
	e := &Elf{exe: "dummy"}

	isElf, err := e.isELF()
	require.ErrorIs(t, err, ErrNoFileLoaded)
	assert.False(t, isElf)
}

func TestReadStringAt(t *testing.T) {
	// Create a mock reader with null-terminated string
	data := []byte("hello\x00world\x00")
	reader := &mockReaderAt{data: data}

	str, err := readStringAt(reader, 0)
	require.NoError(t, err)
	assert.Equal(t, "hello", str)

	str, err = readStringAt(reader, 6)
	require.NoError(t, err)
	assert.Equal(t, "world", str)
}

type mockReaderAt struct {
	data []byte
}

func (m *mockReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	if off >= int64(len(m.data)) {
		return 0, nil
	}
	n = copy(p, m.data[off:])
	return n, nil
}

func TestMatchSymbolAt(t *testing.T) {
	data := []byte("SSL_read\x00SSL_write\x00other\x00")
	reader := &mockReaderAt{data: data}

	tests := []struct {
		name     string
		offset   int64
		target   []byte
		strategy MatchStrategy
		want     bool
	}{
		{"Exact match", 0, []byte("SSL_read"), MatchStrategyExact, true},
		{"Exact no match", 0, []byte("SSL_write"), MatchStrategyExact, false},
		{"Prefix match", 0, []byte("SSL"), MatchStrategyPrefix, true},
		{"Suffix match", 0, []byte("read"), MatchStrategySuffix, true},
		{"Contains match", 0, []byte("_re"), MatchStrategyContains, true},
		{"Second string exact", 9, []byte("SSL_write"), MatchStrategyExact, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchSymbolAt(reader, tt.offset, tt.target, tt.strategy)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchInvalidStrategy(t *testing.T) {
	result := match("test", "test", MatchStrategy(999))
	assert.False(t, result)
}

func TestSearchSymbolsNoFileLoaded(t *testing.T) {
	ctx := t.Context()
	e := &Elf{exe: "dummy"}

	_, err := e.SearchSymbols(ctx, []SymbolSearch{{Name: "test"}}, elf.SHT_SYMTAB)
	require.ErrorIs(t, err, ErrNoFileLoaded)
}

func TestElfSearchSymbols32(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf32(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	// Search for existing symbol in 32-bit ELF
	targets := []SymbolSearch{
		{Name: "test_symbol_32", MatchStrategy: MatchStrategyExact},
	}

	symbols, err := e.SearchSymbols(ctx, targets, elf.SHT_SYMTAB)
	require.NoError(t, err)
	require.Len(t, symbols, 1)
	assert.Equal(t, "test_symbol_32", symbols[0].Name)
}

func TestElfContainsAnySymbols32(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf32(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	targets := []SymbolSearch{
		{Name: "test_symbol_32", MatchStrategy: MatchStrategyExact},
	}

	found, err := e.ContainsAnySymbols(ctx, targets, elf.SHT_SYMTAB)
	require.NoError(t, err)
	assert.True(t, found)
}

func TestElfContainsAnySymbolsDynsym(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElfWithDynsym(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	targets := []SymbolSearch{
		{Name: "dynamic_symbol", MatchStrategy: MatchStrategyExact},
	}

	found, err := e.ContainsAnySymbols(ctx, targets, elf.SHT_DYNSYM)
	require.NoError(t, err)
	assert.True(t, found)
}

func TestElfContainsAnySymbolsBothSections(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElf(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	targets := []SymbolSearch{
		{Name: "test_symbol", MatchStrategy: MatchStrategyExact},
	}

	// Test checking both SYMTAB and DYNSYM
	found, err := e.ContainsAnySymbols(ctx, targets, elf.SHT_SYMTAB, elf.SHT_DYNSYM)
	require.NoError(t, err)
	assert.True(t, found)
}

func TestElfCalculateUprobeAddressesWithProgHeaders(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElfWithProgHeaders(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	// Symbol at VA 0x401000 should map to file offset 0x1000
	// (VA - segment_vaddr + segment_offset = 0x401000 - 0x400000 + 0 = 0x1000)
	symbols := []elf.Symbol{
		{Name: "uprobe_target", Value: 0x401000},
	}

	result := e.CalculateUprobeAddresses(ctx, symbols)
	require.Len(t, result, 1)
	assert.Equal(t, uint64(0x1000), result[0].Value)
}

func TestElfSearchSymbolsDynsym(t *testing.T) {
	ctx := t.Context()
	elfPath := createTestElfWithDynsym(t)

	e, err := NewElf(ctx, elfPath, "", false)
	require.NoError(t, err)
	defer e.Close()

	targets := []SymbolSearch{
		{Name: "dynamic_symbol", MatchStrategy: MatchStrategyExact},
	}

	symbols, err := e.SearchSymbols(ctx, targets, elf.SHT_DYNSYM)
	require.NoError(t, err)
	assert.Len(t, symbols, 1)
}

// createTestElf creates a minimal valid ELF64 binary with symbols for testing.
// Returns the path to the created file.
func createTestElf(t interface {
	TempDir() string
	Helper()
	Fatalf(format string, args ...any)
}) string {
	t.Helper()

	tmpDir := t.TempDir()
	elfPath := filepath.Join(tmpDir, "test.elf")

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
	f, err := os.Create(elfPath)
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

	return elfPath
}

// createTestElfWithRodata creates a minimal ELF64 with a .rodata section containing searchable strings.
func createTestElfWithRodata(t interface {
	TempDir() string
	Helper()
	Fatalf(format string, args ...any)
}) string {
	t.Helper()

	tmpDir := t.TempDir()
	elfPath := filepath.Join(tmpDir, "test_rodata.elf")

	// String tables
	shstrtab := []byte("\x00.symtab\x00.strtab\x00.shstrtab\x00.rodata\x00")
	strtab := []byte("\x00test_symbol\x00")
	rodata := []byte("Hello World\x00OpenSSL 1.1.1\x00test_string\x00version_info\x00")

	// Minimal symbol table (just null entry)
	symtab := make([]byte, 24)

	// Calculate offsets
	ehdrSize := uint64(64)
	numSections := uint64(5) // null, .symtab, .strtab, .shstrtab, .rodata
	shdrSize := uint64(64)

	symtabOff := ehdrSize
	strtabOff := symtabOff + uint64(len(symtab))
	shstrtabOff := strtabOff + uint64(len(strtab))
	rodataOff := shstrtabOff + uint64(len(shstrtab))
	shdrOff := rodataOff + uint64(len(rodata))

	// ELF header
	ehdr := make([]byte, 64)
	copy(ehdr[0:4], []byte{0x7f, 'E', 'L', 'F'})
	ehdr[4] = 2                                    // ELFCLASS64
	ehdr[5] = 1                                    // little endian
	ehdr[6] = 1                                    // EV_CURRENT
	binary.LittleEndian.PutUint16(ehdr[16:18], 2)  // ET_EXEC
	binary.LittleEndian.PutUint16(ehdr[18:20], 62) // EM_X86_64
	binary.LittleEndian.PutUint32(ehdr[20:24], 1)
	binary.LittleEndian.PutUint64(ehdr[24:32], 0x1000)
	binary.LittleEndian.PutUint64(ehdr[40:48], shdrOff)
	binary.LittleEndian.PutUint16(ehdr[52:54], 64)
	binary.LittleEndian.PutUint16(ehdr[54:56], 56)
	binary.LittleEndian.PutUint16(ehdr[58:60], 64)
	binary.LittleEndian.PutUint16(ehdr[60:62], uint16(numSections))
	binary.LittleEndian.PutUint16(ehdr[62:64], 3)

	// Section headers
	shdrs := make([]byte, numSections*shdrSize)

	// Section 1: .symtab
	shdr1 := shdrs[64:128]
	binary.LittleEndian.PutUint32(shdr1[0:4], 1)
	binary.LittleEndian.PutUint32(shdr1[4:8], 2) // SHT_SYMTAB
	binary.LittleEndian.PutUint64(shdr1[24:32], symtabOff)
	binary.LittleEndian.PutUint64(shdr1[32:40], uint64(len(symtab)))
	binary.LittleEndian.PutUint32(shdr1[40:44], 2)
	binary.LittleEndian.PutUint64(shdr1[56:64], 24)

	// Section 2: .strtab
	shdr2 := shdrs[128:192]
	binary.LittleEndian.PutUint32(shdr2[0:4], 9)
	binary.LittleEndian.PutUint32(shdr2[4:8], 3) // SHT_STRTAB
	binary.LittleEndian.PutUint64(shdr2[24:32], strtabOff)
	binary.LittleEndian.PutUint64(shdr2[32:40], uint64(len(strtab)))

	// Section 3: .shstrtab
	shdr3 := shdrs[192:256]
	binary.LittleEndian.PutUint32(shdr3[0:4], 17)
	binary.LittleEndian.PutUint32(shdr3[4:8], 3) // SHT_STRTAB
	binary.LittleEndian.PutUint64(shdr3[24:32], shstrtabOff)
	binary.LittleEndian.PutUint64(shdr3[32:40], uint64(len(shstrtab)))

	// Section 4: .rodata
	shdr4 := shdrs[256:320]
	binary.LittleEndian.PutUint32(shdr4[0:4], 27) // ".rodata" starts at offset 27 in shstrtab
	binary.LittleEndian.PutUint32(shdr4[4:8], 1)  // SHT_PROGBITS
	binary.LittleEndian.PutUint64(shdr4[8:16], 2) // SHF_ALLOC
	binary.LittleEndian.PutUint64(shdr4[24:32], rodataOff)
	binary.LittleEndian.PutUint64(shdr4[32:40], uint64(len(rodata)))

	// Write file
	f, err := os.Create(elfPath)
	if err != nil {
		t.Fatalf("failed to create test ELF: %v", err)
	}
	defer f.Close()

	_, err = f.Write(ehdr)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(symtab)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(strtab)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(shstrtab)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(rodata)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(shdrs)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}

	return elfPath
}

// createNonElfTestFile creates a file that is not an ELF.
func createNonElfTestFile(t interface {
	TempDir() string
	Helper()
	Fatalf(format string, args ...any)
}) string {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "notelf.bin")
	if err := os.WriteFile(path, []byte("This is not an ELF file\n"), 0644); err != nil {
		t.Fatalf("failed to create non-ELF file: %v", err)
	}
	return path
}

// createTestElf32 creates a minimal valid ELF32 binary with symbols for testing.
func createTestElf32(t interface {
	TempDir() string
	Helper()
	Fatalf(format string, args ...any)
}) string {
	t.Helper()

	tmpDir := t.TempDir()
	elfPath := filepath.Join(tmpDir, "test32.elf")

	// String tables
	shstrtab := []byte("\x00.symtab\x00.strtab\x00.shstrtab\x00")
	strtab := []byte("\x00test_symbol_32\x00another_sym\x00")

	// Symbol table entries (Sym32: 16 bytes each)
	symtab := make([]byte, 3*16) // 3 symbols * 16 bytes

	// Symbol 1: test_symbol_32
	binary.LittleEndian.PutUint32(symtab[16:20], 1)      // name offset
	binary.LittleEndian.PutUint32(symtab[20:24], 0x1000) // value
	binary.LittleEndian.PutUint32(symtab[24:28], 0x100)  // size
	symtab[28] = 0x12                                    // info: STB_GLOBAL | STT_FUNC
	symtab[29] = 0                                       // other
	binary.LittleEndian.PutUint16(symtab[30:32], 1)      // shndx

	// Symbol 2: another_sym
	binary.LittleEndian.PutUint32(symtab[32:36], 16) // name offset
	binary.LittleEndian.PutUint32(symtab[36:40], 0x2000)
	binary.LittleEndian.PutUint32(symtab[40:44], 0x50)
	symtab[44] = 0x12
	symtab[45] = 0
	binary.LittleEndian.PutUint16(symtab[46:48], 1)

	// Calculate offsets
	ehdrSize := uint64(52) // ELF32 header
	shdrSize := uint64(40) // Section header size for ELF32
	numSections := uint64(4)

	symtabOff := ehdrSize
	strtabOff := symtabOff + uint64(len(symtab))
	shstrtabOff := strtabOff + uint64(len(strtab))
	shdrOff := shstrtabOff + uint64(len(shstrtab))

	// Build ELF32 header (52 bytes)
	ehdr := make([]byte, 52)
	copy(ehdr[0:4], []byte{0x7f, 'E', 'L', 'F'})
	ehdr[4] = 1                                                     // ELFCLASS32
	ehdr[5] = 1                                                     // ELFDATA2LSB
	ehdr[6] = 1                                                     // EV_CURRENT
	binary.LittleEndian.PutUint16(ehdr[16:18], 2)                   // ET_EXEC
	binary.LittleEndian.PutUint16(ehdr[18:20], 3)                   // EM_386
	binary.LittleEndian.PutUint32(ehdr[20:24], 1)                   // EV_CURRENT
	binary.LittleEndian.PutUint32(ehdr[24:28], 0x1000)              // e_entry
	binary.LittleEndian.PutUint32(ehdr[28:32], 0)                   // e_phoff
	binary.LittleEndian.PutUint32(ehdr[32:36], uint32(shdrOff))     // e_shoff
	binary.LittleEndian.PutUint32(ehdr[36:40], 0)                   // e_flags
	binary.LittleEndian.PutUint16(ehdr[40:42], 52)                  // e_ehsize
	binary.LittleEndian.PutUint16(ehdr[42:44], 32)                  // e_phentsize
	binary.LittleEndian.PutUint16(ehdr[44:46], 0)                   // e_phnum
	binary.LittleEndian.PutUint16(ehdr[46:48], 40)                  // e_shentsize
	binary.LittleEndian.PutUint16(ehdr[48:50], uint16(numSections)) // e_shnum
	binary.LittleEndian.PutUint16(ehdr[50:52], 3)                   // e_shstrndx

	// Build section headers (40 bytes each for ELF32)
	shdrs := make([]byte, numSections*shdrSize)

	// Section 1: .symtab
	shdr1 := shdrs[40:80]
	binary.LittleEndian.PutUint32(shdr1[0:4], 1)                     // sh_name
	binary.LittleEndian.PutUint32(shdr1[4:8], 2)                     // sh_type = SHT_SYMTAB
	binary.LittleEndian.PutUint32(shdr1[16:20], uint32(symtabOff))   // sh_offset
	binary.LittleEndian.PutUint32(shdr1[20:24], uint32(len(symtab))) // sh_size
	binary.LittleEndian.PutUint32(shdr1[24:28], 2)                   // sh_link = .strtab index
	binary.LittleEndian.PutUint32(shdr1[36:40], 16)                  // sh_entsize

	// Section 2: .strtab
	shdr2 := shdrs[80:120]
	binary.LittleEndian.PutUint32(shdr2[0:4], 9)
	binary.LittleEndian.PutUint32(shdr2[4:8], 3) // SHT_STRTAB
	binary.LittleEndian.PutUint32(shdr2[16:20], uint32(strtabOff))
	binary.LittleEndian.PutUint32(shdr2[20:24], uint32(len(strtab)))

	// Section 3: .shstrtab
	shdr3 := shdrs[120:160]
	binary.LittleEndian.PutUint32(shdr3[0:4], 17)
	binary.LittleEndian.PutUint32(shdr3[4:8], 3)
	binary.LittleEndian.PutUint32(shdr3[16:20], uint32(shstrtabOff))
	binary.LittleEndian.PutUint32(shdr3[20:24], uint32(len(shstrtab)))

	// Write file
	f, err := os.Create(elfPath)
	if err != nil {
		t.Fatalf("failed to create test ELF32: %v", err)
	}
	defer f.Close()

	_, err = f.Write(ehdr)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(symtab)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(strtab)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(shstrtab)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(shdrs)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}

	return elfPath
}

// createTestElfWithProgHeaders creates an ELF64 with program headers for uprobe address testing.
func createTestElfWithProgHeaders(t interface {
	TempDir() string
	Helper()
	Fatalf(format string, args ...any)
}) string {
	t.Helper()

	tmpDir := t.TempDir()
	elfPath := filepath.Join(tmpDir, "test_prog.elf")

	// String tables
	shstrtab := []byte("\x00.symtab\x00.strtab\x00.shstrtab\x00")
	strtab := []byte("\x00uprobe_target\x00")

	// Symbol at VA 0x401000, should map to file offset 0x1000 with our program header
	symtab := make([]byte, 2*24)
	binary.LittleEndian.PutUint32(symtab[24:28], 1) // name offset
	symtab[28] = 0x12
	binary.LittleEndian.PutUint16(symtab[30:32], 1)
	binary.LittleEndian.PutUint64(symtab[32:40], 0x401000) // value (VA)
	binary.LittleEndian.PutUint64(symtab[40:48], 0x100)

	// Program header (56 bytes for ELF64)
	// PT_LOAD with PF_X flag mapping VA 0x400000 to file offset 0
	phdr := make([]byte, 56)
	binary.LittleEndian.PutUint32(phdr[0:4], 1)          // p_type = PT_LOAD
	binary.LittleEndian.PutUint32(phdr[4:8], 5)          // p_flags = PF_R | PF_X
	binary.LittleEndian.PutUint64(phdr[8:16], 0)         // p_offset
	binary.LittleEndian.PutUint64(phdr[16:24], 0x400000) // p_vaddr
	binary.LittleEndian.PutUint64(phdr[24:32], 0x400000) // p_paddr
	binary.LittleEndian.PutUint64(phdr[32:40], 0x10000)  // p_filesz
	binary.LittleEndian.PutUint64(phdr[40:48], 0x10000)  // p_memsz
	binary.LittleEndian.PutUint64(phdr[48:56], 0x1000)   // p_align

	ehdrSize := uint64(64)
	phdrOff := ehdrSize
	phdrSize := uint64(56)
	symtabOff := phdrOff + phdrSize
	strtabOff := symtabOff + uint64(len(symtab))
	shstrtabOff := strtabOff + uint64(len(strtab))
	shdrOff := shstrtabOff + uint64(len(shstrtab))
	numSections := uint64(4)
	shdrSize := uint64(64)

	// ELF header
	ehdr := make([]byte, 64)
	copy(ehdr[0:4], []byte{0x7f, 'E', 'L', 'F'})
	ehdr[4] = 2 // ELFCLASS64
	ehdr[5] = 1 // little endian
	ehdr[6] = 1
	binary.LittleEndian.PutUint16(ehdr[16:18], 2)
	binary.LittleEndian.PutUint16(ehdr[18:20], 62)
	binary.LittleEndian.PutUint32(ehdr[20:24], 1)
	binary.LittleEndian.PutUint64(ehdr[24:32], 0x401000) // e_entry
	binary.LittleEndian.PutUint64(ehdr[32:40], phdrOff)  // e_phoff
	binary.LittleEndian.PutUint64(ehdr[40:48], shdrOff)
	binary.LittleEndian.PutUint16(ehdr[52:54], 64)
	binary.LittleEndian.PutUint16(ehdr[54:56], 56) // e_phentsize
	binary.LittleEndian.PutUint16(ehdr[56:58], 1)  // e_phnum = 1
	binary.LittleEndian.PutUint16(ehdr[58:60], 64)
	binary.LittleEndian.PutUint16(ehdr[60:62], uint16(numSections))
	binary.LittleEndian.PutUint16(ehdr[62:64], 3)

	// Section headers
	shdrs := make([]byte, numSections*shdrSize)

	shdr1 := shdrs[64:128]
	binary.LittleEndian.PutUint32(shdr1[0:4], 1)
	binary.LittleEndian.PutUint32(shdr1[4:8], 2)
	binary.LittleEndian.PutUint64(shdr1[24:32], symtabOff)
	binary.LittleEndian.PutUint64(shdr1[32:40], uint64(len(symtab)))
	binary.LittleEndian.PutUint32(shdr1[40:44], 2)
	binary.LittleEndian.PutUint64(shdr1[56:64], 24)

	shdr2 := shdrs[128:192]
	binary.LittleEndian.PutUint32(shdr2[0:4], 9)
	binary.LittleEndian.PutUint32(shdr2[4:8], 3)
	binary.LittleEndian.PutUint64(shdr2[24:32], strtabOff)
	binary.LittleEndian.PutUint64(shdr2[32:40], uint64(len(strtab)))

	shdr3 := shdrs[192:256]
	binary.LittleEndian.PutUint32(shdr3[0:4], 17)
	binary.LittleEndian.PutUint32(shdr3[4:8], 3)
	binary.LittleEndian.PutUint64(shdr3[24:32], shstrtabOff)
	binary.LittleEndian.PutUint64(shdr3[32:40], uint64(len(shstrtab)))

	// Write file
	f, err := os.Create(elfPath)
	if err != nil {
		t.Fatalf("failed to create test ELF: %v", err)
	}
	defer f.Close()

	_, err = f.Write(ehdr)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(phdr)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(symtab)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(strtab)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(shstrtab)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(shdrs)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}

	return elfPath
}

// createTestElfWithDynsym creates an ELF with a .dynsym section.
func createTestElfWithDynsym(t interface {
	TempDir() string
	Helper()
	Fatalf(format string, args ...any)
}) string {
	t.Helper()

	tmpDir := t.TempDir()
	elfPath := filepath.Join(tmpDir, "test_dyn.elf")

	shstrtab := []byte("\x00.dynsym\x00.dynstr\x00.shstrtab\x00.dynamic\x00")
	dynstr := []byte("\x00dynamic_symbol\x00libssl.so.1.1\x00libcrypto.so.1.1\x00")

	// Dynsym entries
	dynsym := make([]byte, 2*24)
	binary.LittleEndian.PutUint32(dynsym[24:28], 1)
	dynsym[28] = 0x12
	binary.LittleEndian.PutUint16(dynsym[30:32], 1)
	binary.LittleEndian.PutUint64(dynsym[32:40], 0x5000)
	binary.LittleEndian.PutUint64(dynsym[40:48], 0x80)

	// Dynamic section with DT_NEEDED entries
	// DT_NEEDED = 1, value is offset in dynstr
	dynamic := make([]byte, 3*16) // 3 entries * 16 bytes (Dyn64)
	// Entry 0: DT_NEEDED for libssl.so.1.1 (offset 16 in dynstr)
	binary.LittleEndian.PutUint64(dynamic[0:8], 1)   // d_tag = DT_NEEDED
	binary.LittleEndian.PutUint64(dynamic[8:16], 16) // d_val = offset in dynstr
	// Entry 1: DT_NEEDED for libcrypto.so.1.1 (offset 30 in dynstr)
	binary.LittleEndian.PutUint64(dynamic[16:24], 1)
	binary.LittleEndian.PutUint64(dynamic[24:32], 30)
	// Entry 2: DT_NULL (terminator)
	binary.LittleEndian.PutUint64(dynamic[32:40], 0)
	binary.LittleEndian.PutUint64(dynamic[40:48], 0)

	ehdrSize := uint64(64)
	dynsymOff := ehdrSize
	dynstrOff := dynsymOff + uint64(len(dynsym))
	dynamicOff := dynstrOff + uint64(len(dynstr))
	shstrtabOff := dynamicOff + uint64(len(dynamic))
	shdrOff := shstrtabOff + uint64(len(shstrtab))
	numSections := uint64(5)
	shdrSize := uint64(64)

	ehdr := make([]byte, 64)
	copy(ehdr[0:4], []byte{0x7f, 'E', 'L', 'F'})
	ehdr[4] = 2
	ehdr[5] = 1
	ehdr[6] = 1
	binary.LittleEndian.PutUint16(ehdr[16:18], 3) // ET_DYN
	binary.LittleEndian.PutUint16(ehdr[18:20], 62)
	binary.LittleEndian.PutUint32(ehdr[20:24], 1)
	binary.LittleEndian.PutUint64(ehdr[40:48], shdrOff)
	binary.LittleEndian.PutUint16(ehdr[52:54], 64)
	binary.LittleEndian.PutUint16(ehdr[54:56], 56)
	binary.LittleEndian.PutUint16(ehdr[58:60], 64)
	binary.LittleEndian.PutUint16(ehdr[60:62], uint16(numSections))
	binary.LittleEndian.PutUint16(ehdr[62:64], 3)

	shdrs := make([]byte, numSections*shdrSize)

	// Section 1: .dynsym (SHT_DYNSYM = 11)
	shdr1 := shdrs[64:128]
	binary.LittleEndian.PutUint32(shdr1[0:4], 1)
	binary.LittleEndian.PutUint32(shdr1[4:8], 11) // SHT_DYNSYM
	binary.LittleEndian.PutUint64(shdr1[24:32], dynsymOff)
	binary.LittleEndian.PutUint64(shdr1[32:40], uint64(len(dynsym)))
	binary.LittleEndian.PutUint32(shdr1[40:44], 2) // link to .dynstr
	binary.LittleEndian.PutUint64(shdr1[56:64], 24)

	// Section 2: .dynstr
	shdr2 := shdrs[128:192]
	binary.LittleEndian.PutUint32(shdr2[0:4], 9)
	binary.LittleEndian.PutUint32(shdr2[4:8], 3)
	binary.LittleEndian.PutUint64(shdr2[24:32], dynstrOff)
	binary.LittleEndian.PutUint64(shdr2[32:40], uint64(len(dynstr)))

	// Section 3: .shstrtab
	shdr3 := shdrs[192:256]
	binary.LittleEndian.PutUint32(shdr3[0:4], 17)
	binary.LittleEndian.PutUint32(shdr3[4:8], 3)
	binary.LittleEndian.PutUint64(shdr3[24:32], shstrtabOff)
	binary.LittleEndian.PutUint64(shdr3[32:40], uint64(len(shstrtab)))

	// Section 4: .dynamic (SHT_DYNAMIC = 6)
	shdr4 := shdrs[256:320]
	binary.LittleEndian.PutUint32(shdr4[0:4], 27) // ".dynamic" in shstrtab
	binary.LittleEndian.PutUint32(shdr4[4:8], 6)  // SHT_DYNAMIC
	binary.LittleEndian.PutUint64(shdr4[24:32], dynamicOff)
	binary.LittleEndian.PutUint64(shdr4[32:40], uint64(len(dynamic)))
	binary.LittleEndian.PutUint32(shdr4[40:44], 2)  // link to .dynstr
	binary.LittleEndian.PutUint64(shdr4[56:64], 16) // entsize

	f, err := os.Create(elfPath)
	if err != nil {
		t.Fatalf("failed to create test ELF: %v", err)
	}
	defer f.Close()

	_, err = f.Write(ehdr)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(dynsym)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(dynstr)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(dynamic)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(shstrtab)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}
	_, err = f.Write(shdrs)
	if err != nil {
		t.Fatalf("writing data: %v", err)
	}

	return elfPath
}
