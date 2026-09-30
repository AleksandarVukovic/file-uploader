package client

import (
	"net"
	"net/http"
	"time"

	goahttp "goa.design/goa/v3/http"
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
	if debug {
		doer = goahttp.NewDebugDoer(doer)
	}
	return doer
}
