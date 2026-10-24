package statement

import (
	"testing"
	"time"

	"github.com/google/uuid"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
)

var (
	meta       = types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "META"}
	metaInst   = uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	renamed    = time.Date(2022, time.June, 9, 0, 0, 0, 0, time.UTC)
	coveredTo  = time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)
	assertedOn = time.Date(2024, time.January, 16, 0, 0, 0, 0, time.UTC)
	notFoundOn = time.Date(2021, time.June, 1, 0, 0, 0, 0, time.UTC)
	massive    = validity.Source{Datasource: "massive", Precedence: 2}
)

// metaFacts are the facts of META: a rename to META on 2022-06-09 with
// coverage from that day, a lookup naming Meta on 2024-01-16, and a lookup on
// 2021-06-01 that found nothing. The negative assertion precedes the
// datasource's first positive one, so it does not count.
func metaFacts() validity.Facts {
	return validity.Facts{
		Assertions: []validity.Assertion{
			{At: assertedOn, Instrument: &metaInst, Source: massive},
			{At: notFoundOn, Source: massive},
		},
		Events:   []validity.Event{{On: renamed}},
		Coverage: []types.Period{{From: renamed, Before: coveredTo}},
	}
}

func metaTicker() *validity.Ticker {
	return &validity.Ticker{Identifier: meta, Derived: validity.Derive(metaFacts())}
}

func TestMark(t *testing.T) {
	const (
		confirmed   = statementv1.TransactionMark_TRANSACTION_MARK_CONFIRMED
		provisional = statementv1.TransactionMark_TRANSACTION_MARK_PROVISIONAL
		unresolved  = statementv1.TransactionMark_TRANSACTION_MARK_UNRESOLVED
	)
	ticker := metaTicker()
	tests := []struct {
		name     string
		arbiter  *gen.Arbiter
		fixedVia *types.IdentifierType
		ticker   *validity.Ticker
		on       time.Time
		want     statementv1.TransactionMark
	}{
		{name: "the user's choice through a ticker", arbiter: ptr.To(gen.ArbiterUser), fixedVia: ptr.To(types.IdentifierTypeMicTicker), want: confirmed},
		{name: "fixed through a stable identifier", arbiter: ptr.To(gen.ArbiterDatasource), fixedVia: ptr.To(types.IdentifierTypeIsin), want: confirmed},
		{name: "fixed through a broker description", arbiter: ptr.To(gen.ArbiterDatasource), fixedVia: ptr.To(types.IdentifierTypeBrokerDescription), want: provisional},
		{name: "fixed through an option symbol", arbiter: ptr.To(gen.ArbiterStated), fixedVia: ptr.To(types.IdentifierTypeOption), want: provisional},
		{name: "fixed through a ticker whose listing has no MIC_TICKER", arbiter: ptr.To(gen.ArbiterDatasource), fixedVia: ptr.To(types.IdentifierTypeOpenfigiTicker), want: provisional},
		{name: "dated, before the event", arbiter: ptr.To(gen.ArbiterDatasource), ticker: ticker, on: notFoundOn, want: unresolved},
		{name: "dated, covered back to the event", arbiter: ptr.To(gen.ArbiterDatasource), ticker: ticker, on: time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC), want: confirmed},
		{name: "dated, on the asserted day", arbiter: ptr.To(gen.ArbiterDatasource), ticker: ticker, on: assertedOn, want: confirmed},
		{name: "dated, after coverage ends", arbiter: ptr.To(gen.ArbiterDatasource), ticker: ticker, on: time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC), want: provisional},
		{name: "no association", want: unresolved},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mark(tc.arbiter, tc.fixedVia, tc.ticker, tc.on); got != tc.want {
				t.Errorf("mark = %v, want %v", got, tc.want)
			}
		})
	}
}
