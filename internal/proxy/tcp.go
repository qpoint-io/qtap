package proxy

import (
	"fmt"
	"io"
	"net"
	"sync"
)

type TcpProxy struct {
	Listen string

	// internal
	stop       bool
	forward    string
	listener   net.Listener
	lock       sync.RWMutex
	activeConn map[net.Conn]struct{}
	connLock   sync.Mutex
}

func (p *TcpProxy) Start() error {
	// set stop to false
	p.stop = false

	// initialize active connections tracker
	p.activeConn = make(map[net.Conn]struct{})

	// listening for incoming connections
	listener, err := net.Listen("tcp", p.Listen)
	if err != nil {
		return fmt.Errorf("starting listener: %w", err)
	}

	// set listener to close later
	p.listener = listener

	// listen for connections
	go p.listen()

	// display
	fmt.Printf("Qtap listening on http://%s\n", p.listener.Addr())

	return nil
}

func (p *TcpProxy) Forward(to string) error {
	// acquire lock
	p.lock.Lock()
	defer p.lock.Unlock()

	// update the forward location
	p.forward = to

	// close active connections
	p.closeActiveConnections()

	return nil
}

func (p *TcpProxy) Stop() error {
	// acquire lock
	p.lock.Lock()
	defer p.lock.Unlock()

	// set stop to true
	p.stop = true

	// close the listener
	if err := p.listener.Close(); err != nil {
		return fmt.Errorf("closing listener: %w", err)
	}

	return nil
}

func (p *TcpProxy) listen() {
	for {
		// should we stop?
		if p.stop {
			return
		}

		// wait for the next connection
		client, err := p.listener.Accept()
		if err != nil && !p.stop {
			fmt.Printf("Failed to accept connection: %s\n", err)
			continue
		}

		// track the connection
		p.trackConnection(client)

		// proxy in a go-routine
		go p.proxy(client)
	}
}

func (p *TcpProxy) proxy(client net.Conn) {
	p.lock.RLock()
	upstream := p.forward
	p.lock.RUnlock()

	// connect upstream
	remote, err := net.Dial("tcp", upstream)
	if err != nil {
		fmt.Printf("Failed to connect upstream: %s\n", err.Error())
		p.untrackConnection(client)
		client.Close()
	}
	defer remote.Close()

	// create an error channel for io copy errors
	errChan := make(chan error, 2)

	go func() {
		_, err := io.Copy(remote, client)
		errChan <- err
	}()

	go func() {
		_, err := io.Copy(client, remote)
		errChan <- err
	}()

	// wait for errors
	for i := 0; i < 2; i++ {
		// wait for errors
		<-errChan

		// move to debug
		// if err := <-errChan; err != nil {
		// 	fmt.Printf("Error while copying data: %s\n", err.Error())
		// }
	}

	p.untrackConnection(client)
	client.Close()
}

func (p *TcpProxy) trackConnection(conn net.Conn) {
	p.connLock.Lock()
	defer p.connLock.Unlock()

	p.activeConn[conn] = struct{}{}
}

func (p *TcpProxy) untrackConnection(conn net.Conn) {
	p.connLock.Lock()
	defer p.connLock.Unlock()

	delete(p.activeConn, conn)
}

func (p *TcpProxy) closeActiveConnections() {
	p.connLock.Lock()
	defer p.connLock.Unlock()

	for conn := range p.activeConn {
		conn.Close()
		delete(p.activeConn, conn)
	}
}
