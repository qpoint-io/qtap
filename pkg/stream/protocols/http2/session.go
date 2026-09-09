package http2

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/telemetry"
	"github.com/rs/xid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"golang.org/x/net/http2/hpack"
)

var tracer = telemetry.Tracer()

type StreamState int

const (
	StreamStateIdle StreamState = iota
	StreamStateRequestHeaders
	StreamStateRequestBody
	StreamStateRequestDone
	StreamStateResponseHeaders
	StreamStateResponseBody
	StreamStateResponseDone
)

var ErrEncodedBody = errors.New("encoded body")

type Session struct {
	ctx   context.Context
	ID    uint32
	State StreamState

	// domain
	domain string

	// total bytes written
	wrBytes int64
	// total bytes read
	rdBytes int64

	// request/response
	req *http.Request
	res *http.Response

	// socket connection
	conn *connection.Connection

	// pluginConn connection
	pluginConn *plugins.Connection

	// plugin manager
	pluginManager *plugins.Manager

	// logger
	logger *zap.Logger

	// closed
	closed bool

	// gRPC support
	isGRPC bool
}

func NewSession(ctx context.Context, id uint32, domain string, logger *zap.Logger, conn *connection.Connection, pluginManager *plugins.Manager) *Session {
	ctx, span := tracer.Start(ctx, "Session")
	span.SetAttributes(attribute.String("session.type", "http2"))

	return &Session{
		ctx:           ctx,
		ID:            id,
		State:         StreamStateIdle,
		logger:        logger,
		conn:          conn,
		pluginManager: pluginManager,
	}
}

func (s *Session) CreateRequest(headers []hpack.HeaderField, endOfStream bool) error {
	s.req = &http.Request{
		Proto:      "HTTP/2.0",
		ProtoMajor: 2,
		ProtoMinor: 0,
		Header:     make(http.Header),
	}

	var method, scheme, host, path string
	var contentLength int64

	for _, hf := range headers {
		switch hf.Name {
		case ":method":
			method = hf.Value
		case ":scheme":
			scheme = hf.Value
		case ":authority":
			host = hf.Value
		case ":path":
			path = hf.Value
		case "content-length":
			if length, err := strconv.ParseInt(hf.Value, 10, 63); err == nil {
				contentLength = length
			}
			s.req.Header.Add(hf.Name, hf.Value)
		default:
			s.req.Header.Add(hf.Name, hf.Value)
		}
	}

	// set the Qpoint request ID
	id := xid.New().String()
	span := trace.SpanFromContext(s.ctx)
	span.SetAttributes(attribute.String("request.id", id))

	if scheme == "" {
		if s.conn.IsTLS {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	if !strings.HasSuffix(path, "/") && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	urlHost := host
	// Handle both IP addresses and domain names
	if ip := net.ParseIP(host); ip != nil {
		// It's an IP address
		if ip.To4() == nil {
			// It's an IPv6 address
			urlHost = "[" + host + "]"
		}
	}

	url, err := url.Parse(scheme + "://" + urlHost + path)
	if err != nil {
		return fmt.Errorf("error parsing URL (scheme: %s, host: %s, path: %s): %w", scheme, host, path, err)
	}
	s.req.URL = url
	s.req.Method = method
	s.req.Host = host
	s.req.RequestURI = path
	s.req.ContentLength = contentLength

	// if host is set and not the same as the domain, update the connection domain
	if s.req.Host != "" && s.req.Host != s.domain {
		s.conn.SetDomain(s.req.Host)
	}

	// create a plugin connection
	if s.pluginManager != nil {
		connType := plugins.ConnectionType_HTTP
		if s.isGRPC {
			connType = plugins.ConnectionType_GRPC
		}
		s.pluginConn, err = s.pluginManager.NewConnection(s.ctx, connType, s.conn, id)
		if err != nil {
			return fmt.Errorf("creating plugin connection: %w", err)
		}
	}

	if s.pluginConn != nil {
		// set the request
		s.pluginConn.SetRequest(s.req)

		// call the request headers callback
		if err := s.pluginConn.OnHttpRequestHeaders(endOfStream); err != nil {
			s.logger.Error("plugin request headers", zap.Error(err))
		}
	}

	return nil
}

func (s *Session) CreateResponse(headers []hpack.HeaderField, endOfStream bool) error {
	header := make(http.Header, len(headers))
	strs := make([]string, len(headers))

	// initialize a new http.Response
	s.res = &http.Response{
		Proto:      "HTTP/2.0",
		ProtoMajor: 2,
		ProtoMinor: 0,
		Header:     header,
	}

	for _, hf := range headers {
		key := http.CanonicalHeaderKey(hf.Name)
		vv := header[key]
		if vv == nil && len(strs) > 0 {
			// More than likely this will be a single-element key.
			// Most headers aren't multi-valued.
			// Set the capacity on strs[0] to 1, so any future append
			// won't extend the slice into the other strings.
			vv, strs = strs[:1:1], strs[1:]
			vv[0] = hf.Value
			header[key] = vv
		} else {
			header[key] = append(vv, hf.Value)
		}
	}

	var method string
	if m := s.req.Header.Get(":method"); m != "" {
		method = m
	}

	// custom handle content-length
	// if the content-length header has more than one value, we don't care
	// it won't effect framing so we ignore to avoid the possibility of
	// smuggling attacks.
	s.res.ContentLength = -1
	if clens := s.res.Header["Content-Length"]; len(clens) == 1 {
		// if this fails, we just ignore it as it won't effect framing
		// and avoids smuggling attacks.
		if cl, err := strconv.ParseUint(clens[0], 10, 63); err == nil {
			s.res.ContentLength = int64(cl)
		}
	} else if endOfStream && !strings.EqualFold(method, "HEAD") {
		s.res.ContentLength = 0
	}

	// custom handle :status and status code
	if status := s.res.Header.Get(":status"); status != "" {
		if code, err := strconv.Atoi(status); err == nil {
			s.res.StatusCode = code
			s.res.Status = http.StatusText(s.res.StatusCode)
		}
	}

	// set the request for this response
	s.res.Request = s.req

	if s.pluginConn != nil {
		// set the response
		s.pluginConn.SetResponse(s.res)

		// call the response headers callback
		if err := s.pluginConn.OnHttpResponseHeaders(endOfStream); err != nil {
			s.logger.Error("plugin response headers", zap.Error(err))
		}
	}

	return nil
}

// HandleTrailers processes gRPC trailer headers, extracting grpc-status and grpc-message.
// It updates the response headers so that downstream plugins and reporters can see the
// gRPC status information.
func (s *Session) HandleTrailers(fields []hpack.HeaderField) {
	meta := extractGRPCTrailers(fields)

	s.logger.Debug("gRPC trailers received",
		zap.String("grpc-status", meta.Status),
		zap.String("grpc-status-name", meta.StatusName),
		zap.String("grpc-message", meta.Message),
	)

	// Set the gRPC trailer metadata on the response headers so plugins can access them.
	// The response object was already created via CreateResponse, so we add trailer info to it.
	if s.res != nil {
		if meta.Status != "" {
			s.res.Header.Set("Grpc-Status", meta.Status)
			s.res.Header.Set("Grpc-Status-Name", meta.StatusName)
		}
		if meta.Message != "" {
			s.res.Header.Set("Grpc-Message", meta.Message)
		}

		// For gRPC, override the HTTP status code with the gRPC status.
		// gRPC always returns HTTP 200, so the real status is in grpc-status.
		// We map grpc-status 0 (OK) → HTTP 200, all others → appropriate error codes.
		if meta.Status != "" && meta.Status != "0" {
			s.res.StatusCode = grpcStatusToHTTP(meta.Status)
			s.res.Status = http.StatusText(s.res.StatusCode)

			// Also update the :status pseudo-header so that plugins reading
			// status via the header map (e.g., report plugin's HeaderMap.Status())
			// see the gRPC-mapped status instead of the original HTTP 200.
			s.res.Header.Set(":status", strconv.Itoa(s.res.StatusCode))
		}
	}
}

// grpcStatusToHTTP maps a gRPC status code to a roughly equivalent HTTP status code
// for reporting purposes. This helps plugins that look at HTTP status codes understand
// gRPC errors without gRPC-specific awareness.
func grpcStatusToHTTP(grpcStatus string) int {
	switch grpcStatus {
	case "0": // OK
		return 200
	case "1": // CANCELLED
		return 499
	case "2": // UNKNOWN
		return 500
	case "3": // INVALID_ARGUMENT
		return 400
	case "4": // DEADLINE_EXCEEDED
		return 504
	case "5": // NOT_FOUND
		return 404
	case "6": // ALREADY_EXISTS
		return 409
	case "7": // PERMISSION_DENIED
		return 403
	case "8": // RESOURCE_EXHAUSTED
		return 429
	case "9": // FAILED_PRECONDITION
		return 400
	case "10": // ABORTED
		return 409
	case "11": // OUT_OF_RANGE
		return 400
	case "12": // UNIMPLEMENTED
		return 501
	case "13": // INTERNAL
		return 500
	case "14": // UNAVAILABLE
		return 503
	case "15": // DATA_LOSS
		return 500
	case "16": // UNAUTHENTICATED
		return 401
	default:
		return 500
	}
}

func (s *Session) WriteRequestBody(data []byte, endStream bool) error {
	// filter out compressed content
	// note: identity is the default and should be omitted on Content-Encoding
	// however, some servers provide it anyway.
	if ce := s.req.Header.Get("Content-Encoding"); ce != "" && !strings.EqualFold(ce, "identity") {
		s.logger.Debug("request body is encoded, skipping plugins", zap.String("domain", s.domain), zap.String("encoding", ce))
		return ErrEncodedBody
	}

	if s.pluginConn != nil {
		// call the request body callback
		if err := s.pluginConn.OnHttpRequestBody(data, endStream); err != nil {
			s.logger.Error("plugin request body", zap.Error(err))
		}
	}

	return nil
}

func (s *Session) WriteResponseBody(data []byte, endStream bool) error {
	// filter out compressed content
	// note: identity is the default and should be omitted on Content-Encoding
	// however, some servers provide it anyway.
	if ce := s.res.Header.Get("Content-Encoding"); ce != "" && !strings.EqualFold(ce, "identity") {
		s.logger.Debug("response body is encoded, skipping plugins", zap.String("domain", s.domain), zap.String("encoding", ce))
		// Still tear the session down on end-of-stream — otherwise plugin
		// instances are never destroyed, no artifact is emitted, and on a
		// reused connection (HTTP/2 multiplexing, keep-alive) every
		// subsequent stream that creates a new pluginConn but shares the
		// same outer Connection leaves a leaked session behind.
		if endStream {
			s.Close()
		}
		return ErrEncodedBody
	}

	if s.pluginConn != nil {
		// call the response body callback
		if err := s.pluginConn.OnHttpResponseBody(data, endStream); err != nil {
			s.logger.Error("plugin response body", zap.Error(err))
		}
	}

	// close the response body if this is the end of the stream
	if endStream {
		// cleanup the session
		s.Close()
	}

	return nil
}

func (s *Session) Close() {
	span := trace.SpanFromContext(s.ctx)
	defer span.End()

	if s.closed {
		return
	}

	if s.pluginConn != nil {
		// update the bandwidth metadata
		s.pluginConn.Meta().SetReadBytes(s.rdBytes)
		s.pluginConn.Meta().SetWriteBytes(s.wrBytes)
		span.SetAttributes(
			attribute.Int64("wr_bytes", s.wrBytes),
			attribute.Int64("rd_bytes", s.rdBytes),
		)

		// For gRPC streams that close without a trailing HEADERS frame
		// (e.g. client cancellation, RST_STREAM), no grpc-status trailer is
		// ever delivered. Default to CANCELLED (1) so plugins always see a
		// meaningful status rather than an empty string.
		if s.isGRPC && s.res != nil && s.res.Header.Get("Grpc-Status") == "" {
			s.res.Header.Set("Grpc-Status", "1")
			s.res.Header.Set("Grpc-Status-Name", "CANCELLED")
			s.res.StatusCode = 499
			s.res.Header.Set(":status", "499")
		}

		// teardown the plugin connection
		s.pluginConn.Teardown()
	}

	// set the closed flag
	s.closed = true
}

func (s *Session) Closed() bool {
	return s.closed
}

func (s *Session) SetState(state StreamState) {
	span := trace.SpanFromContext(s.ctx)
	span.AddEvent(fmt.Sprintf("session.state[%s]", state.String()))
	s.State = state
}

func (s StreamState) String() string {
	switch s {
	case StreamStateIdle:
		return "Idle"
	case StreamStateRequestHeaders:
		return "RequestHeaders"
	case StreamStateRequestBody:
		return "RequestBody"
	case StreamStateRequestDone:
		return "RequestDone"
	case StreamStateResponseHeaders:
		return "ResponseHeaders"
	case StreamStateResponseBody:
		return "ResponseBody"
	case StreamStateResponseDone:
		return "ResponseDone"
	default:
		return ""
	}
}
