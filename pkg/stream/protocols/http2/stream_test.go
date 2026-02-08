package http2

import (
	"bytes"
	"testing"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

// newTestStream creates an HTTPStream with minimal dependencies suitable for unit tests.
func newTestStream(t *testing.T) *HTTPStream {
	t.Helper()
	conn := &connection.Connection{
		OpenEvent: &connection.OpenEvent{Source: connection.Client},
	}
	return NewHTTPStream(t.Context(), "example.com", zap.NewNop(), conn)
}

// encodeHeadersFrame encodes fields using enc into buf and returns a raw HEADERS frame.
// The same encoder must be reused across calls within a direction to maintain dynamic
// table state between frames, mirroring what a real HTTP/2 client or server does.
func encodeHeadersFrame(t *testing.T, enc *hpack.Encoder, buf *bytes.Buffer, streamID uint32, endStream bool, fields []hpack.HeaderField) []byte {
	t.Helper()
	buf.Reset()
	for _, f := range fields {
		require.NoError(t, enc.WriteField(f))
	}
	var out bytes.Buffer
	fr := http2.NewFramer(&out, nil)
	require.NoError(t, fr.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      streamID,
		BlockFragment: buf.Bytes(),
		EndStream:     endStream,
		EndHeaders:    true,
	}))
	return out.Bytes()
}

func processEgress(t *testing.T, stream *HTTPStream, data ...[]byte) {
	t.Helper()
	var combined []byte
	for _, d := range data {
		combined = append(combined, d...)
	}
	require.NoError(t, stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      combined,
	}))
}

func processIngress(t *testing.T, stream *HTTPStream, data ...[]byte) {
	t.Helper()
	var combined []byte
	for _, d := range data {
		combined = append(combined, d...)
	}
	require.NoError(t, stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      combined,
	}))
}

// TestHPACKEgressDecoderPersistsAcrossStreams verifies that the egress HPACK decoder
// maintains its dynamic table across multiple request streams on the same connection.
// HTTP/2 requires one dynamic table per direction, shared across all streams.
func TestHPACKEgressDecoderPersistsAcrossStreams(t *testing.T) {
	stream := newTestStream(t)

	var hdrBuf bytes.Buffer
	enc := hpack.NewEncoder(&hdrBuf)

	// Request 1 on stream 1: adds x-request-id=abc123 to the egress encoder's
	// dynamic table via literal-with-incremental-indexing.
	req1 := encodeHeadersFrame(t, enc, &hdrBuf, 1, true, []hpack.HeaderField{
		{Name: ":method", Value: "GET"},
		{Name: ":scheme", Value: "https"},
		{Name: ":path", Value: "/first"},
		{Name: "x-request-id", Value: "abc123"},
	})
	processEgress(t, stream, []byte(http2.ClientPreface), req1)

	// Request 2 on stream 3: the encoder emits x-request-id=abc123 as a back-reference
	// to the dynamic table entry established in request 1. The egress decoder must
	// have retained that entry to resolve it correctly.
	req2 := encodeHeadersFrame(t, enc, &hdrBuf, 3, true, []hpack.HeaderField{
		{Name: ":method", Value: "GET"},
		{Name: ":scheme", Value: "https"},
		{Name: ":path", Value: "/second"},
		{Name: "x-request-id", Value: "abc123"},
	})
	processEgress(t, stream, req2)

	s1, ok := stream.sessions[1]
	require.True(t, ok, "session for stream 1 should exist")
	require.Equal(t, "/first", s1.req.URL.Path)
	require.Equal(t, "abc123", s1.req.Header.Get("X-Request-Id"))

	s3, ok := stream.sessions[3]
	require.True(t, ok, "session for stream 3 should exist")
	require.Equal(t, "/second", s3.req.URL.Path)
	require.Equal(t, "abc123", s3.req.Header.Get("X-Request-Id"),
		"x-request-id should decode correctly via HPACK back-reference from a prior stream")
}

// TestHPACKDirectionsUseIndependentDecoders verifies that the egress and ingress HPACK
// decoders maintain independent dynamic tables, as required by the HTTP/2 spec (RFC 7541).
//
// The bug this tests: a single shared headerDecoder was used for both directions.
// When the server sent a response HEADERS frame, entries were added to the shared
// decoder's dynamic table, shifting the indices the egress encoder expected. A
// subsequent egress HEADERS back-reference would then resolve to the wrong header.
func TestHPACKDirectionsUseIndependentDecoders(t *testing.T) {
	stream := newTestStream(t)

	var egressHdrBuf, ingressHdrBuf bytes.Buffer
	egressEnc := hpack.NewEncoder(&egressHdrBuf)
	ingressEnc := hpack.NewEncoder(&ingressHdrBuf)

	// Step 1: Request 1 (egress, stream 1).
	// Adds x-request-id=abc123 to the egress encoder's dynamic table at index 62
	// (after :path=/api/v1 is also indexed, shifting it to 63).
	req1 := encodeHeadersFrame(t, egressEnc, &egressHdrBuf, 1, true, []hpack.HeaderField{
		{Name: ":method", Value: "GET"},
		{Name: ":scheme", Value: "https"},
		{Name: ":path", Value: "/api/v1"},
		{Name: "x-request-id", Value: "abc123"},
	})
	processEgress(t, stream, []byte(http2.ClientPreface), req1)

	// Step 2: Response 1 (ingress, stream 1).
	// Adds content-type=text/html to the ingress encoder's dynamic table.
	// With the old shared decoder, this would also insert content-type=text/html
	// at index 62 of the shared decoder, pushing x-request-id=abc123 to index 63.
	// The egress encoder still believes x-request-id=abc123 is at index 62.
	resp1 := encodeHeadersFrame(t, ingressEnc, &ingressHdrBuf, 1, true, []hpack.HeaderField{
		{Name: ":status", Value: "200"},
		{Name: "content-type", Value: "text/html"},
	})
	processIngress(t, stream, resp1)
	// Stream 1's session is now completed and removed from the map.
	_, exists := stream.sessions[1]
	require.False(t, exists, "stream 1 session should have been cleaned up after response")

	// Step 3: Request 2 (egress, stream 3).
	// The egress encoder emits x-request-id=abc123 as an indexed back-reference.
	// With the old shared decoder, the index it resolves to points at content-type=text/html
	// (inserted by the ingress response), producing the wrong header.
	// With the fix (per-direction decoders), the egress decoder's table is untouched
	// by ingress frames and resolves the index to x-request-id=abc123 correctly.
	req2 := encodeHeadersFrame(t, egressEnc, &egressHdrBuf, 3, true, []hpack.HeaderField{
		{Name: ":method", Value: "GET"},
		{Name: ":scheme", Value: "https"},
		{Name: ":path", Value: "/api/v2"},
		{Name: "x-request-id", Value: "abc123"},
	})
	processEgress(t, stream, req2)

	s3, ok := stream.sessions[3]
	require.True(t, ok, "session for stream 3 should exist")
	require.Equal(t, "/api/v2", s3.req.URL.Path)
	require.Equal(t, "abc123", s3.req.Header.Get("X-Request-Id"),
		"x-request-id header was corrupted: egress decoder was polluted by ingress frames (shared decoder bug)")
	require.Empty(t, s3.req.Header.Get("Content-Type"),
		"content-type from the ingress response must not appear in the egress request headers")
}

// frameWriter helps construct raw HTTP/2 frames for testing.
type frameWriter struct {
	buf     bytes.Buffer
	framer  *http2.Framer
	encoder *hpack.Encoder
	hbuf    bytes.Buffer // scratch buffer for HPACK encoding
}

func newFrameWriter() *frameWriter {
	fw := &frameWriter{}
	fw.framer = http2.NewFramer(&fw.buf, nil)
	fw.encoder = hpack.NewEncoder(&fw.hbuf)
	return fw
}

// writeHeaders writes an HTTP/2 HEADERS frame with HPACK-encoded headers.
func (fw *frameWriter) writeHeaders(streamID uint32, endStream bool, headers []hpack.HeaderField) {
	fw.hbuf.Reset()
	for _, hf := range headers {
		if err := fw.encoder.WriteField(hf); err != nil {
			panic("WriteField: " + err.Error())
		}
	}

	if err := fw.framer.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      streamID,
		EndStream:     endStream,
		EndHeaders:    true,
		BlockFragment: fw.hbuf.Bytes(),
	}); err != nil {
		panic("WriteHeaders: " + err.Error())
	}
}

// writeData writes an HTTP/2 DATA frame.
func (fw *frameWriter) writeData(streamID uint32, endStream bool, data []byte) {
	if err := fw.framer.WriteData(streamID, endStream, data); err != nil {
		panic("WriteData: " + err.Error())
	}
}

// writeSettings writes an HTTP/2 SETTINGS frame.
func (fw *frameWriter) writeSettings(settings ...http2.Setting) {
	if err := fw.framer.WriteSettings(settings...); err != nil {
		panic("WriteSettings: " + err.Error())
	}
}

// writeSettingsAck writes a SETTINGS ACK frame.
func (fw *frameWriter) writeSettingsAck() {
	if err := fw.framer.WriteSettingsAck(); err != nil {
		panic("WriteSettingsAck: " + err.Error())
	}
}

// bytes returns the accumulated frame data and resets the buffer.
func (fw *frameWriter) bytes() []byte {
	b := make([]byte, fw.buf.Len())
	copy(b, fw.buf.Bytes())
	fw.buf.Reset()
	return b
}

func createTestStream(t *testing.T) (*HTTPStream, *observer.ObservedLogs) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)

	conn := connection.NewConnection(
		t.Context(),
		logger,
		&connection.OpenEvent{
			Source: connection.Client,
		},
	)

	stream := NewHTTPStream(t.Context(), "test.example.com", logger, conn)
	return stream, logs
}

// TestMultiplexedStreamPathAttribution verifies that each HTTP/2 stream
// on a multiplexed connection gets the correct :path from its own HEADERS frame,
// not from the first stream's headers.
func TestMultiplexedStreamPathAttribution(t *testing.T) {
	stream, _ := createTestStream(t)

	clientWriter := newFrameWriter()

	preface := []byte(http2.ClientPreface)
	clientWriter.writeSettings()

	egressData1 := make([]byte, 0, len(preface)+64)
	egressData1 = append(egressData1, preface...)
	egressData1 = append(egressData1, clientWriter.bytes()...)

	err := stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      egressData1,
	})
	require.NoError(t, err)

	clientWriter.writeHeaders(1, false, []hpack.HeaderField{
		{Name: ":method", Value: "POST"},
		{Name: ":scheme", Value: "http"},
		{Name: ":path", Value: "/hipstershop.ProductCatalogService/ListProducts"},
		{Name: ":authority", Value: "productcatalog:3550"},
		{Name: "content-type", Value: "application/grpc"},
		{Name: "te", Value: "trailers"},
	})
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      clientWriter.bytes(),
	})
	require.NoError(t, err)

	session1 := stream.sessions[1]
	require.NotNil(t, session1, "session 1 should exist")
	require.NotNil(t, session1.req, "session 1 should have a request")
	assert.Equal(t, "/hipstershop.ProductCatalogService/ListProducts", session1.req.RequestURI)
	assert.Equal(t, "POST", session1.req.Method)
	assert.Equal(t, "productcatalog:3550", session1.req.Host)

	serverWriter := newFrameWriter()

	serverWriter.writeSettings()
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      serverWriter.bytes(),
	})
	require.NoError(t, err)

	serverWriter.writeSettingsAck()
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      serverWriter.bytes(),
	})
	require.NoError(t, err)

	clientWriter.writeData(1, true, []byte{0, 0, 0, 0, 0})
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      clientWriter.bytes(),
	})
	require.NoError(t, err)

	serverWriter.writeHeaders(1, false, []hpack.HeaderField{
		{Name: ":status", Value: "200"},
		{Name: "content-type", Value: "application/grpc"},
	})
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      serverWriter.bytes(),
	})
	require.NoError(t, err)

	serverWriter.writeData(1, false, []byte{0, 0, 0, 0, 2, 0x0a, 0x00})
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      serverWriter.bytes(),
	})
	require.NoError(t, err)

	serverWriter.writeHeaders(1, true, []hpack.HeaderField{
		{Name: "grpc-status", Value: "0"},
		{Name: "grpc-message", Value: ""},
	})
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      serverWriter.bytes(),
	})
	require.NoError(t, err)

	_, exists := stream.sessions[1]
	assert.False(t, exists, "session 1 should be cleaned up after completion")

	clientWriter.writeHeaders(3, false, []hpack.HeaderField{
		{Name: ":method", Value: "POST"},
		{Name: ":scheme", Value: "http"},
		{Name: ":path", Value: "/hipstershop.CurrencyService/Convert"},
		{Name: ":authority", Value: "currency:3550"},
		{Name: "content-type", Value: "application/grpc"},
		{Name: "te", Value: "trailers"},
	})
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      clientWriter.bytes(),
	})
	require.NoError(t, err)

	session3 := stream.sessions[3]
	require.NotNil(t, session3, "session 3 should exist")
	require.NotNil(t, session3.req, "session 3 should have a request")
	assert.Equal(t, "/hipstershop.CurrencyService/Convert", session3.req.RequestURI,
		"Stream 3 should have its own path, not stream 1's path or '/'")
	assert.Equal(t, "POST", session3.req.Method)
	assert.Equal(t, "currency:3550", session3.req.Host)

	clientWriter.writeHeaders(5, false, []hpack.HeaderField{
		{Name: ":method", Value: "POST"},
		{Name: ":scheme", Value: "http"},
		{Name: ":path", Value: "/hipstershop.CartService/GetCart"},
		{Name: ":authority", Value: "cart:3550"},
		{Name: "content-type", Value: "application/grpc"},
		{Name: "te", Value: "trailers"},
	})
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      clientWriter.bytes(),
	})
	require.NoError(t, err)

	session5 := stream.sessions[5]
	require.NotNil(t, session5, "session 5 should exist")
	require.NotNil(t, session5.req, "session 5 should have a request")
	assert.Equal(t, "/hipstershop.CartService/GetCart", session5.req.RequestURI,
		"Stream 5 should have its own path")
	assert.Equal(t, "cart:3550", session5.req.Host)
}

// TestConcurrentMultiplexedStreams verifies that multiple in-flight streams
// on the same connection each get the correct path when HEADERS arrive interleaved.
func TestConcurrentMultiplexedStreams(t *testing.T) {
	stream, _ := createTestStream(t)

	clientWriter := newFrameWriter()
	serverWriter := newFrameWriter()

	preface := []byte(http2.ClientPreface)
	clientWriter.writeSettings()
	err := stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      append(preface, clientWriter.bytes()...),
	})
	require.NoError(t, err)

	serverWriter.writeSettings()
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      serverWriter.bytes(),
	})
	require.NoError(t, err)

	clientWriter.writeHeaders(1, true, []hpack.HeaderField{
		{Name: ":method", Value: "POST"},
		{Name: ":scheme", Value: "http"},
		{Name: ":path", Value: "/grpc.health.v1.Health/Check"},
		{Name: ":authority", Value: "server:50051"},
		{Name: "content-type", Value: "application/grpc"},
		{Name: "te", Value: "trailers"},
	})
	clientWriter.writeHeaders(3, true, []hpack.HeaderField{
		{Name: ":method", Value: "POST"},
		{Name: ":scheme", Value: "http"},
		{Name: ":path", Value: "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"},
		{Name: ":authority", Value: "server:50051"},
		{Name: "content-type", Value: "application/grpc"},
		{Name: "te", Value: "trailers"},
	})
	clientWriter.writeHeaders(5, true, []hpack.HeaderField{
		{Name: ":method", Value: "POST"},
		{Name: ":scheme", Value: "http"},
		{Name: ":path", Value: "/myapp.UserService/GetUser"},
		{Name: ":authority", Value: "server:50051"},
		{Name: "content-type", Value: "application/grpc"},
		{Name: "te", Value: "trailers"},
	})

	err = stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      clientWriter.bytes(),
	})
	require.NoError(t, err)

	session1 := stream.sessions[1]
	require.NotNil(t, session1)
	require.NotNil(t, session1.req)
	assert.Equal(t, "/grpc.health.v1.Health/Check", session1.req.RequestURI, "Stream 1 path")

	session3 := stream.sessions[3]
	require.NotNil(t, session3)
	require.NotNil(t, session3.req)
	assert.Equal(t, "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo", session3.req.RequestURI, "Stream 3 path")

	session5 := stream.sessions[5]
	require.NotNil(t, session5)
	require.NotNil(t, session5.req)
	assert.Equal(t, "/myapp.UserService/GetUser", session5.req.RequestURI, "Stream 5 path")
}

// TestInterleavedRequestResponse verifies that request and response HEADERS
// on the same stream are decoded correctly using separate HPACK contexts.
func TestInterleavedRequestResponse(t *testing.T) {
	stream, _ := createTestStream(t)

	clientWriter := newFrameWriter()
	serverWriter := newFrameWriter()

	preface := []byte(http2.ClientPreface)
	clientWriter.writeSettings()
	err := stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      append(preface, clientWriter.bytes()...),
	})
	require.NoError(t, err)

	serverWriter.writeSettings()
	serverWriter.writeSettingsAck()
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      serverWriter.bytes(),
	})
	require.NoError(t, err)

	clientWriter.writeHeaders(1, true, []hpack.HeaderField{
		{Name: ":method", Value: "POST"},
		{Name: ":scheme", Value: "http"},
		{Name: ":path", Value: "/grpc.health.v1.Health/Check"},
		{Name: ":authority", Value: "server:50051"},
		{Name: "content-type", Value: "application/grpc"},
	})
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      clientWriter.bytes(),
	})
	require.NoError(t, err)

	session1 := stream.sessions[1]
	require.NotNil(t, session1)
	require.NotNil(t, session1.req)
	assert.Equal(t, "/grpc.health.v1.Health/Check", session1.req.RequestURI)

	serverWriter.writeHeaders(1, true, []hpack.HeaderField{
		{Name: ":status", Value: "200"},
		{Name: "content-type", Value: "application/grpc"},
		{Name: "grpc-status", Value: "0"},
		{Name: "grpc-message", Value: ""},
	})
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      serverWriter.bytes(),
	})
	require.NoError(t, err)

	_, exists := stream.sessions[1]
	assert.False(t, exists, "session 1 should be cleaned up")

	clientWriter.writeHeaders(3, true, []hpack.HeaderField{
		{Name: ":method", Value: "POST"},
		{Name: ":scheme", Value: "http"},
		{Name: ":path", Value: "/myservice.v1.Foo/Bar"},
		{Name: ":authority", Value: "server:50051"},
		{Name: "content-type", Value: "application/grpc"},
	})
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      clientWriter.bytes(),
	})
	require.NoError(t, err)

	session3 := stream.sessions[3]
	require.NotNil(t, session3)
	require.NotNil(t, session3.req)
	assert.Equal(t, "/myservice.v1.Foo/Bar", session3.req.RequestURI,
		"Stream 3 should have its own path, not '/' or stream 1's path")
	assert.Equal(t, "POST", session3.req.Method)
}

// TestSingleStreamRegression ensures single-stream behavior still works correctly.
func TestSingleStreamRegression(t *testing.T) {
	stream, _ := createTestStream(t)

	clientWriter := newFrameWriter()
	serverWriter := newFrameWriter()

	preface := []byte(http2.ClientPreface)
	clientWriter.writeSettings()
	err := stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      append(preface, clientWriter.bytes()...),
	})
	require.NoError(t, err)

	serverWriter.writeSettings()
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      serverWriter.bytes(),
	})
	require.NoError(t, err)

	clientWriter.writeHeaders(1, true, []hpack.HeaderField{
		{Name: ":method", Value: "GET"},
		{Name: ":scheme", Value: "https"},
		{Name: ":path", Value: "/api/v1/users"},
		{Name: ":authority", Value: "api.example.com"},
	})
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      clientWriter.bytes(),
	})
	require.NoError(t, err)

	session := stream.sessions[1]
	require.NotNil(t, session)
	require.NotNil(t, session.req)
	assert.Equal(t, "/api/v1/users", session.req.RequestURI)
	assert.Equal(t, "GET", session.req.Method)
	assert.Equal(t, "api.example.com", session.req.Host)
	assert.Equal(t, "https", session.req.URL.Scheme)
}

// TestManyStreamsWithSamePath verifies that when multiple streams use the same
// path (common in gRPC health checks), HPACK compression works correctly with
// indexed references.
func TestManyStreamsWithSamePath(t *testing.T) {
	stream, _ := createTestStream(t)

	clientWriter := newFrameWriter()
	serverWriter := newFrameWriter()

	preface := []byte(http2.ClientPreface)
	clientWriter.writeSettings()
	err := stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      append(preface, clientWriter.bytes()...),
	})
	require.NoError(t, err)

	serverWriter.writeSettings()
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      serverWriter.bytes(),
	})
	require.NoError(t, err)

	// Send 10 streams all with the same path (like repeated health checks)
	for i := range 10 {
		streamID := uint32(2*i + 1) // 1, 3, 5, 7, ...

		clientWriter.writeHeaders(streamID, true, []hpack.HeaderField{
			{Name: ":method", Value: "POST"},
			{Name: ":scheme", Value: "http"},
			{Name: ":path", Value: "/grpc.health.v1.Health/Check"},
			{Name: ":authority", Value: "server:50051"},
			{Name: "content-type", Value: "application/grpc"},
			{Name: "te", Value: "trailers"},
		})
		err = stream.Process(&connection.DataEvent{
			Direction: connection.Egress,
			Data:      clientWriter.bytes(),
		})
		require.NoError(t, err, "stream %d request should not fail", streamID)

		session := stream.sessions[streamID]
		require.NotNil(t, session, "session %d should exist", streamID)
		require.NotNil(t, session.req, "session %d should have a request", streamID)
		assert.Equal(t, "/grpc.health.v1.Health/Check", session.req.RequestURI,
			"Stream %d should have the correct path", streamID)
		assert.Equal(t, "POST", session.req.Method)

		serverWriter.writeHeaders(streamID, true, []hpack.HeaderField{
			{Name: ":status", Value: "200"},
			{Name: "content-type", Value: "application/grpc"},
			{Name: "grpc-status", Value: "0"},
		})
		err = stream.Process(&connection.DataEvent{
			Direction: connection.Ingress,
			Data:      serverWriter.bytes(),
		})
		require.NoError(t, err, "stream %d response should not fail", streamID)
	}
}

// TestBadHPACKFailOpen verifies that a bad HPACK frame on one stream
// does not kill the entire connection — other streams continue to be parsed.
func TestBadHPACKFailOpen(t *testing.T) {
	stream, logs := createTestStream(t)

	clientWriter := newFrameWriter()
	serverWriter := newFrameWriter()

	// 1. Client connection preface + SETTINGS
	preface := []byte(http2.ClientPreface)
	clientWriter.writeSettings()

	egressData := make([]byte, 0, len(preface)+64)
	egressData = append(egressData, preface...)
	egressData = append(egressData, clientWriter.bytes()...)

	err := stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      egressData,
	})
	require.NoError(t, err)

	// Server SETTINGS + ACK
	serverWriter.writeSettings()
	serverWriter.writeSettingsAck()
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      serverWriter.bytes(),
	})
	require.NoError(t, err)

	// 2. Send a valid HEADERS frame on stream 5 (request)
	clientWriter.writeHeaders(5, true, []hpack.HeaderField{
		{Name: ":method", Value: "GET"},
		{Name: ":path", Value: "/good"},
		{Name: ":scheme", Value: "https"},
		{Name: ":authority", Value: "test.example.com"},
	})
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      clientWriter.bytes(),
	})
	require.NoError(t, err)

	// 3. Inject a raw HEADERS frame with garbage HPACK on stream 3.
	// We construct a valid HTTP/2 frame header but with invalid HPACK payload.
	badHPACK := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	rawFrame := buildRawHeadersFrame(3, true, true, badHPACK)

	// Use a separate encoder for egress so we don't corrupt the clientWriter's HPACK state
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Egress,
		Data:      rawFrame,
	})
	// Should NOT return an error — fail-open means we skip the bad stream
	require.NoError(t, err, "bad HPACK on stream 3 should not kill the connection")

	// 4. Verify stream 3 was cleaned up (skipped)
	_, stream3Exists := stream.sessions[3]
	assert.False(t, stream3Exists, "stream 3 should be cleaned up after HPACK error")

	// 5. Verify the debug log was emitted
	failOpenLogs := logs.FilterMessage("Skipping stream due to HPACK decode error (fail-open)")
	assert.Equal(t, 1, failOpenLogs.Len(), "should have logged a fail-open skip message")

	// 6. Verify stream 5 is still alive and was parsed correctly
	session5, stream5Exists := stream.sessions[5]
	require.True(t, stream5Exists, "stream 5 should still exist after stream 3 failed")
	assert.Equal(t, "/good", session5.req.URL.Path, "stream 5 should have correct path")
	assert.Equal(t, "GET", session5.req.Method, "stream 5 should have correct method")

	// 7. Server responds to stream 5 — connection still works
	serverWriter.writeHeaders(5, true, []hpack.HeaderField{
		{Name: ":status", Value: "200"},
	})
	err = stream.Process(&connection.DataEvent{
		Direction: connection.Ingress,
		Data:      serverWriter.bytes(),
	})
	require.NoError(t, err, "stream 5 response should succeed after stream 3 failure")

	// Stream 5 should be cleaned up after completing
	_, stream5StillExists := stream.sessions[5]
	assert.False(t, stream5StillExists, "stream 5 should be cleaned up after response")
}

// buildRawHeadersFrame constructs a raw HTTP/2 HEADERS frame with the given payload.
// This bypasses HPACK encoding to allow injecting invalid data.
func buildRawHeadersFrame(streamID uint32, endStream, endHeaders bool, payload []byte) []byte {
	length := len(payload)
	frame := make([]byte, 9+length)
	// Length (24 bits)
	frame[0] = byte(length >> 16)
	frame[1] = byte(length >> 8)
	frame[2] = byte(length)
	// Type: HEADERS = 0x1
	frame[3] = 0x1
	// Flags
	var flags byte
	if endStream {
		flags |= 0x1 // END_STREAM
	}
	if endHeaders {
		flags |= 0x4 // END_HEADERS
	}
	frame[4] = flags
	// Stream ID (31 bits, R bit = 0)
	frame[5] = byte(streamID >> 24)
	frame[6] = byte(streamID >> 16)
	frame[7] = byte(streamID >> 8)
	frame[8] = byte(streamID)
	// Payload
	copy(frame[9:], payload)
	return frame
}
