package plugins

import (
	"context"

	"github.com/qpoint-io/qtap/pkg/synq"
)

type ConnectionContext struct {
	connection *Connection
}

// HttpPluginInstance interface implementation
// this is the client side of the connection that filters
// can use to interact with the connection
func (c *ConnectionContext) GetRequestBodyBuffer() BodyBuffer {
	if c.connection.reqBody == nil {
		c.connection.reqBody = synq.NewLinkedBuffer(c.connection.bufferSize)
	}

	return c.connection.reqBody
}

func (c *ConnectionContext) GetResponseBodyBuffer() BodyBuffer {
	if c.connection.resBody == nil {
		c.connection.resBody = synq.NewLinkedBuffer(c.connection.bufferSize)
	}

	return c.connection.resBody
}

func (c *ConnectionContext) Context() context.Context {
	return c.connection.ctx
}

func (c *ConnectionContext) Meta() Meta {
	return c.connection.meta
}
