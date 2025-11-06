package plugins

import (
	"fmt"
	"sync"

	"github.com/qpoint-io/qtap/pkg/config"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// Stack manages the lifecycle of a StackDeployment
// which contains a list of plugins.
type Stack struct {
	logger            *zap.Logger
	pluginAccessor    PluginAccessor
	persistentPlugins []config.Plugin

	name string

	// activeDeployment
	activeDeployment   *StackDeployment
	inactiveDeployment *StackDeployment

	// stack config snapshot (JSON)
	configSnapshot string

	// mutex
	mu sync.Mutex
}

func NewStack(name string, logger *zap.Logger, pluginAccessor PluginAccessor) *Stack {
	s := &Stack{
		name:           name,
		logger:         logger,
		pluginAccessor: pluginAccessor,
	}

	return s
}

func (s *Stack) SetConfig(conf *config.Stack) error {
	// generate a snapshot of the incoming config
	snapshot, err := yaml.Marshal(conf)
	if err != nil {
		return fmt.Errorf("marshalling stack config: %w", err)
	}

	// if the snapshot is the same, don't do anything
	if s.configSnapshot == string(snapshot) {
		return nil
	}

	// iniialize deployment
	deployment := NewStackDeployment(s.logger, s.name, s.pluginAccessor)
	deployment.SetPersistentPlugins(s.persistentPlugins)
	err = deployment.Setup(conf)
	if err != nil {
		return fmt.Errorf("setting up deployment: %w", err)
	}

	// lock the stack
	s.mu.Lock()
	defer s.mu.Unlock()

	// set the deployments
	if s.inactiveDeployment != nil {
		s.inactiveDeployment.Teardown()
	}
	s.inactiveDeployment = s.activeDeployment
	s.activeDeployment = deployment

	return nil
}

func (s *Stack) SetPersistentPlugins(plugins []config.Plugin) {
	s.persistentPlugins = plugins
}

func (s *Stack) GetActiveDeployment() *StackDeployment {
	return s.activeDeployment
}

func (s *Stack) Teardown() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeDeployment != nil {
		s.activeDeployment.Teardown()
	}
	if s.inactiveDeployment != nil {
		s.inactiveDeployment.Teardown()
	}
}
