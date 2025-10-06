package http1

import (
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
)

const buf8kSize int = 8 << 10

// buf8kPool is a pool of 8KiB buffers this is used to avoid allocating a new buffer
// for each http body read/write.
var buf8kPool = sync.Pool{New: func() any { return new([buf8kSize]byte) }}

// expectsRequestBody determines if a request should have a body
func expectsRequestBody(req *Request) bool {
	return expectsBody(req.Headers)
}

// expectsResponseBody determines if a response should have a body
func expectsResponseBody(reqMethod string, resp *Response) bool {
	// HEAD requests never have a response body
	if reqMethod == http.MethodHead {
		return false
	}

	// Certain status codes never have a body
	if resp.StatusCode == http.StatusNoContent ||
		resp.StatusCode == http.StatusNotModified {
		return false
	}

	if expectsBody(resp.Headers) {
		return true
	}

	// Servers are allowed to withhold content-length or transfer-encoding
	// and just keep sending data until the connection is closed.
	// RFC 9112 Section 6.3
	return strings.EqualFold(resp.Headers.Get("Connection"), "close")
}

// expectsBody determines if a request or response should have a body
// based on the presence of Transfer-Encoding or Content-Length
func expectsBody(headers http.Header) bool {
	// Check for Transfer-Encoding
	if te := headers.Get("Transfer-Encoding"); te != "" {
		return true
	}

	// Check for Content-Length
	if cl := headers.Get("Content-Length"); cl != "" {
		length, _ := strconv.ParseInt(cl, 10, 64)
		return length > 0
	}

	return false
}

// setupTransferEncodingReader applies transfer encoding (chunked or content-length)
func setupTransferEncodingReader(headers http.Header, baseReader io.Reader) io.Reader {
	// Handle Transfer-Encoding (transmission layer)
	if te := headers.Get("Transfer-Encoding"); te != "" {
		if strings.Contains(strings.ToLower(te), "chunked") {
			return httputil.NewChunkedReader(baseReader)
		}
	} else if cl := headers.Get("Content-Length"); cl != "" {
		length, _ := strconv.ParseInt(cl, 10, 64)
		return io.LimitReader(baseReader, length)
	}

	return baseReader
}

// setupContentEncodingReader applies content encoding (gzip, deflate, br)
func setupContentEncodingReader(headers http.Header, baseReader io.Reader, onError ErrorCallback) io.Reader {
	ce := headers.Get("Content-Encoding")
	if ce == "" {
		return baseReader
	}

	// Split by comma and process in REVERSE order
	encodings := strings.Split(ce, ",")
	bodyReader := baseReader

	// Process from last to first (reverse order)
	for i := len(encodings) - 1; i >= 0; i-- {
		encoding := strings.TrimSpace(strings.ToLower(encodings[i]))

		switch encoding {
		case "gzip":
			gzReader, err := gzip.NewReader(bodyReader)
			if err != nil {
				// Log error but continue with what we have
				if onError != nil {
					onError(fmt.Errorf("gzip init failed: %w", err))
				}
				continue
			}
			bodyReader = gzReader

		case "deflate":
			bodyReader = flate.NewReader(bodyReader)

		case "br":
			bodyReader = brotli.NewReader(bodyReader)

		case "identity":
			continue

		default:
			// Unknown encoding - log and continue
			if onError != nil {
				onError(fmt.Errorf("unknown encoding: %s", encoding))
			}
		}
	}

	return bodyReader
}

// streamBody reads from the body reader and streams chunks via callback
func streamBody(bodyReader io.Reader, onBodyChunk BodyCallback, onError ErrorCallback) {
	if onBodyChunk == nil {
		// If no callback, just drain the body
		_, _ = io.Copy(io.Discard, bodyReader)

		return
	}

	// Read in chunks and stream to callback
	buf := buf8kPool.Get().(*[buf8kSize]byte) // 8KiB chunks
	defer buf8kPool.Put(buf)

	for {
		n, err := bodyReader.Read(buf[:])

		if n > 0 {
			// We got data, send it
			chunk := make([]byte, n)
			copy(chunk, buf[:n])

			isComplete := err == io.EOF
			onBodyChunk(chunk, isComplete)
		}

		if err == io.EOF {
			// End of body
			if n == 0 {
				// Send final completion signal if we didn't just send data
				onBodyChunk(nil, true)
			}
			break
		}

		if err != nil {
			// Handle error
			if onError != nil && !errors.Is(err, io.EOF) {
				onError(fmt.Errorf("body read error: %w", err))
			}
			break
		}
	}
}
