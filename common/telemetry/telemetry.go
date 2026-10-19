package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

func Init(ctx context.Context, serviceName, collectorURL string, sampleRatio float64) (func(context.Context) error, error) {
	if sampleRatio < 0 || sampleRatio > 1 {
		return nil, fmt.Errorf("trace sample ratio must be between 0 and 1, got %v", sampleRatio)
	}

	if collectorURL == "" {
		return func(context.Context) error { return nil }, nil
	}

	parsedURL, err := url.Parse(collectorURL)
	if err != nil {
		return nil, err
	}
	insecure := parsedURL.Scheme == "http"

	resourceOpts := []resource.Option{resource.WithAttributes(semconv.ServiceName(serviceName))}
	if hostname, err := os.Hostname(); err == nil {
		resourceOpts = append(resourceOpts, resource.WithAttributes(semconv.ServiceInstanceID(hostname)))
	}
	resourceOpts = append(resourceOpts,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithProcess(),
	)
	otelResource, err := resource.New(ctx, resourceOpts...)
	if err != nil {
		return nil, err
	}

	metricOpts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpointURL(collectorURL)}
	traceOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpointURL(collectorURL)}
	if insecure {
		metricOpts = append(metricOpts, otlpmetricgrpc.WithInsecure())
		traceOpts = append(traceOpts, otlptracegrpc.WithInsecure())
	}

	metricExporter, err := otlpmetricgrpc.New(ctx, metricOpts...)
	if err != nil {
		return nil, err
	}
	meterProvider := metric.NewMeterProvider(
		metric.WithResource(otelResource),
		metric.WithReader(metric.NewPeriodicReader(metricExporter)),
	)

	traceExporter, err := otlptracegrpc.New(ctx, traceOpts...)
	if err != nil {
		_ = metricExporter.Shutdown(ctx)
		return nil, err
	}
	traceProvider := trace.NewTracerProvider(
		trace.WithResource(otelResource),
		trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(sampleRatio))),
		trace.WithBatcher(traceExporter),
	)

	otel.SetMeterProvider(meterProvider)
	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(ctx context.Context) error {
		return errors.Join(traceProvider.Shutdown(ctx), meterProvider.Shutdown(ctx))
	}, nil
}
