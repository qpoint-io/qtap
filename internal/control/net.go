package control

import (
	"net"
	"strconv"
	"time"
)

func findAvailablePort(host string, start int) int {
	for i := start; ; i++ {
		timeout := time.Second

		// try to connect, only for a second
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(i)), timeout)

		// don't forget to cleanup
		if conn != nil {
			defer conn.Close()
		}

		// no connection, probably gtg
		if conn == nil || err != nil {
			return i
		}
	}
}
