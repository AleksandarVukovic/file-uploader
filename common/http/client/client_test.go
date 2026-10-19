package client

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aleksandarv/file-uploader/common/http/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestNewDoer_UsesTLSClientConfig(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := NewDoer(false, &tls.Config{RootCAs: pool}).Do(req)
	if err != nil {
		t.Fatalf("expected the provided tls.Config to be used, got %v", err)
	}
	resp.Body.Close()
}

func TestNewDoer_PropagatesTraceContext(t *testing.T) {
	prevPropagator := otel.GetTextMapPropagator()
	prevProvider := otel.GetTracerProvider()
	t.Cleanup(func() {
		otel.SetTextMapPropagator(prevPropagator)
		otel.SetTracerProvider(prevProvider)
	})
	provider := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("traceparent")
	}))
	defer srv.Close()

	ctx, span := provider.Tracer("test").Start(t.Context(), "parent")
	defer span.End()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := NewDoer(false, nil).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if !strings.Contains(got, span.SpanContext().TraceID().String()) {
		t.Fatalf("expected traceparent carrying the parent trace ID, got %q", got)
	}
}

func TestRequestIDDoer_ForwardsUserID(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(middleware.UserIDHeader)
	}))
	defer srv.Close()

	ctx := middleware.WithUserID(t.Context(), 42)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := NewDoer(false, nil).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if got != "42" {
		t.Fatalf("expected X-User-Id 42, got %q", got)
	}
}

func TestRequestIDDoer_OmitsUserIDWhenAbsent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(middleware.UserIDHeader)
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
		t.Fatalf("expected no X-User-Id header, got %q", got)
	}
}
