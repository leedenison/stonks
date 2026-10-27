package join

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// InstrQueries is the set of queries Instrs reads.
type InstrQueries interface {
	ListIdentifiersOf(ctx context.Context, ids []uuid.UUID) ([]gen.Identifier, error)
	ListListingsOf(ctx context.Context, ids []uuid.UUID) ([]gen.Listing, error)
}

// CoverageQueries is the set of queries InstrsWithCoverage reads.
type CoverageQueries interface {
	InstrQueries
	ListIdentityCoverage(ctx context.Context, instrIds []uuid.UUID) ([]gen.IdentityCoverage, error)
}

var _ CoverageQueries = (*gen.Queries)(nil)

// Instr is an instrument row with its identifier and listing rows.
type Instr struct {
	Row         gen.Instr
	Identifiers []gen.Identifier
	Listings    []gen.Listing
}

// InstrWithCoverage is an Instr with the datasources that have
// answered for it.
type InstrWithCoverage struct {
	Instr
	// Coverage holds the name of each datasource that has answered.
	Coverage map[string]bool
}

// Instrs reads the identifiers and listings of rows, keyed by
// instrument id.
func Instrs(ctx context.Context, q InstrQueries, rows []gen.Instr) (map[uuid.UUID]*Instr, error) {
	out := make(map[uuid.UUID]*Instr, len(rows))
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		out[row.ID] = &Instr{Row: row}
		ids[i] = row.ID
	}
	if len(ids) == 0 {
		return out, nil
	}
	identifiers, err := q.ListIdentifiersOf(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list identifiers: %w", err)
	}
	for _, row := range identifiers {
		j := out[row.InstrID]
		j.Identifiers = append(j.Identifiers, row)
	}
	listings, err := q.ListListingsOf(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list listings: %w", err)
	}
	for _, l := range listings {
		j := out[l.InstrID]
		j.Listings = append(j.Listings, l)
	}
	return out, nil
}

// InstrsWithCoverage reads the identifiers, listings and coverage of
// rows, keyed by instrument id.
func InstrsWithCoverage(ctx context.Context, q CoverageQueries, rows []gen.Instr) (map[uuid.UUID]*InstrWithCoverage, error) {
	joined, err := Instrs(ctx, q, rows)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]*InstrWithCoverage, len(joined))
	ids := make([]uuid.UUID, 0, len(joined))
	for id, j := range joined {
		out[id] = &InstrWithCoverage{Instr: *j, Coverage: map[string]bool{}}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return out, nil
	}
	coverage, err := q.ListIdentityCoverage(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list coverage: %w", err)
	}
	for _, c := range coverage {
		out[c.InstrID].Coverage[c.Datasource] = true
	}
	return out, nil
}

// Covered reports whether datasource has answered for the instrument.
// An instrument without a fetch key is reference data, which every
// datasource covers.
func (j *InstrWithCoverage) Covered(datasource string) bool {
	return j.Row.FetchKeyID == nil || j.Coverage[datasource]
}

// Instr returns the instrument that j describes.
func (j Instr) Instr() types.Instr {
	out := types.Instr{Class: j.Row.AssetClass, Listings: map[string]types.Listing{}}
	famOf := map[uuid.UUID]string{}
	for _, l := range j.Listings {
		famOf[l.ID] = l.Currency
		out.Listings[l.Currency] = types.Listing{Primary: ptr.Deref(l.PrimaryMic, "")}
	}
	for _, id := range j.Identifiers {
		if id.ListingID == nil {
			out.Identifiers = append(out.Identifiers, to.Identifier(id))
			continue
		}
		fam := famOf[*id.ListingID]
		l := out.Listings[fam]
		l.Identifiers = append(l.Identifiers, to.Identifier(id))
		out.Listings[fam] = l
	}
	return out
}

// Listing returns the listing of the currency family fam, nil where the
// instrument has none.
func (j *Instr) Listing(fam string) *gen.Listing {
	for i := range j.Listings {
		if j.Listings[i].Currency == fam {
			return &j.Listings[i]
		}
	}
	return nil
}

// Identifier returns the row of id, nil where the instrument has none.
func (j *Instr) Identifier(id types.Identifier) *gen.Identifier {
	for i := range j.Identifiers {
		if to.Identifier(j.Identifiers[i]) == id {
			return &j.Identifiers[i]
		}
	}
	return nil
}
