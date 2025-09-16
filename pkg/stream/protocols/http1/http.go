package http1

import (
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Request represents a parsed HTTP request
type Request struct {
	Method     string
	RequestURI string
	Proto      string
	Host       string
	URL        *url.URL
	Headers    http.Header
}

func ReadRequest(r io.Reader) (*Request, error) {
	// First, read and parse headers
	headers, err := readHeaders(r)
	if err != nil {
		return nil, err
	}

	// Build the request object
	req := parseRequestLine(headers.requestLine)
	req.Headers = headers.headers

	// RFC 9112 Section 3.2.1
	// CONNECT requests are used two different ways, and neither uses a full URL:
	// The standard use is to tunnel HTTPS through an HTTP proxy.
	// It looks like "CONNECT www.google.com:443 HTTP/1.1", and the parameter is
	// just the authority section of a URL. This information should go in req.URL.Host.
	justAuthority := req.Method == http.MethodConnect && !strings.HasPrefix(req.RequestURI, "/")
	if justAuthority {
		req.RequestURI = "http://" + req.RequestURI
	}

	if req.URL, err = url.ParseRequestURI(req.RequestURI); err != nil {
		return nil, err
	}

	if justAuthority {
		// Strip the bogus "http://" back off.
		req.URL.Scheme = ""
	}

	// RFC 9112, section 3.3: Must treat
	//	GET /index.html HTTP/1.1
	//	Host: www.google.com
	// and
	//	GET http://www.google.com/index.html HTTP/1.1
	//	Host: doesntmatter
	// the same. In the second case, any Host line is ignored.
	req.Host = req.URL.Host
	if req.Host == "" {
		req.Host = req.Headers.Get("Host")
	}

	return req, nil
}

// Response represents a parsed HTTP response
type Response struct {
	StatusCode int
	Status     string
	Proto      string
	Headers    http.Header
	IsInterim  bool // true for 1xx responses
}

func ReadResponse(r io.Reader) (*Response, error) {
	// Read and parse headers
	headers, err := readHeaders(r)
	if err != nil {
		return nil, err
	}

	// Build the resp object
	resp := parseStatusLine(headers.requestLine)
	resp.Headers = headers.headers

	return resp, nil
}
