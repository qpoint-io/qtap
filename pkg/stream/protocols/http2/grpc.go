package http2

import (
	"net/url"

	"golang.org/x/net/http2/hpack"
)

// grpcStatusNames maps gRPC status code strings to human-readable names.
// See https://grpc.github.io/grpc/core/md_doc_statuscodes.html
var grpcStatusNames = map[string]string{
	"0":  "OK",
	"1":  "CANCELLED",
	"2":  "UNKNOWN",
	"3":  "INVALID_ARGUMENT",
	"4":  "DEADLINE_EXCEEDED",
	"5":  "NOT_FOUND",
	"6":  "ALREADY_EXISTS",
	"7":  "PERMISSION_DENIED",
	"8":  "RESOURCE_EXHAUSTED",
	"9":  "FAILED_PRECONDITION",
	"10": "ABORTED",
	"11": "OUT_OF_RANGE",
	"12": "UNIMPLEMENTED",
	"13": "INTERNAL",
	"14": "UNAVAILABLE",
	"15": "DATA_LOSS",
	"16": "UNAUTHENTICATED",
}

// GRPCStatusName returns the human-readable name for a gRPC status code string.
// Returns "UNKNOWN" for unrecognized codes.
func GRPCStatusName(code string) string {
	if name, ok := grpcStatusNames[code]; ok {
		return name
	}
	return "UNKNOWN"
}

// hasStatusHeader checks if a HEADERS frame contains a :status pseudo-header,
// indicating it is a response HEADERS frame (not a request or trailer).
func hasStatusHeader(fields []hpack.HeaderField) bool {
	for _, f := range fields {
		if f.Name == ":status" {
			return true
		}
	}
	return false
}

// isTrailersOnly checks if a HEADERS frame represents a gRPC "Trailers-Only" response.
// A Trailers-Only response has both :status and grpc-status in the same HEADERS frame.
func isTrailersOnly(fields []hpack.HeaderField) bool {
	hasStatus := false
	hasGRPCStatus := false
	for _, f := range fields {
		if f.Name == ":status" {
			hasStatus = true
		}
		if f.Name == "grpc-status" {
			hasGRPCStatus = true
		}
	}
	return hasStatus && hasGRPCStatus
}

// grpcTrailerMeta holds gRPC-specific trailer metadata.
type grpcTrailerMeta struct {
	Status     string // raw grpc-status value (e.g., "0")
	StatusName string // human-readable name (e.g., "OK")
	Message    string // grpc-message (percent-decoded)
}

// extractGRPCTrailers extracts gRPC-specific metadata from trailer header fields.
func extractGRPCTrailers(fields []hpack.HeaderField) grpcTrailerMeta {
	var meta grpcTrailerMeta
	for _, f := range fields {
		switch f.Name {
		case "grpc-status":
			meta.Status = f.Value
			meta.StatusName = GRPCStatusName(f.Value)
		case "grpc-message":
			// gRPC messages are percent-encoded per the spec
			decoded, err := url.QueryUnescape(f.Value)
			if err != nil {
				meta.Message = f.Value // use raw value if decode fails
			} else {
				meta.Message = decoded
			}
		}
	}
	return meta
}
