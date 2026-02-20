package synq

import (
	"bytes"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const defaultBufferSize = 1024 * 1024 * 2 // 2MB

func TestLinkedBufferLength(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(int, ...[]byte) *LinkedBuffer
		expected int
	}{
		{
			name:     "Empty buffer",
			setup:    NewLinkedBuffer,
			expected: 0,
		},
		{
			name: "Single append",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				return b
			},
			expected: 5,
		},
		{
			name: "Multiple appends",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				b.Append([]byte(" "))
				b.Append([]byte("world"))
				return b
			},
			expected: 11,
		},
		{
			name: "Append and prepend",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("world"))
				b.Prepend([]byte("hello "))
				return b
			},
			expected: 11,
		},
		{
			name: "Append, prepend, and drain",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello world"))
				b.Prepend([]byte("start "))
				b.Drain(6)
				return b
			},
			expected: 11,
		},
		{
			name: "Replace",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("initial content"))
				b.Replace([]byte("new content"))
				return b
			},
			expected: 11,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.setup(defaultBufferSize)
			assert.Equal(t, tt.expected, b.Length(), "LinkedBuffer.Length() should return the expected length")
		})
	}
}

func TestLinkedBufferLengthConcurrency(t *testing.T) {
	b := NewLinkedBuffer(defaultBufferSize)
	concurrency := 100
	opsPerGoroutine := 1000

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for range concurrency {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				b.Append([]byte("a"))
				b.Length()
			}
		}()
	}

	wg.Wait()

	expectedLength := concurrency * opsPerGoroutine
	assert.Equal(t, expectedLength, b.Length(), "After concurrent operations, LinkedBuffer.Length() should return the expected length")
}

func TestLinkedBufferSlices(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(int, ...[]byte) *LinkedBuffer
		expected [][]byte
	}{
		{
			name:     "Empty buffer",
			setup:    NewLinkedBuffer,
			expected: nil,
		},
		{
			name: "Single append",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				return b
			},
			expected: [][]byte{[]byte("hello")},
		},
		{
			name: "Multiple appends",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				b.Append([]byte(" "))
				b.Append([]byte("world"))
				return b
			},
			expected: [][]byte{[]byte("hello"), []byte(" "), []byte("world")},
		},
		{
			name: "Append and prepend",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("world"))
				b.Prepend([]byte("hello "))
				return b
			},
			expected: [][]byte{[]byte("hello "), []byte("world")},
		},
		{
			name: "Append, prepend, and drain",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello world"))
				b.Prepend([]byte("start "))
				b.Drain(6)
				return b
			},
			expected: [][]byte{[]byte("hello world")},
		},
		{
			name: "Replace",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("initial content"))
				b.Replace([]byte("new content"))
				return b
			},
			expected: [][]byte{[]byte("new content")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.setup(defaultBufferSize)
			var got [][]byte
			b.Slices(func(view []byte) {
				gotCopy := make([]byte, len(view))
				copy(gotCopy, view)
				got = append(got, gotCopy)
			})

			assert.Equal(t, tt.expected, got, "LinkedBuffer.Slices() should return the expected slices")
		})
	}
}

func TestLinkedBufferSlicesConcurrency(t *testing.T) {
	b := NewLinkedBuffer(defaultBufferSize)
	concurrency := 100
	opsPerGoroutine := 100

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := range concurrency {
		go func(id int) {
			defer wg.Done()
			for range opsPerGoroutine {
				b.Append([]byte{byte(id)})
				var slices [][]byte
				b.Slices(func(view []byte) {
					slices = append(slices, view)
				})
			}
		}(i)
	}

	wg.Wait()

	totalLength := 0
	b.Slices(func(view []byte) {
		totalLength += len(view)
	})

	expectedLength := concurrency * opsPerGoroutine
	assert.Equal(t, expectedLength, totalLength, "After concurrent operations, total length from Slices should match the expected length")
}

func TestLinkedBufferCopy(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(int, ...[]byte) *LinkedBuffer
		expected []byte
	}{
		{
			name:     "Empty buffer",
			setup:    NewLinkedBuffer,
			expected: nil,
		},
		{
			name: "Single append",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				return b
			},
			expected: []byte("hello"),
		},
		{
			name: "Multiple appends",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				b.Append([]byte(" "))
				b.Append([]byte("world"))
				return b
			},
			expected: []byte("hello world"),
		},
		{
			name: "Append and prepend",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("world"))
				b.Prepend([]byte("hello "))
				return b
			},
			expected: []byte("hello world"),
		},
		{
			name: "Append, prepend, and drain",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello world"))
				b.Prepend([]byte("start "))
				b.Drain(6)
				return b
			},
			expected: []byte("hello world"),
		},
		{
			name: "Replace",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("initial content"))
				b.Replace([]byte("new content"))
				return b
			},
			expected: []byte("new content"),
		},
		{
			name: "Binary data",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte{0x00, 0x01, 0x02, 0x03})
				b.Append([]byte{0x04, 0x05})
				b.Prepend([]byte{0xFF, 0xFE})
				return b
			},
			expected: []byte{0xFF, 0xFE, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.setup(defaultBufferSize)
			got := b.Copy()
			assert.Equal(t, tt.expected, got, "LinkedBuffer.Copy() should return the expected byte slice")
		})
	}
}

func TestLinkedBufferCopyConcurrency(t *testing.T) {
	b := NewLinkedBuffer(defaultBufferSize)
	concurrency := 100
	opsPerGoroutine := 100

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := range concurrency {
		go func(id int) {
			defer wg.Done()
			for range opsPerGoroutine {
				b.Append([]byte{byte(id)})
				_ = b.Copy() // Perform a copy operation
			}
		}(i)
	}

	wg.Wait()

	finalCopy := b.Copy()
	assert.Len(t, finalCopy, concurrency*opsPerGoroutine, "After concurrent operations, the length of the final copy should match the expected length")

	// Verify that all bytes are in the range [0, concurrency-1]
	for _, b := range finalCopy {
		assert.Less(t, b, byte(concurrency), "Each byte in the final copy should be less than the number of concurrent operations")
	}
}

func TestLinkedBufferAppend(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(int, ...[]byte) *LinkedBuffer
		appends  [][]byte
		expected []byte
	}{
		{
			name:     "Append to empty buffer",
			setup:    NewLinkedBuffer,
			appends:  [][]byte{[]byte("hello")},
			expected: []byte("hello"),
		},
		{
			name:     "Multiple appends",
			setup:    NewLinkedBuffer,
			appends:  [][]byte{[]byte("hello"), []byte(" "), []byte("world")},
			expected: []byte("hello world"),
		},
		{
			name: "Append to non-empty buffer",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("initial "))
				return b
			},
			appends:  [][]byte{[]byte("content")},
			expected: []byte("initial content"),
		},
		{
			name: "Append empty slice",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				return b
			},
			appends:  [][]byte{[]byte("")},
			expected: []byte("hello"),
		},
		{
			name:     "Append binary data",
			setup:    NewLinkedBuffer,
			appends:  [][]byte{{0x00, 0x01}, {0x02, 0x03}, {0x04, 0x05}},
			expected: []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
		},
		{
			name: "Append after drain",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello world"))
				b.Drain(6)
				return b
			},
			appends:  [][]byte{[]byte("!!")},
			expected: []byte("world!!"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.setup(defaultBufferSize)
			for _, appendData := range tt.appends {
				b.Append(appendData)
			}
			got := b.Copy()
			assert.Equal(t, tt.expected, got, "LinkedBuffer content after Append() operations should match expected")

			// Additional checks
			assert.Equal(t, len(tt.expected), b.Length(), "LinkedBuffer.Length() should match expected length")

			var slicesContent []byte
			b.Slices(func(view []byte) {
				slicesContent = append(slicesContent, view...)
			})
			assert.Equal(t, tt.expected, slicesContent, "Content from Slices() should match expected")
		})
	}
}

func TestLinkedBufferAppendConcurrency(t *testing.T) {
	b := NewLinkedBuffer(defaultBufferSize)
	concurrency := 100
	opsPerGoroutine := 1000
	appendData := []byte("a")

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for range concurrency {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				b.Append(appendData)
			}
		}()
	}

	wg.Wait()

	expectedLength := concurrency * opsPerGoroutine
	assert.Equal(t, expectedLength, b.Length(), "After concurrent operations, LinkedBuffer.Length() should return the expected length")

	content := b.Copy()

	assert.Len(t, content, expectedLength, "Length of copied content should match expected length")

	expectedContent := bytes.Repeat(appendData, expectedLength)
	assert.Equal(t, expectedContent, content, "Content after concurrent Append() operations should match expected")
}

func TestLinkedBufferPrepend(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(int, ...[]byte) *LinkedBuffer
		prepends [][]byte
		expected []byte
	}{
		{
			name:     "Prepend to empty buffer",
			setup:    NewLinkedBuffer,
			prepends: [][]byte{[]byte("hello")},
			expected: []byte("hello"),
		},
		{
			name:     "Multiple prepends",
			setup:    NewLinkedBuffer,
			prepends: [][]byte{[]byte("world"), []byte(" "), []byte("hello")},
			expected: []byte("hello world"),
		},
		{
			name: "Prepend to non-empty buffer",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("content"))
				return b
			},
			prepends: [][]byte{[]byte("initial ")},
			expected: []byte("initial content"),
		},
		{
			name: "Prepend empty slice",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				return b
			},
			prepends: [][]byte{[]byte("")},
			expected: []byte("hello"),
		},
		{
			name:     "Prepend binary data",
			setup:    NewLinkedBuffer,
			prepends: [][]byte{{0x04, 0x05}, {0x02, 0x03}, {0x00, 0x01}},
			expected: []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
		},
		{
			name: "Prepend after drain",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("world"))
				b.Drain(2)
				return b
			},
			prepends: [][]byte{[]byte("hello ")},
			expected: []byte("hello rld"),
		},
		{
			name: "Prepend and append mix",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("middle"))
				return b
			},
			prepends: [][]byte{[]byte("start "), []byte("very ")},
			expected: []byte("very start middle"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.setup(defaultBufferSize)
			for _, prependData := range tt.prepends {
				b.Prepend(prependData)
			}
			got := b.Copy()
			assert.Equal(t, tt.expected, got, "LinkedBuffer content after Prepend() operations should match expected")

			// Additional checks
			assert.Equal(t, len(tt.expected), b.Length(), "LinkedBuffer.Length() should match expected length")

			var slicesContent []byte
			b.Slices(func(view []byte) {
				slicesContent = append(slicesContent, view...)
			})
			assert.Equal(t, tt.expected, slicesContent, "Content from Slices() should match expected")
		})
	}
}

func TestLinkedBufferPrependConcurrency(t *testing.T) {
	b := NewLinkedBuffer(defaultBufferSize)
	concurrency := 100
	opsPerGoroutine := 100
	prependData := []byte("a")

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for range concurrency {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				b.Prepend(prependData)
			}
		}()
	}

	wg.Wait()

	expectedLength := concurrency * opsPerGoroutine
	assert.Equal(t, expectedLength, b.Length(), "After concurrent operations, LinkedBuffer.Length() should return the expected length")

	content := b.Copy()
	assert.Len(t, content, expectedLength, "Length of copied content should match expected length")
	require.NotNil(t, content, "Content should not be nil")

	expectedContent := bytes.Repeat(prependData, expectedLength)
	assert.Equal(t, expectedContent, content, "Content after concurrent Prepend() operations should match expected")

	// Check if the order is reversed (last prepended is first)
	for i := range concurrency {
		segment := content[i*opsPerGoroutine : (i+1)*opsPerGoroutine]
		assert.Equal(t, bytes.Repeat(prependData, opsPerGoroutine), segment,
			"Each segment of the buffer should consist of repeated prependData")
	}
}

func TestLinkedBufferDrain(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(int, ...[]byte) *LinkedBuffer
		drainAmount  int
		expectedData []byte
		expectedLen  int
	}{
		{
			name:         "Drain empty buffer",
			setup:        NewLinkedBuffer,
			drainAmount:  10,
			expectedData: nil,
			expectedLen:  0,
		},
		{
			name: "Drain partial buffer",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello world"))
				return b
			},
			drainAmount:  6,
			expectedData: []byte("world"),
			expectedLen:  5,
		},
		{
			name: "Drain entire buffer",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				return b
			},
			drainAmount:  5,
			expectedData: nil,
			expectedLen:  0,
		},
		{
			name: "Drain more than buffer size",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				return b
			},
			drainAmount:  10,
			expectedData: nil,
			expectedLen:  0,
		},
		{
			name: "Drain zero bytes",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				return b
			},
			drainAmount:  0,
			expectedData: []byte("hello"),
			expectedLen:  5,
		},
		{
			name: "Drain across multiple slices",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				b.Append([]byte(" "))
				b.Append([]byte("world"))
				return b
			},
			drainAmount:  7,
			expectedData: []byte("orld"),
			expectedLen:  4,
		},
		{
			name: "Drain with binary data",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05})
				return b
			},
			drainAmount:  3,
			expectedData: []byte{0x03, 0x04, 0x05},
			expectedLen:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.setup(defaultBufferSize)
			b.Drain(tt.drainAmount)

			got := b.Copy()
			assert.Equal(t, tt.expectedData, got, "LinkedBuffer content after Drain() should match expected")
			assert.Equal(t, tt.expectedLen, b.Length(), "LinkedBuffer.Length() should match expected length after Drain()")

			var slicesContent []byte
			b.Slices(func(view []byte) {
				slicesContent = append(slicesContent, view...)
			})
			assert.Equal(t, tt.expectedData, slicesContent, "Content from Slices() should match expected after Drain()")
		})
	}
}

func TestLinkedBufferDrainConcurrency(t *testing.T) {
	initialData := make([]byte, 10000)
	for i := range initialData {
		initialData[i] = byte(i % 256)
	}

	b := NewLinkedBuffer(defaultBufferSize)
	b.Append(initialData)

	concurrency := 100
	opsPerGoroutine := 10
	drainAmount := 10

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for range concurrency {
		go func() {
			defer wg.Done()
			for range opsPerGoroutine {
				b.Drain(drainAmount)
			}
		}()
	}

	wg.Wait()

	expectedLength := max(len(initialData)-(concurrency*opsPerGoroutine*drainAmount), 0)

	assert.Equal(t, expectedLength, b.Length(), "After concurrent operations, LinkedBuffer.Length() should return the expected length")

	content := b.Copy()
	assert.Len(t, content, expectedLength, "Length of copied content should match expected length after concurrent Drain() operations")
	require.Nil(t, content, "Content should be nil")

	if expectedLength > 0 {
		expectedStart := (concurrency * opsPerGoroutine * drainAmount) % 256
		if len(content) == 0 {
			t.Fatalf("Content should not be nil")
		}
		assert.Equal(t, byte(expectedStart), content[0], "First byte of remaining content should match expected value")
	}
}

func TestLinkedBufferReplace(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(int, ...[]byte) *LinkedBuffer
		replaceData []byte
		expected    []byte
	}{
		{
			name:        "Replace in empty buffer",
			setup:       NewLinkedBuffer,
			replaceData: []byte("new content"),
			expected:    []byte("new content"),
		},
		{
			name: "Replace in non-empty buffer",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("initial content"))
				return b
			},
			replaceData: []byte("replaced content"),
			expected:    []byte("replaced content"),
		},
		{
			name: "Replace with empty data",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("some content"))
				return b
			},
			replaceData: []byte{},
			expected:    nil,
		},
		{
			name: "Replace with nil data",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("some content"))
				return b
			},
			replaceData: nil,
			expected:    nil,
		},
		{
			name: "Replace with binary data",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("text content"))
				return b
			},
			replaceData: []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
			expected:    []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
		},
		{
			name: "Replace after multiple operations",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				b.Prepend([]byte("say "))
				b.Append([]byte(" world"))
				b.Drain(4)
				return b
			},
			replaceData: []byte("completely new"),
			expected:    []byte("completely new"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.setup(defaultBufferSize)
			b.Replace(tt.replaceData)

			got := b.Copy()
			if tt.expected == nil {
				assert.Nil(t, got, "LinkedBuffer.Copy() should return nil after replacing with empty or nil data")
			} else {
				assert.Equal(t, tt.expected, got, "LinkedBuffer content after Replace() should match expected")
			}

			assert.Equal(t, len(tt.expected), b.Length(), "LinkedBuffer.Length() should match expected length after Replace()")

			var slicesContent []byte
			b.Slices(func(view []byte) {
				slicesContent = append(slicesContent, view...)
			})
			assert.Equal(t, tt.expected, slicesContent, "Content from Slices() should match expected after Replace()")
		})
	}
}

func TestLinkedBufferReplaceConcurrency(t *testing.T) {
	b := NewLinkedBuffer(defaultBufferSize)
	concurrency := 100
	replaceData := []byte("concurrent replace")

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for range concurrency {
		go func() {
			defer wg.Done()
			b.Replace(replaceData)
		}()
	}

	wg.Wait()

	assert.Equal(t, len(replaceData), b.Length(), "After concurrent operations, LinkedBuffer.Length() should match the length of replaceData")

	content := b.Copy()
	assert.Equal(t, replaceData, content, "Content after concurrent Replace() operations should match replaceData")
}

func TestLinkedBufferReadAt(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(int, ...[]byte) *LinkedBuffer
		offset      int64
		readLen     int
		expected    []byte
		expectedErr error
	}{
		{
			name: "Read from start of buffer",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello world"))
				return b
			},
			offset:      0,
			readLen:     5,
			expected:    []byte("hello"),
			expectedErr: nil,
		},
		{
			name: "Read from middle of buffer",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello world"))
				return b
			},
			offset:      6,
			readLen:     5,
			expected:    []byte("world"),
			expectedErr: nil,
		},
		{
			name: "Read past end of buffer",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				return b
			},
			offset:      5,
			readLen:     5,
			expected:    []byte{},
			expectedErr: io.EOF,
		},
		{
			name: "Read with negative offset",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				return b
			},
			offset:      -1,
			readLen:     5,
			expected:    []byte{},
			expectedErr: ErrNegativeOffset,
		},
		{
			name: "Read across multiple slices",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				b.Append([]byte(" "))
				b.Append([]byte("world"))
				return b
			},
			offset:      3,
			readLen:     7,
			expected:    []byte("lo worl"),
			expectedErr: nil,
		},
		{
			name: "Read more than available",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				return b
			},
			offset:      0,
			readLen:     10,
			expected:    []byte("hello"),
			expectedErr: io.EOF,
		},
		{
			name:        "Read from empty buffer",
			setup:       NewLinkedBuffer,
			offset:      0,
			readLen:     5,
			expected:    []byte{},
			expectedErr: io.EOF,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.setup(defaultBufferSize)
			buf := make([]byte, tt.readLen)
			n, err := b.ReadAt(buf, tt.offset)

			switch {
			case tt.expectedErr != nil:
				require.Error(t, err)
				assert.Equal(t, tt.expectedErr.Error(), err.Error())
			case n < tt.readLen:
				require.ErrorIs(t, err, io.EOF)
			default:
				require.NoError(t, err)
			}

			assert.Equal(t, len(tt.expected), n, "Number of bytes read should match expected")
			assert.Equal(t, tt.expected, buf[:n], "Read content should match expected")
		})
	}
}

func TestLinkedBufferReadAtConcurrency(t *testing.T) {
	b := NewLinkedBuffer(defaultBufferSize)
	data := []byte("abcdefghijklmnopqrstuvwxyz")
	b.Append(data)

	concurrency := 100
	reads := 1000

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for range concurrency {
		go func() {
			defer wg.Done()
			buf := make([]byte, 1)
			for j := range reads {
				offset := int64(j % len(data))
				n, err := b.ReadAt(buf, offset)
				assert.NoError(t, err)
				assert.Equal(t, 1, n)
				assert.Equal(t, data[offset], buf[0])
			}
		}()
	}

	wg.Wait()
}

func TestBufferReaderRead(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(int, ...[]byte) *LinkedBuffer
		reads       []int
		expected    [][]byte
		expectedErr []error
	}{
		{
			name: "Read entire buffer at once",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello world"))
				return b
			},
			reads:       []int{11},
			expected:    [][]byte{[]byte("hello world")},
			expectedErr: []error{nil},
		},
		{
			name: "Read buffer in parts",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello world"))
				return b
			},
			reads:       []int{5, 6},
			expected:    [][]byte{[]byte("hello"), []byte(" world")},
			expectedErr: []error{nil, nil},
		},
		{
			name: "Read more than available",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				return b
			},
			reads:       []int{10},
			expected:    [][]byte{[]byte("hello")},
			expectedErr: []error{io.EOF},
		},
		{
			name:        "Read from empty buffer",
			setup:       NewLinkedBuffer,
			reads:       []int{5},
			expected:    [][]byte{{}},
			expectedErr: []error{io.EOF},
		},
		{
			name: "Read across multiple slices",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello"))
				b.Append([]byte(" "))
				b.Append([]byte("world"))
				return b
			},
			reads:       []int{7, 4},
			expected:    [][]byte{[]byte("hello w"), []byte("orld")},
			expectedErr: []error{nil, nil},
		},
		{
			name: "Multiple reads until EOF",
			setup: func(size int, _ ...[]byte) *LinkedBuffer {
				b := NewLinkedBuffer(size)
				b.Append([]byte("hello world"))
				return b
			},
			reads:       []int{5, 5, 5},
			expected:    [][]byte{[]byte("hello"), []byte(" worl"), []byte("d")},
			expectedErr: []error{nil, nil, io.EOF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.setup(defaultBufferSize)
			reader := b.NewReader()

			for i, readSize := range tt.reads {
				buf := make([]byte, readSize)
				n, err := reader.Read(buf)

				assert.Equal(t, tt.expectedErr[i], err, "Error should match expected for read %d", i+1)
				assert.Equal(t, tt.expected[i], buf[:n], "Read content should match expected for read %d", i+1)
			}
		})
	}
}

func TestBufferReaderReadConcurrency(t *testing.T) {
	b := NewLinkedBuffer(defaultBufferSize)
	data := []byte("abcdefghijklmnopqrstuvwxyz")
	b.Append(data)

	concurrency := 100
	readsPerGoroutine := 26 // One read per character in data

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for range concurrency {
		go func() {
			defer wg.Done()
			reader := b.NewReader()
			buf := make([]byte, 1)
			for j := range readsPerGoroutine {
				n, err := reader.Read(buf)
				assert.NoError(t, err)
				assert.Equal(t, 1, n)
				assert.Equal(t, data[j], buf[0])
			}
			// After reading all data, the next read should return EOF
			_, err := reader.Read(buf)
			assert.Equal(t, io.EOF, err)
		}()
	}

	wg.Wait()
}
