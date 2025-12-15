package binutils

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchSymbolBufferRefill(t *testing.T) {
	// Content: "ABCDEFGH"
	// Target: "CDEF"
	// Symbol starts at index 2 ('C')
	// Buffer size: 4
	//
	// Execution flow expected:
	// 1. searchSymbol called.
	// 2. Initial state: offset=0, len=0.
	// 3. Logic sees nameOffset (2) is not in current buffer.
	// 4. Seeks to 2? No, Seeks to nameOffset (2) implies it reads starting from 2.
	//    Wait, let's look at searchSymbol logic again.
	//    if nameOffset < *strBufferOffset || nameOffset >= *strBufferOffset+*strBufferLen {
	//       Seek(nameOffset)
	//       Read(buffer) -> reads "CDEF" (if buffer size 4)
	//       offset = 2
	//       len = 4
	//    }
	//    bufferIndex = 2 - 2 = 0.
	//    Loop i=0..3.
	//    bufferIndex+i (0+3) = 3 < 4.
	//    It fits entirely! Refill won't trigger.

	// We need the *initial* read to NOT aligned with nameOffset such that it splits the symbol.
	// searchSymbol seeks to `nameOffset` if it's not in buffer.
	// This means it will ALWAYS position the buffer starting at `nameOffset` if it's empty.
	// So we can't trigger the refill simply by calling it with empty buffer if `Seek` aligns it.

	// However, `searchSymbol` takes `strBufferOffset` and `strBufferLen` as pointers.
	// We can pre-fill the buffer state!

	content := []byte("ABCDEF\x00H")
	reader := bytes.NewReader(content)
	target := []byte("CDEF")
	nameOffset := int64(2)

	// Buffer size 4
	buffer := make([]byte, 4)

	// Pre-condition: Buffer contains "ABCD" (0-3)
	// This simulates that we read a previous symbol or simply that the buffer happens to be this way.
	copy(buffer, content[:4])
	var bufferOffset int64 = 0
	var bufferLen int64 = 4

	// Now nameOffset (2) is within [0, 0+4). So it won't seek.
	// bufferIndex = 2 - 0 = 2.

	// Loop:
	// i=0 ('C'): bufferIndex+i = 2 < 4. Match buffer[2] 'C'. OK.
	// i=1 ('D'): bufferIndex+i = 3 < 4. Match buffer[3] 'D'. OK.
	// i=2 ('E'): bufferIndex+i = 4 >= 4. REFILL TRIGGERED.

	// Refill:
	// Read from reader? Reader is at 0? No, Reader state matters.
	// We must ensure Reader is positioned correctly for the read.
	// The code does: `strReader.Read(strBuffer)`.
	// It assumes the reader is at the end of the current buffer?
	// The `if` block for initial seek:
	//    Seek(nameOffset) ...
	// But if we are IN buffer, we don't seek.
	// So we must ensure `reader` is at `bufferOffset + bufferLen` (i.e. 4).

	_, err := reader.Seek(4, 0)
	require.NoError(t, err)

	// Refill reads "EFGH" into buffer.
	// bufferOffset becomes 4.

	// With BUG: bufferIndex = 0.
	// Check i=2 ('E'). buffer[0+2] = buffer[2] = 'G'.
	// Target[2] = 'E'. 'G' != 'E'. FAIL.

	// With FIX: bufferIndex = 2 - 4 = -2.
	// Check i=2 ('E'). buffer[-2+2] = buffer[0] = 'E'.
	// Target[2] = 'E'. 'E' == 'E'. MATCH.

	found := searchSymbol(reader, nameOffset, target, buffer, &bufferOffset, &bufferLen, false)
	assert.True(t, found, "expect symbol to be found")
}
