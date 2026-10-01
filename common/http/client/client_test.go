package client

import (
	"crypto/tls"
	"net/http"
	"testing"
)

func TestNewDoer_SetsTLSClientConfig(t *testing.T) {
	cfg := &tls.Config{ServerName: "file-service"}

	doer := NewDoer(false, cfg)

	rid, ok := doer.(requestIDDoer)
	if !ok {
		t.Fatalf("expected requestIDDoer, got %T", doer)
	}
	httpClient, ok := rid.Doer.(*http.Client)
	if !ok {
		t.Fatalf("expected *http.Client, got %T", rid.Doer)
	}
	transport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", httpClient.Transport)
	}
	if transport.TLSClientConfig != cfg {
		t.Fatal("expected TLSClientConfig to be the provided tls.Config")
	}
}
