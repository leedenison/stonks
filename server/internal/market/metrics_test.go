package market

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

// reader is the package's one metric reader. Delta temporality makes each
// collection report what happened since the last. It is installed by init,
// since the dbtest build declares the package's TestMain.
var reader = sdkmetric.NewManualReader(
	sdkmetric.WithTemporalitySelector(func(sdkmetric.InstrumentKind) metricdata.Temporality {
		return metricdata.DeltaTemporality
	}),
)

func init() {
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
}

// counts collects what has been recorded since the last collection, rendered
// as "<name>{<attribute>=<value>,...}".
func counts(t *testing.T) map[string]int64 {
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
				got[fmt.Sprintf("%s{%s}", m.Name, strings.Join(parts, ","))] += dp.Value
			}
		}
	}
	return got
}
