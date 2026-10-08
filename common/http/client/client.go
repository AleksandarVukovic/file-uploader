package client

import (
	"crypto/tls"
	"net"
	"net/http"
	"strconv"
	"time"

	commonmw "github.com/aleksandarv/file-uploader/common/http/middleware"
	goahttp "goa.design/goa/v3/http"
	goam "goa.design/goa/v3/middleware"
)

const requestTimeout = 70 * time.Second

func NewDoer(debug bool, tlsConfig *tls.Config) goahttp.Doer {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
		}).DialContext,
		TLSClientConfig:       tlsConfig,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 4 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
	}

	var doer goahttp.Doer = &http.Client{Transport: transport, Timeout: requestTimeout}
	doer = contextHeadersDoer{doer}
	if debug {
		doer = goahttp.NewDebugDoer(doer)
	}
	return doer
}

type contextHeadersDoer struct {
	goahttp.Doer
}

func (d contextHeadersDoer) Do(req *http.Request) (*http.Response, error) {
	if reqID, ok := req.Context().Value(goam.RequestIDKey).(string); ok {
		req.Header.Set(commonmw.RequestIDHeader, reqID)
	}
	if userID, ok := commonmw.UserIDFromCtx(req.Context()); ok {
		req.Header.Set(commonmw.UserIDHeader, strconv.FormatInt(userID, 10))
	}
	return d.Doer.Do(req)
}
