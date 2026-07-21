package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type testConfigProvider struct {
	callback func(*Config) (func(), error)
}

func (p *testConfigProvider) Start() error { return nil }
func (p *testConfigProvider) Stop()        {}
func (p *testConfigProvider) Reload() error {
	return nil
}
func (p *testConfigProvider) OnConfigChange(callback func(*Config) (func(), error)) {
	p.callback = callback
}
func (p *testConfigProvider) apply(cfg *Config) error {
	wait, err := p.callback(cfg)
	if err != nil {
		return err
	}
	if wait != nil {
		wait()
	}
	return nil
}

type configSetterFunc func(*Config)

func (f configSetterFunc) SetConfig(cfg *Config) { f(cfg) }

type configApplierFunc func(*Config) error

func (f configApplierFunc) SetConfig(*Config) {}
func (f configApplierFunc) ApplyConfig(cfg *Config) error {
	return f(cfg)
}

func testGeneration(name string) *Config {
	return &Config{Stacks: map[string]Stack{name: {}}}
}

func requireBlocked(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
		t.Fatal("operation returned before configuration application completed")
	case <-time.After(25 * time.Millisecond):
	}
}

func TestConfigManagerLateSubscribeSetterWaitsForCurrentConfig(t *testing.T) {
	provider := &testConfigProvider{}
	manager := NewConfigManager(zap.NewNop(), provider)
	cfg := testGeneration("current")
	require.NoError(t, provider.apply(cfg))

	entered := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan struct{})
	gotConfig := make(chan *Config, 1)
	subscribeErr := make(chan error, 1)
	go func() {
		subscribeErr <- manager.SubscribeSetter(configSetterFunc(func(got *Config) {
			gotConfig <- got
			close(entered)
			<-release
		}))
		close(returned)
	}()

	<-entered
	require.Same(t, cfg, <-gotConfig)
	requireBlocked(t, returned)
	close(release)
	<-returned
	require.NoError(t, <-subscribeErr)
}

func TestConfigManagerAppliesSettersInRegistrationOrder(t *testing.T) {
	provider := &testConfigProvider{}
	manager := NewConfigManager(zap.NewNop(), provider)
	cfg := testGeneration("ordered")

	firstEntered := make(chan struct{})
	secondEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var mu sync.Mutex
	var order []string
	require.NoError(t, manager.SubscribeSetter(configSetterFunc(func(*Config) {
		mu.Lock()
		order = append(order, "first")
		mu.Unlock()
		close(firstEntered)
		<-releaseFirst
	})))
	require.NoError(t, manager.SubscribeSetter(configSetterFunc(func(*Config) {
		mu.Lock()
		order = append(order, "second")
		mu.Unlock()
		close(secondEntered)
	})))

	done := make(chan struct{})
	applyErr := make(chan error, 1)
	go func() {
		applyErr <- provider.apply(cfg)
		close(done)
	}()
	<-firstEntered
	requireBlocked(t, secondEntered)
	close(releaseFirst)
	<-done
	require.NoError(t, <-applyErr)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"first", "second"}, order)
}

func TestConfigManagerAppliesFallibleSubscribersBeforeOrdinarySetters(t *testing.T) {
	provider := &testConfigProvider{}
	manager := NewConfigManager(zap.NewNop(), provider)
	rejected := testGeneration("rejected")
	accepted := testGeneration("accepted")
	applyErr := errors.New("reject generation")
	var order []string
	ordinaryApplications := 0

	require.NoError(t, manager.SubscribeSetter(configSetterFunc(func(*Config) {
		ordinaryApplications++
		order = append(order, "ordinary")
	})))
	require.NoError(t, manager.SubscribeSetter(configApplierFunc(func(cfg *Config) error {
		order = append(order, "applier")
		if cfg == rejected {
			return applyErr
		}
		return nil
	})))

	require.ErrorIs(t, provider.apply(rejected), applyErr)
	require.Zero(t, ordinaryApplications)
	require.Nil(t, manager.GetConfig())
	require.Equal(t, []string{"applier"}, order)

	order = nil
	require.NoError(t, provider.apply(accepted))
	require.Equal(t, 1, ordinaryApplications)
	require.Same(t, accepted, manager.GetConfig())
	require.Equal(t, []string{"applier", "ordinary"}, order)
}

func TestConfigManagerConcurrentUpdatesCannotOvertake(t *testing.T) {
	provider := &testConfigProvider{}
	manager := NewConfigManager(zap.NewNop(), provider)
	first := testGeneration("first")
	second := testGeneration("second")

	firstEntered := make(chan struct{})
	secondApplied := make(chan struct{})
	releaseFirst := make(chan struct{})
	var mu sync.Mutex
	var order []string
	require.NoError(t, manager.SubscribeSetter(configSetterFunc(func(cfg *Config) {
		switch cfg {
		case first:
			close(firstEntered)
			<-releaseFirst
			mu.Lock()
			order = append(order, "first")
			mu.Unlock()
		case second:
			mu.Lock()
			order = append(order, "second")
			mu.Unlock()
			close(secondApplied)
		}
	})))

	firstDone := make(chan struct{})
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- provider.apply(first)
		close(firstDone)
	}()
	<-firstEntered

	secondDone := make(chan struct{})
	secondErr := make(chan error, 1)
	go func() {
		secondErr <- provider.apply(second)
		close(secondDone)
	}()
	requireBlocked(t, secondApplied)
	close(releaseFirst)
	<-firstDone
	<-secondDone
	require.NoError(t, <-firstErr)
	require.NoError(t, <-secondErr)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"first", "second"}, order)
	require.Same(t, second, manager.GetConfig())
}

func TestConfigManagerPublishesAfterSettersComplete(t *testing.T) {
	provider := &testConfigProvider{}
	manager := NewConfigManager(zap.NewNop(), provider)
	current := testGeneration("current")
	candidate := testGeneration("candidate")
	require.NoError(t, provider.apply(current))

	entered := make(chan struct{})
	release := make(chan struct{})
	require.NoError(t, manager.SubscribeSetter(configSetterFunc(func(cfg *Config) {
		if cfg == candidate {
			close(entered)
			<-release
		}
	})))

	done := make(chan struct{})
	applyErr := make(chan error, 1)
	go func() {
		applyErr <- provider.apply(candidate)
		close(done)
	}()
	<-entered
	require.Same(t, current, manager.GetConfig())
	close(release)
	<-done
	require.NoError(t, <-applyErr)
	require.Same(t, candidate, manager.GetConfig())
}

func TestConfigManagerRejectsInvalidCandidate(t *testing.T) {
	provider := &testConfigProvider{}
	manager := NewConfigManager(zap.NewNop(), provider)
	current := testGeneration("current")
	require.NoError(t, provider.apply(current))

	applications := 0
	require.NoError(t, manager.SubscribeSetter(configSetterFunc(func(*Config) { applications++ })))
	invalid := &Config{Tags: []Tag{{Key: "incomplete"}}}
	require.Error(t, provider.apply(invalid))
	require.Same(t, current, manager.GetConfig())
	require.Equal(t, 1, applications)
}

func TestLocalProviderFailedReloadRetainsCurrentGeneration(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{name: "parse", data: "["},
		{name: "validation", data: "tags:\n  - key: incomplete\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o600))
			provider := NewLocalConfigProvider(zap.NewNop(), path)
			manager := NewConfigManager(zap.NewNop(), provider)
			applications := 0
			require.NoError(t, manager.SubscribeSetter(configSetterFunc(func(*Config) { applications++ })))
			require.NoError(t, provider.Start())
			current := manager.GetConfig()

			require.NoError(t, os.WriteFile(path, []byte(tc.data), 0o600))
			require.Error(t, provider.Reload())
			require.Same(t, current, manager.GetConfig())
			require.Equal(t, 1, applications)
		})
	}
}

func TestLocalAndDefaultProviderReloadWaitsForCallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("{}\n"), 0o600))

	local := NewLocalConfigProvider(zap.NewNop(), path)
	defaultProvider := NewDefaultConfigProvider(zap.NewNop(), false)
	defaultProvider.OnConfigChange(func(*Config) (func(), error) { return func() {}, nil })
	require.NoError(t, defaultProvider.Start())

	for name, provider := range map[string]ConfigProvider{
		"local":   local,
		"default": defaultProvider,
	} {
		t.Run(name, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			provider.OnConfigChange(func(*Config) (func(), error) {
				return func() {
					close(entered)
					<-release
				}, nil
			})

			done := make(chan struct{})
			reloadErr := make(chan error, 1)
			go func() {
				reloadErr <- provider.Reload()
				close(done)
			}()
			<-entered
			requireBlocked(t, done)
			close(release)
			<-done
			require.NoError(t, <-reloadErr, name+" reload")
		})
	}
}
