package noop

import (
	"context"

	"github.com/qpoint-io/qtap/pkg/services"
)

type Factory struct{}

func (f *Factory) Init(ctx context.Context, cfg any) error {
	return nil
}

func (f *Factory) Create(ctx context.Context, svcRegistry *services.ServiceRegistry) (services.Service, error) {
	return f, nil
}

// ServiceType returns the service type
func (f *Factory) FactoryType() services.ServiceType {
	return services.ServiceType("qscan.noop")
}

func (f *Factory) ServiceType() services.ServiceType {
	return services.ServiceType("qscan")
}

func (f *Factory) ServiceEndpoints() []string {
	return []string{}
}
