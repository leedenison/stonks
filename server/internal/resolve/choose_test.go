package resolve

import (
	"fmt"
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

func cand(class gen.AssetClass, currency string, ids ...types.Identifier) market.Candidate {
	return market.Candidate{Class: class, Currency: currency, Identifiers: ids}
}

// families maps GBX to GBP and every other code to itself.
func families(code string) string {
	if code == "GBX" {
		return "GBP"
	}
	return code
}

var (
	isin   = id(types.IdentifierTypeIsin, "", "GB00BH4HKS39")
	cusip  = id(types.IdentifierTypeCusip, "", "92857W308")
	figi   = id(types.IdentifierTypeOpenfigiShareClass, "", "BBG001S5XDT5")
	comp   = id(types.IdentifierTypeOpenfigiComposite, "", "BBG000C6K6G9")
	xlon   = id(types.IdentifierTypeMicTicker, "XLON", "VOD")
	xnas   = id(types.IdentifierTypeMicTicker, "XNAS", "VOD")
	xetr   = id(types.IdentifierTypeMicTicker, "XETR", "VODI")
	ticker = id(types.IdentifierTypeMicTicker, "", "VOD")
)

// served returns a result served under sent, filtered on filtered.
func served(source string, sent types.Identifier, filtered []types.Identifier, cs ...market.Candidate) *result {
	return &result{Source: source, Outcome: gen.FetchOutcomeServed, Sent: &sent, Response: market.IdentityResult{Filtered: filtered, Candidates: cs}}
}

func TestGroups(t *testing.T) {
	figi2 := id(types.IdentifierTypeOpenfigiShareClass, "", "BBG001S5XDT6")
	tests := []struct {
		name string
		r    *result
		want []*group
	}{
		{
			name: "a shared instrument identifier joins candidates",
			r: served("a", isin, nil,
				cand(gen.AssetClassStock, "GBX", figi, isin, comp, xlon),
				cand(gen.AssetClassStock, "GBP", figi, xetr),
				cand(gen.AssetClassStock, "USD", figi2, xnas),
			),
			want: []*group{
				{order: 0, class: gen.AssetClassStock, named: true, instrument: []types.Identifier{figi, isin}, listings: map[string][]types.Identifier{
					"GBP": {comp, xlon, xetr},
				}},
				{order: 2, class: gen.AssetClassStock, instrument: []types.Identifier{figi2}, listings: map[string][]types.Identifier{
					"USD": {xnas},
				}},
			},
		},
		{
			name: "a strict filter is one group holding the identifier",
			r: served("a", isin, []types.Identifier{isin},
				cand(gen.AssetClassUnknown, "GBP", xlon),
				cand(gen.AssetClassStock, "USD", figi, xnas),
			),
			want: []*group{
				{order: 0, class: gen.AssetClassStock, named: true, instrument: []types.Identifier{isin, figi}, listings: map[string][]types.Identifier{
					"GBP": {xlon},
					"USD": {xnas},
				}},
			},
		},
		{
			name: "no currency is a listing of no family, and the venue sent names the group",
			r: served("a", xlon, []types.Identifier{ticker},
				cand(gen.AssetClassStock, "", figi, xlon),
				cand(gen.AssetClassStock, "", figi, xnas),
			),
			want: []*group{
				{order: 0, class: gen.AssetClassStock, named: true, instrument: []types.Identifier{figi}, listings: map[string][]types.Identifier{
					"": {xlon, xnas},
				}},
			},
		},
		{
			name: "no candidates",
			r:    served("a", isin, nil),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := groups(tc.r, families)
			for _, g := range got {
				if g.r != tc.r {
					t.Errorf("group %d names result %p, want %p", g.order, g.r, tc.r)
				}
				g.r = nil
			}
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(group{})); diff != "" {
				t.Errorf("groups mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// outcome writes a choice as the winner, the attached groups, the findings
// and the routine drops, each group named by its source and order.
type outcome struct {
	winner   string
	attached []string
	findings []string
	routine  map[routine]int
}

func label(g *group) string {
	switch {
	case g == nil:
		return ""
	case g.r == nil:
		return "database"
	}
	return fmt.Sprintf("%s#%d", g.r.Source, g.order)
}

// describe writes c, naming each finding's fetch key by its source.
func describe(c choice, source map[uuid.UUID]string) outcome {
	o := outcome{winner: label(c.winner), routine: c.routine}
	for _, g := range c.attached {
		o.attached = append(o.attached, label(g))
	}
	for _, f := range c.findings {
		if f.RunID != uuid.Nil || f.StatedKeyID != nil || f.FetchKeyID == nil || f.Step == nil || f.Detail == nil {
			panic(fmt.Sprintf("finding %+v, want the fetch key, step and detail and nothing else", f))
		}
		o.findings = append(o.findings, fmt.Sprintf("%s %s %s: %s", source[*f.FetchKeyID], f.Kind, *f.Step, *f.Detail))
	}
	return o
}

func TestChoose(t *testing.T) {
	stock := ptr.To(gen.AssetClassStock)
	other := id(types.IdentifierTypeCusip, "", "92857W309")
	figi2 := id(types.IdentifierTypeOpenfigiShareClass, "", "BBG001S5XDT6")
	strict := func(source string, sent types.Identifier, cs ...market.Candidate) *result {
		return served(source, sent, []types.Identifier{sent}, cs...)
	}
	search := func(source string, sent types.Identifier, cs ...market.Candidate) *result {
		return served(source, sent, []types.Identifier{ticker}, cs...)
	}
	held := &group{class: gen.AssetClassStock, instrument: []types.Identifier{isin, cusip}, listings: map[string][]types.Identifier{
		"GBP": {comp, xlon},
	}}
	naming := func(source string, n int) map[routine]int { return map[routine]int{{source, gen.DropStepNaming}: n} }
	tests := []struct {
		name    string
		results []*result
		k       gen.StatedKey
		db      *group
		want    outcome
	}{
		{
			name: "a group not naming the identifier sent is dropped",
			results: []*result{served("a", isin, nil,
				cand(gen.AssetClassStock, "GBP", figi2, xnas),
				cand(gen.AssetClassStock, "GBP", figi, isin, xlon),
			)},
			k:    gen.StatedKey{Identifiers: []types.Identifier{isin}},
			want: outcome{winner: "a#1", attached: []string{"a#1"}, routine: naming("a", 1)},
		},
		{
			name:    "a listing in no stated family contradicts the statement",
			results: []*result{strict("a", isin, cand(gen.AssetClassStock, "USD", figi, xnas))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin}},
			want:    outcome{findings: []string{"a dropped stated: listings in USD, none in the stated GBP"}},
		},
		{
			name:    "a listing of no family does not",
			results: []*result{strict("a", isin, cand(gen.AssetClassStock, "", figi, xnas))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin}},
			want:    outcome{winner: "a#0", attached: []string{"a#0"}},
		},
		{
			name:    "a disjoint class contradicts the statement",
			results: []*result{strict("a", isin, cand(gen.AssetClassEtf, "GBP", figi, xlon))},
			k:       gen.StatedKey{AssetClass: stock, Identifiers: []types.Identifier{isin}},
			want:    outcome{findings: []string{"a dropped stated: class etf contradicts the stated stock"}},
		},
		{
			name:    "an unknown class does not",
			results: []*result{strict("a", isin, cand(gen.AssetClassUnknown, "GBP", figi, xlon))},
			k:       gen.StatedKey{AssetClass: stock, Identifiers: []types.Identifier{isin}},
			want:    outcome{winner: "a#0", attached: []string{"a#0"}},
		},
		{
			name:    "an identifier of a stated type with another value contradicts the statement",
			results: []*result{strict("a", isin, cand(gen.AssetClassStock, "GBP", figi, other, xlon))},
			k:       gen.StatedKey{Identifiers: []types.Identifier{cusip, isin}},
			want:    outcome{findings: []string{"a dropped stated: cusip 92857W309 contradicts the stated cusip 92857W308"}},
		},
		{
			name: "the winner is the highest precedence datasource with a survivor, and the rest attach",
			results: []*result{
				strict("a", isin, cand(gen.AssetClassStock, "GBP", figi, xlon)),
				strict("b", isin, cand(gen.AssetClassStock, "GBP", isin, cusip, comp)),
			},
			k:    gen.StatedKey{Identifiers: []types.Identifier{isin}},
			want: outcome{winner: "a#0", attached: []string{"a#0", "b#0"}},
		},
		{
			name: "a group inconsistent with one chosen above is dropped",
			results: []*result{
				strict("a", isin, cand(gen.AssetClassStock, "GBP", figi, cusip, xlon)),
				strict("b", isin, cand(gen.AssetClassStock, "GBP", figi, other)),
				strict("c", isin, cand(gen.AssetClassEtf, "GBP", figi)),
			},
			k: gen.StatedKey{Identifiers: []types.Identifier{isin}},
			want: outcome{winner: "a#0", attached: []string{"a#0"}, findings: []string{
				"b dropped precedence: cusip 92857W309 contradicts a's cusip 92857W308",
				"c dropped precedence: class etf contradicts a's stock",
			}},
		},
		{
			name: "the database holds the key and a contradicting response is a contradiction",
			results: []*result{
				strict("a", isin, cand(gen.AssetClassStock, "GBP", figi, other, xlon)),
				strict("b", isin, cand(gen.AssetClassStock, "GBP", figi, id(types.IdentifierTypeMicTicker, "XLON", "VODL"))),
			},
			k:  gen.StatedKey{Identifiers: []types.Identifier{isin}},
			db: held,
			want: outcome{winner: "database", findings: []string{
				"a contradiction precedence: cusip 92857W309 contradicts the instrument's cusip 92857W308",
				"b contradiction precedence: mic_ticker XLON:VODL contradicts the instrument's mic_ticker XLON:VOD",
			}},
		},
		{
			name:    "a second composite in a listing contradicts nothing",
			results: []*result{strict("a", isin, cand(gen.AssetClassStock, "GBP", figi, id(types.IdentifierTypeOpenfigiComposite, "", "BBG000C6K6H0")))},
			k:       gen.StatedKey{Identifiers: []types.Identifier{isin}},
			db:      held,
			want:    outcome{winner: "database", attached: []string{"a#0"}},
		},
		{
			name:    "a group sharing no stable identifier with the winner is dropped",
			results: []*result{search("a", xlon, cand(gen.AssetClassStock, "GBP", figi, xlon))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			db:      held,
			want:    outcome{winner: "database", findings: []string{"a dropped corroboration: shares no stable identifier with the instrument"}},
		},
		{
			name: "a lower datasource corroborates the winner through a stable identifier",
			results: []*result{
				strict("a", isin, cand(gen.AssetClassStock, "GBP", figi, xlon)),
				search("b", xlon, cand(gen.AssetClassStock, "GBP", figi, xlon), cand(gen.AssetClassStock, "GBP", figi2, xnas)),
			},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{isin, xlon}},
			want: outcome{winner: "a#0", attached: []string{"a#0", "b#0"}, routine: naming("b", 1)},
		},
		{
			name: "survivors rank by the stated data they confirm, then the datasource's order",
			results: []*result{search("a", xlon,
				cand(gen.AssetClassStock, "GBP", figi, xlon),
				cand(gen.AssetClassStock, "GBP", figi2, cusip, xlon),
				cand(gen.AssetClassStock, "GBP", id(types.IdentifierTypeOpenfigiShareClass, "", "BBG001S5XDT7"), xlon),
			)},
			k:    gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{cusip, xlon}},
			want: outcome{winner: "a#1", attached: []string{"a#1"}, routine: map[routine]int{{"a", gen.DropStepRank}: 2}},
		},
		{
			name:    "the group at the stated venue names the key",
			results: []*result{search("a", xlon, cand(gen.AssetClassStock, "GBP", figi2, xnas), cand(gen.AssetClassStock, "GBP", figi, xlon))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			want:    outcome{winner: "a#1", attached: []string{"a#1"}, routine: naming("a", 1)},
		},
		{
			name:    "a mis-stated venue associates with the one group that survives",
			results: []*result{search("a", xlon, cand(gen.AssetClassStock, "GBP", figi, xetr), cand(gen.AssetClassStock, "USD", figi2, xnas))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			want:    outcome{winner: "a#0", attached: []string{"a#0"}, findings: []string{"a dropped stated: listings in USD, none in the stated GBP"}},
		},
		{
			name:    "a mis-stated venue with several survivors chooses none",
			results: []*result{search("a", xlon, cand(gen.AssetClassStock, "GBP", figi, xetr), cand(gen.AssetClassStock, "GBP", figi2, xnas))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			want:    outcome{routine: naming("a", 2)},
		},
		{
			name:    "a mis-stated venue with the key held narrows to the group corroborating the instrument",
			results: []*result{search("a", xlon, cand(gen.AssetClassStock, "GBP", figi2, xnas), cand(gen.AssetClassStock, "GBP", isin, xetr))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{xlon}},
			db:      held,
			want:    outcome{winner: "database", attached: []string{"a#1"}, findings: []string{"a dropped corroboration: shares no stable identifier with the instrument"}},
		},
		{
			name:    "a bare ticker is never chosen",
			results: []*result{search("a", ticker, cand(gen.AssetClassStock, "GBP", figi, xlon))},
			k:       gen.StatedKey{Currency: ptr.To("GBP"), Identifiers: []types.Identifier{ticker}},
			want:    outcome{routine: naming("a", 1)},
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
			if tc.want.routine == nil {
				tc.want.routine = map[routine]int{}
			}
			got := describe(choose(tc.results, tc.k, tc.db, families), source)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(outcome{}, routine{})); diff != "" {
				t.Errorf("choose mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
