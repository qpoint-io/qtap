package redis

import (
	"time"

	"github.com/qpoint-io/qtap/pkg/plugins"
)

// PendingCommand represents a command awaiting its response
type PendingCommand struct {
	Command    *Command
	Timestamp  time.Time
	PluginConn *plugins.Connection // Plugin connection for this specific command
}

// CommandQueue is a simple FIFO queue for pending commands
type CommandQueue struct {
	commands []*PendingCommand
}

// NewCommandQueue creates a new empty command queue
func NewCommandQueue() *CommandQueue {
	return &CommandQueue{
		commands: make([]*PendingCommand, 0, 16), // Pre-allocate for typical pipelining
	}
}

// Enqueue adds a command to the back of the queue
func (q *CommandQueue) Enqueue(cmd *PendingCommand) {
	q.commands = append(q.commands, cmd)
}

// Dequeue removes and returns the command at the front of the queue
// Returns nil if the queue is empty
func (q *CommandQueue) Dequeue() *PendingCommand {
	if len(q.commands) == 0 {
		return nil
	}
	cmd := q.commands[0]
	q.commands = q.commands[1:]
	return cmd
}

// Len returns the number of commands in the queue
func (q *CommandQueue) Len() int {
	return len(q.commands)
}

// Clear removes all commands from the queue and returns them
func (q *CommandQueue) Clear() []*PendingCommand {
	remaining := q.commands
	q.commands = make([]*PendingCommand, 0, 16)
	return remaining
}
