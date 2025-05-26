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
	s.req.Header.Set("qpoint-request-id", id)
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
		s.pluginConn, err = s.pluginManager.NewConnection(s.ctx, plugins.ConnectionType_HTTP, s.conn)
		if err != nil {
			return fmt.Errorf("creating plugin connection: %w", err)
		}
	}

	if s.pluginConn != nil {
		// set the request
		s.pluginConn.SetRequest(s.req)

		// add qpoint metadata
		s.pluginConn.AppendMetadata("qpoint-request-id", id)
		// set endpointID
		s.pluginConn.AppendMetadata("endpoint-id", s.conn.Domain())
		// set the direction header
		s.pluginConn.AppendMetadata("direction", s.conn.Direction())
		// set the protocol header
		s.pluginConn.AppendMetadata("protocol", s.conn.Proto())
		// set process meta headers
		for k, v := range s.conn.ProcessMeta() {
			s.pluginConn.AppendMetadata("process-"+k, fmt.Sprintf("%v", v))
		}

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
		s.pluginConn.AppendMetadata("wr_bytes", s.wrBytes)
		s.pluginConn.AppendMetadata("rd_bytes", s.rdBytes)
		span.SetAttributes(
			attribute.Int64("wr_bytes", s.wrBytes),
			attribute.Int64("rd_bytes", s.rdBytes),
		)

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
