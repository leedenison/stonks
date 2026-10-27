package join

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
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

func TestInstrsWithCoverage(t *testing.T) {
	a := gen.Instr{ID: uuid.New(), FetchKeyID: ptr.To(uuid.New())}
	b := gen.Instr{ID: uuid.New()}
	isin := gen.Identifier{ID: uuid.New(), InstrID: a.ID, Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"}
	gbp := gen.Listing{ID: uuid.New(), InstrID: b.ID, Currency: "GBP"}
	q := fakeQueries{
		identifiers: []gen.Identifier{isin},
		listings:    []gen.Listing{gbp},
		coverage:    []gen.IdentityCoverage{{InstrID: a.ID, Datasource: "openfigi"}},
	}
	got, err := InstrsWithCoverage(context.Background(), q, []gen.Instr{a, b})
	if err != nil {
		t.Fatalf("InstrsWithCoverage error = %v", err)
	}
	want := map[uuid.UUID]*InstrWithCoverage{
		a.ID: {Instr: Instr{Row: a, Identifiers: []gen.Identifier{isin}}, Coverage: map[string]bool{"openfigi": true}},
		b.ID: {Instr: Instr{Row: b, Listings: []gen.Listing{gbp}}, Coverage: map[string]bool{}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("InstrsWithCoverage mismatch (-want +got):\n%s", diff)
	}
}

func TestCovered(t *testing.T) {
	fetched := &InstrWithCoverage{Instr: Instr{Row: gen.Instr{FetchKeyID: ptr.To(uuid.New())}}, Coverage: map[string]bool{"openfigi": true}}
	reference := &InstrWithCoverage{Instr: Instr{Row: gen.Instr{}}, Coverage: map[string]bool{}}
	tests := []struct {
		name       string
		j          *InstrWithCoverage
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

func TestInstr(t *testing.T) {
	instr := gen.Instr{ID: uuid.New(), AssetClass: types.AssetClassStock}
	gbp := gen.Listing{ID: uuid.New(), InstrID: instr.ID, Currency: "GBP", PrimaryMic: ptr.To("XLON")}
	usd := gen.Listing{ID: uuid.New(), InstrID: instr.ID, Currency: "USD"}
	isin := gen.Identifier{InstrID: instr.ID, Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"}
	xlon := gen.Identifier{InstrID: instr.ID, ListingID: &gbp.ID, Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"}
	want := types.Instr{
		Class:       types.AssetClassStock,
		Identifiers: []types.Identifier{to.Identifier(isin)},
		Listings: map[string]types.Listing{
			"GBP": {Identifiers: []types.Identifier{to.Identifier(xlon)}, Primary: "XLON"},
			"USD": {},
		},
	}
	j := Instr{Row: instr, Identifiers: []gen.Identifier{isin, xlon}, Listings: []gen.Listing{gbp, usd}}
	if diff := cmp.Diff(want, j.Instr()); diff != "" {
		t.Errorf("Instr mismatch (-want +got):\n%s", diff)
	}
}
