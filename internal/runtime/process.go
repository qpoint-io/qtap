package runtime

import (
	"fmt"
	"os"
	"os/exec"
)

type procEventOp struct {
	action string     // the requested action (stop/restart/etc)
	res    chan error // the response channel to syncronize state
}

type Process struct {
	Name     string
	Args     []string
	cmd      *exec.Cmd
	stop     bool
	running  bool
	restarts int
	chEvent  chan *procEventOp
}

func (p *Process) Start() error {
	// create an event channel if doesn't exist
	if p.chEvent == nil {
		p.chEvent = make(chan *procEventOp)
	}

	// init a command
	cmd := exec.Command(p.Name, p.Args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

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
	event := &procEventOp{
		action: "stop",
		res:    make(chan error),
	}

	// send the event
	p.chEvent <- event

	// now wait until we get a response back
	return <-event.res
}

func (p *Process) monitor() {
	// wait for command to end
	p.cmd.Wait()

	// was it supposed to be stopped?
	if !p.stop {
		// send restart
		p.chEvent <- &procEventOp{
			action: "restart",
			res:    make(chan error),
		}
	}
}

func (p *Process) run() {
	for event := range p.chEvent {
		// stop
		if event.action == "stop" {
			event.res <- p.shutdown()
			return
		}

		// restart
		if event.action == "restart" {
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
