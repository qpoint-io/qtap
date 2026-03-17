package cmd

import (
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/connmeta"
	eventstoreaxiom "github.com/qpoint-io/qtap/pkg/services/eventstore/axiom"
	eventstoreconsole "github.com/qpoint-io/qtap/pkg/services/eventstore/console"
	eventstorenoop "github.com/qpoint-io/qtap/pkg/services/eventstore/noop"
	eventstoreotel "github.com/qpoint-io/qtap/pkg/services/eventstore/otel"
	objectstoreconsole "github.com/qpoint-io/qtap/pkg/services/objectstore/console"
	objectstorenoop "github.com/qpoint-io/qtap/pkg/services/objectstore/noop"
	objectstoreotel "github.com/qpoint-io/qtap/pkg/services/objectstore/otel"
	objecstores3 "github.com/qpoint-io/qtap/pkg/services/objectstore/s3"
	"github.com/qpoint-io/qtap/pkg/services/reporter"
	"github.com/qpoint-io/qtap/pkg/services/rulekitsvc"
)

var (
	serviceFactories = []services.FactoryFn{
		// Eventstore services
		func() services.Factory { return &eventstoreconsole.Factory{} },
		func() services.Factory { return &eventstorenoop.Factory{} },
		func() services.Factory { return &eventstoreaxiom.Factory{} },
		func() services.Factory { return &eventstoreotel.Factory{} },

		// Objectstore services
		func() services.Factory { return &objectstoreconsole.Factory{} },
		func() services.Factory { return &objectstorenoop.Factory{} },
		func() services.Factory { return &objecstores3.Factory{} },
		func() services.Factory { return &objectstoreotel.Factory{} },

		// Add more services here...
		func() services.Factory { return &rulekitsvc.Factory{} },
		func() services.Factory { return &connmeta.Factory{} },
		func() services.Factory { return &reporter.Factory{} },
	}

	// extraServiceConfigs are service configs that are added to all configs.
	extraServiceConfigs = []func(*config.Config) *config.ServiceConfig{
		// rulekit
		func(cfg *config.Config) *config.ServiceConfig {
			return &config.ServiceConfig{
				Type:   rulekitsvc.TypeRulekit.String(),
				Config: cfg.Rulekit,
			}
		},
		// connmeta
		func(cfg *config.Config) *config.ServiceConfig {
			return &config.ServiceConfig{
				Type: connmeta.Type.String(),
			}
		},
		// report connections to the default event store
		func(cfg *config.Config) *config.ServiceConfig {
			return &config.ServiceConfig{
				Type: reporter.Type.String(),
				Config: &reporter.Config{
					FirstReportDeadline: 500 * time.Millisecond,
					ReportInterval:      10 * time.Second,
				},
			}
		},
	}
)
