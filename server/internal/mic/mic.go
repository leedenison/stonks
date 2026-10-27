// Package mic normalises a venue to its ISO 10383 operating MIC, and an
// operating MIC to its market.
//
// A venue is named by a MIC, which is either an operating MIC or a segment of one. The
// domain of a MIC_TICKER is always the operating MIC, so where one source names a
// segment and another names its operating MIC, they state the same listing.
//
// A ticker's identifier facts are keyed by its market, so the package maps a MIC to its
// market as well as to its operating MIC; see
// [010_ticker_validity.sql](../db/migrations/010_ticker_validity.sql).
package mic

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Queries is the database access Load needs.
type Queries interface {
	ListMICs(ctx context.Context) ([]gen.MicMarket, error)
}

// MIC is a MIC's operating MIC and market.
type MIC struct {
	Operating string
	Market    string
}

// MICMap maps each MIC to its operating MIC and its market.
type MICMap map[string]MIC

// Of returns a MICMap in which each operating MIC of ops is a market of its
// own.
func Of(ops map[string]string) MICMap {
	t := make(MICMap, len(ops))
	for m, op := range ops {
		t[m] = MIC{Operating: op, Market: op}
	}
	return t
}

// Load reads the MIC reference table.
func Load(ctx context.Context, q Queries) (MICMap, error) {
	rows, err := q.ListMICs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mics: %w", err)
	}
	t := make(MICMap, len(rows))
	for _, r := range rows {
		t[r.Mic] = MIC{Operating: r.OperatingMic, Market: r.Market}
	}
	return t, nil
}

// Operating returns the operating MIC of m, ignoring case and surrounding space. It
// reports false when m is not a MIC.
func (t MICMap) Operating(m string) (string, bool) {
	e, ok := t[strings.ToUpper(strings.TrimSpace(m))]
	return e.Operating, ok
}

// Market returns the market of m, ignoring case and surrounding space. It reports
// false when m is not a MIC.
func (t MICMap) Market(m string) (string, bool) {
	e, ok := t[strings.ToUpper(strings.TrimSpace(m))]
	return e.Market, ok
}

// IsMarket reports whether m is the market of a MIC.
func (t MICMap) IsMarket(m string) bool {
	for _, e := range t {
		if e.Market == m {
			return true
		}
	}
	return false
}

// MarketTicker returns the market ticker of v. It reports false unless v is a
// MIC_TICKER with a value at a MIC.
func (t MICMap) MarketTicker(v types.Identifier) (types.MarketTicker, bool) {
	if v.Type != types.IdentifierTypeMicTicker || v.Value == "" {
		return types.MarketTicker{}, false
	}
	m, ok := t.Market(v.Domain)
	if !ok {
		return types.MarketTicker{}, false
	}
	return types.MarketTicker{Market: m, Value: v.Value}, true
}

// Venues returns the MICs of market, sorted.
func (t MICMap) Venues(market string) []string {
	var out []string
	for m, e := range t {
		if e.Market == market {
			out = append(out, m)
		}
	}
	slices.Sort(out)
	return out
}

// TickerProbes returns one probe per venue of each ticker's market. It fails
// when a ticker's market has no MIC.
func (t MICMap) TickerProbes(ts []types.MarketTicker) ([][]byte, error) {
	var out [][]byte
	for _, mt := range ts {
		venues := t.Venues(mt.Market)
		if len(venues) == 0 {
			return nil, fmt.Errorf("probe %s: unknown market", mt.Name())
		}
		for _, v := range venues {
			b, err := probe(types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: v, Value: mt.Value})
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		}
	}
	return out, nil
}

// MarketProbes returns one probe per venue of each market, which finds the
// keys stating any ticker of the market.
func (t MICMap) MarketProbes(markets []string) ([][]byte, error) {
	var out [][]byte
	for _, m := range markets {
		venues := t.Venues(m)
		if len(venues) == 0 {
			return nil, fmt.Errorf("probe %s: unknown market", m)
		}
		for _, v := range venues {
			b, err := probe(types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: v})
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		}
	}
	return out, nil
}

// probe writes id as a JSON array of one identifier object.
func probe(id types.Identifier) ([]byte, error) {
	obj := map[string]string{"type": string(id.Type), "domain": id.Domain}
	if !id.IsDomain() {
		obj["value"] = id.Value
	}
	b, err := json.Marshal([]map[string]string{obj})
	if err != nil {
		return nil, fmt.Errorf("probe %s: %w", id.Name(), err)
	}
	return b, nil
}
