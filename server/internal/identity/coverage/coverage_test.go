package coverage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/coverage/mock"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

var (
	acme  = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	alpha = ptr.To("alpha")
	k1    = uuid.MustParse("00000000-0000-0000-0000-000000000011")
	k2    = uuid.MustParse("00000000-0000-0000-0000-000000000012")
	// mics puts XNAS and XNYS in the market US.
	mics = mic.MICMap{
		"XNAS": {Operating: "XNAS", Market: "US"},
		"XNGS": {Operating: "XNAS", Market: "US"},
		"XNYS": {Operating: "XNYS", Market: "US"},
	}
)

// day is the nth day of March 2026.
func day(n int) time.Time { return time.Date(2026, time.March, n, 0, 0, 0, 0, time.UTC) }

func ticker(value string) types.MarketTicker {
	return types.MarketTicker{Market: "US", Value: value}
}

func asserted(n int, instrument *uuid.UUID) gen.ListIdentifierAssertionsRow {
	return gen.ListIdentifierAssertionsRow{
		IdentifierAssertion: gen.IdentifierAssertion{ID: uuid.New(), Type: types.IdentifierTypeMicTicker, Domain: "US", Value: "ACME", AssertedOn: day(n), InstrumentID: instrument, FetchKeyID: ptr.To(uuid.New())},
		Datasource:          alpha, Precedence: ptr.To(int32(1)),
	}
}

func txn(key uuid.UUID, n int) gen.ListTickerTxnsRow {
	return gen.ListTickerTxnsRow{StatedKeyID: key, AsAt: day(n)}
}

// facts serves the facts of v: assertions and nothing else.
func facts(q *mock.MockQueries, v types.MarketTicker, assertions []gen.ListIdentifierAssertionsRow) {
	q.EXPECT().ListIdentifierAssertions(gomock.Any(), gen.ListIdentifierAssertionsParams{Markets: []string{v.Market}, Values: []string{v.Value}}).Return(assertions, nil)
	q.EXPECT().ListIdentifierEvents(gomock.Any(), gen.ListIdentifierEventsParams{Markets: []string{v.Market}, Values: []string{v.Value}}).Return(nil, nil)
	q.EXPECT().ListIdentifierCoverage(gomock.Any(), gen.ListIdentifierCoverageParams{Markets: []string{v.Market}, Values: []string{v.Value}}).Return(nil, nil)
}

// TestTicker checks that a ticker is read at its market, with the
// transactions of keys at every venue of it, its days, intervals, gaps and
// the names of its instruments.
func TestTicker(t *testing.T) {
	ctrl := gomock.NewController(t)
	q := mock.NewMockQueries(ctrl)
	v := ticker("ACME")
	assertions := []gen.ListIdentifierAssertionsRow{asserted(10, &acme), asserted(20, &acme)}
	facts(q, v, assertions)
	probes, err := mics.TickerProbes([]types.MarketTicker{v})
	require.NoError(t, err)
	if len(probes) != 3 {
		t.Fatalf("probes = %d, want one per venue of the market", len(probes))
	}
	q.EXPECT().ListTickerTxns(gomock.Any(), probes).
		Return([]gen.ListTickerTxnsRow{txn(k1, 5), txn(k2, 5), txn(k2, 15)}, nil)
	name := gen.InstrumentName{InstrumentID: acme, Type: types.IdentifierTypeIsin, Value: "US0000000001"}
	q.EXPECT().ListInstrumentNames(gomock.Any(), []uuid.UUID{acme}).Return([]gen.InstrumentName{name}, nil)

	got, err := New(q, mics).Ticker(context.Background(), v, day(30))
	if err != nil {
		t.Fatalf("Ticker: %v", err)
	}
	from, mid, before := day(10), day(21), day(31)
	want := &Page{
		Ticker: v,
		Days:   []time.Time{day(5), day(15)},
		Rows:   tickervalidity.Rows{Assertions: assertions},
		Intervals: []tickervalidity.Interval{
			{Before: &from, Instrument: acme, Validity: tickervalidity.Provisional},
			{From: &from, Before: &mid, Instrument: acme, Validity: tickervalidity.Confirmed},
			{From: &mid, Instrument: acme, Validity: tickervalidity.Provisional},
		},
		Gaps: []Gap{
			{Period: types.Period{From: day(5), Before: from}, Keys: 2, Txns: 2},
			{Period: types.Period{From: mid, Before: before}},
		},
		Names: map[uuid.UUID]types.Identifier{acme: {Type: types.IdentifierTypeIsin, Value: "US0000000001"}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Ticker mismatch (-want +got):\n%s", diff)
	}
}

// TestTickerNoTxns checks that a ticker no key states has no days
// and no gaps.
func TestTickerNoTxns(t *testing.T) {
	ctrl := gomock.NewController(t)
	q := mock.NewMockQueries(ctrl)
	v := ticker("ACME")
	facts(q, v, nil)
	q.EXPECT().ListTickerTxns(gomock.Any(), gomock.Any()).Return(nil, nil)

	got, err := New(q, mics).Ticker(context.Background(), v, day(30))
	if err != nil {
		t.Fatalf("Ticker: %v", err)
	}
	if len(got.Days) != 0 || len(got.Gaps) != 0 || len(got.Intervals) != 0 || got.Names != nil {
		t.Errorf("Ticker = %+v, want no days, gaps, intervals or names", got)
	}
}

func TestTickerRefused(t *testing.T) {
	tests := []struct {
		name    string
		v       types.MarketTicker
		wantErr error
	}{
		{name: "venue for market", v: types.MarketTicker{Market: "XNAS", Value: "ACME"}, wantErr: tickervalidity.ErrSpelling},
		{name: "unknown market", v: types.MarketTicker{Market: "ZZ", Value: "ACME"}, wantErr: tickervalidity.ErrSpelling},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			_, err := New(mock.NewMockQueries(ctrl), mics).Ticker(context.Background(), tc.v, day(30))
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Ticker(%s) error = %v, want %v", tc.v.Name(), err, tc.wantErr)
			}
		})
	}
}
