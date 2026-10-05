package auth

import (
	"testing"

	"github.com/leedenison/stonks/server/internal/testutil/metrictest"
)

// reader is the package's one metric reader.
var reader = metrictest.Install()

// counts collects what has been recorded since the last collection.
func counts(t *testing.T) map[string]int64 {
	t.Helper()
	return metrictest.Counts(t, reader)
}
