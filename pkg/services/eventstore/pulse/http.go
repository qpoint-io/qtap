package pulse

import (
	"net"
	"net/http"
	"time"
)

type http2ClientOptions struct {
	h2c bool
}

func newHTTP2Client(opts http2ClientOptions) *http.Client {
	var protocols http.Protocols
	protocols.SetHTTP1(false)
	protocols.SetHTTP2(true)
	protocols.SetUnencryptedHTTP2(opts.h2c)

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		Protocols:             &protocols,
	}

	return &http.Client{
		Transport: transport,
	}
}
