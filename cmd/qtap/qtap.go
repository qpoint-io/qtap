package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kingpin/v2"
	"github.com/qpoint-io/qtap/internal/control"
	"github.com/qpoint-io/qtap/internal/download"
	"github.com/qpoint-io/qtap/internal/proxy"
	"github.com/qpoint-io/qtap/internal/runtime"
	"github.com/qpoint-io/qtap/internal/watch"
)

var (
	// todo: replace all of this with a registration url/token
	qpointId      = kingpin.Flag("qpoint-id", "Qpoint destination ID").Envar("QPOINT_ID").Required().String()
	downloadToken = kingpin.Flag("download-token", "Token to download release bundles").Envar("DOWNLOAD_TOKEN").Required().String()
	notifyToken   = kingpin.Flag("notify-token", "Token to receive update notifications").Envar("NOTIFY_TOKEN").Required().String()

	listen  = kingpin.Flag("listen", "IP:PORT to listen on").Envar("LISTEN").Default("127.0.0.1:3333").String()
	engine  = kingpin.Flag("runtime", "Javascript runtime").Envar("RUNTIME").Default("deno").String()
	dataDir = kingpin.Flag("data-dir", "Directory to store state").Envar("DATA_DIR").Default("/tmp/qtap").String()
)

func main() {
	// parse flags/env
	kingpin.Parse()

	// initialize a watcher
	ably := &watch.Ably{
		QpointID: *qpointId,
		Token:    *notifyToken,
	}

	// initialize a downloader
	warehouse := &download.Warehouse{
		QpointID: *qpointId,
		Token:    *downloadToken,
		DataDir:  *dataDir,
	}

	// initialize a proxy
	tcpProxy := &proxy.TcpProxy{
		Listen: *listen,
	}

	// initialize a runtime
	jsRuntime := runtime.Factory(*engine)

	// initialize an app controller
	app := &control.App{
		Watcher:    ably,
		Downloader: warehouse,
		Forwarder:  tcpProxy,
		Runtime:    jsRuntime,
		DataDir:    *dataDir,
	}

	// start the app
	if err := app.Start(); err != nil {
		fmt.Printf("Error: failed to start: %s\n", err.Error())
		os.Exit(1)
	}

	// channel to receive os signals for stopping
	interrupt := make(chan os.Signal, 1)

	// forward sigint and sigterm signals
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)

	// wait for interrupt
	<-interrupt

	// cleanup
	if err := app.Stop(); err != nil {
		fmt.Printf("Error: failed to cleanup: %s\n", err.Error())
		os.Exit(1)
	}
}
