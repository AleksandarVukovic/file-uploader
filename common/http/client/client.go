package client

import (
	"net"
	"net/http"
	"time"

	goahttp "goa.design/goa/v3/http"
	goam "goa.design/goa/v3/middleware"
)

const requestTimeout = 70 * time.Second

func NewDoer(debug bool) goahttp.Doer {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 4 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
	}

	var doer goahttp.Doer = &http.Client{Transport: transport, Timeout: requestTimeout}
	doer = requestIDDoer{doer}
	if debug {
		doer = goahttp.NewDebugDoer(doer)
	}
	return doer
}

type requestIDDoer struct {
	goahttp.Doer
}

func (d requestIDDoer) Do(req *http.Request) (*http.Response, error) {
	if reqID, ok := req.Context().Value(goam.RequestIDKey).(string); ok {
		req.Header.Set("X-Request-Id", reqID)
	}
	return d.Doer.Do(req)
}
