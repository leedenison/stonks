package resolve

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/join"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/types"
)

// writeTarget is where a key's write lands.
type writeTarget struct {
	// instrument is nil where the write creates an instrument.
	instrument *join.InstrumentWithCoverage
	// taken is the identifiers of the instruments the write leaves apart
	// from instrument.
	taken    map[types.Identifier]bool
	findings []gen.CreateFindingParams
}

// findWriteTarget decides which instrument in the database a key takes: the
// instrument identified by the key's identifiers and by the claims that share
// a stable identifier with the winner. When it finds none, the write creates
// one. If the identifiers identify several instruments, findWriteTarget
// merges them. A claim joined through the ticker alone does not choose the
// instrument; see dropHeld.
func findWriteTarget(ctx context.Context, q Queries, res *resolution, verdict claimVerdict) (writeTarget, error) {
	ids := lookupIDs(res.globalAndDescriptionIDs)
	for _, c := range verdict.attached {
		if c != verdict.winner && !c.sharesStable(verdict.winner) {
			continue
		}
		for id := range c.AllIDs() {
			if !id.Ticker() {
				ids = appendUnique(ids, id)
			}
		}
	}
	matches, err := findInstrumentsByIdentifier(ctx, q, ids)
	if err != nil {
		return writeTarget{}, err
	}
	switch len(matches) {
	case 0:
		return writeTarget{}, nil
	case 1:
		return writeTarget{instrument: matches[0].instrument}, nil
	}
	var fetchKeyID *uuid.UUID
	if verdict.winner != nil && verdict.winner.fetchKey != nil {
		fetchKeyID = &verdict.winner.fetchKey.ID
	}
	return merge(ctx, q, res, fetchKeyID, matches)
}

// dropHeld keeps the write from merging two instruments through a ticker.
// A claim that shares no stable identifier with chooser joined the verdict
// through the ticker alone, whichever choice attached it. Where such a claim
// holds an identifier of a stored instrument other than target, dropHeld
// drops it from verdict and returns a contradiction finding for it.
func dropHeld(ctx context.Context, q Queries, verdict *claimVerdict, chooser *identityClaim, target *join.InstrumentWithCoverage) ([]gen.CreateFindingParams, error) {
	var joined []*identityClaim
	var ids []types.Identifier
	for _, c := range verdict.attached {
		if c == chooser || (chooser != nil && c.sharesStable(chooser)) {
			continue
		}
		joined = append(joined, c)
		for id := range c.AllIDs() {
			if !id.Ticker() {
				ids = appendUnique(ids, id)
			}
		}
	}
	if len(joined) == 0 {
		return nil, nil
	}
	found, err := findInstrumentsByIdentifier(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	others := slices.DeleteFunc(found, func(m instrumentMatch) bool {
		return target != nil && m.instrument.Row.ID == target.Row.ID
	})
	var out []gen.CreateFindingParams
	for _, c := range joined {
		if id, ok := heldBy(c, others); ok {
			verdict.attached = slices.DeleteFunc(verdict.attached, func(a *identityClaim) bool { return a == c })
			out = append(out, finding(c.fetchKey, gen.FindingKindContradiction, "", c.label(id.Name()+" identifies another instrument (database)")))
		}
	}
	return out, nil
}

// heldBy returns the first identifier of c that identifies an instrument of
// matches, and false where none does.
func heldBy(c *identityClaim, matches []instrumentMatch) (types.Identifier, bool) {
	for _, m := range matches {
		for _, id := range m.matched {
			if c.IdentifiedBy(id) {
				return id, true
			}
		}
	}
	return types.Identifier{}, false
}

// findInstrumentsByIdentifier finds the instruments any of ids identifies,
// each with the identifiers that matched it, the earliest created first.
func findInstrumentsByIdentifier(ctx context.Context, q Queries, ids []types.Identifier) ([]instrumentMatch, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	arg := gen.ListInstrumentsByIdentifiersParams{Types: make([]string, len(ids)), Domains: make([]string, len(ids)), Values: make([]string, len(ids))}
	for i, id := range ids {
		arg.Types[i], arg.Domains[i], arg.Values[i] = string(id.Type), id.Domain, id.Value
	}
	rows, err := q.ListInstrumentsByIdentifiers(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("find instruments: %w", err)
	}
	var instruments []gen.Instrument
	matched := map[uuid.UUID][]types.Identifier{}
	for _, row := range rows {
		if _, ok := matched[row.Instrument.ID]; !ok {
			instruments = append(instruments, row.Instrument)
		}
		matched[row.Instrument.ID] = append(matched[row.Instrument.ID], to.Identifier(row.Identifier))
	}
	loaded, err := join.InstrumentsWithCoverage(ctx, q, instruments)
	if err != nil {
		return nil, err
	}
	matches := make([]instrumentMatch, len(instruments))
	for i, inst := range instruments {
		matches[i] = instrumentMatch{instrument: loaded[inst.ID], matched: matched[inst.ID]}
	}
	slices.SortFunc(matches, func(a, b instrumentMatch) int {
		return a.instrument.Row.CreatedAt.Compare(b.instrument.Row.CreatedAt)
	})
	return matches, nil
}

// instrumentMatch is an instrument findInstrumentsByIdentifier found, with the
// identifiers that matched it.
type instrumentMatch struct {
	instrument *join.InstrumentWithCoverage
	matched    []types.Identifier
}
