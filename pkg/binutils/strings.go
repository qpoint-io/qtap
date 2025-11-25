package binutils

import (
	"bytes"
	"fmt"
	"io"
	"unicode"
)

const minStrLength = 4

func (e *Elf) SearchString(searchStr string, strategy MatchStrategy) (string, error) {
	if e.isClosed.Load() {
		return "", ErrFileClosed
	}

	var buffer [bufferSize]byte
	var window [bufferSize * 2]byte
	windowLen := 0
	offset := int64(0)

	for {
		n, err := e.file.ReadAt(buffer[:], offset)
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("error reading file: %w", err)
		}

		// Copy buffer to window
		copy(window[windowLen:], buffer[:n])
		windowLen += n

		for windowLen >= minStrLength {
			idx := bytes.IndexFunc(window[:windowLen], func(r rune) bool {
				return !unicode.IsPrint(r)
			})

			if idx == -1 {
				break
			}

			if idx >= minStrLength {
				str := string(window[:idx])
				if match(str, searchStr, strategy) {
					return str, nil
				}
			}

			// Shift window contents
			copy(window[:], window[idx+1:windowLen])
			windowLen -= idx + 1
		}

		if windowLen > bufferSize {
			copy(window[:], window[windowLen-bufferSize:windowLen])
			windowLen = bufferSize
		}

		offset += int64(n)

		if err == io.EOF {
			break
		}
	}

	// Check the last string in the window
	if windowLen >= minStrLength {
		str := string(window[:windowLen])
		if match(str, searchStr, strategy) {
			return str, nil
		}
	}

	return "", fmt.Errorf("no match found for '%s' using strategy %v", searchStr, strategy)
}
