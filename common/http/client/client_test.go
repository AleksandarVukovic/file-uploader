package client

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aleksandarv/file-uploader/common/http/middleware"
)

func TestNewDoer_SetsTLSClientConfig(t *testing.T) {
	cfg := &tls.Config{ServerName: "file-service"}

	doer := NewDoer(false, cfg)

	rid, ok := doer.(contextHeadersDoer)
	if !ok {
		t.Fatalf("expected contextHeadersDoer, got %T", doer)
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

func TestRequestIDDoer_ForwardsUsername(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(middleware.UsernameHeader)
	}))
	defer srv.Close()

	ctx := middleware.WithUsername(t.Context(), "alice")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := NewDoer(false, nil).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if got != "alice" {
		t.Fatalf("expected X-Username alice, got %q", got)
	}
}

func TestRequestIDDoer_OmitsUsernameWhenAbsent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(middleware.UsernameHeader)
	}))
	defer srv.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := NewDoer(false, nil).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if got != "" {
		t.Fatalf("expected no X-Username header, got %q", got)
	}
}
