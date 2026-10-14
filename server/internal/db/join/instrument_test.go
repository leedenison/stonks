package join

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

type fakeQueries struct {
	identifiers []gen.Identifier
	listings    []gen.Listing
	coverage    []gen.IdentityCoverage
}

func (f fakeQueries) ListIdentifiersOf(context.Context, []uuid.UUID) ([]gen.Identifier, error) {
	return f.identifiers, nil
}

func (f fakeQueries) ListListingsOf(context.Context, []uuid.UUID) ([]gen.Listing, error) {
	return f.listings, nil
}

func (f fakeQueries) ListIdentityCoverage(context.Context, []uuid.UUID) ([]gen.IdentityCoverage, error) {
	return f.coverage, nil
}

func TestInstrumentsWithCoverage(t *testing.T) {
	a := gen.Instrument{ID: uuid.New(), FetchKeyID: ptr.To(uuid.New())}
	b := gen.Instrument{ID: uuid.New()}
	isin := gen.Identifier{ID: uuid.New(), InstrumentID: a.ID, Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"}
	gbp := gen.Listing{ID: uuid.New(), InstrumentID: b.ID, Currency: "GBP"}
	q := fakeQueries{
		identifiers: []gen.Identifier{isin},
		listings:    []gen.Listing{gbp},
		coverage:    []gen.IdentityCoverage{{InstrumentID: a.ID, Datasource: "openfigi"}},
	}
	got, err := InstrumentsWithCoverage(context.Background(), q, []gen.Instrument{a, b})
	if err != nil {
		t.Fatalf("InstrumentsWithCoverage error = %v", err)
	}
	want := map[uuid.UUID]*InstrumentWithCoverage{
		a.ID: {Instrument: Instrument{Row: a, Identifiers: []gen.Identifier{isin}}, Coverage: map[string]bool{"openfigi": true}},
		b.ID: {Instrument: Instrument{Row: b, Listings: []gen.Listing{gbp}}, Coverage: map[string]bool{}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("InstrumentsWithCoverage mismatch (-want +got):\n%s", diff)
	}
}

func TestCovered(t *testing.T) {
	fetched := &InstrumentWithCoverage{Instrument: Instrument{Row: gen.Instrument{FetchKeyID: ptr.To(uuid.New())}}, Coverage: map[string]bool{"openfigi": true}}
	reference := &InstrumentWithCoverage{Instrument: Instrument{Row: gen.Instrument{}}, Coverage: map[string]bool{}}
	tests := []struct {
		name       string
		j          *InstrumentWithCoverage
		datasource string
		want       bool
	}{
		{name: "answered", j: fetched, datasource: "openfigi", want: true},
		{name: "not answered", j: fetched, datasource: "eodhd", want: false},
		{name: "reference data", j: reference, datasource: "eodhd", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.j.Covered(tc.datasource); got != tc.want {
				t.Errorf("Covered(%q) = %v, want %v", tc.datasource, got, tc.want)
			}
		})
	}
}

func TestInstrument(t *testing.T) {
	inst := gen.Instrument{ID: uuid.New(), AssetClass: types.AssetClassStock}
	gbp := gen.Listing{ID: uuid.New(), InstrumentID: inst.ID, Currency: "GBP", PrimaryMic: ptr.To("XLON")}
	usd := gen.Listing{ID: uuid.New(), InstrumentID: inst.ID, Currency: "USD"}
	isin := gen.Identifier{InstrumentID: inst.ID, Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"}
	xlon := gen.Identifier{InstrumentID: inst.ID, ListingID: &gbp.ID, Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"}
	want := types.Instrument{
		Class:       types.AssetClassStock,
		Identifiers: []types.Identifier{to.Identifier(isin)},
		Listings: map[string]types.Listing{
			"GBP": {Identifiers: []types.Identifier{to.Identifier(xlon)}, Primary: "XLON"},
			"USD": {},
		},
	}
	j := Instrument{Row: inst, Identifiers: []gen.Identifier{isin, xlon}, Listings: []gen.Listing{gbp, usd}}
	if diff := cmp.Diff(want, j.Instrument()); diff != "" {
		t.Errorf("Instrument mismatch (-want +got):\n%s", diff)
	}
}
