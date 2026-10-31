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
	"github.com/leedenison/stonks/server/internal/identity/tickers"
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

func asserted(n int, instr *uuid.UUID) gen.ListIdentifierAssertionsRow {
	return gen.ListIdentifierAssertionsRow{
		IdentifierAssertion: gen.IdentifierAssertion{ID: uuid.New(), Market: "US", AssertedFrom: day(n), AssertedBefore: day(n).AddDate(0, 0, 1), InstrID: instr, FetchKeyID: ptr.To(uuid.New())}, Value: "ACME",
		Datasource: alpha, Precedence: ptr.To(int32(1)),
	}
}

func txn(key uuid.UUID, n int) gen.ListTickerTxnsRow {
	return gen.ListTickerTxnsRow{StatedKeyID: key, AsAt: day(n)}
}

// facts serves v's assertions and its stored ticker validity ivs.
func facts(q *mock.MockQueries, v types.MarketTicker, assertions []gen.ListIdentifierAssertionsRow, ivs tickervalidity.Intervals) {
	q.EXPECT().ListIdentifierAssertions(gomock.Any(), gen.ListIdentifierAssertionsParams{Markets: []string{v.Market}, Values: []string{v.Value}}).Return(assertions, nil)
	q.EXPECT().ListIdentifierAssertionCoverage(gomock.Any(), gen.ListIdentifierAssertionCoverageParams{Markets: []string{v.Market}, Values: []string{v.Value}}).Return(nil, nil)
	var stored []gen.ListTickerValidityRow
	for _, iv := range ivs {
		stored = append(stored, gen.ListTickerValidityRow{Market: v.Market, Value: v.Value, InstrID: iv.Instr, ValidFrom: iv.From, ValidBefore: iv.Before, Validity: iv.Validity})
	}
	q.EXPECT().ListTickerValidity(gomock.Any(), gen.ListTickerValidityParams{Markets: []string{v.Market}, Values: []string{v.Value}}).Return(stored, nil)
}

// TestTicker checks that a ticker is read at its market, with the
// transactions of keys at every venue of it, its days, intervals, gaps and
// the names of its instruments.
func TestTicker(t *testing.T) {
	ctrl := gomock.NewController(t)
	q := mock.NewMockQueries(ctrl)
	v := ticker("ACME")
	assertions := []gen.ListIdentifierAssertionsRow{asserted(10, &acme), asserted(20, &acme)}
	from, mid, before := day(10), day(21), day(31)
	ivs := tickervalidity.Intervals{
		{Before: &from, Instr: acme, Validity: tickervalidity.Provisional},
		{From: &from, Before: &mid, Instr: acme, Validity: tickervalidity.Confirmed},
		{From: &mid, Instr: acme, Validity: tickervalidity.Provisional},
	}
	facts(q, v, assertions, ivs)
	probes, err := mics.TickerProbes([]types.MarketTicker{v})
	require.NoError(t, err)
	if len(probes) != 3 {
		t.Fatalf("probes = %d, want one per venue of the market", len(probes))
	}
	q.EXPECT().ListTickerTxns(gomock.Any(), probes).
		Return([]gen.ListTickerTxnsRow{txn(k1, 5), txn(k2, 5), txn(k2, 15)}, nil)
	name := gen.InstrName{InstrID: acme, Type: types.IdentifierTypeIsin, Value: "US0000000001"}
	q.EXPECT().ListInstrNames(gomock.Any(), []uuid.UUID{acme}).Return([]gen.InstrName{name}, nil)

	got, err := New(q, mics).Ticker(context.Background(), v, day(30))
	if err != nil {
		t.Fatalf("Ticker: %v", err)
	}
	want := &Page{
		Ticker:    v,
		Days:      []time.Time{day(5), day(15)},
		Rows:      tickervalidity.Rows{Assertions: assertions},
		Intervals: ivs,
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
	facts(q, v, nil, nil)
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
		{name: "venue for market", v: types.MarketTicker{Market: "XNAS", Value: "ACME"}, wantErr: tickers.ErrSpelling},
		{name: "unknown market", v: types.MarketTicker{Market: "ZZ", Value: "ACME"}, wantErr: tickers.ErrSpelling},
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

func stated(value string, key uuid.UUID, n, txns int) gen.ListStatedTickerTxnsRow {
	return gen.ListStatedTickerTxnsRow{Market: "US", Value: value, StatedKeyID: key, AsAt: day(n), Txns: int32(txns)}
}

// TestUnconfirmed checks that a ticker without an interval is read as naming
// nothing, with its keys, transactions and first day. A ticker with intervals
// is read by the gaps of its period, under the instrument of its latest
// interval. A ticker whose confirmed interval covers its period appears in
// neither.
func TestUnconfirmed(t *testing.T) {
	ctrl := gomock.NewController(t)
	q := mock.NewMockQueries(ctrl)
	beta := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	q.EXPECT().ListStatedTickerTxns(gomock.Any()).Return([]gen.ListStatedTickerTxnsRow{
		stated("ACME", k1, 5, 1), stated("ACME", k2, 15, 1),
		stated("FULL", k1, 5, 1),
		stated("NONE", k1, 3, 2), stated("NONE", k2, 5, 1),
		stated("TWO", k2, 2, 1),
	}, nil)
	from, mid, early := day(10), day(21), day(4)
	q.EXPECT().ListTickerValidity(gomock.Any(), gen.ListTickerValidityParams{
		Markets: []string{"US", "US", "US", "US"}, Values: []string{"ACME", "FULL", "NONE", "TWO"},
	}).Return([]gen.ListTickerValidityRow{
		{Market: "US", Value: "ACME", InstrID: acme, ValidFrom: &from, ValidBefore: &mid, Validity: tickervalidity.Confirmed},
		{Market: "US", Value: "ACME", InstrID: acme, ValidFrom: &mid, Validity: tickervalidity.Provisional},
		{Market: "US", Value: "FULL", InstrID: beta, Validity: tickervalidity.Confirmed},
		{Market: "US", Value: "TWO", InstrID: acme, ValidBefore: &early, Validity: tickervalidity.Confirmed},
		{Market: "US", Value: "TWO", InstrID: beta, ValidFrom: &early, Validity: tickervalidity.Provisional},
	}, nil)
	name := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0000000001"}
	q.EXPECT().ListInstrNames(gomock.Any(), []uuid.UUID{acme, beta}).
		Return([]gen.InstrName{{InstrID: acme, Type: name.Type, Value: name.Value}}, nil)

	nothing, gapped, err := New(q, mics).Unconfirmed(context.Background(), day(30))
	if err != nil {
		t.Fatalf("Unconfirmed: %v", err)
	}
	wantNothing := []NamesNothing{{Ticker: ticker("NONE"), Keys: 2, Txns: 3, First: day(3)}}
	if diff := cmp.Diff(wantNothing, nothing); diff != "" {
		t.Errorf("Unconfirmed names nothing mismatch (-want +got):\n%s", diff)
	}
	wantGaps := []TickerGap{
		{Ticker: ticker("ACME"), Instr: acme, Name: name, Gap: Gap{Period: types.Period{From: day(5), Before: from}, Keys: 1, Txns: 1}},
		{Ticker: ticker("ACME"), Instr: acme, Name: name, Gap: Gap{Period: types.Period{From: mid, Before: day(31)}}},
		{Ticker: ticker("TWO"), Instr: beta, Gap: Gap{Period: types.Period{From: early, Before: day(31)}}},
	}
	if diff := cmp.Diff(wantGaps, gapped); diff != "" {
		t.Errorf("Unconfirmed gaps mismatch (-want +got):\n%s", diff)
	}
}

// TestUnconfirmedNone checks that with no stated ticker nothing else is read.
func TestUnconfirmedNone(t *testing.T) {
	ctrl := gomock.NewController(t)
	q := mock.NewMockQueries(ctrl)
	q.EXPECT().ListStatedTickerTxns(gomock.Any()).Return(nil, nil)
	nothing, gapped, err := New(q, mics).Unconfirmed(context.Background(), day(30))
	if err != nil || nothing != nil || gapped != nil {
		t.Errorf("Unconfirmed = %v, %v, %v, want nothing", nothing, gapped, err)
	}
}

func TestUnconfirmedFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	q := mock.NewMockQueries(ctrl)
	boom := errors.New("boom")
	q.EXPECT().ListStatedTickerTxns(gomock.Any()).Return(nil, boom)
	if _, _, err := New(q, mics).Unconfirmed(context.Background(), day(30)); !errors.Is(err, boom) {
		t.Errorf("Unconfirmed error = %v, want %v", err, boom)
	}
}
