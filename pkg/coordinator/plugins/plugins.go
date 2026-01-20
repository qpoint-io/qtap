package plugins

type Process struct {
	PID       int
	Exe       string
	Container *Container
}

func (ProcessStarted) Topic() string { return "process.started" }

type ProcessStarted struct {
	Process *Process
}

func (ProcessStopped) Topic() string { return "process.stopped" }

type ProcessStopped struct {
	PID int
}

type Container struct {
	ID    string
	Name  string
	Image string
}

type ConnectionOpened struct {
	ID              string
	SourceIP        string
	SourcePort      int
	DestinationIP   string
	DestinationPort int

	Process *Process
}

func (ConnectionOpened) Topic() string { return "connection.opened" }

type ConnectionClosed struct {
	ID string
}

func (ConnectionClosed) Topic() string { return "connection.closed" }
