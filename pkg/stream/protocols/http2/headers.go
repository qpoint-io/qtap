package http2

import (
	"fmt"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

// This has be adapted from the http2 framer's readMetaFrame method
// because we need to handle the headers in a different way than the
// http2 framer does.
//
// Original: https://github.com/golang/net/blob/master/http2/frame.go#L1527

const (
	MaxHeaderListSize = 16 << 20 // 16 MB, per http2 spec
	MaxStringLength   = 0        // no limit
)

type headersEnder interface {
	HeadersEnded() bool
}

type headersOrContinuation interface {
	headersEnder
	HeaderBlockFragment() []byte
}

// readMetaFrame returns 0 or more CONTINUATION frames from fr and
// merge them into the provided hf and returns a MetaHeadersFrame
// with the decoded hpack values.
//
// This is a modified version of the http2 framer's readMetaFrame method
// which allows for illegal protocol operations.
func (t *HTTPStream) readMetaFrame(hf *http2.HeadersFrame, framer *http2.Framer, decoder *hpack.Decoder) (*http2.MetaHeadersFrame, error) {
	mh := &http2.MetaHeadersFrame{
		HeadersFrame: hf,
	}
	var remainSize uint32 = MaxHeaderListSize

	// var invalid error // pseudo header field errors
	hdec := decoder
	hdec.SetEmitEnabled(true)
	hdec.SetMaxStringLength(MaxStringLength) // no limit
	hdec.SetEmitFunc(func(hf hpack.HeaderField) {
		// in a normal http2 framer readMetaFrame method, this is where it checks for
		// invalid pseudo headers and regular headers. we're not going to do that.

		size := hf.Size()
		if size > remainSize {
			hdec.SetEmitEnabled(false)
			mh.Truncated = true
			remainSize = 0
			return
		}
		remainSize -= size

		mh.Fields = append(mh.Fields, hf)
	})

	// Lose reference to MetaHeadersFrame:
	defer hdec.SetEmitFunc(func(hf hpack.HeaderField) {})

	var hc headersOrContinuation = hf
	for {
		frag := hc.HeaderBlockFragment()

		// Avoid parsing large amounts of headers that we will then discard.
		// If the sender exceeds the max header list size by too much,
		// skip parsing the fragment and close the connection.
		//
		// "Too much" is either any CONTINUATION frame after we've already
		// exceeded the max header list size (in which case remainSize is 0),
		// or a frame whose encoded size is more than twice the remaining
		// header list bytes we're willing to accept.
		if int64(len(frag)) > int64(2*remainSize) {
			return mh, fmt.Errorf("header list size exceeded: domain=%s, fragment_size %d, remaining_size %d", t.domain, len(frag), remainSize)
		}

		if _, err := hdec.Write(frag); err != nil {
			if hc.HeadersEnded() {
				break
			}

			return mh, fmt.Errorf("failed to write header fragment: domain=%s, fragment_size=%d, remaining_size %d error=%w",
				t.domain,
				len(frag),
				remainSize,
				err)
		}

		if hc.HeadersEnded() {
			break
		}
		if f, err := framer.ReadFrame(); err != nil {
			return nil, err
		} else {
			hc = f.(*http2.ContinuationFrame) // guaranteed by checkFrameOrder
		}
	}

	if err := hdec.Close(); err != nil {
		return mh, fmt.Errorf("failed to close header decoder: %w", err)
	}

	return mh, nil
}
