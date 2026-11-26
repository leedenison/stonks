package google

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	adminpricev1 "github.com/leedenison/stonks/proto/admin/price/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
)

// today is the day of every step in these tests: a Wednesday.
var today = time.Date(2026, time.November, 25, 15, 30, 0, 0, time.UTC)

func d(y int, m time.Month, dd int) time.Time { return time.Date(y, m, dd, 0, 0, 0, 0, time.UTC) }

func span(from, before string) *typev1.Period { return &typev1.Period{From: &from, Before: &before} }

func ticker(listing, venue, value string, periods ...*typev1.Period) *adminpricev1.PriceGap {
	return &adminpricev1.PriceGap{
		ListingId:  listing,
		Identifier: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: venue, Value: value},
		Periods:    periods,
	}
}

func rate(listing, code string, periods ...*typev1.Period) *adminpricev1.PriceGap {
	return &adminpricev1.PriceGap{
		ListingId:  listing,
		Identifier: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: code},
		Periods:    periods,
	}
}

func TestPlan(t *testing.T) {
	decade := []period{{From: d(2010, 1, 4), Before: d(2012, 1, 1)}}
	tests := []struct {
		name        string
		gaps        []*adminpricev1.PriceGap
		want        []block
		wantSkipped []skip
	}{
		{
			name: "a venue Google lists under one code",
			gaps: []*adminpricev1.PriceGap{ticker("l1", "XNAS", "AAPL", span("2010-01-04", "2012-01-01"))},
			want: []block{{Listing: "l1", Symbol: "NASDAQ:AAPL", Periods: decade}},
		},
		{
			name: "a venue Google lists under three codes",
			gaps: []*adminpricev1.PriceGap{ticker("l1", "XNYS", "IBM", span("2010-01-04", "2012-01-01"))},
			want: []block{
				{Listing: "l1", Symbol: "NYSE:IBM", Periods: decade},
				{Listing: "l1", Symbol: "NYSEARCA:IBM", Periods: decade},
				{Listing: "l1", Symbol: "NYSEAMERICAN:IBM", Periods: decade},
			},
		},
		{
			name: "a rate",
			gaps: []*adminpricev1.PriceGap{rate("l2", "EUR", span("2010-01-04", "2012-01-01"))},
			want: []block{{Listing: "l2", Symbol: "CURRENCY:EURUSD", Rate: true, Periods: decade}},
		},
		{
			name: "several periods stay apart",
			gaps: []*adminpricev1.PriceGap{ticker("l1", "XLON", "VOD", span("2010-01-04", "2012-01-01"), span("2015-06-01", "2015-06-08"))},
			want: []block{{Listing: "l1", Symbol: "LON:VOD", Periods: []period{{From: d(2010, 1, 4), Before: d(2012, 1, 1)}, {From: d(2015, 6, 1), Before: d(2015, 6, 8)}}}},
		},
		{
			name:        "a venue the map lacks",
			gaps:        []*adminpricev1.PriceGap{ticker("l1", "XJPX", "7203", span("2010-01-04", "2012-01-01"))},
			wantSkipped: []skip{{Name: "XJPX:7203", Reason: "the tool maps the venue XJPX to no exchange code of Google"}},
		},
		{
			name: "an identifier that is neither a ticker nor a currency",
			gaps: []*adminpricev1.PriceGap{{
				ListingId:  "l1",
				Identifier: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "US0378331005"},
				Periods:    []*typev1.Period{span("2010-01-04", "2012-01-01")},
			}},
			wantSkipped: []skip{{Name: ":US0378331005", Reason: "the gap is asked under an identifier that is neither a ticker nor a currency"}},
		},
		{
			name:        "a gap without a period",
			gaps:        []*adminpricev1.PriceGap{ticker("l1", "XNAS", "AAPL")},
			wantSkipped: []skip{{Name: "XNAS:AAPL", Reason: "the gap states no period the tool can read"}},
		},
		{
			// The margin reaches back a week, and the formula ends before
			// today, so a gap that starts a week from now asks for nothing.
			name:        "a gap that starts after the margin",
			gaps:        []*adminpricev1.PriceGap{ticker("l1", "XNAS", "AAPL", span("2026-12-02", "2026-12-09"))},
			wantSkipped: []skip{{Name: "XNAS:AAPL", Reason: "the gap holds no day before today"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, skipped := plan(tc.gaps, today)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("plan() blocks mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantSkipped, skipped); diff != "" {
				t.Errorf("plan() skipped mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPlanBudget checks that a listing waits whole for the next round when
// its blocks would take the tab past its budget of cells, and that a later
// listing which still fits is taken.
func TestPlanBudget(t *testing.T) {
	// A gap of two years asks for 744 days with its margin, so each block
	// needs 752 rows of two columns: 1504 cells.
	const fit = budget / 1504
	var gaps []*adminpricev1.PriceGap
	for range fit - 2 {
		gaps = append(gaps, ticker("l", "XNAS", "AAPL", span("2010-01-04", "2012-01-04")))
	}
	gaps = append(gaps,
		ticker("three", "XNYS", "IBM", span("2010-01-04", "2012-01-04")),
		ticker("one", "XLON", "VOD", span("2010-01-04", "2012-01-04")),
		ticker("another", "XLON", "BP", span("2010-01-04", "2012-01-04")),
		ticker("last", "XLON", "HSBA", span("2010-01-04", "2012-01-04")),
	)
	got, skipped := plan(gaps, today)
	if len(got) != fit {
		t.Errorf("plan() made %d blocks, want the %d the budget holds", len(got), fit)
	}
	for _, b := range got {
		if b.Listing == "three" || b.Listing == "last" {
			t.Errorf("plan() kept a block of %s, want it to wait", b.Listing)
		}
	}
	if rows, columns := size(got, today); rows != 752 || rows*columns > budget {
		t.Errorf("size() = %d rows by %d columns, want 752 rows within the budget of %d cells", rows, columns, budget)
	}
	want := []skip{{Name: "2 series", Reason: "the spreadsheet is full, so they wait for the next round"}}
	if diff := cmp.Diff(want, skipped); diff != "" {
		t.Errorf("plan() skipped mismatch (-want +got):\n%s", diff)
	}
}

// TestPlanLongSeries checks that the first listing is taken even when it
// alone passes the budget, so a round always makes progress, and that a long
// series makes the listings after it wait.
func TestPlanLongSeries(t *testing.T) {
	gaps := []*adminpricev1.PriceGap{
		ticker("long", "XNYS", "IBM", span("1700-01-01", "2026-11-01")),
		ticker("short", "XNAS", "AAPL", span("2026-10-01", "2026-11-01")),
	}
	got, skipped := plan(gaps, today)
	if len(got) != 3 || got[0].Listing != "long" {
		t.Errorf("plan() = %v, want the three blocks of the long series", got)
	}
	want := []skip{{Name: "1 series", Reason: "the spreadsheet is full, so they wait for the next round"}}
	if diff := cmp.Diff(want, skipped); diff != "" {
		t.Errorf("plan() skipped mismatch (-want +got):\n%s", diff)
	}
}

func TestSize(t *testing.T) {
	blocks := []block{
		{Listing: "l1", Symbol: "LON:VOD", Periods: []period{{From: d(2026, 11, 2), Before: d(2026, 11, 9)}}},
		{Listing: "l2", Symbol: "CURRENCY:EURUSD", Rate: true, Periods: []period{{From: d(2026, 10, 1), Before: d(2026, 11, 25)}}},
	}
	// The longer block asks from 24 September through 25 November: 63 days
	// under six rows of headers and the header of the answer.
	if rows, columns := size(blocks, today); rows != 70 || columns != 4 {
		t.Errorf("size() = %d rows by %d columns, want 70 by 4", rows, columns)
	}
}

func TestAsked(t *testing.T) {
	tests := []struct {
		name    string
		periods []period
		want    period
	}{
		{
			name:    "the margin at each end",
			periods: []period{{From: d(2010, 1, 4), Before: d(2012, 1, 1)}},
			want:    period{From: d(2009, 12, 28), Before: d(2012, 1, 8)},
		},
		{
			name:    "the hull of several periods",
			periods: []period{{From: d(2015, 6, 1), Before: d(2015, 6, 8)}, {From: d(2010, 1, 4), Before: d(2012, 1, 1)}},
			want:    period{From: d(2009, 12, 28), Before: d(2015, 6, 15)},
		},
		{
			name:    "cut to end before today",
			periods: []period{{From: d(2026, 11, 2), Before: d(2026, 11, 25)}},
			want:    period{From: d(2026, 10, 26), Before: d(2026, 11, 25)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := block{Periods: tc.periods}.asked(day(today))
			if !ok || got != tc.want {
				t.Errorf("asked() = %v, %v, want %v", got, ok, tc.want)
			}
		})
	}
}

func TestCells(t *testing.T) {
	blocks := []block{
		{Listing: "l1", Symbol: "LON:VOD", Periods: []period{{From: d(2010, 1, 4), Before: d(2012, 1, 1)}, {From: d(2015, 6, 1), Before: d(2015, 6, 8)}}},
		{Listing: "l2", Symbol: "CURRENCY:EURUSD", Rate: true, Periods: []period{{From: d(2026, 11, 2), Before: d(2026, 11, 25)}}},
	}
	want := [][]any{
		{"=TODAY()", "", "", ""},
		{"listing", "'l1", "listing", "'l2"},
		{"symbol", "'LON:VOD", "symbol", "'CURRENCY:EURUSD"},
		{"periods", "'2010-01-04/2012-01-01 2015-06-01/2015-06-08", "periods", "'2026-11-02/2026-11-25"},
		{"currency", `=GOOGLEFINANCE("LON:VOD","currency")`, "currency", "'USD"},
		{
			`=GOOGLEFINANCE("LON:VOD","close",DATE(2009,12,28),DATE(2015,6,15),"DAILY")`, "",
			`=GOOGLEFINANCE("CURRENCY:EURUSD","price",DATE(2026,10,26),DATE(2026,11,25),"DAILY")`, "",
		},
	}
	if diff := cmp.Diff(want, cells(blocks, today)); diff != "" {
		t.Errorf("cells() mismatch (-want +got):\n%s", diff)
	}
}
