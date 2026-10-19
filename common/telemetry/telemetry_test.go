package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func newRecordedSpan(t *testing.T) (context.Context, *tracetest.SpanRecorder, func()) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("test").Start(context.Background(), "span")
	return ctx, recorder, func() { span.End() }
}

func TestInit_InvalidSampleRatio(t *testing.T) {
	for _, ratio := range []float64{-0.1, 1.1} {
		shutdown, err := Init(context.Background(), "svc", "", ratio)
		require.Error(t, err)
		require.Nil(t, shutdown)
	}
}

func TestInit_EmptyCollectorURLIsNoop(t *testing.T) {
	shutdown, err := Init(context.Background(), "svc", "", 1.0)
	require.NoError(t, err)
	require.NoError(t, shutdown(context.Background()))
}

func TestTraceID_WithoutSpan(t *testing.T) {
	require.Empty(t, TraceID(context.Background()))
}

func TestTraceID_WithSpan(t *testing.T) {
	ctx, _, end := newRecordedSpan(t)
	defer end()

	got := TraceID(ctx)

	require.Len(t, got, 32)
	require.NotEqual(t, "00000000000000000000000000000000", got)
}

func TestSetFileID_SetsAttributeOnSpan(t *testing.T) {
	ctx, recorder, end := newRecordedSpan(t)

	SetFileID(ctx, "file-1")
	end()

	ended := recorder.Ended()
	require.Len(t, ended, 1)
	var found bool
	for _, attr := range ended[0].Attributes() {
		if attr.Key == FileIDKey {
			found = true
			require.Equal(t, "file-1", attr.Value.AsString())
		}
	}
	require.True(t, found, "expected file.id attribute on span")
}

func TestSetFileID_WithoutSpan(t *testing.T) {
	require.NotPanics(t, func() { SetFileID(context.Background(), "file-1") })
}
