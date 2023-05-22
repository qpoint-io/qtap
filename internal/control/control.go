package control

import "fmt"

// version watcher
type watcher interface {
	Watch() (chan string, error)
	Stop() error
}

// bundle downloader
type downloader interface {
	Fetch(version string) (string, error)
}

// network proxy
type proxy interface {
	Start(from, to string) error
	Replace(to string) error
	Stop() error
}

// js/ts runtime
type runtime interface {
	Start(bundle string, listen string) (stoppable, error)
}

// a stoppable process
type stoppable interface {
	Stop() error
}

// qpoint bundle version
type bundle struct {
	id       string
	location string
	port     int
	proc     stoppable
}

// Sending events to the controller needs to block the caller until
// the event handler has completed. To do this we need to create
// a stateful event with an ad-hoc channel to receive the response
type eventOp struct {
	action string     // the requested action (stop/resume/etc)
	res    chan error // the response channel to syncronize state
}

type App struct {
	// components
	Watcher    watcher
	Downloader downloader
	Proxy      proxy
	Runtime    runtime

	// config
	Address string

	// internal
	chEvent   chan *eventOp
	chVersion chan string
	version   *bundle
}

func (a *App) Start() error {
	// start the version watcher
	chVersion, err := a.Watcher.Watch()
	if err != nil {
		return fmt.Errorf("unable to start version watcher: %w", err)
	}

	// set the version watcher on the struct
	a.chVersion = chVersion

	// create an event channel
	a.chEvent = make(chan *eventOp)

	// run internal loop in a goroutine
	go a.run()

	return nil
}

func (a *App) Stop() error {
	// create the 'stop' event
	event := &eventOp{
		action: "stop",
		res:    make(chan error),
	}

	// send the event
	a.chEvent <- event

	// now wait until we get a response back
	return <-event.res
}

func (a *App) run() {
	for {
		select {
		case version := <-a.chVersion:
			if err := a.runVersion(version); err != nil {
				fmt.Printf("Error: failed to run version: %s\n", err.Error())
			}
		case event := <-a.chEvent:
			// stop?
			if event.action == "stop" {
				event.res <- a.stop()
			}
		}
	}
}

func (a *App) runVersion(version string) error {
	// download bundle
	location, err := a.Downloader.Fetch(version)
	if err != nil {
		return fmt.Errorf("failed to fetch bundle version %s: %w", version, err)
	}

	// find an available port
	port := findAvailablePort("127.0.0.1", 11001)

	// run bundle
	proc, err := a.Runtime.Start(location, fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("failed to run bundle %s: %w", location, err)
	}

	// update proxy if already running
	if a.version != nil {
		err = a.Proxy.Replace(fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return fmt.Errorf("failed to update proxy: %w", err)
		}
	}

	// start proxy if not running
	if a.version == nil {
		err = a.Proxy.Start(a.Address, fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return fmt.Errorf("failed to start proxy: %w", err)
		}
	}

	// stop previous version if running
	if a.version != nil {
		err = a.version.proc.Stop()
		if err != nil {
			return fmt.Errorf("failed to start previous process: %w", err)
		}
	}

	// persist new version
	a.version = &bundle{
		id:       version,
		location: location,
		port:     port,
		proc:     proc,
	}

	return nil
}

func (a *App) stop() error {
	// stop the runtime
	if err := a.version.proc.Stop(); err != nil {
		return fmt.Errorf("unable to stop running bundle: %w", err)
	}

	// stop the proxy
	if err := a.Proxy.Stop(); err != nil {
		return fmt.Errorf("unable to stop the proxy: %w", err)
	}

	// stop the watcher
	if err := a.Watcher.Stop(); err != nil {
		return fmt.Errorf("unable to stop the version watcher: %w", err)
	}

	return nil
}
