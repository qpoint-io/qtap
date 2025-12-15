package telemetry

import (
	"context"
	"fmt"
	"reflect"
	"time"
)

// remoteCancelCtx is a context that is a child of a parent context but has a different cancel function.
// Deadline, Done, and Err are delegated to the cancel context.
// Value is delegated to the parent context.
type remoteCancelCtx struct {
	parentCtx context.Context
	cancelCtx context.Context
}

func (c remoteCancelCtx) Deadline() (deadline time.Time, ok bool) {
	return c.cancelCtx.Deadline()
}

func (c remoteCancelCtx) Done() <-chan struct{} {
	return c.cancelCtx.Done()
}

func (c remoteCancelCtx) Err() error {
	return c.cancelCtx.Err()
}

func (c remoteCancelCtx) Value(key any) any {
	return c.parentCtx.Value(key)
}

func (c remoteCancelCtx) String() string {
	return fmt.Sprintf("%s.WithRemoteCancel(%s)", contextName(c.parentCtx), contextName(c.cancelCtx))
}

// copied from context/context.go
func contextName(c context.Context) string {
	if s, ok := c.(fmt.Stringer); ok {
		return s.String()
	}
	return reflect.TypeOf(c).String()
}
