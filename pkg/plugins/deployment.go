package plugins

import (
	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/log"
	"github.com/qpoint-io/qtap/pkg/services"
	"go.uber.org/zap"
)

// StackDeployment manages the lifecycle of a collection of plugins
// and plugin instances. This is a one off deployment and does not
// support configuration changes.
type StackDeployment struct {
	logger         *zap.Logger
	pluginAccessor PluginAccessor

	name              string
	plugins           []Plugin
	persistentPlugins []config.Plugin
}

func NewStackDeployment(logger *zap.Logger, name string, pluginAccessor PluginAccessor) *StackDeployment {
	return &StackDeployment{
		name:           name,
		logger:         logger,
		pluginAccessor: pluginAccessor,
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

	// initialize the plugins
	for _, cp := range plugins {
		// create a plugin
		plugin := d.pluginAccessor.Get(PluginType(cp.Type))
		if plugin == nil {
			d.logger.Error("plugin not found", zap.String("type", cp.Type))
			continue
		}
		plugin.Init(d.logger.With(zap.String("plugin", cp.Type)), cp.Config)

		// add to the list of plugins
		d.plugins = append(d.plugins, plugin)
	}

	return nil
}

// NewStack creates plugin instances for the given connection type
func (d *StackDeployment) NewStack(connType ConnectionType, ctx PluginContext, svcs *services.ServiceRegistry) []PluginInstance {
	instances := make([]PluginInstance, 0)
	for _, p := range d.plugins {
		switch connType {
		case ConnectionType_HTTP, ConnectionType_GRPC:
			if httpP, ok := p.(HttpPlugin); ok {
				if i := httpP.NewHttpInstance(ctx, svcs); i != nil {
					instances = append(instances, i)
				}
			} else {
				d.logger.Log(log.TraceLevel, "plugin does not support HTTP protocol, skipping",
					zap.Stringer("plugin", p.PluginType()),
					zap.String("connection_type", string(connType)))
			}
		case ConnectionType_REDIS:
			if redisP, ok := p.(RedisPlugin); ok {
				if i := redisP.NewRedisInstance(ctx, svcs); i != nil {
					instances = append(instances, i)
				}
			} else {
				d.logger.Log(log.TraceLevel, "plugin does not support Redis protocol, skipping",
					zap.Stringer("plugin", p.PluginType()),
					zap.String("connection_type", string(connType)))
			}
		case ConnectionType_MYSQL:
			if mysqlP, ok := p.(MySQLPlugin); ok {
				if i := mysqlP.NewMySQLInstance(ctx, svcs); i != nil {
					instances = append(instances, i)
				}
			} else {
				d.logger.Log(log.TraceLevel, "plugin does not support MySQL protocol, skipping",
					zap.Stringer("plugin", p.PluginType()),
					zap.String("connection_type", string(connType)))
			}
		}
	}
	return instances
}

func (d *StackDeployment) Teardown() {
	for _, p := range d.plugins {
		p.Destroy()
	}
}
