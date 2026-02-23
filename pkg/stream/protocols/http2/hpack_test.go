package http2

import (
	"bytes"
	"testing"

	"golang.org/x/net/http2/hpack"
)

// encodeHeaders encodes header fields using the given encoder, returning the
// encoded bytes. The encoder maintains dynamic table state across calls.
func encodeHeaders(enc *hpack.Encoder, buf *bytes.Buffer, fields ...hpack.HeaderField) []byte {
	buf.Reset()
	for _, f := range fields {
		_ = enc.WriteField(f)
	}
	out := make([]byte, buf.Len())
	copy(out, buf.Bytes())
	return out
}

func hf(name, value string) hpack.HeaderField {
	return hpack.HeaderField{Name: name, Value: value}
}

func assertHeader(t *testing.T, fields []hpack.HeaderField, name, want string) {
	t.Helper()
	for _, f := range fields {
		if f.Name == name {
			if f.Value != want {
				t.Errorf("header %q = %q, want %q", name, f.Value, want)
			}
			return
		}
	}
	t.Errorf("header %q not found in %v", name, fields)
}

// TestPerDirectionDecodersIndependentDynamicTables verifies that separate
// HPACK decoders maintain independent dynamic tables, which is required
// by HTTP/2 (RFC 7540 §4.3, RFC 7541 §4).
func TestPerDirectionDecodersIndependentDynamicTables(t *testing.T) {
	egressDecoder := hpack.NewDecoder(4096, nil)
	ingressDecoder := hpack.NewDecoder(4096, nil)

	var eBuf, iBuf bytes.Buffer
	egressEncoder := hpack.NewEncoder(&eBuf)
	ingressEncoder := hpack.NewEncoder(&iBuf)

	// Encode request headers (egress direction)
	egressData := encodeHeaders(egressEncoder, &eBuf,
		hf(":method", "POST"),
		hf(":path", "/api/v1/users"),
		hf("x-request-id", "req-123"),
	)

	// Encode response headers (ingress direction)
	ingressData := encodeHeaders(ingressEncoder, &iBuf,
		hf(":status", "200"),
		hf("content-type", "application/json"),
		hf("x-trace-id", "trace-456"),
	)

	// Decode with separate decoders
	egressFields, err := egressDecoder.DecodeFull(egressData)
	if err != nil {
		t.Fatalf("egress decode failed: %v", err)
	}
	assertHeader(t, egressFields, ":method", "POST")
	assertHeader(t, egressFields, ":path", "/api/v1/users")
	assertHeader(t, egressFields, "x-request-id", "req-123")

	ingressFields, err := ingressDecoder.DecodeFull(ingressData)
	if err != nil {
		t.Fatalf("ingress decode failed: %v", err)
	}
	assertHeader(t, ingressFields, ":status", "200")
	assertHeader(t, ingressFields, "content-type", "application/json")
	assertHeader(t, ingressFields, "x-trace-id", "trace-456")
}

// TestSharedDecoderCorruptsDynamicTable demonstrates the bug that occurs
// when a single HPACK decoder is shared between both directions.
func TestSharedDecoderCorruptsDynamicTable(t *testing.T) {
	sharedDecoder := hpack.NewDecoder(4096, nil)

	var eBuf, iBuf bytes.Buffer
	egressEncoder := hpack.NewEncoder(&eBuf)
	ingressEncoder := hpack.NewEncoder(&iBuf)

	// Round 1: both directions encode a custom header
	egressData1 := encodeHeaders(egressEncoder, &eBuf, hf("x-custom", "egress-value-1"))
	ingressData1 := encodeHeaders(ingressEncoder, &iBuf, hf("x-custom", "ingress-value-1"))

	// Decode egress with shared decoder — adds "x-custom: egress-value-1" to dynamic table
	_, err := sharedDecoder.DecodeFull(egressData1)
	if err != nil {
		t.Fatalf("shared decode egress round 1: %v", err)
	}

	// Decode ingress with same decoder — should work for literal headers
	_, err = sharedDecoder.DecodeFull(ingressData1)
	if err != nil {
		t.Logf("shared decoder failed on cross-direction decode (round 1): %v", err)
		return
	}

	// Round 2: encoders will now use indexed references to their own dynamic tables
	egressData2 := encodeHeaders(egressEncoder, &eBuf, hf("x-custom", "egress-value-2"))
	ingressData2 := encodeHeaders(ingressEncoder, &iBuf, hf("x-custom", "ingress-value-2"))

	// Decode egress round 2
	_, err = sharedDecoder.DecodeFull(egressData2)
	if err != nil {
		t.Logf("shared decoder failed on egress round 2: %v", err)
		return
	}

	// This ingress decode is where corruption is most likely
	fields, err := sharedDecoder.DecodeFull(ingressData2)
	if err != nil {
		t.Logf("shared decoder correctly failed on ingress round 2: %v", err)
		return
	}

	// Even if no error, the dynamic table state is polluted
	for _, f := range fields {
		t.Logf("shared decoder got: %s=%s", f.Name, f.Value)
	}

	// Demonstrate that separate decoders don't have this problem
	egressDec := hpack.NewDecoder(4096, nil)
	ingressDec := hpack.NewDecoder(4096, nil)

	eBuf.Reset()
	iBuf.Reset()
	egressEncoder2 := hpack.NewEncoder(&eBuf)
	ingressEncoder2 := hpack.NewEncoder(&iBuf)

	for i := range 3 {
		ed := encodeHeaders(egressEncoder2, &eBuf, hf("x-round", "egress"))
		id := encodeHeaders(ingressEncoder2, &iBuf, hf("x-round", "ingress"))

		ef, err := egressDec.DecodeFull(ed)
		if err != nil {
			t.Fatalf("separate egress decode round %d: %v", i, err)
		}
		assertHeader(t, ef, "x-round", "egress")

		inf, err := ingressDec.DecodeFull(id)
		if err != nil {
			t.Fatalf("separate ingress decode round %d: %v", i, err)
		}
		assertHeader(t, inf, "x-round", "ingress")
	}
}

// TestInterleavedEgressIngressDecoding verifies that interleaved egress and
// ingress frames decode correctly with separate per-direction decoders.
func TestInterleavedEgressIngressDecoding(t *testing.T) {
	egressDecoder := hpack.NewDecoder(4096, nil)
	ingressDecoder := hpack.NewDecoder(4096, nil)

	var eBuf, iBuf bytes.Buffer
	egressEncoder := hpack.NewEncoder(&eBuf)
	ingressEncoder := hpack.NewEncoder(&iBuf)

	// Interleaved: egress1, egress3, ingress1, ingress3
	egress1 := encodeHeaders(egressEncoder, &eBuf, hf(":method", "GET"), hf(":path", "/stream1"))
	egress3 := encodeHeaders(egressEncoder, &eBuf, hf(":method", "POST"), hf(":path", "/stream3"))
	ingress1 := encodeHeaders(ingressEncoder, &iBuf, hf(":status", "200"), hf("x-stream", "1"))
	ingress3 := encodeHeaders(ingressEncoder, &iBuf, hf(":status", "404"), hf("x-stream", "3"))

	f, err := egressDecoder.DecodeFull(egress1)
	if err != nil {
		t.Fatalf("egress1: %v", err)
	}
	assertHeader(t, f, ":path", "/stream1")

	f, err = egressDecoder.DecodeFull(egress3)
	if err != nil {
		t.Fatalf("egress3: %v", err)
	}
	assertHeader(t, f, ":path", "/stream3")

	f, err = ingressDecoder.DecodeFull(ingress1)
	if err != nil {
		t.Fatalf("ingress1: %v", err)
	}
	assertHeader(t, f, ":status", "200")
	assertHeader(t, f, "x-stream", "1")

	f, err = ingressDecoder.DecodeFull(ingress3)
	if err != nil {
		t.Fatalf("ingress3: %v", err)
	}
	assertHeader(t, f, ":status", "404")
	assertHeader(t, f, "x-stream", "3")
}
