package connection

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// ConnectionState represents the lifecycle state of a connection
type ConnectionState int

const (
	// StateOpen indicates the connection is active and processing events
	StateOpen ConnectionState = iota
	// StateClosing indicates a close event was received but cleanup is not complete
	StateClosing
	// StateFinalizing indicates cleanup is in progress (stream processors finishing, etc.)
	StateFinalizing  
	// StateFinalized indicates the connection is ready to be removed from the manager
	StateFinalized
)

func (s ConnectionState) String() string {
	switch s {
	case StateOpen:
		return "open"
	case StateClosing:
		return "closing"
	case StateFinalizing:
		return "finalizing"
	case StateFinalized:
		return "finalized"
	default:
		return "unknown"
	}
}

// ManagedConnection wraps a Connection with lifecycle state management and time tracking
type ManagedConnection struct {
	*Connection
	
	// state management
	mu            sync.RWMutex
	state         ConnectionState
	lastEventTime time.Time
	stateChangedAt time.Time
}

// NewManagedConnection creates a new ManagedConnection wrapping the provided Connection
func NewManagedConnection(conn *Connection) *ManagedConnection {
	now := time.Now()
	return &ManagedConnection{
		Connection:     conn,
		state:         StateOpen,
		lastEventTime: now,
		stateChangedAt: now,
	}
}

// State returns the current connection state (thread-safe)
func (mc *ManagedConnection) State() ConnectionState {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.state
}

// LastEventTime returns when the last event was processed (thread-safe)
func (mc *ManagedConnection) LastEventTime() time.Time {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.lastEventTime
}

// StateChangedAt returns when the current state was entered (thread-safe)
func (mc *ManagedConnection) StateChangedAt() time.Time {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.stateChangedAt
}

// UpdateEventTime updates the last event time to now (thread-safe)
func (mc *ManagedConnection) UpdateEventTime() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.lastEventTime = time.Now()
}

// TransitionTo attempts to transition to a new state, returns true if successful
func (mc *ManagedConnection) TransitionTo(newState ConnectionState) bool {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	// Define valid state transitions
	validTransition := false
	switch mc.state {
	case StateOpen:
		validTransition = newState == StateClosing
	case StateClosing:
		validTransition = newState == StateFinalizing
	case StateFinalizing:
		validTransition = newState == StateFinalized
	case StateFinalized:
		// No transitions allowed from finalized state
		validTransition = false
	}
	
	if !validTransition {
		mc.Connection.logger.Warn("invalid state transition attempted",
			zap.String("from", mc.state.String()),
			zap.String("to", newState.String()))
		return false
	}
	
	oldState := mc.state
	mc.state = newState
	mc.stateChangedAt = time.Now()
	
	mc.Connection.logger.Debug("connection state transition",
		zap.String("from", oldState.String()),
		zap.String("to", newState.String()))
	
	return true
}

// TimeSinceLastEvent returns the duration since the last event was processed
func (mc *ManagedConnection) TimeSinceLastEvent() time.Duration {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return time.Since(mc.lastEventTime)
}

// TimeSinceStateChange returns the duration since entering the current state
func (mc *ManagedConnection) TimeSinceStateChange() time.Duration {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return time.Since(mc.stateChangedAt)
}

// IsIdle returns true if the connection has been idle longer than the specified duration
func (mc *ManagedConnection) IsIdle(timeout time.Duration) bool {
	return mc.TimeSinceLastEvent() > timeout
}

// HasBeenInStateFor returns true if the connection has been in its current state longer than the specified duration
func (mc *ManagedConnection) HasBeenInStateFor(duration time.Duration) bool {
	return mc.TimeSinceStateChange() > duration
}

// CanFinalize returns true if the connection is ready to transition to finalizing
// This checks if stream processors are done and there's no pending work
func (mc *ManagedConnection) CanFinalize() bool {
	if mc.Connection.streamProcessor == nil {
		return true
	}
	return mc.Connection.streamProcessor.Closed()
}

// processCloseEvent handles close event state transitions
func (mc *ManagedConnection) processCloseEvent(_ CloseEvent) {
	mc.Connection.logger.Debug("managed connection processing close event",
		zap.String("current_state", mc.State().String()),
		zap.String("conn_id", mc.Connection.ID()))
	
	// Transition from Open to Closing when we receive a close event
	if mc.State() == StateOpen {
		if mc.TransitionTo(StateClosing) {
			mc.Connection.logger.Debug("connection transitioned to closing state due to close event",
				zap.String("conn_id", mc.Connection.ID()))
		}
	}
}