// Package resolve resolves stated keys to system owned instruments.
//
// A key is resolved through the identifiers it states that are recognised
// globally, such as an ISIN, a SEDOL or a ticker with its venue. It also
// resolves through the broker's description of the line, when the system
// holds one. A key that states only a bare ticker associates only when the
// user confirms a candidate for it. The same holds for a venue ticker whose
// venue none of the returned listings names. The resolver never writes a
// stated description as an identifier row.
//
// The database is consulted before any datasource, and only the keys it
// leaves undecided are sent out. An association is never copied between a
// user's keys: a confirmation associates the one key. Two runs stating one
// key produce one instrument, however they interleave; see write.go.
//
// A key that states a venue ticker without a stable identifier is dated: it
// names the ticker, and each of its transactions takes the instrument the
// ticker's validity names on its order date when it is read;
// see [validity.go](../validity/validity.go). Where the ticker has facts, the
// key is dated through them without an identity fetch. Identifier events are
// fetched for the keys that may be dated, and recorded after the keys are
// written.
//
// A synchronous resolution runs in the caller for one key the user is
// confirming. It shares its lookups and its fetches with a resolution run;
// see Candidates and Confirm.
//
// A datasource that lists only some of an instrument's listings never
// contradicts a stated currency.
//
// Only a currency key can be rejected, so whether a statement is refused
// never depends on what another user resolved first.
package resolve

import (
	"context"
	"log/slog"
	"time"

	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
	runs "github.com/leedenison/stonks/server/internal/run"
)

// Resolver resolves stated keys against the database and the datasources.
type Resolver struct {
	store    Store
	fetcher  Fetcher
	sources  Sources
	mics     mic.MICMap
	families currency.Families
	clock    func() time.Time
	log      *slog.Logger
}

// New returns a Resolver. clock supplies the present, against which a
// request for identifier events picks its days.
func New(store Store, fetcher Fetcher, sources Sources, mics mic.MICMap, families currency.Families, clock func() time.Time, log *slog.Logger) *Resolver {
	return &Resolver{store: store, fetcher: fetcher, sources: sources, mics: mics, families: families, clock: clock, log: log}
}

// KeyOrderDates is a stated key and the order dates the caller adds to its
// stored transactions' dates: the rows of the statement being ingested.
type KeyOrderDates struct {
	Row   gen.StatedKey
	Dates []time.Time
}

// Resolve is the body of a resolution run. It returns one resolution key per
// key, in the order of keys, and the identifiers of the identifier facts its
// fetch runs wrote. A run that fails partway leaves the keys it has already
// written resolved.
func (r *Resolver) Resolve(ctx context.Context, run gen.Run, keys []KeyOrderDates) ([]gen.ResolutionKey, []WrittenIdentifierFacts, error) {
	b, err := r.prepare(ctx, run, keys, true)
	if err != nil {
		return nil, nil, err
	}
	out := make([]gen.ResolutionKey, 0, len(b.resolutions))
	for _, res := range b.resolutions {
		rk, err := r.write(ctx, run, res, nil)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, rk)
	}
	written, err := r.recordIdentifierFacts(ctx, run, b)
	if err != nil {
		return nil, nil, err
	}
	return out, written, nil
}

// batch is the keys of one resolution and the identifier events fetched for
// them.
type batch struct {
	resolutions []*resolution
	events      []eventKey
}

// prepare runs the lookups and the fetches for keys. A resolution run and a
// synchronous resolution share it. A synchronous resolution passes dating
// false: a user's choice is fixed, so it neither dates a key nor asks for
// identifier events.
//
// A resolution acts for its run's user, so prepare refuses a system run.
func (r *Resolver) prepare(ctx context.Context, run gen.Run, keys []KeyOrderDates, dating bool) (*batch, error) {
	user, err := runs.Owner(run)
	if err != nil {
		return nil, err
	}
	b := &batch{resolutions: make([]*resolution, len(keys))}
	for i, k := range keys {
		res := &resolution{row: k.Row, family: keyFamily(r.families, k.Row)}
		res.globalAndDescriptionIDs = globalAndDescriptionIDs(k.Row)
		b.resolutions[i] = res
	}
	if err := r.findResolutionInstrument(ctx, b.resolutions); err != nil {
		return nil, err
	}
	if dating {
		if err := r.findResolutionDated(ctx, user, b.resolutions, keys); err != nil {
			return nil, err
		}
	}
	if err := r.fetchResolutionIdentities(ctx, run, b.resolutions); err != nil {
		return nil, err
	}
	if dating {
		if b.events, err = r.fetchResolutionEvents(ctx, run, b.resolutions); err != nil {
			return nil, err
		}
	}
	return b, nil
}
