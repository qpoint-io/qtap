package remote

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ably/ably-go/ably"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// AblyUpdater implements RemoteUpdater interface using Ably realtime
type AblyUpdater struct {
	logger        *zap.Logger
	token         string
	client        *ably.Realtime
	mu            sync.Mutex
	subscriptions map[string]func()
	initialized   bool
}

// NewAblyUpdater creates a new Ably-based updater
func NewAblyUpdater(logger *zap.Logger, token string) *AblyUpdater {
	return &AblyUpdater{
		logger:        logger,
		token:         token,
		subscriptions: make(map[string]func()),
	}
}

// Init initializes the Ably client
func (a *AblyUpdater) Init() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.initialized {
		return nil
	}

	client, err := ably.NewRealtime(
		ably.WithKey(a.token),
		ably.WithLogHandler(&ablyLogger{logger: a.logger}),
		ably.WithLogLevel(translateLogLevel(a.logger.Level())),
	)
	if err != nil {
		return fmt.Errorf("connecting to ably realtime: %w", err)
	}

	a.client = client
	a.initialized = true
	return nil
}

// RegisterForUpdates registers a callback for configuration updates
func (a *AblyUpdater) RegisterForUpdates(channelName string, eventName string, callback func()) (unsubscribe func(), err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.initialized {
		if err := a.Init(); err != nil {
			return nil, fmt.Errorf("initializing ably client: %w", err)
		}
	}

	// Create subscription key for tracking
	subKey := channelName + ":" + eventName

	// Check if we're already subscribed
	if _, exists := a.subscriptions[subKey]; exists {
		return nil, errors.New("already subscribed to this channel and event")
	}

	// Connect to the channel
	channel := a.client.Channels.Get(channelName)

	// Subscribe to events
	unsub, err := channel.Subscribe(context.Background(), eventName, func(msg *ably.Message) {
		// Just call the callback - we don't care about the message payload
		callback()
	})
	if err != nil {
		return nil, fmt.Errorf("subscribing to channel %s event %s: %w", channelName, eventName, err)
	}

	// Store unsubscribe function
	a.subscriptions[subKey] = unsub

	// Return unsubscribe function
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()

		if unsub, exists := a.subscriptions[subKey]; exists {
			unsub()
			delete(a.subscriptions, subKey)
		}
	}, nil
}

// Close cleans up all subscriptions
func (a *AblyUpdater) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.initialized {
		return nil
	}

	// Unsubscribe from all channels
	for _, unsub := range a.subscriptions {
		unsub()
	}
	a.subscriptions = make(map[string]func())

	// Close the client
	a.client.Close()
	a.initialized = false

	return nil
}

func translateLogLevel(level zapcore.Level) ably.LogLevel {
	switch level {
	case zapcore.WarnLevel:
		return ably.LogWarning
	case zapcore.ErrorLevel, zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		return ably.LogError
	default:
		return ably.LogNone
	}
}

type ablyLogger struct {
	logger *zap.Logger
}

func (l *ablyLogger) Printf(level ably.LogLevel, format string, v ...any) {
	switch level {
	case ably.LogError:
		l.logger.Error(fmt.Sprintf(format, v...))
	case ably.LogWarning:
		l.logger.Warn(fmt.Sprintf(format, v...))
	case ably.LogInfo:
		l.logger.Info(fmt.Sprintf(format, v...))
	case ably.LogVerbose:
		l.logger.Debug(fmt.Sprintf(format, v...))
	case ably.LogDebug:
		l.logger.Debug(fmt.Sprintf(format, v...))
	}
}
