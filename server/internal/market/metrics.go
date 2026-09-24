package market

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// meterName scopes the instruments below to this package.
const meterName = "github.com/leedenison/stonks/server/internal/market"

// The attribute keys and the closed set of values the outcome takes. The
// datasource is bounded by the rows an instance seeds.
const (
	datasourceKey = attribute.Key("datasource")
	outcomeKey    = attribute.Key("outcome")
)

// instruments count what this package decides; see
// [metrics.go](../auth/metrics.go) for why construction is unguarded.
type instruments struct {
	keys metric.Int64Counter
}

var instr = newInstruments(otel.Meter(meterName))

func newInstruments(m metric.Meter) instruments {
	c, err := m.Int64Counter("stonks.datasource.keys", metric.WithUnit("{key}"),
		metric.WithDescription("Keys of completed fetches, by datasource and outcome."))
	if err != nil {
		otel.Handle(err)
	}
	return instruments{keys: c}
}

func (i instruments) key(ctx context.Context, datasource string, outcome gen.FetchOutcome) {
	i.keys.Add(ctx, 1, metric.WithAttributes(datasourceKey.String(datasource), outcomeKey.String(string(outcome))))
}
