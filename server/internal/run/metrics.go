package run

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// meterName scopes the instruments below to this package.
const meterName = "github.com/leedenison/stonks/server/internal/run"

// The attribute keys. Every value is a database enum, so each set is closed.
const (
	kindKey    = attribute.Key("kind")
	triggerKey = attribute.Key("trigger")
	outcomeKey = attribute.Key("outcome")
)

// instruments count what this package and the writers of findings decide; see
// [metrics.go](../auth/metrics.go) for why construction is unguarded.
type instruments struct {
	runs     metric.Int64Counter
	findings metric.Int64Counter
}

var instr = newInstruments(otel.Meter(meterName))

func newInstruments(m metric.Meter) instruments {
	var errs []error
	counter := func(name, unit, desc string) metric.Int64Counter {
		c, err := m.Int64Counter(name, metric.WithUnit(unit), metric.WithDescription(desc))
		errs = append(errs, err)
		return c
	}
	i := instruments{
		runs:     counter("stonks.runs", "{run}", "Runs reaching a terminal state, by kind, trigger and outcome."),
		findings: counter("stonks.findings", "{finding}", "Findings written, by kind."),
	}
	if err := errors.Join(errs...); err != nil {
		otel.Handle(err)
	}
	return i
}

func (i instruments) run(ctx context.Context, row gen.Run, outcome gen.RunState) {
	i.runs.Add(ctx, 1, metric.WithAttributes(
		kindKey.String(string(row.Kind)), triggerKey.String(string(row.Trigger)), outcomeKey.String(string(outcome))))
}

// Found counts a finding once its row is written.
func Found(ctx context.Context, kind gen.FindingKind) {
	instr.findings.Add(ctx, 1, metric.WithAttributes(kindKey.String(string(kind))))
}
