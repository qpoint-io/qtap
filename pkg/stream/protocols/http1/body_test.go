package http1

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpectsRequestBody(t *testing.T) {
	tests := []struct {
		name     string
		request  *Request
		expected bool
	}{
		{
			name: "POST with Content-Length",
			request: &Request{
				Method: "POST",
				Headers: http.Header{
					"Content-Length": []string{"100"},
				},
			},
			expected: true,
		},
		{
			name: "POST with Transfer-Encoding chunked",
			request: &Request{
				Method: "POST",
				Headers: http.Header{
					"Transfer-Encoding": []string{"chunked"},
				},
			},
			expected: true,
		},
		{
			name: "GET with no body headers",
			request: &Request{
				Method:  "GET",
				Headers: http.Header{},
			},
			expected: false,
		},
		{
			name: "POST with Content-Length 0",
			request: &Request{
				Method: "POST",
				Headers: http.Header{
					"Content-Length": []string{"0"},
				},
			},
			expected: false,
		},
		{
			name: "PUT with Content-Length",
			request: &Request{
				Method: "PUT",
				Headers: http.Header{
					"Content-Length": []string{"50"},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expectsRequestBody(tt.request)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExpectsResponseBody(t *testing.T) {
	tests := []struct {
		name      string
		reqMethod string
		response  *Response
		expected  bool
	}{
		{
			name:      "HEAD request - never has body",
			reqMethod: "HEAD",
			response: &Response{
				StatusCode: 200,
				Headers: http.Header{
					"Content-Length": []string{"100"},
				},
			},
			expected: false,
		},
		{
			name:      "GET with 204 No Content",
			reqMethod: "GET",
			response: &Response{
				StatusCode: 204,
				Headers:    http.Header{},
			},
			expected: false,
		},
		{
			name:      "GET with 304 Not Modified",
			reqMethod: "GET",
			response: &Response{
				StatusCode: 304,
				Headers:    http.Header{},
			},
			expected: false,
		},
		{
			name:      "GET with 200 and Content-Length",
			reqMethod: "GET",
			response: &Response{
				StatusCode: 200,
				Headers: http.Header{
					"Content-Length": []string{"100"},
				},
			},
			expected: true,
		},
		{
			name:      "POST with 200 and Transfer-Encoding",
			reqMethod: "POST",
			response: &Response{
				StatusCode: 200,
				Headers: http.Header{
					"Transfer-Encoding": []string{"chunked"},
				},
			},
			expected: true,
		},
		{
			name:      "GET with 200 and no body headers",
			reqMethod: "GET",
			response: &Response{
				StatusCode: 200,
				Headers:    http.Header{},
			},
			expected: false,
		},
		{
			name:      "GET with 404 and Content-Length",
			reqMethod: "GET",
			response: &Response{
				StatusCode: 404,
				Headers: http.Header{
					"Content-Length": []string{"25"},
				},
			},
			expected: true,
		},
		{
			name:      "with Connection close and no Content-Length or Transfer-Encoding",
			reqMethod: "GET",
			response: &Response{
				StatusCode: 200,
				Headers: http.Header{
					"Connection": []string{"close"},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expectsResponseBody(tt.reqMethod, tt.response)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExpectsBody(t *testing.T) {
	tests := []struct {
		name     string
		headers  http.Header
		expected bool
	}{
		{
			name: "with Transfer-Encoding chunked",
			headers: http.Header{
				"Transfer-Encoding": []string{"chunked"},
			},
			expected: true,
		},
		{
			name: "with Transfer-Encoding gzip",
			headers: http.Header{
				"Transfer-Encoding": []string{"gzip"},
			},
			expected: true,
		},
		{
			name: "with Content-Length > 0",
			headers: http.Header{
				"Content-Length": []string{"100"},
			},
			expected: true,
		},
		{
			name: "with Content-Length 0",
			headers: http.Header{
				"Content-Length": []string{"0"},
			},
			expected: false,
		},
		{
			name: "with negative Content-Length",
			headers: http.Header{
				"Content-Length": []string{"-1"},
			},
			expected: false,
		},
		{
			name: "with invalid Content-Length",
			headers: http.Header{
				"Content-Length": []string{"abc"},
			},
			expected: false,
		},
		{
			name:     "with no body headers",
			headers:  http.Header{},
			expected: false,
		},
		{
			name: "with both Transfer-Encoding and Content-Length",
			headers: http.Header{
				"Transfer-Encoding": []string{"chunked"},
				"Content-Length":    []string{"100"},
			},
			expected: true,
		},
		{
			name: "with empty Transfer-Encoding",
			headers: http.Header{
				"Transfer-Encoding": []string{""},
			},
			expected: false,
		},
		{
			name: "with empty Content-Length",
			headers: http.Header{
				"Content-Length": []string{""},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expectsBody(tt.headers)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSetupTransferEncodingReader(t *testing.T) {
	baseData := "Hello, World!"

	tests := []struct {
		name     string
		headers  http.Header
		testFunc func(t *testing.T, reader io.Reader)
	}{
		{
			name: "chunked transfer encoding",
			headers: http.Header{
				"Transfer-Encoding": []string{"chunked"},
			},
			testFunc: func(t *testing.T, reader io.Reader) {
				// Should return a chunked reader - verify it's not the original reader
				originalReader := strings.NewReader(baseData)
				assert.NotEqual(t, originalReader, reader, "Should return a different reader for chunked")
			},
		},
		{
			name: "chunked with other encodings",
			headers: http.Header{
				"Transfer-Encoding": []string{"gzip, chunked"},
			},
			testFunc: func(t *testing.T, reader io.Reader) {
				// Should return a chunked reader - verify it's not the original reader
				originalReader := strings.NewReader(baseData)
				assert.NotEqual(t, originalReader, reader, "Should return a different reader for chunked")
			},
		},
		{
			name: "content-length",
			headers: http.Header{
				"Content-Length": []string{"5"},
			},
			testFunc: func(t *testing.T, reader io.Reader) {
				// Should return a LimitReader
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				assert.Equal(t, "Hello", string(data))
			},
		},
		{
			name: "content-length zero",
			headers: http.Header{
				"Content-Length": []string{"0"},
			},
			testFunc: func(t *testing.T, reader io.Reader) {
				// Should return a LimitReader with 0 bytes
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				assert.Empty(t, data)
			},
		},
		{
			name: "invalid content-length",
			headers: http.Header{
				"Content-Length": []string{"invalid"},
			},
			testFunc: func(t *testing.T, reader io.Reader) {
				// Should return a LimitReader with 0 bytes (invalid parses to 0)
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				assert.Empty(t, data)
			},
		},
		{
			name:    "no transfer encoding headers",
			headers: http.Header{},
			testFunc: func(t *testing.T, reader io.Reader) {
				// Should return original reader
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				assert.Equal(t, baseData, string(data))
			},
		},
		{
			name: "transfer-encoding without chunked",
			headers: http.Header{
				"Transfer-Encoding": []string{"gzip"},
			},
			testFunc: func(t *testing.T, reader io.Reader) {
				// Should return original reader since no chunked
				data, err := io.ReadAll(reader)
				require.NoError(t, err)
				assert.Equal(t, baseData, string(data))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset reader for each test
			baseReader := strings.NewReader(baseData)
			reader := setupTransferEncodingReader(tt.headers, baseReader)
			tt.testFunc(t, reader)
		})
	}
}

func TestSetupContentEncodingReader(t *testing.T) {
	// Helper to create gzipped data
	createGzipData := func(data string) []byte {
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		_, err := gw.Write([]byte(data))
		require.NoError(t, err)
		gw.Close()
		return buf.Bytes()
	}

	// Helper to create deflate data
	createDeflateData := func(data string) []byte {
		var buf bytes.Buffer
		fw, _ := flate.NewWriter(&buf, flate.DefaultCompression)
		_, err := fw.Write([]byte(data))
		require.NoError(t, err)
		fw.Close()
		return buf.Bytes()
	}

	// Helper to create brotli data
	createBrotliData := func(data string) []byte {
		var buf bytes.Buffer
		bw := brotli.NewWriter(&buf)
		_, err := bw.Write([]byte(data))
		require.NoError(t, err)
		bw.Close()
		return buf.Bytes()
	}

	originalData := "Hello, World!"

	tests := []struct {
		name         string
		headers      http.Header
		inputData    []byte
		expectedData string
		expectError  bool
	}{
		{
			name:         "no content encoding",
			headers:      http.Header{},
			inputData:    []byte(originalData),
			expectedData: originalData,
		},
		{
			name: "gzip encoding",
			headers: http.Header{
				"Content-Encoding": []string{"gzip"},
			},
			inputData:    createGzipData(originalData),
			expectedData: originalData,
		},
		{
			name: "deflate encoding",
			headers: http.Header{
				"Content-Encoding": []string{"deflate"},
			},
			inputData:    createDeflateData(originalData),
			expectedData: originalData,
		},
		{
			name: "brotli encoding",
			headers: http.Header{
				"Content-Encoding": []string{"br"},
			},
			inputData:    createBrotliData(originalData),
			expectedData: originalData,
		},
		{
			name: "identity encoding",
			headers: http.Header{
				"Content-Encoding": []string{"identity"},
			},
			inputData:    []byte(originalData),
			expectedData: originalData,
		},
		{
			name: "multiple encodings",
			headers: http.Header{
				"Content-Encoding": []string{"gzip, deflate"},
			},
			inputData:    createDeflateData(string(createGzipData(originalData))),
			expectedData: originalData,
		},
		{
			name: "unknown encoding",
			headers: http.Header{
				"Content-Encoding": []string{"unknown"},
			},
			inputData:    []byte(originalData),
			expectedData: originalData,
			expectError:  true,
		},
		{
			name: "invalid gzip data",
			headers: http.Header{
				"Content-Encoding": []string{"gzip"},
			},
			inputData:   []byte("invalid gzip data"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedErrors []error
			errorCallback := func(err error) {
				capturedErrors = append(capturedErrors, err)
			}

			baseReader := bytes.NewReader(tt.inputData)
			reader := setupContentEncodingReader(tt.headers, baseReader, errorCallback)

			data, err := io.ReadAll(reader)

			if tt.expectError {
				assert.NotEmpty(t, capturedErrors, "Expected error to be captured")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedData, string(data))
				assert.Empty(t, capturedErrors, "No errors should be captured")
			}
		})
	}
}

func TestSetupContentEncodingReaderWithoutErrorCallback(t *testing.T) {
	headers := http.Header{
		"Content-Encoding": []string{"unknown"},
	}
	baseReader := strings.NewReader("test data")

	// Should not panic when onError is nil
	reader := setupContentEncodingReader(headers, baseReader, nil)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, "test data", string(data))
}

func TestStreamBody(t *testing.T) {
	t.Run("stream with callback", func(t *testing.T) {
		data := "Hello, World! This is a test message that's longer than 8 bytes."
		reader := strings.NewReader(data)

		var chunks [][]byte
		var completions []bool
		callback := func(chunk []byte, isComplete bool) {
			if chunk != nil {
				chunks = append(chunks, chunk)
			}
			completions = append(completions, isComplete)
		}

		streamBody(reader, callback, nil)

		// Verify we got data
		assert.NotEmpty(t, chunks)

		// Verify final completion signal
		assert.True(t, completions[len(completions)-1], "Last call should be complete")

		// Reconstruct data
		var reconstructed []byte
		for _, chunk := range chunks {
			reconstructed = append(reconstructed, chunk...)
		}
		assert.Equal(t, data, string(reconstructed))
	})

	t.Run("stream without callback - should drain", func(t *testing.T) {
		data := "This data should be drained"
		reader := strings.NewReader(data)

		// Should not panic and should complete
		streamBody(reader, nil, nil)

		// Reader should be exhausted
		remaining, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Empty(t, remaining)
	})

	t.Run("empty reader", func(t *testing.T) {
		reader := strings.NewReader("")

		var called bool
		var lastComplete bool
		callback := func(chunk []byte, isComplete bool) {
			called = true
			lastComplete = isComplete
		}

		streamBody(reader, callback, nil)

		assert.True(t, called, "Callback should be called")
		assert.True(t, lastComplete, "Should signal completion")
	})

	t.Run("reader with error", func(t *testing.T) {
		// Create a reader that will error
		reader := &errorReader{data: []byte("test"), errorAfter: 2}

		var chunks [][]byte
		var errors []error
		bodyCallback := func(chunk []byte, isComplete bool) {
			if chunk != nil {
				chunks = append(chunks, chunk)
			}
		}
		errorCallback := func(err error) {
			errors = append(errors, err)
		}

		streamBody(reader, bodyCallback, errorCallback)

		// Should have gotten some data before error
		assert.NotEmpty(t, chunks)
		assert.NotEmpty(t, errors)
	})

	t.Run("large data in chunks", func(t *testing.T) {
		// Create data larger than 8KB buffer
		largeData := strings.Repeat("a", 10000)
		reader := strings.NewReader(largeData)

		var chunks [][]byte
		callback := func(chunk []byte, isComplete bool) {
			if chunk != nil {
				// Verify chunks are reasonably sized (should be <= 8192)
				assert.LessOrEqual(t, len(chunk), 8192)
				chunks = append(chunks, chunk)
			}
		}

		streamBody(reader, callback, nil)

		// Verify we got multiple chunks
		assert.Greater(t, len(chunks), 1)

		// Reconstruct and verify data
		var reconstructed []byte
		for _, chunk := range chunks {
			reconstructed = append(reconstructed, chunk...)
		}
		assert.Equal(t, largeData, string(reconstructed))
	})
}

// errorReader is a helper for testing error conditions
type errorReader struct {
	data       []byte
	pos        int
	errorAfter int
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	if r.pos >= r.errorAfter {
		return 0, errors.New("simulated read error")
	}

	if r.pos >= len(r.data) {
		return 0, io.EOF
	}

	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func TestStreamBodyErrorHandling(t *testing.T) {
	t.Run("error callback is nil", func(t *testing.T) {
		reader := &errorReader{data: []byte("test"), errorAfter: 2}

		var chunks [][]byte
		bodyCallback := func(chunk []byte, isComplete bool) {
			if chunk != nil {
				chunks = append(chunks, chunk)
			}
		}

		// Should not panic when error callback is nil
		streamBody(reader, bodyCallback, nil)

		// Should still get some data before error
		assert.NotEmpty(t, chunks)
	})
}
