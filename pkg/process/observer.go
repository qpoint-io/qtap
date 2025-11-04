package process

type Observer interface {
	ProcessStarted(*Process) error
	ProcessReplaced(*Process) error
	ProcessStopped(*Process) error
}

type DefaultObserver struct{}

func (d *DefaultObserver) ProcessStarted(proc *Process) error {
	return nil
}

func (d *DefaultObserver) ProcessReplaced(proc *Process) error {
	return nil
}

func (d *DefaultObserver) ProcessStopped(proc *Process) error {
	return nil
}
