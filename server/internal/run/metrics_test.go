package run

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/testutil/metrictest"
)

// reader is the package's one metric reader.
var reader = metrictest.Install()

// counts collects what has been recorded since the last collection.
func counts(t *testing.T) map[string]int64 {
	t.Helper()
	return metrictest.Counts(t, reader)
}

func TestFound(t *testing.T) {
	counts(t)
	Found(context.Background(), gen.FindingKindBlock)
	want := map[string]int64{"stonks.findings{kind=block}": 1}
	if diff := cmp.Diff(want, counts(t)); diff != "" {
		t.Errorf("Found() counts mismatch (-want +got):\n%s", diff)
	}
}
