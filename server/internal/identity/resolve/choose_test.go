package resolve

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

func id(t types.IdentifierType, domain, value string) types.Identifier {
	return types.Identifier{Type: t, Domain: domain, Value: value}
}

func listing(class types.AssetClass, currency string, ids ...types.Identifier) market.Listing {
	return market.Listing{Class: class, Currency: currency, Identifiers: ids}
}

// cur maps GBX to GBP and every other code to itself.
var cur = currency.Families{"EUR": "EUR", "GBP": "GBP", "GBX": "GBP", "USD": "USD"}

var (
	isin   = id(types.IdentifierTypeIsin, "", "GB00BH4HKS39")
	cusip  = id(types.IdentifierTypeCusip, "", "92857W308")
	figi   = id(types.IdentifierTypeOpenfigiShareClass, "", "BBG001S5XDT5")
	figi2  = id(types.IdentifierTypeOpenfigiShareClass, "", "BBG001S5XDT6")
	comp   = id(types.IdentifierTypeOpenfigiComposite, "", "BBG000C6K6G9")
	xlon   = id(types.IdentifierTypeMicTicker, "XLON", "VOD")
	xnas   = id(types.IdentifierTypeMicTicker, "XNAS", "VOD")
	bats   = id(types.IdentifierTypeMicTicker, "BATS", "VOD")
	xetr   = id(types.IdentifierTypeMicTicker, "XETR", "VODI")
	ticker = id(types.IdentifierTypeMicTicker, "", "VOD")
	descr  = id(types.IdentifierTypeBrokerDescription, "ibkr", "ACME CORP")
)

// joinMICs puts XNAS and BATS in the market US, and XLON and XETR each in a
// market of its own.
var joinMICs = mic.MICMap{
	"XNAS": {Operating: "XNAS", Market: "US"},
	"BATS": {Operating: "BATS", Market: "US"},
	"XLON": {Operating: "XLON", Market: "XLON"},
	"XETR": {Operating: "XETR", Market: "XETR"},
}

// answeredAt is the moment every served fetch key of these tests was
// answered, unless nextDay moves it.
var answeredAt = time.Date(2026, time.January, 2, 10, 0, 0, 0, time.UTC)

// served returns a fetch key served under sent, filtered on filtered.
func served(source string, sent types.Identifier, filtered []types.Identifier, cs ...market.Listing) *fetchKey {
	return &fetchKey{Source: source, Outcome: gen.FetchOutcomeServed, SentID: &sent, Response: market.IdentityResult{Filtered: filtered, Listings: cs}, AnsweredAt: answeredAt}
}

// nextDay moves fk's answer to the day after answeredAt.
func nextDay(fk *fetchKey) *fetchKey {
	fk.AnsweredAt = answeredAt.AddDate(0, 0, 1)
	return fk
}

// limited marks fk as an answer from a provider that lists only some of an
// instrument's listings.
func limited(fk *fetchKey) *fetchKey {
	fk.Response.Limited = true
	return fk
}

func TestClaims(t *testing.T) {
	tests := []struct {
		name string
		r    *fetchKey
		want []*identityClaim
		// named holds, for each claim, whether it names the identifier sent.
		named []bool
	}{
		{
			name: "a shared instrument identifier joins listings",
			r: served("a", isin, nil,
				listing(types.AssetClassStock, "GBP", figi, isin, comp, xlon),
				listing(types.AssetClassStock, "GBP", figi, xetr),
				listing(types.AssetClassStock, "USD", figi2, xnas),
			),
			want: []*identityClaim{
				{Instr: types.Instr{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi, isin}, Listings: map[string]types.Listing{
					"GBP": {Identifiers: []types.Identifier{comp, xlon, xetr}},
				}}},
				{Instr: types.Instr{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi2}, Listings: map[string]types.Listing{
					"USD": {Identifiers: []types.Identifier{xnas}},
				}}},
			},
			named: []bool{true, false},
		},
		{
			name: "a strict filter is one claim identified by the identifier",
			r: served("a", isin, []types.Identifier{isin},
				listing(types.AssetClassUnknown, "GBP", xlon),
				listing(types.AssetClassStock, "USD", figi, xnas),
			),
			want: []*identityClaim{
				{Instr: types.Instr{Class: types.AssetClassStock, Identifiers: []types.Identifier{isin, figi}, Listings: map[string]types.Listing{
					"GBP": {Identifiers: []types.Identifier{xlon}},
					"USD": {Identifiers: []types.Identifier{xnas}},
				}}},
			},
			named: []bool{true},
		},
		{
			name: "no currency is a listing of no family, and the venue sent names the claim",
			r: served("a", xlon, []types.Identifier{ticker},
				listing(types.AssetClassStock, "", figi, xlon),
				listing(types.AssetClassStock, "", figi, xnas),
			),
			want: []*identityClaim{
				{Instr: types.Instr{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi}, Listings: map[string]types.Listing{
					"": {Identifiers: []types.Identifier{xlon, xnas}},
				}}},
			},
			named: []bool{true},
		},
		{
			name: "a listing takes the primary venue of the first listing stating one",
			r: served("a", isin, []types.Identifier{isin},
				listing(types.AssetClassStock, "GBP", xetr),
				market.Listing{Class: types.AssetClassStock, Currency: "GBP", Primary: "XLON", Identifiers: []types.Identifier{xlon}},
				market.Listing{Class: types.AssetClassStock, Currency: "GBP", Primary: "XETR", Identifiers: []types.Identifier{comp}},
			),
			want: []*identityClaim{
				{Instr: types.Instr{Class: types.AssetClassStock, Identifiers: []types.Identifier{isin}, Listings: map[string]types.Listing{
					"GBP": {Identifiers: []types.Identifier{xetr, xlon, comp}, Primary: "XLON"},
				}}},
			},
			named: []bool{true},
		},
		{
			name: "no listings",
			r:    served("a", isin, nil),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := claims(tc.r, cur)
			var named []bool
			for i, c := range got {
				if c.fetchKey != tc.r {
					t.Errorf("claim %d names fetch key %p, want %p", i, c.fetchKey, tc.r)
				}
				named = append(named, c.named())
				c.fetchKey = nil
			}
			if diff := cmp.Diff(tc.named, named); diff != "" {
				t.Errorf("named mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(identityClaim{})); diff != "" {
				t.Errorf("claims mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// outcome renders a verdict for comparison, each claim named by its source
// and its position among the claims of its response.
type outcome struct {
	winner    string
	refused   string
	attached  []string
	findings  []string
	notNaming map[string]int
	offered   int
}

func label(c *identityClaim) string {
	switch {
	case c == nil:
		return ""
	case c.fetchKey == nil:
		return "database"
	}
	i := slices.IndexFunc(claims(c.fetchKey, cur), func(o *identityClaim) bool { return cmp.Equal(o.Instr, c.Instr) })
	return fmt.Sprintf("%s#%d", c.fetchKey.Source, i)
}

// describe writes verdict, naming each finding's fetch key by its source.
func describe(verdict claimVerdict, source map[uuid.UUID]string) outcome {
	o := outcome{winner: label(verdict.winner), refused: verdict.refused, notNaming: verdict.notNaming, offered: len(verdict.offered)}
	for _, c := range verdict.attached {
		o.attached = append(o.attached, label(c))
	}
	for _, f := range verdict.findings {
		if f.RunID != uuid.Nil || f.StatedKeyID != nil || f.FetchKeyID == nil || f.Detail == nil || (f.Step != nil) != (f.Kind == gen.FindingKindDropped) {
			panic(fmt.Sprintf("finding %+v, want the fetch key and detail, a step only when dropped, and nothing else", f))
		}
		s := fmt.Sprintf("%s %s", source[*f.FetchKeyID], f.Kind)
		if f.Step != nil {
			s += " " + string(*f.Step)
		}
		o.findings = append(o.findings, s+": "+*f.Detail)
	}
	return o
}

func TestChoose(t *testing.T) {
	stock := ptr.To(types.AssetClassStock)
	other := id(types.IdentifierTypeCusip, "", "92857W309")
	strict := func(source string, sent types.Identifier, cs ...market.Listing) *fetchKey {
		return served(source, sent, []types.Identifier{sent}, cs...)
	}
	search := func(source string, sent types.Identifier, cs ...market.Listing) *fetchKey {
		return served(source, sent, []types.Identifier{ticker}, cs...)
	}
	existing := &identityClaim{Instr: types.Instr{Class: types.AssetClassStock, Identifiers: []types.Identifier{isin, cusip}, Listings: map[string]types.Listing{
		"GBP": {Identifiers: []types.Identifier{comp, xlon}},
	}}}
	tests := []struct {
		name      string
		fetchKeys []*fetchKey
		k         gen.StatedKey
		known     *identityClaim
		want      outcome
	}{
		{
			name: "a claim not naming the identifier sent is dropped",
			fetchKeys: []*fetchKey{served("a", isin, nil,
				listing(types.AssetClassStock, "GBP", figi2, xnas),
				listing(types.AssetClassStock, "GBP", figi, isin, xlon),
			)},
			k:    gen.StatedKey{Identifiers: []types.Identifier{isin}},
			want: outcome{winner: "a#1", attached: []string{"a#1"}, notNaming: map[string]int{"a": 1}},
		},
		{
			name:      "a listing in no stated family contradicts the statement",
			fetchKeys: []*fetchKey{strict("a", isin, listing(types.AssetClassStock, "USD", figi, xnas))},
			k:         gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin}},
			want:      outcome{findings: []string{"a dropped stated: isin GB00BH4HKS39: stated GBP has no listing among USD (a)"}},
		},
		{
			name:      "a listing of no family does not",
			fetchKeys: []*fetchKey{strict("a", isin, listing(types.AssetClassStock, "", figi, xnas))},
			k:         gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin}},
			want:      outcome{winner: "a#0", attached: []string{"a#0"}},
		},
		{
			name:      "a limited datasource's listing in no stated family contradicts nothing",
			fetchKeys: []*fetchKey{limited(strict("m", isin, listing(types.AssetClassStock, "USD", figi, xnas)))},
			k:         gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin}},
			want:      outcome{winner: "m#0", attached: []string{"m#0"}},
		},
		{
			name: "below the winner a limited claim in no stated family attaches through a stable identifier",
			fetchKeys: []*fetchKey{
				strict("a", isin, listing(types.AssetClassStock, "GBP", figi, xlon)),
				limited(strict("m", isin, listing(types.AssetClassStock, "USD", figi, xnas))),
			},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin}},
			want: outcome{winner: "a#0", attached: []string{"a#0", "m#0"}},
		},
		{
			name: "below the winner a limited claim sharing no stable identifier, answered on another day, is dropped",
			fetchKeys: []*fetchKey{
				strict("a", isin, listing(types.AssetClassStock, "GBP", figi, xlon)),
				nextDay(limited(search("m", xlon, listing(types.AssetClassStock, "USD", xlon)))),
			},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin, xlon}},
			want: outcome{winner: "a#0", attached: []string{"a#0"}, findings: []string{"m dropped corroboration: mic_ticker XLON:VOD: shares neither a stable identifier nor a same-day ticker in one market with the candidate identified by isin GB00BH4HKS39 (a)"}},
		},
		{
			name:      "a disjoint class contradicts the statement",
			fetchKeys: []*fetchKey{strict("a", isin, listing(types.AssetClassEtf, "GBP", figi, xlon))},
			k:         gen.StatedKey{AssetClass: stock, Identifiers: []types.Identifier{isin}},
			want:      outcome{findings: []string{"a dropped stated: isin GB00BH4HKS39: stated stock contradicts the class etf (a)"}},
		},
		{
			name:      "an unknown class does not",
			fetchKeys: []*fetchKey{strict("a", isin, listing(types.AssetClassUnknown, "GBP", figi, xlon))},
			k:         gen.StatedKey{AssetClass: stock, Identifiers: []types.Identifier{isin}},
			want:      outcome{winner: "a#0", attached: []string{"a#0"}},
		},
		{
			name:      "an identifier of a stated type with another value contradicts the statement",
			fetchKeys: []*fetchKey{strict("a", isin, listing(types.AssetClassStock, "GBP", figi, other, xlon))},
			k:         gen.StatedKey{Identifiers: []types.Identifier{cusip, isin}},
			want:      outcome{findings: []string{"a dropped stated: isin GB00BH4HKS39: stated cusip 92857W308 contradicts cusip 92857W309 (a)"}},
		},
		{
			name: "the winner is the highest precedence datasource with a survivor, and the rest attach",
			fetchKeys: []*fetchKey{
				strict("a", isin, listing(types.AssetClassStock, "GBP", figi, xlon)),
				strict("b", isin, listing(types.AssetClassStock, "GBP", isin, cusip, comp)),
			},
			k:    gen.StatedKey{Identifiers: []types.Identifier{isin}},
			want: outcome{winner: "a#0", attached: []string{"a#0", "b#0"}},
		},
		{
			name: "a claim inconsistent with one chosen above is dropped",
			fetchKeys: []*fetchKey{
				strict("a", isin, listing(types.AssetClassStock, "GBP", figi, cusip, xlon)),
				strict("b", isin, listing(types.AssetClassStock, "GBP", figi, other)),
				strict("c", isin, listing(types.AssetClassEtf, "GBP", figi)),
			},
			k: gen.StatedKey{Identifiers: []types.Identifier{isin}},
			want: outcome{winner: "a#0", attached: []string{"a#0"}, findings: []string{
				"b dropped precedence: isin GB00BH4HKS39: cusip 92857W309 contradicts cusip 92857W308 (a)",
				"c dropped precedence: isin GB00BH4HKS39: class etf contradicts the class stock (a)",
			}},
		},
		{
			name: "the database names the key and a contradicting response is a contradiction",
			fetchKeys: []*fetchKey{
				strict("a", isin, listing(types.AssetClassStock, "GBP", figi, other, xlon)),
				strict("b", isin, listing(types.AssetClassStock, "GBP", figi, id(types.IdentifierTypeMicTicker, "XLON", "VODL"))),
			},
			k:     gen.StatedKey{Identifiers: []types.Identifier{isin}},
			known: existing,
			want: outcome{winner: "database", findings: []string{
				"a contradiction: isin GB00BH4HKS39: cusip 92857W309 contradicts cusip 92857W308 (database)",
				"b contradiction: isin GB00BH4HKS39: mic_ticker XLON:VODL contradicts mic_ticker XLON:VOD (database)",
			}},
		},
		{
			name:      "a ticker of another currency line at the venue contradicts nothing",
			fetchKeys: []*fetchKey{strict("a", isin, listing(types.AssetClassStock, "USD", figi, id(types.IdentifierTypeMicTicker, "XLON", "VODUSD")))},
			k:         gen.StatedKey{Identifiers: []types.Identifier{isin}},
			known:     existing,
			want:      outcome{winner: "database", attached: []string{"a#0"}},
		},
		{
			name:      "a second composite in a listing contradicts nothing",
			fetchKeys: []*fetchKey{strict("a", isin, listing(types.AssetClassStock, "GBP", figi, id(types.IdentifierTypeOpenfigiComposite, "", "BBG000C6K6H0")))},
			k:         gen.StatedKey{Identifiers: []types.Identifier{isin}},
			known:     existing,
			want:      outcome{winner: "database", attached: []string{"a#0"}},
		},
		{
			name:      "a claim does not join the database's instrument through a ticker",
			fetchKeys: []*fetchKey{search("a", xlon, listing(types.AssetClassStock, "GBP", figi, xlon))},
			k:         gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			known:     existing,
			want:      outcome{winner: "database", findings: []string{"a dropped corroboration: openfigi_share_class BBG001S5XDT5: shares neither a stable identifier nor a same-day ticker in one market with the instrument identified by isin GB00BH4HKS39 (database)"}},
		},
		{
			name: "a lower datasource corroborates the winner through a stable identifier",
			fetchKeys: []*fetchKey{
				strict("a", isin, listing(types.AssetClassStock, "GBP", figi, xlon)),
				search("b", xlon, listing(types.AssetClassStock, "GBP", figi, xlon), listing(types.AssetClassStock, "GBP", figi2, xnas)),
			},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin, xlon}},
			want: outcome{winner: "a#0", attached: []string{"a#0", "b#0"}, notNaming: map[string]int{"b": 1}},
		},
		{
			name: "two answers on one day sharing a venue ticker attach",
			fetchKeys: []*fetchKey{
				strict("a", isin, listing(types.AssetClassStock, "GBP", figi, xlon)),
				search("b", xlon, listing(types.AssetClassStock, "GBP", cusip, xlon)),
			},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin, xlon}},
			want: outcome{winner: "a#0", attached: []string{"a#0", "b#0"}},
		},
		{
			name: "two answers on one day sharing a ticker at two venues of a market attach",
			fetchKeys: []*fetchKey{
				strict("a", isin, listing(types.AssetClassStock, "USD", figi, xnas)),
				search("b", bats, listing(types.AssetClassStock, "USD", cusip, bats)),
			},
			k:    gen.StatedKey{Currency: ptr.To("USD"), Identifiers: []types.Identifier{isin, xnas}},
			want: outcome{winner: "a#0", attached: []string{"a#0", "b#0"}},
		},
		{
			name: "two answers on one day sharing a value in two markets do not",
			fetchKeys: []*fetchKey{
				strict("a", isin, listing(types.AssetClassStock, "USD", figi, xnas)),
				search("b", xlon, listing(types.AssetClassStock, "USD", cusip, xlon)),
			},
			k:    gen.StatedKey{Currency: ptr.To("USD"), Identifiers: []types.Identifier{isin, xnas}},
			want: outcome{winner: "a#0", attached: []string{"a#0"}, findings: []string{"b dropped corroboration: cusip 92857W308: shares neither a stable identifier nor a same-day ticker in one market with the candidate identified by isin GB00BH4HKS39 (a)"}},
		},
		{
			name: "two answers on different days do not",
			fetchKeys: []*fetchKey{
				strict("a", isin, listing(types.AssetClassStock, "GBP", figi, xlon)),
				nextDay(search("b", xlon, listing(types.AssetClassStock, "GBP", cusip, xlon))),
			},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin, xlon}},
			want: outcome{winner: "a#0", attached: []string{"a#0"}, findings: []string{"b dropped corroboration: cusip 92857W308: shares neither a stable identifier nor a same-day ticker in one market with the candidate identified by isin GB00BH4HKS39 (a)"}},
		},
		{
			name: "the precedence step drops a contradicting claim that shares a venue ticker",
			fetchKeys: []*fetchKey{
				strict("a", isin, listing(types.AssetClassStock, "GBP", figi, cusip, xlon)),
				search("b", xlon, listing(types.AssetClassStock, "GBP", other, xlon)),
			},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin, xlon}},
			want: outcome{winner: "a#0", attached: []string{"a#0"}, findings: []string{"b dropped precedence: cusip 92857W309: cusip 92857W309 contradicts cusip 92857W308 (a)"}},
		},
		{
			name: "survivors rank by the stated data they confirm, then the datasource's order",
			fetchKeys: []*fetchKey{search("a", xlon,
				listing(types.AssetClassStock, "GBP", figi, xlon),
				listing(types.AssetClassStock, "GBP", figi2, cusip, xlon),
				listing(types.AssetClassStock, "GBP", id(types.IdentifierTypeOpenfigiShareClass, "", "BBG001S5XDT7"), xlon),
			)},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{cusip, xlon}},
			want: outcome{winner: "a#1", attached: []string{"a#1"}},
		},
		{
			name:      "the claim at the stated venue names the key",
			fetchKeys: []*fetchKey{search("a", xlon, listing(types.AssetClassStock, "GBP", figi2, xnas), listing(types.AssetClassStock, "GBP", figi, xlon))},
			k:         gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			want:      outcome{winner: "a#1", attached: []string{"a#1"}, notNaming: map[string]int{"a": 1}},
		},
		{
			name:      "a mis-stated venue associates with the one claim that survives",
			fetchKeys: []*fetchKey{search("a", xlon, listing(types.AssetClassStock, "GBP", figi, xetr), listing(types.AssetClassStock, "USD", figi2, xnas))},
			k:         gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			want:      outcome{winner: "a#0", attached: []string{"a#0"}, findings: []string{"a dropped stated: openfigi_share_class BBG001S5XDT6: stated GBP has no listing among USD (a)"}},
		},
		{
			name:      "a mis-stated venue with several survivors offers them",
			fetchKeys: []*fetchKey{search("a", xlon, listing(types.AssetClassStock, "GBP", figi, xetr), listing(types.AssetClassStock, "GBP", figi2, xnas))},
			k:         gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			want:      outcome{offered: 2},
		},
		{
			name:      "a mis-stated venue with the key found narrows to the claim corroborating the instrument",
			fetchKeys: []*fetchKey{search("a", xlon, listing(types.AssetClassStock, "GBP", figi2, xnas), listing(types.AssetClassStock, "GBP", isin, xetr))},
			k:         gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			known:     existing,
			want:      outcome{winner: "database", attached: []string{"a#1"}, findings: []string{"a dropped corroboration: openfigi_share_class BBG001S5XDT6: shares neither a stable identifier nor a same-day ticker in one market with the instrument identified by isin GB00BH4HKS39 (database)"}},
		},
		{
			name:      "a bare ticker is never chosen, and its claims are offered",
			fetchKeys: []*fetchKey{search("a", ticker, listing(types.AssetClassStock, "GBP", figi, xlon))},
			k:         gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want:      outcome{notNaming: map[string]int{"a": 1}, offered: 1},
		},
		{
			name: "two datasources describing one security under a bare ticker offer one candidate",
			fetchKeys: []*fetchKey{
				search("a", ticker, listing(types.AssetClassStock, "GBP", figi, xlon)),
				search("b", ticker, listing(types.AssetClassStock, "GBP", figi, isin, xlon)),
			},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want: outcome{notNaming: map[string]int{"a": 1, "b": 1}, offered: 1},
		},
		{
			name:      "a claim contradicting the statement is not offered",
			fetchKeys: []*fetchKey{search("a", ticker, listing(types.AssetClassStock, "GBP", figi, xlon), listing(types.AssetClassStock, "USD", figi2, xnas))},
			k:         gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want:      outcome{notNaming: map[string]int{"a": 2}, offered: 1},
		},
		{
			name:      "nothing served",
			fetchKeys: []*fetchKey{strict("a", isin), {Source: "b", Outcome: gen.FetchOutcomeFailedTemporary}},
			k:         gen.StatedKey{Identifiers: []types.Identifier{isin}},
			want:      outcome{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := map[uuid.UUID]string{}
			for i, fk := range tc.fetchKeys {
				fk.ID = uuid.UUID{byte(i + 1)}
				source[fk.ID] = fk.Source
			}
			if tc.want.notNaming == nil {
				tc.want.notNaming = map[string]int{}
			}
			got := describe(choose(&resolution{row: tc.k, fetchKeys: tc.fetchKeys, family: keyFamily(cur, tc.k), families: cur, mics: joinMICs}, tc.known, nil), source)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(outcome{})); diff != "" {
				t.Errorf("choose mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestChooseUserChoice checks the verdict of a confirmation: the head of
// the user's choice wins unless the database names the instrument, the
// other members attach where they agree with the claims that outrank them,
// the other datasources attach nothing, and a head that fails a check is
// refused.
func TestChooseUserChoice(t *testing.T) {
	strict := func(source string, sent types.Identifier, cs ...market.Listing) *fetchKey {
		return served(source, sent, []types.Identifier{sent}, cs...)
	}
	existing := &identityClaim{Instr: types.Instr{Class: types.AssetClassStock, Identifiers: []types.Identifier{isin, cusip}, Listings: map[string]types.Listing{
		"GBP": {Identifiers: []types.Identifier{comp, xlon}},
	}}}
	other := id(types.IdentifierTypeCusip, "", "92857W309")
	tests := []struct {
		name      string
		fetchKeys []*fetchKey
		// userChoice names, for each member, the fetch key and the claim
		// within it, the head first.
		userChoice [][2]int
		k          gen.StatedKey
		known      *identityClaim
		want       outcome
	}{
		{
			name: "the head wins and the members attach",
			fetchKeys: []*fetchKey{
				strict("a", ticker, listing(types.AssetClassStock, "GBP", figi, xlon)),
				strict("b", ticker, listing(types.AssetClassStock, "GBP", figi, isin, xlon)),
			},
			userChoice: [][2]int{{0, 0}, {1, 0}},
			k:          gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want:       outcome{winner: "a#0", attached: []string{"a#0", "b#0"}},
		},
		{
			name: "the user choice's datasource's other claims are passed over",
			fetchKeys: []*fetchKey{
				strict("a", ticker, listing(types.AssetClassStock, "GBP", figi, xlon), listing(types.AssetClassStock, "GBP", figi2, xetr)),
			},
			userChoice: [][2]int{{0, 0}},
			k:          gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want:       outcome{winner: "a#0", attached: []string{"a#0"}},
		},
		{
			name: "a datasource with no member attaches nothing, even where it agrees",
			fetchKeys: []*fetchKey{
				strict("a", ticker, listing(types.AssetClassStock, "GBP", figi, isin, xlon)),
				strict("b", ticker, listing(types.AssetClassStock, "GBP", isin, xlon)),
			},
			userChoice: [][2]int{{1, 0}},
			k:          gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want:       outcome{winner: "b#0", attached: []string{"b#0"}},
		},
		{
			name:       "a user choice whose listing is in no stated family is refused",
			fetchKeys:  []*fetchKey{strict("a", ticker, listing(types.AssetClassStock, "USD", figi, xnas))},
			userChoice: [][2]int{{0, 0}},
			k:          gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want:       outcome{refused: "openfigi_share_class BBG001S5XDT5: stated GBP has no listing among USD (a)"},
		},
		{
			name:       "a user choice contradicting the instrument the database names is refused",
			fetchKeys:  []*fetchKey{strict("a", ticker, listing(types.AssetClassStock, "GBP", figi, other, xlon))},
			userChoice: [][2]int{{0, 0}},
			k:          gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			known:      existing,
			want:       outcome{winner: "database", refused: "openfigi_share_class BBG001S5XDT5: cusip 92857W309 contradicts cusip 92857W308 (database)"},
		},
		{
			name:       "a user choice attaches to the instrument the database names",
			fetchKeys:  []*fetchKey{strict("a", ticker, listing(types.AssetClassStock, "GBP", figi, isin, xlon))},
			userChoice: [][2]int{{0, 0}},
			k:          gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			known:      existing,
			want:       outcome{winner: "database", attached: []string{"a#0"}},
		},
		{
			name: "a member contradicting the instrument the database names is a contradiction, and the others attach",
			fetchKeys: []*fetchKey{
				strict("a", ticker, listing(types.AssetClassStock, "GBP", isin, xlon)),
				strict("b", ticker, listing(types.AssetClassStock, "GBP", isin, other, xlon)),
				strict("c", ticker, listing(types.AssetClassStock, "GBP", isin, comp)),
			},
			userChoice: [][2]int{{0, 0}, {1, 0}, {2, 0}},
			k:          gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			known:      existing,
			want: outcome{winner: "database", attached: []string{"a#0", "c#0"}, findings: []string{
				"b contradiction: isin GB00BH4HKS39: cusip 92857W309 contradicts cusip 92857W308 (database)",
			}},
		},
		{
			name: "a member sharing no stable identifier with the instrument the database names is dropped",
			fetchKeys: []*fetchKey{
				strict("a", ticker, listing(types.AssetClassStock, "GBP", isin, xlon)),
				strict("b", ticker, listing(types.AssetClassStock, "GBP", figi, xlon)),
			},
			userChoice: [][2]int{{0, 0}, {1, 0}},
			k:          gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			known:      existing,
			want: outcome{winner: "database", attached: []string{"a#0"}, findings: []string{
				"b dropped corroboration: openfigi_share_class BBG001S5XDT5: shares neither a stable identifier nor a same-day ticker in one market with the instrument identified by isin GB00BH4HKS39 (database)",
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := map[uuid.UUID]string{}
			for i, fk := range tc.fetchKeys {
				fk.ID = uuid.UUID{byte(i + 1)}
				source[fk.ID] = fk.Source
			}
			if tc.want.notNaming == nil {
				tc.want.notNaming = map[string]int{}
			}
			res := &resolution{row: tc.k, fetchKeys: tc.fetchKeys, family: keyFamily(cur, tc.k), families: cur, mics: joinMICs}
			var userChoice consistentClaims
			for _, p := range tc.userChoice {
				userChoice = append(userChoice, claims(tc.fetchKeys[p[0]], cur)[p[1]])
			}
			got := describe(choose(res, tc.known, userChoice), source)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(outcome{})); diff != "" {
				t.Errorf("choose mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestGather checks which claims each candidate gathers: the head and, for
// each datasource below it, the best ranked claim that agrees with every
// member and corroborates the head.
func TestGather(t *testing.T) {
	other := id(types.IdentifierTypeCusip, "", "92857W309")
	search := func(source string, cs ...market.Listing) *fetchKey {
		return served(source, ticker, []types.Identifier{ticker}, cs...)
	}
	bare := gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}}
	tests := []struct {
		name      string
		fetchKeys []*fetchKey
		k         gen.StatedKey
		want      [][]string
	}{
		{
			name: "two datasources sharing a stable identifier form one candidate",
			fetchKeys: []*fetchKey{
				search("a", listing(types.AssetClassStock, "GBP", figi, xlon)),
				search("b", listing(types.AssetClassStock, "GBP", figi, isin, xlon)),
			},
			k:    bare,
			want: [][]string{{"a#0", "b#0"}},
		},
		{
			name: "a claim corroborating a member but not the head heads its own candidate",
			fetchKeys: []*fetchKey{
				search("a", listing(types.AssetClassStock, "GBP", figi, xlon)),
				search("b", listing(types.AssetClassStock, "GBP", figi, isin, xlon)),
				search("c", listing(types.AssetClassStock, "GBP", isin, xetr)),
			},
			k:    bare,
			want: [][]string{{"a#0", "b#0"}, {"c#0"}},
		},
		{
			name: "two datasources sharing a venue ticker on one day form one candidate",
			fetchKeys: []*fetchKey{
				search("a", listing(types.AssetClassStock, "GBP", figi, xlon)),
				search("b", listing(types.AssetClassStock, "GBP", isin, xlon)),
			},
			k:    bare,
			want: [][]string{{"a#0", "b#0"}},
		},
		{
			name: "two datasources sharing a venue ticker on different days head their own candidates",
			fetchKeys: []*fetchKey{
				search("a", listing(types.AssetClassStock, "GBP", figi, xlon)),
				nextDay(search("b", listing(types.AssetClassStock, "GBP", isin, xlon))),
			},
			k:    bare,
			want: [][]string{{"a#0"}, {"b#0"}},
		},
		{
			name: "a claim agreeing with two heads that contradict each other joins both",
			fetchKeys: []*fetchKey{
				search("a", listing(types.AssetClassStock, "GBP", figi, cusip, xlon)),
				search("b", listing(types.AssetClassStock, "GBP", figi, other, xlon)),
				search("c", listing(types.AssetClassStock, "GBP", figi, xlon)),
			},
			k:    bare,
			want: [][]string{{"a#0", "c#0"}, {"b#0", "c#0"}},
		},
		{
			name: "of two claims of one datasource agreeing with a head, the better ranked joins and the other heads its own candidate",
			fetchKeys: []*fetchKey{
				search("a", listing(types.AssetClassStock, "GBP", figi, isin, xlon)),
				search("b", listing(types.AssetClassStock, "GBP", figi, xetr), listing(types.AssetClassStock, "GBP", isin, xlon)),
			},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker, isin}},
			want: [][]string{{"a#0", "b#1"}, {"b#0"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for i, fk := range tc.fetchKeys {
				fk.ID = uuid.UUID{byte(i + 1)}
			}
			res := &resolution{row: tc.k, fetchKeys: tc.fetchKeys, family: keyFamily(cur, tc.k), families: cur, mics: joinMICs}
			cs, _, _ := offeredClaims(res)
			var got [][]string
			for _, a := range gatherConsistentClaims(res, cs) {
				var labels []string
				for _, c := range a {
					labels = append(labels, label(c))
				}
				got = append(got, labels)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("gather mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestExclusive checks that a second value of an exclusive type contradicts
// the first, and a second description of one broker does not.
func TestExclusive(t *testing.T) {
	isin := id(types.IdentifierTypeIsin, "", "GB00BH4HKS39")
	other := id(types.IdentifierTypeIsin, "", "US0378331005")
	descr := id(types.IdentifierTypeBrokerDescription, "ibkr", "VODAFONE GROUP PLC")
	renamed := id(types.IdentifierTypeBrokerDescription, "ibkr", "VODAFONE GRP")
	c := &identityClaim{Instr: types.Instr{Identifiers: []types.Identifier{isin, descr}}}
	if got, ok := c.contradicts(other, noFamily); !ok || got != isin {
		t.Errorf("contradicts(%s) = %v, %v, want the isin", other.Name(), got, ok)
	}
	if got, ok := c.contradicts(renamed, noFamily); ok {
		t.Errorf("contradicts(%s) = %v, want no contradiction between two descriptions", renamed.Name(), got)
	}
}
