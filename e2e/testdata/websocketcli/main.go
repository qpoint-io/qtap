// websocketcli is tailored CLI used by TestHTTP_WebSocket in e2e/http_test.go.
//
// usage: websocketcli (ws|wss)://<ws-url>
//
// It performs a scripted WebSocket connection and prints output that is then
// verified by the test.
//
// The expected flow is as such:
//  1. Server sends "Hello", "World", "!" as separate messages with a 500ms delay
//     between each.
//  2. Client sends "Hello from client"
//  3. Client closes connection with code 1000
//
//go:debug http2server=2
//go:debug http2client=2
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"golang.org/x/net/websocket"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) != 2 {
		log.Fatal("usage: websocketcli <ws-url>")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Println("dialing")
	url := os.Args[1]
	conf, err := websocket.NewConfig(url, url)
	if err != nil {
		log.Fatal("config err:", err)
	}
	conf.TlsConfig = &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"http/1.1"},
	}
	ws, err := conf.DialContext(ctx)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer ws.Close()

	var sentMessage bool
	// Read messages until connection is closed
	for {
		log.Println("receiving")
		var message string
		err := websocket.Message.Receive(ws, &message)
		if errors.Is(err, io.EOF) {
			fmt.Println("EOF")
			break
		} else if err != nil {
			log.Fatalf("error: %v", err)
		}
		fmt.Printf("-> %s\n", message)

		if message == "!" {
			err := websocket.Message.Send(ws, "Hello from client")
			if err != nil {
				log.Fatalf("error: %v", err)
			}
			sentMessage = true
			log.Println("sending close")
			ws.WriteClose(1000)
		}
	}
	if !sentMessage {
		log.Fatal("did not send message")
	}
	fmt.Println("done")
}
