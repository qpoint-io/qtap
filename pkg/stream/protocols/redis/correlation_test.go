package redis

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandQueue(t *testing.T) {
	q := NewCommandQueue()
	assert.NotNil(t, q)
	assert.Equal(t, 0, q.Len())
}

func TestCommandQueue_EnqueueDequeue(t *testing.T) {
	q := NewCommandQueue()

	cmd1 := &PendingCommand{Command: &Command{Name: "GET", Args: []string{"key1"}}}
	cmd2 := &PendingCommand{Command: &Command{Name: "SET", Args: []string{"key2", "value2"}}}

	q.Enqueue(cmd1)
	assert.Equal(t, 1, q.Len())

	q.Enqueue(cmd2)
	assert.Equal(t, 2, q.Len())

	// Dequeue should return in FIFO order
	result1 := q.Dequeue()
	assert.Equal(t, cmd1, result1)
	assert.Equal(t, 1, q.Len())

	result2 := q.Dequeue()
	assert.Equal(t, cmd2, result2)
	assert.Equal(t, 0, q.Len())

	// Dequeue from empty queue returns nil
	result3 := q.Dequeue()
	assert.Nil(t, result3)
}

func TestCommandQueue_FIFO(t *testing.T) {
	q := NewCommandQueue()

	// Enqueue 100 commands
	for i := range 100 {
		q.Enqueue(&PendingCommand{
			Command:   &Command{Name: fmt.Sprintf("CMD%d", i)},
			Timestamp: time.Now(),
		})
	}

	assert.Equal(t, 100, q.Len())

	// Dequeue and verify FIFO order
	for i := range 100 {
		cmd := q.Dequeue()
		require.NotNil(t, cmd)
		assert.Equal(t, fmt.Sprintf("CMD%d", i), cmd.Command.Name)
	}

	assert.Equal(t, 0, q.Len())
}

func TestCommandQueue_Clear(t *testing.T) {
	q := NewCommandQueue()

	cmd1 := &PendingCommand{Command: &Command{Name: "GET"}}
	cmd2 := &PendingCommand{Command: &Command{Name: "SET"}}
	cmd3 := &PendingCommand{Command: &Command{Name: "DEL"}}

	q.Enqueue(cmd1)
	q.Enqueue(cmd2)
	q.Enqueue(cmd3)

	assert.Equal(t, 3, q.Len())

	// Clear returns all pending commands
	remaining := q.Clear()
	assert.Len(t, remaining, 3)
	assert.Equal(t, cmd1, remaining[0])
	assert.Equal(t, cmd2, remaining[1])
	assert.Equal(t, cmd3, remaining[2])

	// Queue is now empty
	assert.Equal(t, 0, q.Len())

	// Can still enqueue after clear
	q.Enqueue(&PendingCommand{Command: &Command{Name: "PING"}})
	assert.Equal(t, 1, q.Len())
}

func TestCommandQueue_ClearEmpty(t *testing.T) {
	q := NewCommandQueue()

	remaining := q.Clear()
	assert.Empty(t, remaining)
	assert.Equal(t, 0, q.Len())
}

func TestCommandQueue_DequeueEmpty(t *testing.T) {
	q := NewCommandQueue()

	result := q.Dequeue()
	assert.Nil(t, result)

	// Multiple dequeues on empty queue should be safe
	result = q.Dequeue()
	assert.Nil(t, result)
	result = q.Dequeue()
	assert.Nil(t, result)
}

func TestPendingCommand_Timestamp(t *testing.T) {
	before := time.Now()
	cmd := &PendingCommand{
		Timestamp: time.Now(),
	}
	after := time.Now()

	assert.True(t, cmd.Timestamp.After(before) || cmd.Timestamp.Equal(before))
	assert.True(t, cmd.Timestamp.Before(after) || cmd.Timestamp.Equal(after))
}
