package plugins

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/connmeta"
	"github.com/qpoint-io/qtap/pkg/synq"
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

const (
	commandQueueSize = 8192
)

// Command represents a unit of work to be processed asynchronously
type command struct {
	name string       // for debugging
	fn   func() error // the actual work
}

type Connection struct {
	ctx    context.Context
	logger *zap.Logger
	Type   ConnectionType

	meta         *meta
	req          *http.Request
	resp         *http.Response
	reqHeaderMap *HttpHeaderMap
	resHeaderMap *HttpHeaderMap
	reqBody      *synq.LinkedBuffer
	resBody      *synq.LinkedBuffer

	services      *services.ServiceRegistry
	stackInstance StackInstance
	controlValues map[string]any
	bufferSize    int

	// async processing infra
	commandQueue chan command
	workerDone   chan struct{}
	shutdownOnce sync.Once
}

func NewConnection(ctx context.Context, logger *zap.Logger, requestID string, bufferSize int, connectionType ConnectionType, stack *StackDeployment, svcs *services.ServiceRegistry) *Connection {
	ctx, span := tracer.Start(ctx, "plugin.Connection")
	span.SetAttributes(attribute.String("connection.type", string(connectionType)))

	c := &Connection{
		ctx:        ctx,
		logger:     logger,
		Type:       connectionType,
		bufferSize: bufferSize,
		meta: &meta{
			requestID: requestID,
		},
		reqBody:  synq.NewLinkedBuffer(bufferSize),
		resBody:  synq.NewLinkedBuffer(bufferSize),
		services: svcs,

		commandQueue: make(chan command, commandQueueSize),
		workerDone:   make(chan struct{}),
	}

	if svc, err := services.GetService[connmeta.Service](ctx, svcs, connmeta.Type, ""); err != nil {
		c.logger.Error("failed to get connmeta", zap.Error(err))
	} else {
		c.meta.Service = svc
	}

	// set the deployment
	c.stackInstance = stack.NewInstance(c)

	// start the worker
	go c.worker()

	return c
}

// worker processes commands sequentially from the queue
func (c *Connection) worker() {
	defer close(c.workerDone)

	for {
		select {
		case cmd, ok := <-c.commandQueue:
			span := trace.SpanFromContext(c.ctx)

			if !ok {
				// Channel closed, worker should exit
				span.AddEvent("worker command channel closed")
				c.logger.Debug("worker shutting down")
				return
			}

			// Process the command
			// TODO(Jon): add a timeout for each step in the plugin stack
			span.AddEvent("worker command processing", trace.WithAttributes(attribute.String("command", cmd.name)))
			if err := cmd.fn(); err != nil {
				c.logger.Error("command execution failed",
					zap.String("command", cmd.name),
					zap.Error(err))
			}

		case <-c.ctx.Done():
			span := trace.SpanFromContext(c.ctx)
			span.AddEvent("worker context cancelled")

			// Context cancelled, drain remaining commands if needed
			c.logger.Debug("worker context cancelled, draining remaining commands")

			// Drain remaining commands with a timeout
			drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			for {
				select {
				case cmd, ok := <-c.commandQueue:
					if !ok {
						return
					}

					// Process command with timeout
					if err := cmd.fn(); err != nil {
						c.logger.Error("command execution failed (while draining)",
							zap.String("command", cmd.name),
							zap.Error(err))
					}

				case <-drainCtx.Done():
					c.logger.Warn("drain timeout exceeded, stopping command processing")
					return
				}
			}
		}
	}
}

// enqueue adds a command to the processing queue
// Returns error if the connection is shutting down
func (c *Connection) enqueue(name string, fn func() error) error {
	select {
	case c.commandQueue <- command{name: name, fn: fn}:
		return nil
	default:
		// Queue is full - return an error
		return fmt.Errorf("command queue full, rejecting command: %s", name)
	}
}

// teardown the connection
func (c *Connection) Teardown() {
	span := trace.SpanFromContext(c.ctx)
	defer span.End()

	// Ensure we only shutdown once
	c.shutdownOnce.Do(func() {
		// Stop accepting new commands
		close(c.commandQueue)

		// Wait for worker to finish processing
		<-c.workerDone

		// Now safe to cleanup
		for _, i := range c.stackInstance {
			i.Destroy()
		}

		c.reqBody.Replace(nil)
		c.resBody.Replace(nil)
	})
}

// set the request and request body
func (c *Connection) SetRequest(req *http.Request) {
	span := trace.SpanFromContext(c.ctx)
	span.AddEvent("set_request")

	// extract URL pieces and set them as headers
	// TODO(Jon): we should not be setting these
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
	span := trace.SpanFromContext(c.ctx)
	span.AddEvent("set_response")

	// extract URL pieces and set them as headers
	res.Header.Set(":status", strconv.Itoa(res.StatusCode))
	if len(res.TransferEncoding) > 0 {
		res.Header.Set(":transfer-encoding", strings.Join(res.TransferEncoding, ","))
	}

	// set the response and response body
	c.resp = res
	c.resHeaderMap = NewHeaders(res.Header)
}

func (c *Connection) SetControlValues(values map[string]any) {
	c.controlValues = values
}

// session is done
func (c *Connection) ProxyOnDone() error {
	return nil
}

// request headers are ready
func (c *Connection) OnHttpRequestHeaders(endOfStream bool) error {
	span := trace.SpanFromContext(c.ctx)
	span.AddEvent("on_request_headers")

	return c.enqueue("request_headers", func() error {
		for _, i := range c.stackInstance {
			status := i.RequestHeaders(c.reqHeaderMap, endOfStream)

			switch status {
			case HeadersStatusContinue:
				// continue to the next plugin
			case HeadersStatusStopIteration:
				// stop plugin execution
				return nil
			default:
				c.logger.DPanic("unimplemented request headers status",
					zap.Any("status", status))
			}
		}
		return nil
	})
}

// response headers are ready
func (c *Connection) OnHttpResponseHeaders(endOfStream bool) error {
	span := trace.SpanFromContext(c.ctx)
	span.AddEvent("on_response_headers")

	return c.enqueue("response_headers", func() error {
		for _, i := range c.stackInstance {
			status := i.ResponseHeaders(c.resHeaderMap, endOfStream)

			switch status {
			case HeadersStatusContinue:
				// continue to the next plugin
			case HeadersStatusStopIteration:
				// stop plugin execution and buffer the response
				return nil

			default:
				c.logger.DPanic("unimplemented response headers status", zap.Any("status", status))
			}
		}

		return nil
	})
}

// request body is ready
func (c *Connection) OnHttpRequestBody(frame []byte, endOfStream bool) error {
	span := trace.SpanFromContext(c.ctx)
	span.AddEvent("on_request_body")

	return c.enqueue("request_body", func() error {
		_, err := c.reqBody.Write(frame)
		if err != nil {
			c.logger.Error("error writing request body", zap.Error(err))
		}

		shouldClearBuffer := !endOfStream
		for _, i := range c.stackInstance {
			status := i.RequestBody(c.reqBody, endOfStream)

			switch status {
			case BodyStatusContinue:
				// continue to the next plugin
			case BodyStatusContinueAndBuffer:
				// buffer the response and continue
				shouldClearBuffer = false
			case BodyStatusStopIterationAndBuffer:
				// stop plugin execution and buffer the response
				return nil
			default:
				c.logger.DPanic("unimplemented request body status", zap.Any("status", status))
			}
		}

		// all plugins returned continue, clear the buffer and continue
		if shouldClearBuffer {
			c.reqBody.Replace(nil)
		}
		return nil
	})
}

// response body is ready
func (c *Connection) OnHttpResponseBody(frame []byte, endOfStream bool) error {
	span := trace.SpanFromContext(c.ctx)
	span.AddEvent("on_response_body")

	return c.enqueue("response_body", func() error {
		_, err := c.resBody.Write(frame)
		if err != nil {
			c.logger.Error("error writing response body", zap.Error(err))
		}

		shouldClearBuffer := !endOfStream
		for _, i := range c.stackInstance {
			status := i.ResponseBody(c.resBody, endOfStream)

			switch status {
			case BodyStatusContinue:
				// continue to the next plugin
			case BodyStatusContinueAndBuffer:
				// buffer the response and continue
				shouldClearBuffer = false
			case BodyStatusStopIterationAndBuffer:
				// stop plugin execution and buffer the response
				return nil
			default:
				c.logger.DPanic("unimplemented response body status", zap.Any("status", status))
			}
		}

		// all plugins returned continue, clear the buffer and continue
		if shouldClearBuffer {
			c.resBody.Replace(nil)
		}
		return nil
	})
}

func (c *Connection) Context() *ConnectionContext {
	return &ConnectionContext{
		connection: c,
	}
}

func (c *Connection) Meta() Meta {
	return c.meta
}
