package api

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	outcomeCreated          = "created"
	outcomeReservedUsername = "reserved_username"
	outcomeWeakPassword     = "weak_password"
	outcomeUsernameTaken    = "username_taken"
	outcomeEmailTaken       = "email_taken"
	outcomeError            = "error"
)

var metricCreations = usersMetrics()

func usersMetrics() metric.Int64Counter {
	meter := otel.Meter("user-service/api")

	creations, err := meter.Int64Counter("users.creations",
		metric.WithDescription("Number of user creation attempts, by outcome"),
		metric.WithUnit("{user}"),
	)
	if err != nil {
		otel.Handle(err)
	}

	return creations
}

func addMetricCreate(ctx context.Context, outcome string) {
	metricCreations.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}
