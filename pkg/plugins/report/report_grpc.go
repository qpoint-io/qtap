package report

import (
	"context"

	"github.com/qpoint-io/qtap/pkg/plugins/tools"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
)

// grpcFilterInstance embeds filterInstance to reuse all HTTP plugin hook methods
// and shared helpers. Only Destroy() is overridden to emit a GrpcRequest instead
// of a plain Request.
type grpcFilterInstance struct {
	filterInstance
}

func (h *grpcFilterInstance) Destroy() {
	base := h.buildBaseRequest()

	rhm := tools.NewHeaderMap(h.resheaders)
	hm := tools.NewHeaderMap(h.reqheaders)
	grpcService, grpcMethod := hm.GRPCServiceMethod()
	grpcStatus, _ := rhm.Get("Grpc-Status")
	grpcStatusName, _ := rhm.Get("Grpc-Status-Name")
	grpcMessage, _ := rhm.Get("Grpc-Message")

	r := eventstore.GrpcRequest{
		Request:        base,
		GrpcService:    grpcService,
		GrpcMethod:     grpcMethod,
		GrpcStatus:     grpcStatus,
		GrpcStatusName: grpcStatusName,
		GrpcMessage:    grpcMessage,
	}

	h.eventstore.Save(context.TODO(), &r)
	h.logger.Debug("gRPC plugin instance destroyed")
}
