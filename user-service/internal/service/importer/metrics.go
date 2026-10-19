package importer

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	outcomeProcessed      = "processed"
	outcomeUnknownType    = "unknown_type"
	outcomeInvalidPayload = "invalid_payload"
	outcomeRequeued       = "requeued"
)

const (
	recordOutcomeImported  = "imported"
	recordOutcomeDuplicate = "duplicate"
	recordOutcomeReserved  = "reserved_username"
)

var metricMessages, metricRecords, metricDuration = importerMetrics()

func importerMetrics() (metric.Int64Counter, metric.Int64Counter, metric.Float64Histogram) {
	meter := otel.Meter("user-service/importer")

	messages, err := meter.Int64Counter("users.import.messages",
		metric.WithDescription("Number of UsersImported messages processed, by outcome"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	records, err := meter.Int64Counter("users.import.records",
		metric.WithDescription("Number of user records processed, by outcome"),
		metric.WithUnit("{user}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	duration, err := meter.Float64Histogram("users.import.duration",
		metric.WithDescription("Time to process a UsersImported message end-to-end"),
		metric.WithUnit("s"),
	)
	if err != nil {
		otel.Handle(err)
	}

	return messages, records, duration
}

func addMetricMessage(ctx context.Context, outcome string) {
	metricMessages.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

func addMetricRecords(ctx context.Context, outcome string, n int64) {
	if n == 0 {
		return
	}
	metricRecords.Add(ctx, n, metric.WithAttributes(attribute.String("outcome", outcome)))
}
