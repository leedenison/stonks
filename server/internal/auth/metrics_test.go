package auth

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// reader is the package's one metric reader. The API binds an instrument to
// the first provider installed and ignores a second, so every case here reads
// this one. Delta temporality makes each collection report what the case just
// did rather than everything before it.
var reader = sdkmetric.NewManualReader(
	sdkmetric.WithTemporalitySelector(func(sdkmetric.InstrumentKind) metricdata.Temporality {
		return metricdata.DeltaTemporality
	}),
)

func TestMain(m *testing.M) {
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	os.Exit(m.Run())
}

// counts collects what has been recorded since the last collection, rendered
// as "<name>{<attribute>=<value>}" so a case states its expectation in one
// literal.
func counts(t *testing.T) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				parts := make([]string, 0, dp.Attributes.Len())
				for _, kv := range dp.Attributes.ToSlice() {
					parts = append(parts, fmt.Sprintf("%s=%s", kv.Key, kv.Value.String()))
				}
				sort.Strings(parts)
				name := metric.Name
				if len(parts) > 0 {
					name = fmt.Sprintf("%s{%s}", name, strings.Join(parts, ","))
				}
				got[name] += dp.Value
			}
		}
	}
	return got
}
