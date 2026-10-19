package api

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const (
	outcomeStored           = "stored"
	outcomeMissingUserID    = "missing_user_id"
	outcomeInvalidChecksum  = "invalid_checksum"
	outcomeChecksumMismatch = "checksum_mismatch"
	outcomeIncompleteBody   = "incomplete_body"
	outcomeDuplicate        = "duplicate"
	outcomeError            = "error"
)

var metricUploads, metricFileSize = fileMetrics()

func fileMetrics() (metric.Int64Counter, metric.Int64Histogram) {
	meter := otel.Meter("file-service/api")

	counter, err := meter.Int64Counter("files.uploads",
		metric.WithDescription("Number of file uploads handled, by outcome"),
		metric.WithUnit("{upload}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	histogram, err := meter.Int64Histogram("files.upload.size",
		metric.WithDescription("Size of successfully stored files"),
		metric.WithUnit("By"),
	)
	if err != nil {
		otel.Handle(err)
	}

	return counter, histogram
}
