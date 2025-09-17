package http1

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRequestLine(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *Request
	}{
		{
			name:  "valid GET request",
			input: "GET /path HTTP/1.1",
			expected: &Request{
				Method:     "GET",
				RequestURI: "/path",
				Proto:      "HTTP/1.1",
			},
		},
		{
			name:  "valid POST request",
			input: "POST /api/users HTTP/1.1",
			expected: &Request{
				Method:     "POST",
				RequestURI: "/api/users",
				Proto:      "HTTP/1.1",
			},
		},
		{
			name:  "valid PUT request with query params",
			input: "PUT /users/123?active=true HTTP/1.1",
			expected: &Request{
				Method:     "PUT",
				RequestURI: "/users/123?active=true",
				Proto:      "HTTP/1.1",
			},
		},
		{
			name:  "valid DELETE request",
			input: "DELETE /resource/456 HTTP/1.0",
			expected: &Request{
				Method:     "DELETE",
				RequestURI: "/resource/456",
				Proto:      "HTTP/1.0",
			},
		},
		{
			name:  "valid PATCH request",
			input: "PATCH /api/data HTTP/1.1",
			expected: &Request{
				Method:     "PATCH",
				RequestURI: "/api/data",
				Proto:      "HTTP/1.1",
			},
		},
		{
			name:     "empty string",
			input:    "",
			expected: &Request{},
		},
		{
			name:     "single field",
			input:    "GET",
			expected: &Request{},
		},
		{
			name:     "two fields",
			input:    "GET /path",
			expected: &Request{},
		},
		{
			name:     "too many fields",
			input:    "GET /path HTTP/1.1 extra",
			expected: &Request{},
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: &Request{},
		},
		{
			name:  "extra whitespace between fields",
			input: "GET    /path    HTTP/1.1",
			expected: &Request{
				Method:     "GET",
				RequestURI: "/path",
				Proto:      "HTTP/1.1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRequestLine(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseStatusLine(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *Response
	}{
		{
			name:  "200 OK with reason phrase",
			input: "HTTP/1.1 200 OK",
			expected: &Response{
				Proto:      "HTTP/1.1",
				StatusCode: 200,
				Status:     "OK",
			},
		},
		{
			name:  "404 Not Found",
			input: "HTTP/1.1 404 Not Found",
			expected: &Response{
				Proto:      "HTTP/1.1",
				StatusCode: 404,
				Status:     "Not Found",
			},
		},
		{
			name:  "500 Internal Server Error",
			input: "HTTP/1.1 500 Internal Server Error",
			expected: &Response{
				Proto:      "HTTP/1.1",
				StatusCode: 500,
				Status:     "Internal Server Error",
			},
		},
		{
			name:  "201 Created",
			input: "HTTP/1.0 201 Created",
			expected: &Response{
				Proto:      "HTTP/1.0",
				StatusCode: 201,
				Status:     "Created",
			},
		},
		{
			name:  "status without reason phrase",
			input: "HTTP/1.1 204",
			expected: &Response{
				Proto:      "HTTP/1.1",
				StatusCode: 204,
				Status:     "",
			},
		},
		{
			name:  "multi-word reason phrase",
			input: "HTTP/1.1 422 Unprocessable Entity",
			expected: &Response{
				Proto:      "HTTP/1.1",
				StatusCode: 422,
				Status:     "Unprocessable Entity",
			},
		},
		{
			name:     "empty string",
			input:    "",
			expected: &Response{},
		},
		{
			name:     "single field",
			input:    "HTTP/1.1",
			expected: &Response{},
		},
		{
			name:  "non-numeric status code",
			input: "HTTP/1.1 abc Not a Number",
			expected: &Response{
				Proto:      "HTTP/1.1",
				StatusCode: 0,
				Status:     "Not a Number",
			},
		},
		{
			name:  "negative status code",
			input: "HTTP/1.1 -1 Negative",
			expected: &Response{
				Proto:      "HTTP/1.1",
				StatusCode: -1,
				Status:     "Negative",
			},
		},
		{
			name:  "reason phrase with extra spaces",
			input: "HTTP/1.1 200 OK with extra spaces",
			expected: &Response{
				Proto:      "HTTP/1.1",
				StatusCode: 200,
				Status:     "OK with extra spaces",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseStatusLine(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReadHeaders(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedErr bool
		expectData  bool
	}{
		{
			name:       "simple headers",
			input:      "GET /path HTTP/1.1\r\nHost: example.com\r\nUser-Agent: test\r\n\r\n",
			expectData: true,
		},
		{
			name:       "headers with colon in value",
			input:      "GET /path HTTP/1.1\r\nHost: example.com:8080\r\nAuthorization: Bearer token:with:colons\r\n\r\n",
			expectData: true,
		},
		{
			name:       "empty headers",
			input:      "GET /path HTTP/1.1\r\n\r\n",
			expectData: true,
		},
		{
			name:        "just boundary",
			input:       "\r\n\r\n",
			expectedErr: true,
		},
		{
			name:       "multiple header values",
			input:      "POST /api HTTP/1.1\r\nContent-Type: application/json\r\nContent-Length: 123\r\nAccept: application/json\r\nAccept: text/plain\r\n\r\n",
			expectData: true,
		},
		{
			name:        "no boundary - EOF",
			input:       "GET /path HTTP/1.1\r\nHost: example.com",
			expectedErr: true,
		},
		{
			name:        "incomplete boundary",
			input:       "GET /path HTTP/1.1\r\nHost: example.com\r\n\r",
			expectedErr: true,
		},
		{
			name:       "boundary with extra data after",
			input:      "GET /path HTTP/1.1\r\nHost: example.com\r\n\r\nbody data here",
			expectData: true,
		},
		{
			name:       "LF-only line terminators",
			input:      "GET /path HTTP/1.1\nHost: example.com\nUser-Agent: test\n\n",
			expectData: true,
		},
		{
			name:       "Ignore preceeding empty lines",
			input:      "\r\n\r\n\r\nGET /path HTTP/1.1\r\nHost: example.com\r\n\r\n",
			expectData: true,
		},
		{
			name:        "Max Header Size",
			input:       "GET /path HTTP/1.1\r\nHost: example.com\r\n" + strings.Repeat("A", 32<<10) + "\r\n\r\n",
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			result, err := readHeaders(reader)

			if tt.expectedErr {
				require.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				if tt.expectData {
					assert.NotNil(t, result)
					assert.NotEmpty(t, result.requestLine)
				} else {
					assert.NotNil(t, result)
				}
			}
		})
	}
}

func TestReadHeadersDoesNotOverRead(t *testing.T) {
	// Test that readHeaders stops exactly at the boundary and doesn't consume body data
	input := "GET /path HTTP/1.1\r\nHost: example.com\r\n\r\nbody data should remain"
	reader := strings.NewReader(input)

	_, err := readHeaders(reader)
	require.NoError(t, err)

	// Check that the body data is still available in the reader
	remaining := make([]byte, 100)
	n, _ := reader.Read(remaining)
	assert.Equal(t, "body data should remain", string(remaining[:n]))
}

func TestParseHeaderBytes(t *testing.T) {
	tests := []struct {
		name         string
		input        []byte
		expectedReq  string
		expectedHdrs map[string]string
		expectedErr  bool
	}{
		{
			name:        "valid request headers",
			input:       []byte("GET /path HTTP/1.1\r\nHost: example.com\r\nUser-Agent: test-client\r\n\r\n"),
			expectedReq: "GET /path HTTP/1.1",
			expectedHdrs: map[string]string{
				"Host":       "example.com",
				"User-Agent": "test-client",
			},
		},
		{
			name:        "response status line",
			input:       []byte("HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: 123\r\n\r\n"),
			expectedReq: "HTTP/1.1 200 OK",
			expectedHdrs: map[string]string{
				"Content-Type":   "text/html",
				"Content-Length": "123",
			},
		},
		{
			name:         "no headers",
			input:        []byte("GET /path HTTP/1.1\r\n\r\n"),
			expectedReq:  "GET /path HTTP/1.1",
			expectedHdrs: map[string]string{},
		},
		{
			name:        "header with colon in value",
			input:       []byte("GET /path HTTP/1.1\r\nAuthorization: Bearer token:with:colons\r\n\r\n"),
			expectedReq: "GET /path HTTP/1.1",
			expectedHdrs: map[string]string{
				"Authorization": "Bearer token:with:colons",
			},
		},
		{
			name:        "multiple values for same header",
			input:       []byte("GET /path HTTP/1.1\r\nAccept: text/html\r\nAccept: application/json\r\n\r\n"),
			expectedReq: "GET /path HTTP/1.1",
			expectedHdrs: map[string]string{
				"Accept": "text/html",
			},
		},
		{
			name:        "header names case insensitive",
			input:       []byte("GET /path HTTP/1.1\r\ncontent-type: application/json\r\nCONTENT-LENGTH: 123\r\n\r\n"),
			expectedReq: "GET /path HTTP/1.1",
			expectedHdrs: map[string]string{
				"Content-Type":   "application/json",
				"Content-Length": "123",
			},
		},
		{
			name:        "empty input",
			input:       []byte(""),
			expectedErr: true,
		},
		{
			name:        "just newline",
			input:       []byte("\r\n"),
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseHeaderBytes(tt.input)

			if tt.expectedErr {
				require.Error(t, err)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)

				assert.Equal(t, tt.expectedReq, result.requestLine)

				// Check expected headers exist and have correct values
				for key, expectedValue := range tt.expectedHdrs {
					actualValue := result.headers.Get(key)
					assert.Equal(t, expectedValue, actualValue, "Header %s mismatch", key)
				}

				// Check no unexpected headers exist
				for key := range result.headers {
					_, expected := tt.expectedHdrs[key]
					assert.True(t, expected, "Unexpected header: %s", key)
				}
			}
		})
	}
}

func TestReadHeadersWithDifferentReaders(t *testing.T) {
	headerData := "GET /path HTTP/1.1\r\nHost: example.com\r\nUser-Agent: test\r\n\r\n"

	tests := []struct {
		name   string
		reader io.Reader
	}{
		{
			name:   "string reader",
			reader: strings.NewReader(headerData),
		},
		{
			name:   "bytes reader",
			reader: bytes.NewReader([]byte(headerData)),
		},
		{
			name:   "bytes buffer",
			reader: bytes.NewBufferString(headerData),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := readHeaders(tt.reader)
			require.NoError(t, err)
			require.NotNil(t, result)

			assert.Equal(t, "GET /path HTTP/1.1", result.requestLine)
			assert.Equal(t, "example.com", result.headers.Get("Host"))
			assert.Equal(t, "test", result.headers.Get("User-Agent"))
		})
	}
}

func TestHeaderDataStruct(t *testing.T) {
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	headers.Set("Host", "example.com")

	hd := &headerData{
		requestLine: "POST /api HTTP/1.1",
		headers:     headers,
	}

	assert.Equal(t, "POST /api HTTP/1.1", hd.requestLine)
	assert.Equal(t, "application/json", hd.headers.Get("Content-Type"))
	assert.Equal(t, "example.com", hd.headers.Get("Host"))
}
