package plugins

type Process struct {
	PID int
	Exe string
}

// Add process started exited events for devtools

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

	Process   *Process
	Container *Container
}

func (ConnectionOpened) Topic() string { return "connection.opened" }

type ConnectionClosed struct {
	ID string
}

func (ConnectionClosed) Topic() string { return "connection.closed" }
