package plugins

import (
	"context"

	"github.com/qpoint-io/qtap/pkg/plugins/metadata"
	"github.com/qpoint-io/qtap/pkg/synq"
	"github.com/qpoint-io/qtap/pkg/tags"
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

// Metadata returns connection specific metadata in a map[string]any.
func (c *ConnectionContext) Metadata() map[string]MetadataValue {
	return c.connection.metadata
}

// GetMetadata returns a key value of type any, if the key exists.
func (c *ConnectionContext) GetMetadata(key string) MetadataValue {
	if c.connection.metadata == nil {
		return &metadata.MetadataValue{}
	}

	if value, ok := c.connection.metadata[key]; ok {
		return value
	}

	// if the key doesn't exist, return an empty value
	// this is to avoid nil pointers
	// an OK() method is provided to check if the value is set
	return &metadata.MetadataValue{}
}

func (c *ConnectionContext) Tags() tags.List {
	return c.connection.tags
}

func (c *ConnectionContext) Context() context.Context {
	return c.connection.ctx
}

func (c *ConnectionContext) ControlValues() map[string]any {
	return c.connection.controlValues
}
