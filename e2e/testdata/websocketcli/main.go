package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"golang.org/x/net/websocket"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) != 2 {
		log.Fatal("usage: websocketcli <ws-url>")
	}

	log.Println("dialing")
	url := os.Args[1]
	ws, err := websocket.Dial(url, "", "ws://localhost")
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
