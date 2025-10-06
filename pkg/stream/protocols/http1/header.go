package http1

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
)

// MaxHeaderSize is the maximum size of a header in bytes
const MaxHeaderSize = 32 << 10 // 32 KiB

var (
	ErrIncompleteHeader = errors.New("incomplete header")
)

// headerData holds parsed header information
type headerData struct {
	requestLine string // First line (request or status line)
	headers     http.Header
}

// parseRequestLine parses the HTTP request line
func parseRequestLine(line string) *Request {
	parts := strings.Fields(line)
	if len(parts) != 3 {
		return &Request{}
	}

	return &Request{
		Method:     parts[0],
		RequestURI: parts[1],
		Proto:      parts[2],
	}
}

// parseStatusLine parses the HTTP status line
func parseStatusLine(line string) *Response {
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		return &Response{}
	}

	statusCode, _ := strconv.Atoi(parts[1])

	resp := &Response{
		Proto:      parts[0],
		StatusCode: statusCode,
	}

	if len(parts) >= 3 {
		resp.Status = parts[2]
	}

	return resp
}

// readHeaders reads headers from a reader until it finds the boundary
// Supports both CRLF (\r\n\r\n) and LF (\n\n) boundaries
// Limits the max header size to 32 KiB
func readHeaders(r io.Reader) (*headerData, error) {
	var headers bytes.Buffer
	var lastFour [4]byte
	var lastTwo [2]byte
	var b [1]byte
	var headerStarted bool

	// Limit the reader to the maximum header size
	lr := io.LimitReader(r, MaxHeaderSize)

	// Read byte by byte until we see \r\n\r\n or \n\n
	for {
		n, err := lr.Read(b[:])
		if err != nil {
			// If we've received some data, but not a complete header,
			// return a incomplete header error. If we've received no data,
			// return the error.
			if errors.Is(err, io.EOF) && headers.Len() > 0 {
				return nil, ErrIncompleteHeader
			}
			return nil, err
		}

		if n > 0 {
			// Skip leading empty lines (CRLF or LF) before headers start
			if !headerStarted {
				if b[0] == '\r' || b[0] == '\n' {
					continue
				}
				headerStarted = true
			}

			headers.WriteByte(b[0])

			// Shift sliding windows
			lastFour[0] = lastFour[1]
			lastFour[1] = lastFour[2]
			lastFour[2] = lastFour[3]
			lastFour[3] = b[0]

			lastTwo[0] = lastTwo[1]
			lastTwo[1] = b[0]

			// Check for CRLF boundary first (HTTP standard)
			if lastFour == [4]byte{'\r', '\n', '\r', '\n'} {
				return parseHeaderBytes(headers.Bytes())
			}

			// Check for LF-only boundary
			if lastTwo == [2]byte{'\n', '\n'} {
				return parseHeaderBytes(headers.Bytes())
			}
		}
	}
}

// parseHeaderBytes parses the accumulated header bytes
func parseHeaderBytes(data []byte) (*headerData, error) {
	// Create a reader for the header data
	reader := bufio.NewReader(bytes.NewReader(data))

	// Read the first line (request or status line)
	firstLine, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}

	// Now use textproto to parse the remaining headers
	// This is where we briefly use bufio.Reader, but only for parsing
	// the already-accumulated headers, not for reading from the stream
	tp := textproto.NewReader(reader)
	mimeHeaders, err := tp.ReadMIMEHeader()
	if err != nil && errors.Is(err, io.EOF) {
		return nil, err
	}

	return &headerData{
		requestLine: strings.TrimSpace(firstLine),
		headers:     http.Header(mimeHeaders),
	}, nil
}
