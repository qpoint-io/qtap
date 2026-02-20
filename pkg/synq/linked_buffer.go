package synq

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

var (
	ErrNegativeOffset  = errors.New("negative offset")
	ErrPayloadTooLarge = errors.New("payload too large")
)

type slice struct {
	data []byte
	next *slice
}

type LinkedBuffer struct {
	bufferSize int
	head       *slice
	tail       *slice
	length     int
	mu         sync.RWMutex
}

func NewLinkedBuffer(bufferSize int, data ...[]byte) *LinkedBuffer {
	l := &LinkedBuffer{
		bufferSize: bufferSize,
	}

	for _, data := range data {
		l.Append(data)
	}

	return l
}

// Implements io.Writer that will not grow beyond a max size.
// The buffer will drain the oldest data if the max size is exceeded.
func (b *LinkedBuffer) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	if b.bufferSize < len(p) {
		return 0, fmt.Errorf("%w: %d > %d", ErrPayloadTooLarge, len(p), b.bufferSize)
	}

	if b.length+len(p) > b.bufferSize {
		b.Drain(b.length + len(p) - b.bufferSize)
	}

	b.Append(p)

	return len(p), nil
}

func (b *LinkedBuffer) ReadAt(p []byte, off int64) (n int, err error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if off < 0 {
		return 0, ErrNegativeOffset
	}
	if off >= int64(b.length) {
		return 0, io.EOF
	}

	remaining := int(off)
	current := b.head
	for remaining >= len(current.data) {
		remaining -= len(current.data)
		current = current.next
	}

	copied := 0
	for copied < len(p) && current != nil {
		toCopy := min(len(p)-copied, len(current.data)-remaining)
		copy(p[copied:], current.data[remaining:remaining+toCopy])
		copied += toCopy
		remaining = 0
		current = current.next
	}

	if copied < len(p) {
		err = io.EOF
	}
	return copied, err
}

func (b *LinkedBuffer) Length() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.length
}

func (b *LinkedBuffer) Slices(iter func(view []byte)) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for current := b.head; current != nil; current = current.next {
		iter(current.data)
	}
}

func (b *LinkedBuffer) Copy() []byte {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.length == 0 {
		return nil
	}

	result := make([]byte, b.length)
	offset := 0
	for current := b.head; current != nil; current = current.next {
		copy(result[offset:], current.data)
		offset += len(current.data)
	}
	return result
}

func (b *LinkedBuffer) Append(data []byte) {
	if len(data) == 0 {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	newSlice := &slice{data: data}

	if b.tail == nil {
		b.head = newSlice
		b.tail = newSlice
	} else {
		b.tail.next = newSlice
		b.tail = newSlice
	}

	b.length += len(data)
}

func (b *LinkedBuffer) Prepend(data []byte) {
	if len(data) == 0 {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	newSlice := &slice{data: data}

	newSlice.next = b.head
	b.head = newSlice
	if b.tail == nil {
		b.tail = newSlice
	}

	b.length += len(data)
}

func (b *LinkedBuffer) Drain(length int) {
	if length <= 0 {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for length > 0 && b.head != nil {
		if length >= len(b.head.data) {
			length -= len(b.head.data)
			b.length -= len(b.head.data)
			b.head = b.head.next
			if b.head == nil {
				b.tail = nil
			}
		} else {
			b.head.data = b.head.data[length:]
			b.length -= length
			length = 0
		}
	}
}

func (b *LinkedBuffer) Replace(data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	newSlice := &slice{data: data}

	b.head = newSlice
	b.tail = newSlice
	b.length = len(data)
}

func (b *LinkedBuffer) NewReader() io.Reader {
	return &bufferReader{buffer: b}
}

type bufferReader struct {
	buffer *LinkedBuffer
	offset int64
}

func (r *bufferReader) Read(p []byte) (n int, err error) {
	n, err = r.buffer.ReadAt(p, r.offset)
	r.offset += int64(n)
	return
}
