package plugins

import (
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"go.uber.org/zap"
)

// A collection of plugin instances for a single connection
type StackInstance []HttpPluginInstance

// StackDeployment manages the lifecycle of a collection of plugins
// and plugin instances. This is a one off deployment and does not
// support configuration changes.
type StackDeployment struct {
	// logger
	logger *zap.Logger

	// name
	name string

	// plugins
	plugins []HttpPlugin

	// required services
	requiredServices []services.ServiceType

	// plugin accessor
	pluginAccessor PluginAccessor
}

func NewStackDeployment(logger *zap.Logger, name string, pluginAccessor PluginAccessor) *StackDeployment {
	return &StackDeployment{
		name:           name,
		logger:         logger,
		pluginAccessor: pluginAccessor,
	}
}

func (d *StackDeployment) Setup(conf *config.Stack) error {
	// initilize the plugins
	for _, cp := range conf.Plugins {
		// create an plugin
		plugin := d.pluginAccessor.Get(PluginType(cp.Type))
		if plugin == nil {
			d.logger.Error("plugin not found", zap.String("type", cp.Type))
			continue
		}
		plugin.Init(d.logger.With(zap.String("plugin", cp.Type)), cp.Config)

		// add the required services
		rm := map[services.ServiceType]struct{}{}
		for _, rs := range plugin.RequiredServices() {
			if _, ok := rm[rs]; !ok {
				rm[rs] = struct{}{}
				d.requiredServices = append(d.requiredServices, rs)
			}
		}

		// add to the list of plugins
		d.plugins = append(d.plugins, plugin)
	}

	return nil
}

func (d *StackDeployment) NewInstance(connection *Connection) StackInstance {
	instances := make(StackInstance, 0, len(d.plugins))
	for _, p := range d.plugins {
		instances = append(instances, p.NewInstance(connection.Context(), connection.services...))
	}
	return instances
}

func (d *StackDeployment) Teardown() {
	for _, p := range d.plugins {
		p.Destroy()
	}
}
