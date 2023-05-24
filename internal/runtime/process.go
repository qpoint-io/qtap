package runtime

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/qpoint-io/qtap/internal/event"
)

type Process struct {
	Name     string
	Args     []string
	Env      []string
	cmd      *exec.Cmd
	stop     bool
	running  bool
	restarts int
	chEvent  chan *event.Op
}

func (p *Process) Start() error {
	// create an event channel if doesn't exist
	if p.chEvent == nil {
		p.chEvent = make(chan *event.Op)
	}

	// init a command
	cmd := exec.Command(p.Name, p.Args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// merge environment variables
	if p.Env != nil {
		cmd.Env = append(os.Environ(), p.Env...)
	}

	// start the process
	err := cmd.Start()
	if err != nil {
		return err
	}

	// attach meta
	p.cmd = cmd

	// bump restarts
	if p.running {
		p.restarts += 1
	}

	// set initial state
	if !p.running {
		p.restarts = 0
		p.running = true
	}

	// watch for process to exit
	go p.monitor()

	// handle events
	go p.run()

	fmt.Printf("Process %s started (PID: %d)\n", p.Name, cmd.Process.Pid)

	return nil
}

func (p *Process) Stop() error {
	// create the 'stop' event
	event := &event.Op{
		Action: "stop",
		Res:    make(chan error),
	}

	// send the event
	p.chEvent <- event

	// now wait until we get a response back
	return <-event.Res
}

func (p *Process) monitor() {
	// wait for command to end
	p.cmd.Wait()

	// was it supposed to be stopped?
	if !p.stop {
		// send restart
		p.chEvent <- &event.Op{
			Action: "restart",
			Res:    make(chan error),
		}
	}
}

func (p *Process) run() {
	for event := range p.chEvent {
		// stop
		if event.Action == "stop" {
			event.Res <- p.shutdown()
			return
		}

		// restart
		if event.Action == "restart" {
			p.Start()
		}
	}
}

func (p *Process) shutdown() error {
	// state state so the watcher doesn't try to restart
	p.stop = true

	// send the sigint
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		return err
	}

	// wait
	p.cmd.Process.Wait()

	// log
	fmt.Printf("Process %s stopped\n", p.Name)

	return nil
}
