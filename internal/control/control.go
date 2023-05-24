package control

import (
	"fmt"

	"github.com/qpoint-io/qtap/internal/download"
	"github.com/qpoint-io/qtap/internal/event"
	"github.com/qpoint-io/qtap/internal/proxy"
	"github.com/qpoint-io/qtap/internal/runtime"
	"github.com/qpoint-io/qtap/internal/watch"
)

// qpoint bundle version
type bundle struct {
	id       string
	location string
	port     int
	proc     runtime.Stoppable
}

type App struct {
	// components
	watch.Watcher
	download.Downloader
	proxy.Forwarder
	runtime.Runtime

	// internal
	chEvent   chan *event.Op
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

	// start the proxy
	if err := a.Forwarder.Start(); err != nil {
		return fmt.Errorf("failed to start the proxy: %w", err)
	}

	// create an event channel
	a.chEvent = make(chan *event.Op)

	// run internal loop in a goroutine
	go a.run()

	// drop the initial version
	a.chVersion <- "latest"

	return nil
}

func (a *App) Stop() error {
	// create the 'stop' event
	event := &event.Op{
		Action: "stop",
		Res:    make(chan error),
	}

	// send the event
	a.chEvent <- event

	// now wait until we get a response back
	return <-event.Res
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
			if event.Action == "stop" {
				event.Res <- a.stop()
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

	// update proxy
	if a.version != nil {
		err = a.Forwarder.Forward(fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return fmt.Errorf("failed to update proxy: %w", err)
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
	if a.version != nil {
		if err := a.version.proc.Stop(); err != nil {
			return fmt.Errorf("unable to stop running bundle: %w", err)
		}
	}

	// stop the proxy
	if err := a.Forwarder.Stop(); err != nil {
		return fmt.Errorf("unable to stop the proxy: %w", err)
	}

	// stop the watcher
	if err := a.Watcher.Stop(); err != nil {
		return fmt.Errorf("unable to stop the version watcher: %w", err)
	}

	return nil
}
