package processor

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	outcomeProcessed        = "processed"
	outcomeUnknownType      = "unknown_type"
	outcomeInvalidPayload   = "invalid_payload"
	outcomeObjectNotFound   = "object_not_found"
	outcomeBucketNotAllowed = "bucket_not_allowed"
	outcomeInvalidHeader    = "invalid_header"
	outcomeRequeued         = "requeued"
)

const (
	rowOutcomeValid     = "valid"
	rowOutcomeMalformed = "malformed"
	rowOutcomeInvalid   = "invalid"
)

var metricMessages, metricCSVRows, metricDuration = processorMetrics()

func processorMetrics() (metric.Int64Counter, metric.Int64Counter, metric.Float64Histogram) {
	meter := otel.Meter("file-processing-service/processor")

	messages, err := meter.Int64Counter("files.processing.messages",
		metric.WithDescription("Number of FileUploaded messages processed, by outcome"),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	rows, err := meter.Int64Counter("files.processing.csv.rows",
		metric.WithDescription("Number of CSV rows processed, by outcome"),
		metric.WithUnit("{row}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	duration, err := meter.Float64Histogram("files.processing.duration",
		metric.WithDescription("Time to process a FileUploaded message end-to-end"),
		metric.WithUnit("s"),
	)
	if err != nil {
		otel.Handle(err)
	}

	return messages, rows, duration
}

func addMetricMessage(ctx context.Context, outcome string) {
	metricMessages.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

func addMetricRows(ctx context.Context, outcome string, n int64) {
	if n == 0 {
		return
	}
	metricCSVRows.Add(ctx, n, metric.WithAttributes(attribute.String("outcome", outcome)))
}
