// Package mic normalises a venue to its ISO 10383 operating MIC.
//
// A venue is named by a MIC, which is either an operating MIC or a segment of one. The
// domain of a MIC_TICKER is always the operating MIC, so where one source names a
// segment and another names its operating MIC, they state the same listing.
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
	ListMICs(ctx context.Context) ([]gen.Mic, error)
}

// MICMap maps each MIC to its operating MIC.
type MICMap map[string]string

// Load reads the MIC reference table.
func Load(ctx context.Context, q Queries) (MICMap, error) {
	rows, err := q.ListMICs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mics: %w", err)
	}
	t := make(MICMap, len(rows))
	for _, r := range rows {
		t[r.Mic] = r.OperatingMic
	}
	return t, nil
}

// Operating returns the operating MIC of m, ignoring case and surrounding space. It
// reports false when m is not a MIC.
func (t MICMap) Operating(m string) (string, bool) {
	op, ok := t[strings.ToUpper(strings.TrimSpace(m))]
	return op, ok
}

// Venues returns the MICs that map to op, sorted, which include op itself.
// The result is empty when op is not an operating MIC.
func (t MICMap) Venues(op string) []string {
	var out []string
	for m, o := range t {
		if o == op {
			out = append(out, m)
		}
	}
	slices.Sort(out)
	return out
}

// Probes writes the probes that find a key stating each of ids: one per
// venue of its operating MIC, since a statement may name a segment, or one
// under the domain itself where the map lists no venue of it. A domain
// identifier's probe leaves the value out, so it matches every ticker of the
// domain. A probe is a JSON array of one identifier object.
func (t MICMap) Probes(ids []types.Identifier) ([][]byte, error) {
	var out [][]byte
	for _, id := range ids {
		venues := t.Venues(id.Domain)
		if len(venues) == 0 {
			venues = []string{id.Domain}
		}
		for _, v := range venues {
			obj := map[string]string{"type": string(id.Type), "domain": v}
			if !id.IsDomain() {
				obj["value"] = id.Value
			}
			b, err := json.Marshal([]map[string]string{obj})
			if err != nil {
				return nil, fmt.Errorf("probe %s: %w", id.Name(), err)
			}
			out = append(out, b)
		}
	}
	return out, nil
}
