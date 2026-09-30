package fileservice

import (
	fsfiles "github.com/aleksandarv/file-uploader/file-service/gen/files"
	fsclient "github.com/aleksandarv/file-uploader/file-service/gen/http/files/client"
	goahttp "goa.design/goa/v3/http"
)

func NewClient(scheme, host string, debug bool, doer goahttp.Doer) fsfiles.Service {
	c := fsclient.NewClient(scheme, host, doer, goahttp.RequestEncoder, goahttp.ResponseDecoder, false)
	return fsfiles.NewClient(c.Upload())
}
