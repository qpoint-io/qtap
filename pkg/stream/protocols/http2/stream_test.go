package http2

import (
	"bytes"
	"testing"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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
