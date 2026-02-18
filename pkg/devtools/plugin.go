package devtools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/qpoint-io/qtap/pkg/config"
	"github.com/qpoint-io/qtap/pkg/plugins"
	"github.com/qpoint-io/qtap/pkg/plugins/httpcapture"
	"github.com/qpoint-io/qtap/pkg/services"
	"github.com/qpoint-io/qtap/pkg/services/eventstore"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

/*
	The devtools plugin uses the `http_capture` plugin to broadcast all http transactions to any subscribed clients.
*/

const (
	PluginTypeDevTools plugins.PluginType = "devtools"
)

var httpCaptureConfig yaml.Node

func init() {
	err := httpCaptureConfig.Encode(&httpcapture.HttpCaptureConfig{
		Level:  httpcapture.CaptureLevelFull,
		Format: httpcapture.OutputFormatJSON,
		ObjectStoreSelector: config.ObjectStoreSelector{
			ID: "devtools",
		},
	})
	if err != nil {
		panic(err)
	}
}

type PluginFactory struct {
	httpCapture *httpcapture.Factory
	logger      *zap.Logger
}

func (f *PluginFactory) Init(logger *zap.Logger, config yaml.Node) {
	// initialize the http_capture plugin and pass our own custom config
	f.httpCapture.Init(
		logger.With(zap.Stringer("plugin", httpcapture.PluginTypeHttpCapture)),
		httpCaptureConfig,
	)
}

func (f *PluginFactory) NewHttpInstance(conn plugins.PluginContext, svcs *services.ServiceRegistry) plugins.HttpPluginInstance {
	f.logger.Debug("new plugin instance created")

	// TODO: should we return a no-op plugin instance if there are no connected devtools clients?
	return f.httpCapture.NewHttpInstance(conn, svcs)
}

func (f *PluginFactory) NewRedisInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.RedisPluginInstance {
	f.logger.Debug("new devtools Redis plugin instance created")

	es, err := services.GetService[eventstore.EventStore](ctx.Context(), svcs, eventstore.TypeEventStore, "devtools")
	if err != nil {
		f.logger.Error("failed to get devtools event store for Redis", zap.Error(err))
		return nil
	}

	return &devtoolsRedisInstance{
		logger:     f.logger,
		ctx:        ctx,
		eventstore: es,
	}
}

func (f *PluginFactory) NewMySQLInstance(ctx plugins.PluginContext, svcs *services.ServiceRegistry) plugins.MySQLPluginInstance {
	f.logger.Debug("new devtools MySQL plugin instance created")

	es, err := services.GetService[eventstore.EventStore](ctx.Context(), svcs, eventstore.TypeEventStore, "devtools")
	if err != nil {
		f.logger.Error("failed to get devtools event store for MySQL", zap.Error(err))
		return nil
	}

	return &devtoolsMySQLInstance{
		logger:     f.logger,
		ctx:        ctx,
		eventstore: es,
	}
}

func (f *PluginFactory) Destroy() {}

func (f *PluginFactory) PluginType() plugins.PluginType {
	return PluginTypeDevTools
}

// devtoolsRedisInstance captures Redis commands and saves them as DatabaseRequests
// to the devtools eventstore, which broadcasts them via SSE.
type devtoolsRedisInstance struct {
	logger     *zap.Logger
	ctx        plugins.PluginContext
	eventstore eventstore.EventStore

	command *plugins.RedisCommand
	result  *plugins.RedisResult
}

func (r *devtoolsRedisInstance) OnRedisCommand(cmd *plugins.RedisCommand) plugins.RedisStatus {
	r.command = cmd
	return plugins.RedisStatusContinue
}

func (r *devtoolsRedisInstance) OnRedisResult(res *plugins.RedisResult) plugins.RedisStatus {
	r.result = res
	return plugins.RedisStatusContinue
}

func (r *devtoolsRedisInstance) Destroy() {
	if r.command == nil {
		return
	}

	meta := r.ctx.Meta()
	req := &eventstore.DatabaseRequest{
		Timestamp:    time.Now().UTC(),
		Direction:    meta.Direction(),
		DatabaseType: "redis",
		Statement:    r.buildStatement(r.command),
		WrBytes:      meta.WriteBytes(),
		RdBytes:      meta.ReadBytes(),
	}

	if !r.command.Timestamp.IsZero() {
		duration := time.Since(r.command.Timestamp).Milliseconds()
		if duration == 0 {
			duration = 1
		}
		req.Duration = duration
	}

	if r.result != nil {
		req.ResultType = r.result.Type
		req.IsError = r.result.IsError
		if r.result.IsError {
			req.ErrorMsg = fmt.Sprintf("%v", r.result.Value)
		}
	}

	req.SetConnectionID(meta.ConnectionID())
	req.SetRequestID(meta.RequestID())

	if p := meta.Process(); p != nil {
		req.Process = &eventstore.DatabaseRequestProcess{
			Exe: p.Exe,
			Pid: p.Pid,
		}
		if c, _ := p.Container(); c != nil {
			req.Process.ContainerName = c.Name
			req.Process.ContainerImage = c.Image
		}
		if pod, _ := p.Pod(); pod != nil {
			req.Process.PodName = pod.Name
			req.Process.PodNamespace = pod.Namespace
		}
	}

	r.eventstore.Save(context.TODO(), req)
}

func (r *devtoolsRedisInstance) buildStatement(cmd *plugins.RedisCommand) string {
	if len(cmd.Args) == 0 {
		return cmd.Name
	}
	return fmt.Sprintf("%s %s", cmd.Name, strings.Join(cmd.Args, " "))
}

// devtoolsMySQLInstance captures MySQL commands and saves them as DatabaseRequests
type devtoolsMySQLInstance struct {
	logger     *zap.Logger
	ctx        plugins.PluginContext
	eventstore eventstore.EventStore

	command *plugins.MySQLCommand
	result  *plugins.MySQLResult
}

func (m *devtoolsMySQLInstance) OnMySQLCommand(cmd *plugins.MySQLCommand) plugins.MySQLStatus {
	m.command = cmd
	return plugins.MySQLStatusContinue
}

func (m *devtoolsMySQLInstance) OnMySQLResult(res *plugins.MySQLResult) plugins.MySQLStatus {
	m.result = res
	return plugins.MySQLStatusContinue
}

func (m *devtoolsMySQLInstance) Destroy() {
	if m.command == nil {
		return
	}

	meta := m.ctx.Meta()
	req := &eventstore.DatabaseRequest{
		Timestamp:    time.Now().UTC(),
		Direction:    meta.Direction(),
		DatabaseType: "mysql",
		Statement:    m.command.Query,
		WrBytes:      meta.WriteBytes(),
		RdBytes:      meta.ReadBytes(),
	}

	if !m.command.Timestamp.IsZero() {
		duration := time.Since(m.command.Timestamp).Milliseconds()
		if duration == 0 {
			duration = 1
		}
		req.Duration = duration
	}

	if m.result != nil {
		req.ResultType = m.result.Type
		req.IsError = m.result.Type == "Error"
		req.AffectedCount = int64(m.result.AffectedRows) //nolint:gosec
		req.ResultCount = int64(m.result.RowCount)
		if m.result.ErrorMessage != "" {
			req.ErrorMsg = m.result.ErrorMessage
		}
	}

	req.SetConnectionID(meta.ConnectionID())
	req.SetRequestID(meta.RequestID())

	if p := meta.Process(); p != nil {
		req.Process = &eventstore.DatabaseRequestProcess{
			Exe: p.Exe,
			Pid: p.Pid,
		}
		if c, _ := p.Container(); c != nil {
			req.Process.ContainerName = c.Name
			req.Process.ContainerImage = c.Image
		}
		if pod, _ := p.Pod(); pod != nil {
			req.Process.PodName = pod.Name
			req.Process.PodNamespace = pod.Namespace
		}
	}

	m.eventstore.Save(context.TODO(), req)
}
