package core

func (ProcessStarted) Topic() string { return "process.started" }

type ProcessStarted struct {
	PID         int
	Exe         string
	ContainerID string
}

func (ProcessStopped) Topic() string { return "process.stopped" }

type ProcessStopped struct {
	PID int
}

func (ContainerStarted) Topic() string { return "container.started" }

type ContainerStarted struct {
	ID    string
	Name  string
	Image string
}

func (ContainerStopped) Topic() string { return "container.stopped" }

type ContainerStopped struct {
	ID string
}

type ConnectionOpened struct {
	ID              string
	PID             int
	SourceIP        string
	SourcePort      int
	DestinationIP   string
	DestinationPort int
}

func (ConnectionOpened) Topic() string { return "connection.opened" }

type ConnectionClosed struct {
	ID string
}

func (ConnectionClosed) Topic() string { return "connection.closed" }
