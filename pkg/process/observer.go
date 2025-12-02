package process

import "context"

type Observer interface {
	ProcessStarted(ctx context.Context, proc *Process) error
	ProcessReplaced(ctx context.Context, proc *Process) error
	ProcessStopped(ctx context.Context, proc *Process) error
}

type DefaultObserver struct{}

func (d *DefaultObserver) ProcessStarted(ctx context.Context, proc *Process) error {
	return nil
}

func (d *DefaultObserver) ProcessReplaced(ctx context.Context, proc *Process) error {
	return nil
}

func (d *DefaultObserver) ProcessStopped(ctx context.Context, proc *Process) error {
	return nil
}
