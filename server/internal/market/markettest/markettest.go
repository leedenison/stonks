// Package markettest holds the checks every identity integration's tests
// share.
package markettest

import (
	"context"
	"testing"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

// Unauthorised asserts that fetching sent through c fails, and that c reads
// the failure as blocking the datasource, as a refused credential must.
func Unauthorised(t *testing.T, c market.Identity, sent types.Identifier) {
	t.Helper()
	_, err := c.Fetch(context.Background(), []market.Request[gen.StatedKey]{{Sent: sent}})
	if err == nil {
		t.Fatal("Fetch() with a refused key succeeded")
	}
	if got, want := c.Classify(err), (market.Failure{Scope: market.ScopeDatasource}); got != want {
		t.Errorf("Classify(%v) = %+v, want %+v", err, got, want)
	}
}

// VenuesAreMICs asserts that every venue is in the MIC reference table, so
// a venue table of an integration names only venues the system knows.
func VenuesAreMICs(t *testing.T, mics mic.MICMap, venues ...string) {
	t.Helper()
	for _, v := range venues {
		if _, ok := mics.Operating(v); !ok {
			t.Errorf("venue %s is not in the MIC reference table", v)
		}
	}
}
