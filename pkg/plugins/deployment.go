package plugins

import (
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/rulekit/set"
	"go.uber.org/zap"
)

// A collection of plugin instances for a single connection
type StackInstance []HttpPluginInstance

// StackDeployment manages the lifecycle of a collection of plugins
// and plugin instances. This is a one off deployment and does not
// support configuration changes.
type StackDeployment struct {
	logger         *zap.Logger
	pluginAccessor PluginAccessor

	name              string
	requiredServices  set.Set[services.ServiceType]
	plugins           []HttpPlugin
	persistentPlugins []config.Plugin
}

func NewStackDeployment(logger *zap.Logger, name string, pluginAccessor PluginAccessor) *StackDeployment {
	return &StackDeployment{
		name:             name,
		logger:           logger,
		pluginAccessor:   pluginAccessor,
		requiredServices: set.NewSet[services.ServiceType](),
	}
}

func (d *StackDeployment) SetPersistentPlugins(plugins []config.Plugin) {
	d.persistentPlugins = plugins
}

func (d *StackDeployment) Setup(conf *config.Stack) error {
	plugins := conf.Plugins
	if len(d.persistentPlugins) > 0 {
		plugins = make([]config.Plugin, 0, len(plugins)+len(d.persistentPlugins))
		plugins = append(plugins, conf.Plugins...)
		plugins = append(plugins, d.persistentPlugins...)
	}

	// initilize the plugins
	for _, cp := range plugins {
		// create an plugin
		plugin := d.pluginAccessor.Get(PluginType(cp.Type))
		if plugin == nil {
			d.logger.Error("plugin not found", zap.String("type", cp.Type))
			continue
		}
		plugin.Init(d.logger.With(zap.String("plugin", cp.Type)), cp.Config)

		// add the required services
		d.requiredServices.Add(plugin.RequiredServices()...)

		// add to the list of plugins
		d.plugins = append(d.plugins, plugin)
	}

	return nil
}

func (d *StackDeployment) NewInstance(connection *Connection) StackInstance {
	instances := make(StackInstance, 0, len(d.plugins))
	for _, p := range d.plugins {
		instances = append(instances, p.NewInstance(connection.Context(), connection.services))
	}
	return instances
}

func (d *StackDeployment) Teardown() {
	for _, p := range d.plugins {
		p.Destroy()
	}
}
