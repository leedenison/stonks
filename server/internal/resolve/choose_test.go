package resolve

import (
	"fmt"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/ptr"
)

func id(t types.IdentifierType, domain, value string) types.Identifier {
	return types.Identifier{Type: t, Domain: domain, Value: value}
}

func cand(class types.AssetClass, currency string, ids ...types.Identifier) market.Candidate {
	return market.Candidate{Class: class, Currency: currency, Identifiers: ids}
}

// cur maps GBX to GBP and every other code to itself.
var cur = currencies{"EUR": "EUR", "GBP": "GBP", "GBX": "GBP", "USD": "USD"}

var (
	isin   = id(types.IdentifierTypeIsin, "", "GB00BH4HKS39")
	cusip  = id(types.IdentifierTypeCusip, "", "92857W308")
	figi   = id(types.IdentifierTypeOpenfigiShareClass, "", "BBG001S5XDT5")
	figi2  = id(types.IdentifierTypeOpenfigiShareClass, "", "BBG001S5XDT6")
	comp   = id(types.IdentifierTypeOpenfigiComposite, "", "BBG000C6K6G9")
	xlon   = id(types.IdentifierTypeMicTicker, "XLON", "VOD")
	xnas   = id(types.IdentifierTypeMicTicker, "XNAS", "VOD")
	xetr   = id(types.IdentifierTypeMicTicker, "XETR", "VODI")
	ticker = id(types.IdentifierTypeMicTicker, "", "VOD")
	descr  = id(types.IdentifierTypeBrokerDescription, "ibkr", "ACME CORP")
)

// served returns a result served under sent, filtered on filtered.
func served(source string, sent types.Identifier, filtered []types.Identifier, cs ...market.Candidate) *result {
	return &result{Source: source, Outcome: gen.FetchOutcomeServed, Sent: &sent, Response: market.IdentityResult{Filtered: filtered, Candidates: cs}}
}

// limited marks r as an answer from a provider that lists only some of an
// instrument's listings.
func limited(r *result) *result {
	r.Response.Limited = true
	return r
}

func TestClaims(t *testing.T) {
	tests := []struct {
		name string
		r    *result
		want []*claim
		// named holds, for each claim, whether it names the identifier sent.
		named []bool
	}{
		{
			name: "a shared instrument identifier joins candidates",
			r: served("a", isin, nil,
				cand(types.AssetClassStock, "GBP", figi, isin, comp, xlon),
				cand(types.AssetClassStock, "GBP", figi, xetr),
				cand(types.AssetClassStock, "USD", figi2, xnas),
			),
			want: []*claim{
				{Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi, isin}, Listings: map[string]types.Listing{
					"GBP": {Identifiers: []types.Identifier{comp, xlon, xetr}},
				}}},
				{Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi2}, Listings: map[string]types.Listing{
					"USD": {Identifiers: []types.Identifier{xnas}},
				}}},
			},
			named: []bool{true, false},
		},
		{
			name: "a strict filter is one claim identified by the identifier",
			r: served("a", isin, []types.Identifier{isin},
				cand(types.AssetClassUnknown, "GBP", xlon),
				cand(types.AssetClassStock, "USD", figi, xnas),
			),
			want: []*claim{
				{Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{isin, figi}, Listings: map[string]types.Listing{
					"GBP": {Identifiers: []types.Identifier{xlon}},
					"USD": {Identifiers: []types.Identifier{xnas}},
				}}},
			},
			named: []bool{true},
		},
		{
			name: "no currency is a listing of no family, and the venue sent names the claim",
			r: served("a", xlon, []types.Identifier{ticker},
				cand(types.AssetClassStock, "", figi, xlon),
				cand(types.AssetClassStock, "", figi, xnas),
			),
			want: []*claim{
				{Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi}, Listings: map[string]types.Listing{
					"": {Identifiers: []types.Identifier{xlon, xnas}},
				}}},
			},
			named: []bool{true},
		},
		{
			name: "a listing takes the primary venue of the first candidate stating one",
			r: served("a", isin, []types.Identifier{isin},
				cand(types.AssetClassStock, "GBP", xetr),
				market.Candidate{Class: types.AssetClassStock, Currency: "GBP", Primary: "XLON", Identifiers: []types.Identifier{xlon}},
				market.Candidate{Class: types.AssetClassStock, Currency: "GBP", Primary: "XETR", Identifiers: []types.Identifier{comp}},
			),
			want: []*claim{
				{Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{isin}, Listings: map[string]types.Listing{
					"GBP": {Identifiers: []types.Identifier{xetr, xlon, comp}, Primary: "XLON"},
				}}},
			},
			named: []bool{true},
		},
		{
			name: "no candidates",
			r:    served("a", isin, nil),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := claims(tc.r)
			var named []bool
			for i, c := range got {
				if c.query != tc.r {
					t.Errorf("claim %d names result %p, want %p", i, c.query, tc.r)
				}
				named = append(named, c.named())
				c.query = nil
			}
			if diff := cmp.Diff(tc.named, named); diff != "" {
				t.Errorf("named mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(claim{})); diff != "" {
				t.Errorf("claims mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// outcome renders a choice for comparison, each claim named by its source
// and its position among the claims of its response.
type outcome struct {
	winner    string
	refused   string
	attached  []string
	findings  []string
	notNaming map[string]int
	offered   int
}

func label(c *claim) string {
	switch {
	case c == nil:
		return ""
	case c.query == nil:
		return "database"
	}
	i := slices.IndexFunc(claims(c.query), func(o *claim) bool { return cmp.Equal(o.Instrument, c.Instrument) })
	return fmt.Sprintf("%s#%d", c.query.Source, i)
}

// describe writes ch, naming each finding's fetch key by its source.
func describe(ch choice, source map[uuid.UUID]string) outcome {
	o := outcome{winner: label(ch.winner), refused: ch.refused, notNaming: ch.notNaming, offered: len(ch.offered)}
	for _, c := range ch.attached {
		o.attached = append(o.attached, label(c))
	}
	for _, f := range ch.findings {
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
	strict := func(source string, sent types.Identifier, cs ...market.Candidate) *result {
		return served(source, sent, []types.Identifier{sent}, cs...)
	}
	search := func(source string, sent types.Identifier, cs ...market.Candidate) *result {
		return served(source, sent, []types.Identifier{ticker}, cs...)
	}
	existing := &claim{Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{isin, cusip}, Listings: map[string]types.Listing{
		"GBP": {Identifiers: []types.Identifier{comp, xlon}},
	}}}
	tests := []struct {
		name    string
		results []*result
		k       gen.StatedKey
		db      *claim
		want    outcome
	}{
		{
			name: "a claim not naming the identifier sent is dropped",
			results: []*result{served("a", isin, nil,
				cand(types.AssetClassStock, "GBP", figi2, xnas),
				cand(types.AssetClassStock, "GBP", figi, isin, xlon),
			)},
			k:    gen.StatedKey{Identifiers: []types.Identifier{isin}},
			want: outcome{winner: "a#1", attached: []string{"a#1"}, notNaming: map[string]int{"a": 1}},
		},
		{
			name:    "a listing in no stated family contradicts the statement",
			results: []*result{strict("a", isin, cand(types.AssetClassStock, "USD", figi, xnas))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin}},
			want:    outcome{findings: []string{"a dropped stated: isin GB00BH4HKS39: stated GBP has no listing among USD (a)"}},
		},
		{
			name:    "a listing of no family does not",
			results: []*result{strict("a", isin, cand(types.AssetClassStock, "", figi, xnas))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin}},
			want:    outcome{winner: "a#0", attached: []string{"a#0"}},
		},
		{
			name:    "a limited datasource's listing in no stated family contradicts nothing",
			results: []*result{limited(strict("m", isin, cand(types.AssetClassStock, "USD", figi, xnas)))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin}},
			want:    outcome{winner: "m#0", attached: []string{"m#0"}},
		},
		{
			name: "below the winner a limited claim in no stated family attaches through a stable identifier",
			results: []*result{
				strict("a", isin, cand(types.AssetClassStock, "GBP", figi, xlon)),
				limited(strict("m", isin, cand(types.AssetClassStock, "USD", figi, xnas))),
			},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin}},
			want: outcome{winner: "a#0", attached: []string{"a#0", "m#0"}},
		},
		{
			name: "below the winner a limited claim sharing no stable identifier is dropped",
			results: []*result{
				strict("a", isin, cand(types.AssetClassStock, "GBP", figi, xlon)),
				limited(search("m", xlon, cand(types.AssetClassStock, "USD", xlon))),
			},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin, xlon}},
			want: outcome{winner: "a#0", attached: []string{"a#0"}, findings: []string{"m dropped corroboration: mic_ticker XLON:VOD: shares no stable identifier with the candidate identified by isin GB00BH4HKS39 (a)"}},
		},
		{
			name:    "a disjoint class contradicts the statement",
			results: []*result{strict("a", isin, cand(types.AssetClassEtf, "GBP", figi, xlon))},
			k:       gen.StatedKey{AssetClass: stock, Identifiers: []types.Identifier{isin}},
			want:    outcome{findings: []string{"a dropped stated: isin GB00BH4HKS39: stated stock contradicts the class etf (a)"}},
		},
		{
			name:    "an unknown class does not",
			results: []*result{strict("a", isin, cand(types.AssetClassUnknown, "GBP", figi, xlon))},
			k:       gen.StatedKey{AssetClass: stock, Identifiers: []types.Identifier{isin}},
			want:    outcome{winner: "a#0", attached: []string{"a#0"}},
		},
		{
			name:    "an identifier of a stated type with another value contradicts the statement",
			results: []*result{strict("a", isin, cand(types.AssetClassStock, "GBP", figi, other, xlon))},
			k:       gen.StatedKey{Identifiers: []types.Identifier{cusip, isin}},
			want:    outcome{findings: []string{"a dropped stated: isin GB00BH4HKS39: stated cusip 92857W308 contradicts cusip 92857W309 (a)"}},
		},
		{
			name: "the winner is the highest precedence datasource with a survivor, and the rest attach",
			results: []*result{
				strict("a", isin, cand(types.AssetClassStock, "GBP", figi, xlon)),
				strict("b", isin, cand(types.AssetClassStock, "GBP", isin, cusip, comp)),
			},
			k:    gen.StatedKey{Identifiers: []types.Identifier{isin}},
			want: outcome{winner: "a#0", attached: []string{"a#0", "b#0"}},
		},
		{
			name: "a claim inconsistent with one chosen above is dropped",
			results: []*result{
				strict("a", isin, cand(types.AssetClassStock, "GBP", figi, cusip, xlon)),
				strict("b", isin, cand(types.AssetClassStock, "GBP", figi, other)),
				strict("c", isin, cand(types.AssetClassEtf, "GBP", figi)),
			},
			k: gen.StatedKey{Identifiers: []types.Identifier{isin}},
			want: outcome{winner: "a#0", attached: []string{"a#0"}, findings: []string{
				"b dropped precedence: isin GB00BH4HKS39: cusip 92857W309 contradicts cusip 92857W308 (a)",
				"c dropped precedence: isin GB00BH4HKS39: class etf contradicts the class stock (a)",
			}},
		},
		{
			name: "the database names the key and a contradicting response is a contradiction",
			results: []*result{
				strict("a", isin, cand(types.AssetClassStock, "GBP", figi, other, xlon)),
				strict("b", isin, cand(types.AssetClassStock, "GBP", figi, id(types.IdentifierTypeMicTicker, "XLON", "VODL"))),
			},
			k:  gen.StatedKey{Identifiers: []types.Identifier{isin}},
			db: existing,
			want: outcome{winner: "database", findings: []string{
				"a contradiction: isin GB00BH4HKS39: cusip 92857W309 contradicts cusip 92857W308 (database)",
				"b contradiction: isin GB00BH4HKS39: mic_ticker XLON:VODL contradicts mic_ticker XLON:VOD (database)",
			}},
		},
		{
			name:    "a ticker of another currency line at the venue contradicts nothing",
			results: []*result{strict("a", isin, cand(types.AssetClassStock, "USD", figi, id(types.IdentifierTypeMicTicker, "XLON", "VODUSD")))},
			k:       gen.StatedKey{Identifiers: []types.Identifier{isin}},
			db:      existing,
			want:    outcome{winner: "database", attached: []string{"a#0"}},
		},
		{
			name:    "a second composite in a listing contradicts nothing",
			results: []*result{strict("a", isin, cand(types.AssetClassStock, "GBP", figi, id(types.IdentifierTypeOpenfigiComposite, "", "BBG000C6K6H0")))},
			k:       gen.StatedKey{Identifiers: []types.Identifier{isin}},
			db:      existing,
			want:    outcome{winner: "database", attached: []string{"a#0"}},
		},
		{
			name:    "a claim sharing no stable identifier with the winner is dropped",
			results: []*result{search("a", xlon, cand(types.AssetClassStock, "GBP", figi, xlon))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			db:      existing,
			want:    outcome{winner: "database", findings: []string{"a dropped corroboration: openfigi_share_class BBG001S5XDT5: shares no stable identifier with the instrument identified by isin GB00BH4HKS39 (database)"}},
		},
		{
			name: "a lower datasource corroborates the winner through a stable identifier",
			results: []*result{
				strict("a", isin, cand(types.AssetClassStock, "GBP", figi, xlon)),
				search("b", xlon, cand(types.AssetClassStock, "GBP", figi, xlon), cand(types.AssetClassStock, "GBP", figi2, xnas)),
			},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin, xlon}},
			want: outcome{winner: "a#0", attached: []string{"a#0", "b#0"}, notNaming: map[string]int{"b": 1}},
		},
		{
			name: "survivors rank by the stated data they confirm, then the datasource's order",
			results: []*result{search("a", xlon,
				cand(types.AssetClassStock, "GBP", figi, xlon),
				cand(types.AssetClassStock, "GBP", figi2, cusip, xlon),
				cand(types.AssetClassStock, "GBP", id(types.IdentifierTypeOpenfigiShareClass, "", "BBG001S5XDT7"), xlon),
			)},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{cusip, xlon}},
			want: outcome{winner: "a#1", attached: []string{"a#1"}},
		},
		{
			name:    "the claim at the stated venue names the key",
			results: []*result{search("a", xlon, cand(types.AssetClassStock, "GBP", figi2, xnas), cand(types.AssetClassStock, "GBP", figi, xlon))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			want:    outcome{winner: "a#1", attached: []string{"a#1"}, notNaming: map[string]int{"a": 1}},
		},
		{
			name:    "a mis-stated venue associates with the one claim that survives",
			results: []*result{search("a", xlon, cand(types.AssetClassStock, "GBP", figi, xetr), cand(types.AssetClassStock, "USD", figi2, xnas))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			want:    outcome{winner: "a#0", attached: []string{"a#0"}, findings: []string{"a dropped stated: openfigi_share_class BBG001S5XDT6: stated GBP has no listing among USD (a)"}},
		},
		{
			name:    "a mis-stated venue with several survivors offers them",
			results: []*result{search("a", xlon, cand(types.AssetClassStock, "GBP", figi, xetr), cand(types.AssetClassStock, "GBP", figi2, xnas))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			want:    outcome{offered: 2},
		},
		{
			name:    "a mis-stated venue with the key found narrows to the claim corroborating the instrument",
			results: []*result{search("a", xlon, cand(types.AssetClassStock, "GBP", figi2, xnas), cand(types.AssetClassStock, "GBP", isin, xetr))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			db:      existing,
			want:    outcome{winner: "database", attached: []string{"a#1"}, findings: []string{"a dropped corroboration: openfigi_share_class BBG001S5XDT6: shares no stable identifier with the instrument identified by isin GB00BH4HKS39 (database)"}},
		},
		{
			name:    "a bare ticker is never chosen, and its claims are offered",
			results: []*result{search("a", ticker, cand(types.AssetClassStock, "GBP", figi, xlon))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want:    outcome{notNaming: map[string]int{"a": 1}, offered: 1},
		},
		{
			name: "two datasources describing one security under a bare ticker offer one candidate",
			results: []*result{
				search("a", ticker, cand(types.AssetClassStock, "GBP", figi, xlon)),
				search("b", ticker, cand(types.AssetClassStock, "GBP", figi, isin, xlon)),
			},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want: outcome{notNaming: map[string]int{"a": 1, "b": 1}, offered: 1},
		},
		{
			name:    "a claim contradicting the statement is not offered",
			results: []*result{search("a", ticker, cand(types.AssetClassStock, "GBP", figi, xlon), cand(types.AssetClassStock, "USD", figi2, xnas))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want:    outcome{notNaming: map[string]int{"a": 2}, offered: 1},
		},
		{
			name:    "nothing served",
			results: []*result{strict("a", isin), {Source: "b", Outcome: gen.FetchOutcomeFailedTemporary}},
			k:       gen.StatedKey{Identifiers: []types.Identifier{isin}},
			want:    outcome{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := map[uuid.UUID]string{}
			for i, r := range tc.results {
				r.ID = uuid.UUID{byte(i + 1)}
				source[r.ID] = r.Source
			}
			if tc.want.notNaming == nil {
				tc.want.notNaming = map[string]int{}
			}
			got := describe(choose(&resolution{row: tc.k, results: tc.results, fam: cur.family(tc.k)}, tc.db, nil), source)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(outcome{})); diff != "" {
				t.Errorf("choose mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestChoosePick checks the choice of a confirmation: the head of the pick
// wins unless the database names the instrument, the other members attach
// where they agree with the claims that outrank them, the other datasources
// attach nothing, and a head that fails a check is refused.
func TestChoosePick(t *testing.T) {
	strict := func(source string, sent types.Identifier, cs ...market.Candidate) *result {
		return served(source, sent, []types.Identifier{sent}, cs...)
	}
	existing := &claim{Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{isin, cusip}, Listings: map[string]types.Listing{
		"GBP": {Identifiers: []types.Identifier{comp, xlon}},
	}}}
	other := id(types.IdentifierTypeCusip, "", "92857W309")
	tests := []struct {
		name    string
		results []*result
		// pick names, for each member, the result and the claim within
		// it, the head first.
		pick [][2]int
		k    gen.StatedKey
		db   *claim
		want outcome
	}{
		{
			name: "the head wins and the members attach",
			results: []*result{
				strict("a", ticker, cand(types.AssetClassStock, "GBP", figi, xlon)),
				strict("b", ticker, cand(types.AssetClassStock, "GBP", figi, isin, xlon)),
			},
			pick: [][2]int{{0, 0}, {1, 0}},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want: outcome{winner: "a#0", attached: []string{"a#0", "b#0"}},
		},
		{
			name: "the pick's datasource's other claims are passed over",
			results: []*result{
				strict("a", ticker, cand(types.AssetClassStock, "GBP", figi, xlon), cand(types.AssetClassStock, "GBP", figi2, xetr)),
			},
			pick: [][2]int{{0, 0}},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want: outcome{winner: "a#0", attached: []string{"a#0"}},
		},
		{
			name: "a datasource with no member attaches nothing, even where it agrees",
			results: []*result{
				strict("a", ticker, cand(types.AssetClassStock, "GBP", figi, isin, xlon)),
				strict("b", ticker, cand(types.AssetClassStock, "GBP", isin, xlon)),
			},
			pick: [][2]int{{1, 0}},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want: outcome{winner: "b#0", attached: []string{"b#0"}},
		},
		{
			name:    "a pick whose listing is in no stated family is refused",
			results: []*result{strict("a", ticker, cand(types.AssetClassStock, "USD", figi, xnas))},
			pick:    [][2]int{{0, 0}},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want:    outcome{refused: "openfigi_share_class BBG001S5XDT5: stated GBP has no listing among USD (a)"},
		},
		{
			name:    "a pick contradicting the instrument the database names is refused",
			results: []*result{strict("a", ticker, cand(types.AssetClassStock, "GBP", figi, other, xlon))},
			pick:    [][2]int{{0, 0}},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			db:      existing,
			want:    outcome{winner: "database", refused: "openfigi_share_class BBG001S5XDT5: cusip 92857W309 contradicts cusip 92857W308 (database)"},
		},
		{
			name:    "a pick attaches to the instrument the database names",
			results: []*result{strict("a", ticker, cand(types.AssetClassStock, "GBP", figi, isin, xlon))},
			pick:    [][2]int{{0, 0}},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			db:      existing,
			want:    outcome{winner: "database", attached: []string{"a#0"}},
		},
		{
			name: "a member contradicting the instrument the database names is a contradiction, and the others attach",
			results: []*result{
				strict("a", ticker, cand(types.AssetClassStock, "GBP", isin, xlon)),
				strict("b", ticker, cand(types.AssetClassStock, "GBP", isin, other, xlon)),
				strict("c", ticker, cand(types.AssetClassStock, "GBP", isin, comp)),
			},
			pick: [][2]int{{0, 0}, {1, 0}, {2, 0}},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			db:   existing,
			want: outcome{winner: "database", attached: []string{"a#0", "c#0"}, findings: []string{
				"b contradiction: isin GB00BH4HKS39: cusip 92857W309 contradicts cusip 92857W308 (database)",
			}},
		},
		{
			name: "a member sharing no stable identifier with the instrument the database names is dropped",
			results: []*result{
				strict("a", ticker, cand(types.AssetClassStock, "GBP", isin, xlon)),
				strict("b", ticker, cand(types.AssetClassStock, "GBP", figi, xlon)),
			},
			pick: [][2]int{{0, 0}, {1, 0}},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			db:   existing,
			want: outcome{winner: "database", attached: []string{"a#0"}, findings: []string{
				"b dropped corroboration: openfigi_share_class BBG001S5XDT5: shares no stable identifier with the instrument identified by isin GB00BH4HKS39 (database)",
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := map[uuid.UUID]string{}
			for i, r := range tc.results {
				r.ID = uuid.UUID{byte(i + 1)}
				source[r.ID] = r.Source
			}
			if tc.want.notNaming == nil {
				tc.want.notNaming = map[string]int{}
			}
			res := &resolution{row: tc.k, results: tc.results, fam: cur.family(tc.k)}
			var pick agreement
			for _, p := range tc.pick {
				pick = append(pick, claims(tc.results[p[0]])[p[1]])
			}
			got := describe(choose(res, tc.db, pick), source)
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
	search := func(source string, cs ...market.Candidate) *result {
		return served(source, ticker, []types.Identifier{ticker}, cs...)
	}
	bare := gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}}
	tests := []struct {
		name    string
		results []*result
		k       gen.StatedKey
		want    [][]string
	}{
		{
			name: "two datasources sharing a stable identifier form one candidate",
			results: []*result{
				search("a", cand(types.AssetClassStock, "GBP", figi, xlon)),
				search("b", cand(types.AssetClassStock, "GBP", figi, isin, xlon)),
			},
			k:    bare,
			want: [][]string{{"a#0", "b#0"}},
		},
		{
			name: "a claim corroborating a member but not the head heads its own candidate",
			results: []*result{
				search("a", cand(types.AssetClassStock, "GBP", figi, xlon)),
				search("b", cand(types.AssetClassStock, "GBP", figi, isin, xlon)),
				search("c", cand(types.AssetClassStock, "GBP", isin, xlon)),
			},
			k:    bare,
			want: [][]string{{"a#0", "b#0"}, {"c#0"}},
		},
		{
			name: "a claim agreeing with two heads that contradict each other joins both",
			results: []*result{
				search("a", cand(types.AssetClassStock, "GBP", figi, cusip, xlon)),
				search("b", cand(types.AssetClassStock, "GBP", figi, other, xlon)),
				search("c", cand(types.AssetClassStock, "GBP", figi, xlon)),
			},
			k:    bare,
			want: [][]string{{"a#0", "c#0"}, {"b#0", "c#0"}},
		},
		{
			name: "of two claims of one datasource agreeing with a head, the better ranked joins and the other heads its own candidate",
			results: []*result{
				search("a", cand(types.AssetClassStock, "GBP", figi, isin, xlon)),
				search("b", cand(types.AssetClassStock, "GBP", figi, xetr), cand(types.AssetClassStock, "GBP", isin, xlon)),
			},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker, isin}},
			want: [][]string{{"a#0", "b#1"}, {"b#0"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for i, r := range tc.results {
				r.ID = uuid.UUID{byte(i + 1)}
			}
			res := &resolution{row: tc.k, results: tc.results, fam: cur.family(tc.k)}
			cs, _, _ := offers(res)
			var got [][]string
			for _, a := range gather(res, cs) {
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
