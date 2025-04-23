package plugins

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/qpoint-io/qtap/pkg/plugins/metadata"
	"github.com/qpoint-io/qtap/pkg/services"
	serviceRegistrar "github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/tags"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// context type enum (http, grpc, etc)
type ConnectionType string

const (
	ConnectionType_UNKNOWN ConnectionType = "unknown"
	ConnectionType_HTTP    ConnectionType = "http"
	ConnectionType_GRPC    ConnectionType = "grpc"
)

type Connection struct {
	ctx    context.Context
	logger *zap.Logger
	id     string
	Type   ConnectionType

	req          *http.Request
	resp         *http.Response
	reqHeaderMap *HttpHeaderMap
	resHeaderMap *HttpHeaderMap
	reqBody      *synq.LinkedBuffer
	resBody      *synq.LinkedBuffer

	services      []serviceRegistrar.Service
	stackInstance StackInstance
	metadata      map[string]MetadataValue
	tags          tags.List
	bufferSize    int
}

func NewConnection(ctx context.Context, logger *zap.Logger, connID string, bufferSize int, connectionType ConnectionType, stack *StackDeployment, tags tags.List, svcs []services.Service) *Connection {
	ctx, span := tracer.Start(ctx, "plugin.Connection")
	span.SetAttributes(attribute.String("connection.type", string(connectionType)))

	c := &Connection{
		ctx:        ctx,
		logger:     logger,
		id:         connID,
		Type:       connectionType,
		bufferSize: bufferSize,
		reqBody:    synq.NewLinkedBuffer(bufferSize),
		resBody:    synq.NewLinkedBuffer(bufferSize),
		tags:       tags,
		services:   svcs,
	}

	// set the deployment
	c.stackInstance = stack.NewInstance(c)

	return c
}

// teardown the connection
func (c *Connection) Teardown() {
	span := trace.SpanFromContext(c.ctx)
	defer span.End()

	for _, i := range c.stackInstance {
		i.Destroy()
	}

	// clear the buffers
	c.reqBody.Replace(nil)
	c.resBody.Replace(nil)
}

// set the request and request body
func (c *Connection) SetRequest(req *http.Request) {
	// extract URL pieces and set them as headers
	req.Header.Set(":authority", req.Host)
	req.Header.Set(":method", req.Method)
	req.Header.Set(":path", req.URL.Path)
	req.Header.Set(":scheme", req.URL.Scheme)

	// set the request and request body
	c.req = req
	c.reqHeaderMap = NewHeaders(req.Header)
}

// set the response and response body
func (c *Connection) SetResponse(res *http.Response) {
	// extract URL pieces and set them as headers
	res.Header.Set(":status", strconv.Itoa(res.StatusCode))
	if len(res.TransferEncoding) > 0 {
		res.Header.Set(":transfer-encoding", strings.Join(res.TransferEncoding, ","))
	}

	// set the response and response body
	c.resp = res
	c.resHeaderMap = NewHeaders(res.Header)
}

// session is done
func (c *Connection) ProxyOnDone() error {
	return nil
}

func (c *Connection) AppendMetadata(key string, value any) {
	if c.metadata == nil {
		c.metadata = make(map[string]MetadataValue)
	}

	c.metadata[key] = &metadata.MetadataValue{Value: value}
}

// request headers are ready
func (c *Connection) OnHttpRequestHeaders(endOfStream bool) error {
	for _, i := range c.stackInstance {
		status := i.RequestHeaders(c.reqHeaderMap, endOfStream)

		switch status {
		case HeadersStatusContinue:
			// continue to the next plugin
		case HeadersStatusStopIteration:
			// stop plugin execution and buffer the response
			return nil

		// [not implemented] case abi.RequestHeadersStatusStopAllIterationAndBuffer:

		default:
			c.logger.DPanic("unimplemented request headers status", zap.Any("status", status))
		}
	}

	return nil
}

// response headers are ready
func (c *Connection) OnHttpResponseHeaders(endOfStream bool) error {
	for _, i := range c.stackInstance {
		status := i.ResponseHeaders(c.resHeaderMap, endOfStream)

		switch status {
		case HeadersStatusContinue:
			// continue to the next plugin
		case HeadersStatusStopIteration:
			// stop plugin execution and buffer the response
			return nil

		// [not implemented] case abi.ResponseHeadersStatusStopAllIterationAndBuffer:

		default:
			c.logger.DPanic("unimplemented response headers status", zap.Any("status", status))
		}
	}

	return nil
}

// request body is ready
func (c *Connection) OnHttpRequestBody(frame []byte, endOfStream bool) error {
	_, err := c.reqBody.Write(frame)
	if err != nil {
		c.logger.Error("error writing request body", zap.Error(err))
	}

	for _, i := range c.stackInstance {
		status := i.RequestBody(c.reqBody, endOfStream)

		switch status {
		case BodyStatusContinue:
			// continue to the next plugin
		case BodyStatusStopIterationAndBuffer:
			// stop plugin execution and buffer the response
			return nil
		default:
			c.logger.DPanic("unimplemented request body status", zap.Any("status", status))
		}
	}

	// all plugins returned continue, clear the buffer and continue
	if !endOfStream {
		c.reqBody.Replace(nil)
	}
	return nil
}

// response body is ready
func (c *Connection) OnHttpResponseBody(frame []byte, endOfStream bool) error {
	_, err := c.resBody.Write(frame)
	if err != nil {
		c.logger.Error("error writing response body", zap.Error(err))
	}

	for _, i := range c.stackInstance {
		status := i.ResponseBody(c.resBody, endOfStream)

		switch status {
		case BodyStatusContinue:
			// continue to the next plugin
		case BodyStatusStopIterationAndBuffer:
			// stop plugin execution and buffer the response
			return nil
		default:
			c.logger.DPanic("unimplemented response body status", zap.Any("status", status))
		}
	}

	// all plugins returned continue, clear the buffer and continue
	if !endOfStream {
		c.resBody.Replace(nil)
	}
	return nil
}

func (c *Connection) Context() *ConnectionContext {
	return &ConnectionContext{
		connection: c,
	}
}
