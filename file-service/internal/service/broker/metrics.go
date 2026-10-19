package broker

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	outcomePublished     = "published"
	outcomeUnknownType   = "unknown_type"
	outcomePublishFailed = "publish_failed"
	outcomeMarkFailed    = "mark_failed"
)

var metricEvents, metricPublishDuration, metricBatchSize = brokerMetrics()

func brokerMetrics() (metric.Int64Counter, metric.Float64Histogram, metric.Int64Histogram) {
	meter := otel.Meter("file-service/broker")

	counter, err := meter.Int64Counter("files.events",
		metric.WithDescription("Number of outbox file events processed by the broker, by outcome"),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	duration, err := meter.Float64Histogram("files.events.publish.duration",
		metric.WithDescription("Time to publish a file event and receive the broker confirm"),
		metric.WithUnit("s"),
	)
	if err != nil {
		otel.Handle(err)
	}

	batch, err := meter.Int64Histogram("files.events.batch.size",
		metric.WithDescription("Number of unpublished file events read from the outbox per poll"),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	return counter, duration, batch
}

func addMetricEvent(ctx context.Context, outcome string) {
	metricEvents.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}
