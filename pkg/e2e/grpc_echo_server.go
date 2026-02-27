package e2e

import (
	"google.golang.org/grpc"
)

// rawCodec is a gRPC codec that passes raw bytes through without
// marshaling/unmarshaling. This lets us build an echo server without
// needing any proto definitions.
type rawCodec struct{}

func (rawCodec) Marshal(v any) ([]byte, error) {
	return v.([]byte), nil
}

func (rawCodec) Unmarshal(data []byte, v any) error {
	*(v.(*[]byte)) = data
	return nil
}

func (rawCodec) Name() string {
	return "raw"
}

// echoHandler is a gRPC stream handler that reads a single message and
// echoes it back. It works for any service/method because it's registered
// via grpc.UnknownServiceHandler.
func echoHandler(srv any, stream grpc.ServerStream) error {
	var msg []byte
	if err := stream.RecvMsg(&msg); err != nil {
		return err
	}
	return stream.SendMsg(msg)
}

// NewGRPCEchoServer creates a gRPC server that echoes any request back as-is.
// It uses ForceServerCodec with raw bytes and UnknownServiceHandler to handle
// any service/method without proto definitions.
func NewGRPCEchoServer(opts ...grpc.ServerOption) *grpc.Server {
	opts = append(opts,
		grpc.ForceServerCodec(rawCodec{}),
		grpc.UnknownServiceHandler(echoHandler),
	)
	return grpc.NewServer(opts...)
}
