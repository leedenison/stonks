// Package metrictest reads back the metrics a package's tests record.
package metrictest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// Install sets the process's meter provider and returns its reader. Each
// collection from the reader reports only what was recorded since the
// previous one. The API binds an instrument to the first provider set and
// ignores a later one, so a package installs once, before its tests run.
func Install() *sdkmetric.ManualReader {
	reader := sdkmetric.NewManualReader(
		sdkmetric.WithTemporalitySelector(func(sdkmetric.InstrumentKind) metricdata.Temporality {
			return metricdata.DeltaTemporality
		}),
	)
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	return reader
}

// Counts collects the counters recorded since the last collection. Each key
// is "<name>{<attribute>=<value>,...}", or the bare name for a counter with no
// attributes, so a case states its expectation in one literal. Zero values and
// metrics other than counters are left out.
func Counts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				if dp.Value == 0 {
					continue
				}
				parts := make([]string, 0, dp.Attributes.Len())
				for _, kv := range dp.Attributes.ToSlice() {
					parts = append(parts, fmt.Sprintf("%s=%s", kv.Key, kv.Value.String()))
				}
				sort.Strings(parts)
				name := m.Name
				if len(parts) > 0 {
					name = fmt.Sprintf("%s{%s}", name, strings.Join(parts, ","))
				}
				got[name] += dp.Value
			}
		}
	}
	return got
}
