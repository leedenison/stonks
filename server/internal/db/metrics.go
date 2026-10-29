package db

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// meterName scopes the instruments below to this package.
const meterName = "github.com/leedenison/stonks/server/internal/db"

// The attribute key and the closed set of values it takes.
const (
	causeKey = attribute.Key("cause")

	causeUniqueViolation      = "unique_violation"
	causeDeadlock             = "deadlock"
	causeSerializationFailure = "serialization_failure"
	causeConcurrentWrite      = "concurrent_write"
)

// instruments count what this package decides; see
// [metrics.go](../auth/metrics.go) for why construction is unguarded.
type instruments struct {
	retries metric.Int64Counter
}

var metrics = newInstruments(otel.Meter(meterName))

func newInstruments(m metric.Meter) instruments {
	c, err := m.Int64Counter("stonks.db.tx_retries", metric.WithUnit("{retry}"),
		metric.WithDescription("Transactions run again after a concurrent transaction caused a failure, by cause."))
	if err != nil {
		otel.Handle(err)
	}
	return instruments{retries: c}
}

func (i instruments) retry(ctx context.Context, cause string) {
	i.retries.Add(ctx, 1, metric.WithAttributes(causeKey.String(cause)))
}
