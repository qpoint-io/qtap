package pulse

import (
	"maps"
	"slices"

	typev1 "github.com/qpoint-io/proto/gen/go/qpoint/type/v1"
	"github.com/qpoint-io/qtap/pkg/qnet"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"github.com/qpoint-io/qtap/pkg/tlsutils"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func pbRequest(req *eventstore.Request) *typev1.Request {
	return typev1.Request_builder{
		Timestamp:     timestamppb.New(req.Timestamp),
		Direction:     req.Direction,
		ConnectionId:  req.ConnectionID,
		EndpointId:    req.EndpointId,
		Id:            req.RequestId,
		Url:           req.Url,
		Path:          req.URLPath,
		Method:        req.Method,
		Status:        uint32(req.Status),
		Duration:      uint64(req.Duration),
		ContentType:   req.ContentType,
		Category:      req.Category,
		Agent:         req.Agent,
		Tags:          req.Tags,
		BytesReceived: uint64(req.RdBytes),
		BytesSent:     uint64(req.WrBytes),
		AuthToken: typev1.Request_AuthToken_builder{
			Mask:   req.AuthTokenMask,
			Hash:   req.AuthTokenHash,
			Source: req.AuthTokenSource,
			Type:   req.AuthTokenType,
		}.Build(),
	}.Build()
}

func pbIssue(issue *eventstore.Issue) *typev1.Issue {
	triggers := make([]*typev1.Issue_Trigger, len(issue.TriggerConditions))
	for i, c := range issue.TriggerConditions {
		var desc string
		if len(issue.TriggerReasons) > i {
			desc = issue.TriggerReasons[i]
		}
		triggers[i] = typev1.Issue_Trigger_builder{
			Plugin:      string(c.Plugin),
			Condition:   c.Condition,
			Description: desc,
		}.Build()
	}

	return typev1.Issue_builder{
		Timestamp:    timestamppb.New(issue.Timestamp),
		Direction:    issue.Direction,
		ConnectionId: issue.ConnectionID,
		EndpointId:   issue.EndpointId,
		RequestId:    issue.RequestId,
		Error:        issue.Error,
		Url:          issue.URL,
		Path:         issue.URLPath,
		Method:       issue.Method,
		Status:       uint32(issue.Status),
		Tags:         issue.Tags,
		Triggers:     triggers,
	}.Build()
}

func pbArtifactRecord(artifact *eventstore.ArtifactRecord) *typev1.Artifact {
	summary, _ := structpb.NewStruct(artifact.Summary)
	return typev1.Artifact_builder{
		Timestamp:    timestamppb.New(artifact.Timestamp),
		ConnectionId: artifact.ConnectionID,
		EndpointId:   artifact.EndpointId,
		RequestId:    artifact.RequestId,
		Type:         string(artifact.Type),
		Digest:       artifact.Digest,
		Url:          artifact.URL,
		Summary:      summary,
		Tags:         artifact.Tags,
	}.Build()
}

func pbPIIEntity(pii *eventstore.PIIEntity) *typev1.PIIEntity {
	return typev1.PIIEntity_builder{
		Timestamp:     timestamppb.New(pii.Timestamp),
		ConnectionId:  pii.ConnectionID,
		EndpointId:    pii.EndpointId,
		RequestId:     pii.RequestId,
		Tags:          pii.Tags,
		EntityType:    pii.EntityType,
		Score:         pii.Score,
		Source:        pii.EntitySource,
		FieldPath:     pii.FieldPath,
		ValueHash:     pii.ValueHash,
		RequestMethod: pii.RequestMethod,
		RequestPath:   pii.RequestPath,
	}.Build()
}

func pbDatabaseRequest(req *eventstore.DatabaseRequest) *typev1.DatabaseRequest {
	return typev1.DatabaseRequest_builder{
		Id:            req.RequestId,
		Timestamp:     timestamppb.New(req.Timestamp),
		Direction:     req.Direction,
		ConnectionId:  req.ConnectionID,
		EndpointId:    req.EndpointId,
		DatabaseType:  req.DatabaseType,
		Statement:     req.Statement,
		ResultType:    req.ResultType,
		IsError:       req.IsError,
		ErrorMsg:      req.ErrorMsg,
		AffectedCount: req.AffectedCount,
		ResultCount:   req.ResultCount,
		Duration:      uint64(req.Duration),
		BytesReceived: uint64(req.RdBytes),
		BytesSent:     uint64(req.WrBytes),
		Tags:          req.Tags,
	}.Build()
}

func pbGrpcRequest(req *eventstore.GrpcRequest) *typev1.GrpcRequest {
	return typev1.GrpcRequest_builder{
		Request:        pbRequest(&req.Request),
		GrpcService:    req.GrpcService,
		GrpcMethod:     req.GrpcMethod,
		GrpcStatus:     req.GrpcStatus,
		GrpcStatusName: req.GrpcStatusName,
		GrpcMessage:    req.GrpcMessage,
	}.Build()
}

func pbConnection(conn *eventstore.Connection) *typev1.Connection {
	c := typev1.Connection_builder{
		Id:                    conn.ConnectionID,
		Timestamp:             timestamppb.New(conn.CreatedAt),
		Finalized:             conn.Finalized,
		Part:                  conn.Part,
		EndpointId:            conn.EndpointId,
		L7Protocol:            pbL7Protocol(conn.L7Protocol),
		BytesReceived:         conn.BytesReceived,
		BytesSent:             conn.BytesSent,
		TlsVersion:            pbTlsVersion(conn.TLSVersion),
		TlsProbeTypesDetected: conn.TLSProbeTypesDetected,
		TlsProbeIntrospected:  conn.TLSIntrospected,
		SocketProtocol:        pbSocketProtocol(conn.SocketProtocol),
		Direction:             pbDirection(conn.Direction),
		Labels:                conn.Labels,
	}

	if conn.Source != nil {
		c.Source = pbConnectionEndpoint(conn.Source)
	}
	if conn.Destination != nil {
		c.Destination = pbConnectionEndpoint(conn.Destination)
	}

	if conn.System != nil {
		c.System = typev1.Connection_System_builder{
			Hostname:      conn.System.Hostname,
			Agent:         conn.System.Agent,
			AgentInstance: conn.System.AgentInstance,
		}.Build()
	}

	if conn.Tags != nil {
		c.Tags = pbTags(conn.Tags)
	}

	return c.Build()
}

func pbConnectionEndpoint(endpoint eventstore.ConnectionEndpoint) *typev1.Connection_Endpoint {
	switch endpoint := endpoint.(type) {
	case *eventstore.ConnectionEndpointLocal:
		return typev1.Connection_Endpoint_builder{
			Local: pbConnectionEndpointLocal(endpoint),
		}.Build()
	case *eventstore.ConnectionEndpointRemote:
		return typev1.Connection_Endpoint_builder{
			Remote: pbConnectionEndpointRemote(endpoint),
		}.Build()
	default:
		return nil
	}
}

func pbConnectionEndpointRemote(endpoint *eventstore.ConnectionEndpointRemote) *typev1.Connection_Endpoint_Remote {
	return typev1.Connection_Endpoint_Remote_builder{
		Address: pbAddress(endpoint.Address),
	}.Build()
}

func pbConnectionEndpointLocal(endpoint *eventstore.ConnectionEndpointLocal) *typev1.Connection_Endpoint_Local {
	e := typev1.Connection_Endpoint_Local_builder{
		Address:  pbAddress(endpoint.Address),
		Hostname: endpoint.Hostname,
		Exe:      endpoint.Exe,
		User:     endpoint.User,
		UserId:   uint32(endpoint.UserID),
	}

	if endpoint.Container != nil {
		e.Container = pbContainer(endpoint.Container)
	}

	return e.Build()
}

func pbAddress(address qnet.NetAddr) *typev1.Address {
	return typev1.Address_builder{
		Ip:   qnet.IPString(address.IP),
		Port: uint32(address.Port),
	}.Build()
}

func pbContainer(container *eventstore.Container) *typev1.Container {
	c := typev1.Container_builder{
		Id:    container.ID,
		Name:  container.Name,
		Image: container.Image,
	}

	if p := container.Pod; p != nil {
		c.Pod = typev1.Pod_builder{
			Name:      p.Name,
			Namespace: p.Namespace,
		}.Build()
	}

	return c.Build()
}

func pbTlsVersion(version tlsutils.TLSVersion) typev1.TlsVersion {
	switch version {
	case tlsutils.VersionTLS10:
		return typev1.TlsVersion_TLS_VERSION_V1_0
	case tlsutils.VersionTLS11:
		return typev1.TlsVersion_TLS_VERSION_V1_1
	case tlsutils.VersionTLS12:
		return typev1.TlsVersion_TLS_VERSION_V1_2
	case tlsutils.VersionTLS13:
		return typev1.TlsVersion_TLS_VERSION_V1_3
	default:
		return typev1.TlsVersion_TLS_VERSION_UNSPECIFIED
	}
}

func pbSocketProtocol(protocol eventstore.SocketProtocol) typev1.SocketProtocol {
	switch protocol {
	case eventstore.SocketProtocol_TCP:
		return typev1.SocketProtocol_SOCKET_PROTOCOL_TCP
	case eventstore.SocketProtocol_UDP:
		return typev1.SocketProtocol_SOCKET_PROTOCOL_UDP
	case eventstore.SocketProtocol_RAW:
		return typev1.SocketProtocol_SOCKET_PROTOCOL_RAW
	case eventstore.SocketProtocol_ICMP:
		return typev1.SocketProtocol_SOCKET_PROTOCOL_ICMP
	default:
		return typev1.SocketProtocol_SOCKET_PROTOCOL_UNSPECIFIED
	}
}

func pbL7Protocol(protocol eventstore.L7Protocol) typev1.L7Protocol {
	switch protocol {
	case eventstore.L7Protocol_HTTP1:
		return typev1.L7Protocol_L7_PROTOCOL_HTTP1
	case eventstore.L7Protocol_HTTP2:
		return typev1.L7Protocol_L7_PROTOCOL_HTTP2
	case eventstore.L7Protocol_DNS:
		return typev1.L7Protocol_L7_PROTOCOL_DNS
	case eventstore.L7Protocol_GRPC:
		return typev1.L7Protocol_L7_PROTOCOL_GRPC
	case eventstore.L7Protocol_MYSQL:
		return typev1.L7Protocol_L7_PROTOCOL_MYSQL
	case "":
		return typev1.L7Protocol_L7_PROTOCOL_UNSPECIFIED
	default:
		return typev1.L7Protocol_L7_PROTOCOL_OTHER
	}
}

func pbTags(tags map[string][]string) []*typev1.Tag {
	if len(tags) == 0 {
		return nil
	}

	protoTags := make([]*typev1.Tag, 0, len(tags))
	for _, k := range slices.Sorted(maps.Keys(tags)) {
		protoTags = append(protoTags, typev1.Tag_builder{
			Key:    k,
			Values: tags[k],
		}.Build())
	}
	return protoTags
}

func pbDirection(direction eventstore.Direction) typev1.Direction {
	switch direction {
	case eventstore.Direction_Ingress:
		return typev1.Direction_DIRECTION_INGRESS
	case eventstore.Direction_Egress:
		return typev1.Direction_DIRECTION_EGRESS
	case eventstore.Direction_EgressInternal:
		return typev1.Direction_DIRECTION_EGRESS_INTERNAL
	case eventstore.Direction_EgressExternal:
		return typev1.Direction_DIRECTION_EGRESS_EXTERNAL
	default:
		return typev1.Direction_DIRECTION_UNSPECIFIED
	}
}
