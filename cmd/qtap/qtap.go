package main

import (
	"fmt"

	"github.com/alecthomas/kingpin/v2"
)

var (
	// todo: replace all of this with a registration url/token
	qpointId      = kingpin.Flag("qpoint-id", "Qpoint destination ID").Envar("QPOINT_ID").Required().String()
	downloadToken = kingpin.Flag("download-token", "Token to download release bundles").Envar("DOWNLOAD_TOKEN").Required().String()
	notifyToken   = kingpin.Flag("notify-token", "Token to receive update notifications").Envar("NOTIFY_TOKEN").Required().String()

	runtime = kingpin.Flag("runtime", "Javascript runtime").Envar("RUNTIME").Default("deno").String()
	dataDir = kingpin.Flag("data-dir", "Directory to store state").Envar("DATA_DIR").Default("/tmp/qtap").String()
)

func main() {
	// parse flags/env
	kingpin.Parse()

	fmt.Printf("qpointId: %s\n", *qpointId)
	fmt.Printf("downloadToken: %s\n", *downloadToken)
	fmt.Printf("notifyToken: %s\n", *notifyToken)
	fmt.Printf("runtime: %s\n", *runtime)
	fmt.Printf("dataDir: %s\n", *dataDir)

	// initialize a watcher

	// initialize a downloader

	// initialize a proxy

	// initialize a runtime

	// initialize a controller

}
