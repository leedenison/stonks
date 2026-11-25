package price

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/corpact"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// point is a Point in a form that compares by value.
type point struct {
	Day         string
	Price       string
	Datasource  string
	Provisional bool
}

func points(ps []Point) []point {
	var out []point
	for _, p := range ps {
		out = append(out, point{Day: p.Day.Format(time.DateOnly), Price: p.Price.String(), Datasource: p.Datasource, Provisional: p.Provisional})
	}
	return out
}

func traded(d time.Time, price, datasource string) stated {
	return stated{day: d, asAt: d, price: decimal.RequireFromString(price), datasource: datasource}
}

// TestSeriesRestates checks the restatement of each price from its as at day
// to today. Each split has an ex-date of Monday 10 June 2024, and the first
// two cases use 20 for 1.
func TestSeriesRestates(t *testing.T) {
	today := day(2026, 3, 10)
	split := func(from, to int64) corpact.Act {
		return corpact.Act{ExDate: day(2024, 6, 10), Ratio: types.Ratio{From: decimal.NewFromInt(from), To: decimal.NewFromInt(to)}, Handled: true}
	}
	calendar := func(before time.Time, acts ...corpact.Act) *corpact.Calendar {
		return &corpact.Calendar{Answer: corpact.Answer{Acts: acts, Coverage: []types.Period{{From: day(2020, 1, 1), Before: before}}}}
	}
	whole := today.AddDate(0, 0, 1)
	// adjusted is a close from before the split, served after it at one
	// twentieth, as a datasource that adjusts its history serves it.
	adjusted := stated{day: day(2024, 6, 6), asAt: day(2026, 1, 5), price: decimal.RequireFromString("60.45"), datasource: "sheets"}
	tests := []struct {
		name   string
		cal    *corpact.Calendar
		prices []stated
		want   []point
	}{
		{
			name: "an as traded price and an adjusted price meet on one basis", cal: calendar(whole, split(1, 20)),
			prices: []stated{adjusted, traded(day(2024, 6, 7), "1208.88", "alpha"), traded(day(2024, 6, 10), "121.79", "alpha")},
			want: []point{
				{Day: "2024-06-06", Price: "60.45", Datasource: "sheets"},
				{Day: "2024-06-07", Price: "60.444", Datasource: "alpha"},
				{Day: "2024-06-10", Price: "121.79", Datasource: "alpha"},
			},
		},
		{
			name: "a calendar that stops short of today marks each price it cannot vouch for", cal: calendar(day(2026, 2, 1), split(1, 20)),
			prices: []stated{adjusted, traded(day(2024, 6, 7), "1208.88", "alpha")},
			want: []point{
				{Day: "2024-06-06", Price: "60.45", Datasource: "sheets", Provisional: true},
				{Day: "2024-06-07", Price: "60.444", Datasource: "alpha", Provisional: true},
			},
		},
		{
			name: "a quotient that does not end rounds to six places", cal: calendar(whole, split(1, 3)),
			prices: []stated{traded(day(2024, 6, 7), "100.00", "alpha")},
			want:   []point{{Day: "2024-06-07", Price: "33.333333", Datasource: "alpha"}},
		},
		{
			name: "a price stored with more places keeps them", cal: calendar(whole, split(1, 2)),
			prices: []stated{traded(day(2024, 6, 7), "1.23456788", "alpha")},
			want:   []point{{Day: "2024-06-07", Price: "0.61728394", Datasource: "alpha"}},
		},
		{
			name:   "a listing that is never restated reads as stored",
			prices: []stated{traded(day(2024, 6, 7), "1.0842", "alpha")},
			want:   []point{{Day: "2024-06-07", Price: "1.0842", Datasource: "alpha"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := types.Period{From: tc.prices[0].day, Before: tc.prices[len(tc.prices)-1].day.AddDate(0, 0, 1)}
			if diff := cmp.Diff(tc.want, points(series(tc.prices, nil, nil, tc.cal, p, today))); diff != "" {
				t.Errorf("series() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSeriesCarries checks the days without a price. Friday 6 March 2026 has
// a close, and the coverage decides how far it carries.
func TestSeriesCarries(t *testing.T) {
	friday := traded(day(2026, 3, 6), "10", "alpha")
	p := types.Period{From: day(2026, 3, 6), Before: day(2026, 3, 10)}
	tests := []struct {
		name    string
		prices  []stated
		last    *stated
		covered []types.Period
		p       types.Period
		want    []point
	}{
		{
			name: "a close carries across covered days", prices: []stated{friday, traded(day(2026, 3, 9), "11", "beta")}, p: p,
			covered: []types.Period{{From: day(2026, 3, 1), Before: day(2026, 3, 10)}},
			want: []point{
				{Day: "2026-03-06", Price: "10", Datasource: "alpha"}, {Day: "2026-03-07", Price: "10", Datasource: "alpha"},
				{Day: "2026-03-08", Price: "10", Datasource: "alpha"}, {Day: "2026-03-09", Price: "11", Datasource: "beta"},
			},
		},
		{
			name: "a close stops at a break in coverage", prices: []stated{friday}, p: p,
			covered: []types.Period{{From: day(2026, 3, 1), Before: day(2026, 3, 8)}, {From: day(2026, 3, 9), Before: day(2026, 3, 10)}},
			want:    []point{{Day: "2026-03-06", Price: "10", Datasource: "alpha"}, {Day: "2026-03-07", Price: "10", Datasource: "alpha"}},
		},
		{
			name: "a close from before the period carries into it", last: &friday, p: types.Period{From: day(2026, 3, 7), Before: day(2026, 3, 9)},
			covered: []types.Period{{From: day(2026, 3, 1), Before: day(2026, 3, 10)}},
			want:    []point{{Day: "2026-03-07", Price: "10", Datasource: "alpha"}, {Day: "2026-03-08", Price: "10", Datasource: "alpha"}},
		},
		{
			name: "a covered day with no earlier close is absent", p: p,
			covered: []types.Period{{From: day(2026, 3, 1), Before: day(2026, 3, 10)}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, points(series(tc.prices, tc.last, tc.covered, nil, tc.p, day(2026, 3, 10)))); diff != "" {
				t.Errorf("series() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestReadRestatesByClass checks that Read restates the prices of a stock
// through the calendar of its instrument. It reads a rate as stored and not
// provisional, and asks for the calendar of the stock alone.
func TestReadRestatesByClass(t *testing.T) {
	ctx := context.Background()
	stock, cash := uuid.MustParse("00000000-0000-0000-0000-0000000000b1"), uuid.MustParse("00000000-0000-0000-0000-0000000000b2")
	shares, rate := uuid.MustParse("00000000-0000-0000-0000-0000000000b3"), uuid.MustParse("00000000-0000-0000-0000-0000000000b4")
	answer := uuid.MustParse("00000000-0000-0000-0000-0000000000b5")
	today := day(2026, 3, 10)
	p := types.Period{From: day(2024, 6, 7), Before: day(2024, 6, 8)}
	from, before := day(2010, 1, 1), today.AddDate(0, 0, 1)
	listings := []uuid.UUID{shares, rate}
	q := NewMockReadQueries(gomock.NewController(t))
	q.EXPECT().ListListingClasses(ctx, listings).Return([]gen.ListListingClassesRow{
		{ID: shares, InstrID: stock, AssetClass: types.AssetClassStock}, {ID: rate, InstrID: cash, AssetClass: types.AssetClassCash},
	}, nil)
	q.EXPECT().ListInstrMarkets(ctx, []uuid.UUID{stock}).Return([]gen.ListInstrMarketsRow{{InstrID: stock, Market: "US"}}, nil)
	q.EXPECT().ListTickersNaming(ctx, []uuid.UUID{stock}).Return([]gen.ListTickersNamingRow{{Market: "US", Value: "ACME"}}, nil)
	q.EXPECT().ListTickerValidity(ctx, gomock.Any()).Return([]gen.ListTickerValidityRow{{Market: "US", Value: "ACME", InstrID: stock, Validity: gen.ValidityConfirmed}}, nil)
	q.EXPECT().ListCorpActAnswers(ctx, gomock.Any()).Return([]gen.ListCorpActAnswersRow{{
		CorpActAnswer: gen.CorpActAnswer{ID: answer, Datasource: "alpha", CoveredFrom: &from, CoveredBefore: &before}, Market: "US", Value: "ACME",
	}}, nil)
	q.EXPECT().ListCorpActs(ctx, gomock.Any()).Return([]gen.CorpAct{{
		ID: uuid.New(), AnswerID: answer, ExDate: day(2024, 6, 10), RatioFrom: decimal.NewFromInt(1), RatioTo: decimal.NewFromInt(20), Handled: true,
	}}, nil)
	q.EXPECT().ListPrices(ctx, gen.ListPricesParams{ListingIds: listings, FromDay: p.From, Before: p.Before}).Return([]gen.ListPricesRow{
		{ListingID: shares, Day: p.From, Price: decimal.RequireFromString("1208.88"), AsAt: p.From, Datasource: "alpha"},
		{ListingID: rate, Day: p.From, Price: decimal.RequireFromString("1.2731"), AsAt: p.From, Datasource: "alpha"},
	}, nil)
	q.EXPECT().ListPricesBefore(ctx, gen.ListPricesBeforeParams{ListingIds: listings, Day: p.From}).Return(nil, nil)
	q.EXPECT().ListPriceCoverage(ctx, listings).Return(nil, nil)

	got, err := Read(ctx, q, listings, p, today)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	want := map[uuid.UUID][]point{
		shares: {{Day: "2024-06-07", Price: "60.444", Datasource: "alpha"}},
		rate:   {{Day: "2024-06-07", Price: "1.2731", Datasource: "alpha"}},
	}
	if diff := cmp.Diff(want, map[uuid.UUID][]point{shares: points(got[shares]), rate: points(got[rate])}); diff != "" {
		t.Errorf("Read() mismatch (-want +got):\n%s", diff)
	}
}
