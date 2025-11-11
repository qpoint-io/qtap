package connmeta

import (
	"context"

	"github.com/qpoint-io/qtap/pkg/connection"
	"github.com/qpoint-io/qtap/pkg/process"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/tags"
)

type Service interface {
	services.Service
	ConnectionID() string
	Endpoint() string
	Tags() tags.List
	Direction() string
	Protocol() string
	Process() *process.Process
}

const (
	Type services.ServiceType = "connmeta"
)

type Factory struct{}

func (f *Factory) FactoryType() services.ServiceType {
	return Type
}

func (f *Factory) ServiceType() services.ServiceType {
	return Type
}

func (f *Factory) Init(ctx context.Context, cfg any) error {
	return nil
}

func (f *Factory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	return &service{}, nil
}

type service struct {
	conn *connection.Connection
}

func (s *service) ServiceType() services.ServiceType {
	return Type
}

// SetConnection implements the ConnectionAdapter interface.
func (s *service) SetConnection(conn *connection.Connection) {
	s.conn = conn
}

func (s *service) Tags() tags.List {
	return s.conn.Tags()
}

func (s *service) ConnectionID() string {
	return s.conn.ID()
}

func (s *service) Endpoint() string {
	return s.conn.Domain()
}

func (s *service) Direction() string {
	return s.conn.Direction()
}

func (s *service) Protocol() string {
	return s.conn.Proto()
}

func (s *service) Process() *process.Process {
	return s.conn.Process()
}
