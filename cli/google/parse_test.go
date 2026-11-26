package google

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"

	adminpricev1 "github.com/leedenison/stonks/proto/admin/price/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
)

// pasted returns the cells of testdata/values.json. The file is modelled on
// a real paste of the values tab, with its listing ids replaced by invented
// ones. It was evaluated on 2026-11-26 and holds, in order:
//
//   - NASDAQ:AMZN, whose gap starts on a Saturday, with rows from the Monday
//     before it to the Friday after it;
//   - IBM under the three codes of XNYS, of which only NYSE answered;
//   - LON:VOD, quoted in pence;
//   - the rate of GBP, which has a row for each day of the weekend;
//   - NASDAQ:ZZZZQX, which Google does not know.
func pasted(t *testing.T) [][]any {
	t.Helper()
	b, err := os.ReadFile("testdata/values.json")
	require.NoError(t, err)
	var cells [][]any
	require.NoError(t, json.Unmarshal(b, &cells))
	return cells
}

// serial is the serial number of the close of a session on a day.
func serial(y int, m time.Month, dd int) float64 {
	return d(y, m, dd).Sub(epoch).Hours()/24 + 2.0/3
}

// tab builds a values tab evaluated on 2026-11-26 from blocks, each given
// as its listing, symbol, periods and currency, then its rows as pairs of a
// serial number and a price. A block without rows shows the error Google
// puts in place of the header.
func tab(blocks ...[]any) [][]any {
	rows := [][]any{{serial(2026, 11, 26) - 2.0/3}, {}, {}, {}, {}, {}}
	for i, b := range blocks {
		c := 2 * i
		pad := func(r int) {
			for len(rows) <= r {
				rows = append(rows, []any{})
			}
			for len(rows[r]) < c+2 {
				rows[r] = append(rows[r], "")
			}
		}
		for r, label := range map[int]string{listingRow: "listing", symbolRow: "symbol", periodsRow: "periods", currencyRow: "currency"} {
			pad(r)
			rows[r][c], rows[r][c+1] = label, b[r-listingRow]
		}
		data := b[4:]
		pad(seriesRow)
		if len(data) == 0 {
			rows[seriesRow][c] = "#N/A"
			continue
		}
		rows[seriesRow][c], rows[seriesRow][c+1] = dateHeader, closeHeader
		for j := 0; j < len(data); j += 2 {
			r := seriesRow + 1 + j/2
			pad(r)
			rows[r][c], rows[r][c+1] = data[j], data[j+1]
		}
	}
	return rows
}

func covered(from, before string) *typev1.Period { return &typev1.Period{From: &from, Before: &before} }

func TestParsePaste(t *testing.T) {
	asAt, series, skipped, err := parse(pasted(t))
	require.NoError(t, err)
	if want := d(2026, 11, 26); !asAt.Equal(want) {
		t.Errorf("parse() as at %v, want %v", asAt, want)
	}
	type summary struct {
		Listing, Symbol, Currency string
		Coverage                  []string
		Prices                    int
		First, Last               string
	}
	var got []summary
	for _, s := range series {
		sum := summary{Listing: s.GetListingId()[34:], Symbol: s.GetSymbol(), Currency: s.GetCurrency(), Prices: len(s.GetPrices())}
		for _, p := range s.GetCoverage() {
			sum.Coverage = append(sum.Coverage, p.GetFrom()+"/"+p.GetBefore())
		}
		first, last := s.GetPrices()[0], s.GetPrices()[len(s.GetPrices())-1]
		sum.First, sum.Last = first.GetDay()+" "+first.GetPrice(), last.GetDay()+" "+last.GetPrice()
		got = append(got, sum)
	}
	want := []summary{
		// The gap starts on a Saturday. A row before the gap makes its first
		// weekend covered, and the rows outside the gap are left out.
		{Listing: "a1", Symbol: "NASDAQ:AMZN", Currency: "USD", Coverage: []string{"2026-11-07/2026-11-14"}, Prices: 5, First: "2026-11-09 266.93", Last: "2026-11-13 266.66"},
		// The last row is the Friday, so coverage stops before the Saturday.
		{Listing: "a2", Symbol: "NYSE:IBM", Currency: "USD", Coverage: []string{"2026-11-02/2026-11-14"}, Prices: 10, First: "2026-11-02 252.51", Last: "2026-11-13 247.13"},
		{Listing: "a3", Symbol: "LON:VOD", Currency: "GBX", Coverage: []string{"2026-11-02/2026-11-14"}, Prices: 10, First: "2026-11-02 116.85", Last: "2026-11-13 116.95"},
		{Listing: "a4", Symbol: "CURRENCY:GBPUSD", Currency: "USD", Coverage: []string{"2026-11-02/2026-11-14"}, Prices: 12, First: "2026-11-02 1.34088", Last: "2026-11-13 1.3212"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("parse() series mismatch (-want +got):\n%s", diff)
	}
	wantSkipped := []skip{{Name: "NASDAQ:ZZZZQX", Reason: "Google answered under none of the symbols"}}
	if diff := cmp.Diff(wantSkipped, skipped); diff != "" {
		t.Errorf("parse() skipped mismatch (-want +got):\n%s", diff)
	}
}

func TestParse(t *testing.T) {
	week := "2026-11-02/2026-11-07"
	mon, tue, fri := serial(2026, 11, 2), serial(2026, 11, 3), serial(2026, 11, 6)
	tests := []struct {
		name        string
		cells       [][]any
		want        []*adminpricev1.PriceSeries
		wantSkipped []skip
	}{
		{
			name:  "a gap whose edges lack a row beyond them is covered from the first row to the last",
			cells: tab([]any{"l1", "NASDAQ:AAPL", week, "USD", tue, 10.5, serial(2026, 11, 5), 11.25}),
			want: []*adminpricev1.PriceSeries{{
				ListingId: "l1", Symbol: "NASDAQ:AAPL", Currency: "USD", Coverage: []*typev1.Period{covered("2026-11-03", "2026-11-06")},
				Prices: []*adminpricev1.DayPrice{{Day: "2026-11-03", Price: "10.5"}, {Day: "2026-11-05", Price: "11.25"}},
			}},
		},
		{
			name: "the rows between two gaps are left out",
			cells: tab([]any{"l1", "NASDAQ:AAPL", "2026-11-02/2026-11-04 2026-11-05/2026-11-07", "USD",
				mon, 10.0, tue, 11.0, serial(2026, 11, 4), 12.0, serial(2026, 11, 5), 13.0, fri, 14.0}),
			want: []*adminpricev1.PriceSeries{{
				ListingId: "l1", Symbol: "NASDAQ:AAPL", Currency: "USD",
				Coverage: []*typev1.Period{covered("2026-11-02", "2026-11-04"), covered("2026-11-05", "2026-11-07")},
				Prices: []*adminpricev1.DayPrice{
					{Day: "2026-11-02", Price: "10"}, {Day: "2026-11-03", Price: "11"}, {Day: "2026-11-05", Price: "13"}, {Day: "2026-11-06", Price: "14"},
				},
			}},
		},
		{
			name: "the first candidate that answered is taken",
			cells: tab(
				[]any{"l1", "ETR:SAP", week, "EUR"},
				[]any{"l1", "FRA:SAP", week, "EUR", mon, 208.8, fri, 212.7},
			),
			want: []*adminpricev1.PriceSeries{{
				ListingId: "l1", Symbol: "FRA:SAP", Currency: "EUR", Coverage: []*typev1.Period{covered("2026-11-02", "2026-11-07")},
				Prices: []*adminpricev1.DayPrice{{Day: "2026-11-02", Price: "208.8"}, {Day: "2026-11-06", Price: "212.7"}},
			}},
		},
		{
			name: "the earlier of two candidates that both answered is taken",
			cells: tab(
				[]any{"l1", "ETR:SAP", week, "EUR", mon, 210.5},
				[]any{"l1", "FRA:SAP", week, "EUR", mon, 208.8},
			),
			want: []*adminpricev1.PriceSeries{{
				ListingId: "l1", Symbol: "ETR:SAP", Currency: "EUR", Coverage: []*typev1.Period{covered("2026-11-02", "2026-11-03")},
				Prices: []*adminpricev1.DayPrice{{Day: "2026-11-02", Price: "210.5"}},
			}},
		},
		{
			name:  "a rate is quoted in USD whatever its currency cell holds",
			cells: tab([]any{"l1", "CURRENCY:KRWUSD", week, "#N/A", mon, 0.0007433342}),
			want: []*adminpricev1.PriceSeries{{
				ListingId: "l1", Symbol: "CURRENCY:KRWUSD", Currency: "USD", Coverage: []*typev1.Period{covered("2026-11-02", "2026-11-03")},
				Prices: []*adminpricev1.DayPrice{{Day: "2026-11-02", Price: "0.0007433342"}},
			}},
		},
		{
			name:        "a security without a currency",
			cells:       tab([]any{"l1", "NASDAQ:AAPL", week, "#N/A", mon, 10.0}),
			wantSkipped: []skip{{Name: "NASDAQ:AAPL", Reason: "Google states no currency: #N/A"}},
		},
		{
			name:        "no candidate answered",
			cells:       tab([]any{"l1", "NYSE:IBM", week, "#N/A"}, []any{"l1", "NYSEARCA:IBM", week, "#N/A"}),
			wantSkipped: []skip{{Name: "NYSE:IBM, NYSEARCA:IBM", Reason: "Google answered under none of the symbols"}},
		},
		{
			name:        "rows outside every gap",
			cells:       tab([]any{"l1", "NASDAQ:AAPL", week, "USD", serial(2026, 11, 10), 10.0}),
			wantSkipped: []skip{{Name: "NASDAQ:AAPL", Reason: "Google answered for no day of the gaps before the evaluation"}},
		},
		{
			// The evaluation is on 2026-11-26, so its own row is left out and
			// coverage ends before it.
			name:  "a row on the day of the evaluation",
			cells: tab([]any{"l1", "NASDAQ:AAPL", "2026-11-23/2026-11-28", "USD", serial(2026, 11, 25), 10.0, serial(2026, 11, 26), 11.0}),
			want: []*adminpricev1.PriceSeries{{
				ListingId: "l1", Symbol: "NASDAQ:AAPL", Currency: "USD", Coverage: []*typev1.Period{covered("2026-11-25", "2026-11-26")},
				Prices: []*adminpricev1.DayPrice{{Day: "2026-11-25", Price: "10"}},
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, got, skipped, err := parse(tc.cells)
			require.NoError(t, err)
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("parse() series mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantSkipped, skipped); diff != "" {
				t.Errorf("parse() skipped mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseRefuses(t *testing.T) {
	week := "2026-11-02/2026-11-07"
	mon := serial(2026, 11, 2)
	block := []any{"l1", "NASDAQ:AAPL", week, "USD", mon, 10.0}
	edit := func(fn func(cells [][]any)) [][]any {
		cells := tab(block)
		fn(cells)
		return cells
	}
	tests := []struct {
		name  string
		cells [][]any
		want  string
	}{
		{name: "an empty tab", cells: nil, want: "lacks the date"},
		// A paste that starts below the date cell leaves the date out.
		{name: "a paste without the date cell", cells: tab(block)[1:], want: "lacks the date"},
		{name: "a date cell that still holds its formula", cells: edit(func(c [][]any) { c[dateRow][0] = "=TODAY()" }), want: "holds the formula =TODAY()"},
		{name: "a series that still holds its formula", cells: edit(func(c [][]any) { c[seriesRow][0] = `=GOOGLEFINANCE("NASDAQ:AAPL","close")` }), want: "holds the formula"},
		{name: "a date without a series", cells: [][]any{{mon}}, want: "holds no series"},
		{name: "a block without its listing", cells: edit(func(c [][]any) { c[listingRow][1] = "" }), want: "lacks its listing"},
		{name: "a period out of order", cells: edit(func(c [][]any) { c[periodsRow][1] = "2026-11-07/2026-11-02" }), want: "not two days in order"},
		{name: "a period that is not a date", cells: edit(func(c [][]any) { c[periodsRow][1] = "soon" }), want: "not two days in order"},
		{name: "a block without periods", cells: edit(func(c [][]any) { c[periodsRow][1] = "" }), want: "lacks its periods"},
		{name: "a price that is not a number", cells: edit(func(c [][]any) { c[seriesRow+1][1] = "#N/A" }), want: "the price on 2026-11-02 is #N/A"},
		{name: "a price of zero", cells: edit(func(c [][]any) { c[seriesRow+1][1] = 0.0 }), want: "the price on 2026-11-02 is 0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := parse(tc.cells)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("parse() error = %v, want one that holds %q", err, tc.want)
			}
		})
	}
}
