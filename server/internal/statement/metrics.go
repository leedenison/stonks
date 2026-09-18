package statement

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// meterName scopes the instruments below to this package.
const meterName = "github.com/leedenison/stonks/server/internal/statement"

// The attribute key and the closed set of values it takes.
const (
	outcomeKey = attribute.Key("outcome")

	outcomeAccepted = "accepted"
	outcomeRejected = "rejected"
)

// instruments count what this package decides; see
// [metrics.go](../auth/metrics.go) for why construction is unguarded.
type instruments struct {
	rowsWritten metric.Int64Counter
}

var instr = newInstruments(otel.Meter(meterName))

func newInstruments(m metric.Meter) instruments {
	c, err := m.Int64Counter("stonks.statement.rows", metric.WithUnit("{row}"), metric.WithDescription("Rows of completed statements, by outcome."))
	if err != nil {
		otel.Handle(err)
	}
	return instruments{rowsWritten: c}
}

func (i instruments) rows(ctx context.Context, outcome string, n int64) {
	i.rowsWritten.Add(ctx, n, metric.WithAttributes(outcomeKey.String(outcome)))
}
