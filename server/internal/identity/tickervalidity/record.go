package tickervalidity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

// RecordQueries is what Record queries.
type RecordQueries interface {
	CreateIdentifierAssertion(ctx context.Context, arg gen.CreateIdentifierAssertionParams) error
	CreateIdentifierPeriodAssertion(ctx context.Context, arg gen.CreateIdentifierPeriodAssertionParams) error
	FindIdentifier(ctx context.Context, arg gen.FindIdentifierParams) (gen.FindIdentifierRow, error)
	LockAdvisory(ctx context.Context, names []string) error
	CreateInstr(ctx context.Context, arg gen.CreateInstrParams) (gen.Instr, error)
	CreateListing(ctx context.Context, arg gen.CreateListingParams) (gen.Listing, error)
	CreateIdentifier(ctx context.Context, arg gen.CreateIdentifierParams) (gen.Identifier, error)
}

var _ RecordQueries = (*gen.Queries)(nil)

// ErrFuture reports an identifier attestation of a day after the day it is
// made.
var ErrFuture = errors.New("an identifier attestation cannot reach past the day it is made")

// RecordTickers returns the row of every ticker whose facts a served key of
// keys states, creating the rows that do not exist. Like tickers.Get, it
// commits on its own, so the caller calls it before the transaction that runs
// Record. It refuses every key with tickers.ErrSpelling when one key states a
// ticker or a market in the wrong spelling.
func RecordTickers(ctx context.Context, q tickers.Queries, mics mic.MICMap, keys []market.FetchKey[market.IdentifierFactRequest, market.IdentifierFactResult]) (map[types.MarketTicker]gen.Ticker, error) {
	var ts []types.MarketTicker
	for _, k := range keys {
		if k.Outcome != gen.FetchOutcomeServed {
			continue
		}
		for _, p := range k.Response.PeriodAssertions {
			if p.Ticker == nil {
				if err := tickers.SpelledMarket(mics, p.Market); err != nil {
					return nil, err
				}
			}
		}
		ts = append(ts, k.Response.Tickers()...)
	}
	return tickers.Get(ctx, q, mics, ts)
}

// Record writes the assertions and period assertions of each served key. rows
// holds the row of each ticker the keys state, as RecordTickers returns them.
// A negative assertion is always written.
//
// A positive assertion names the instrument of the first stable identifier
// it states that is stored. A MIC_TICKER row of identifiers names the
// instrument its ticker identifies today, which is the wrong instrument for an
// assertion of a past day, so Record never maps through one. Where no stable
// identifier of the assertion is stored, Record creates the instrument with
// them, without a MIC_TICKER row, and with the fetch key as provenance. A
// later answer that states one of them finds that instrument. Record skips an
// assertion that leaves no identifier to create the instrument with.
func Record(ctx context.Context, q RecordQueries, families currency.Families, rows map[types.MarketTicker]gen.Ticker, keys []market.FetchKey[market.IdentifierFactRequest, market.IdentifierFactResult]) error {
	for _, k := range keys {
		if k.Outcome != gen.FetchOutcomeServed {
			continue
		}
		if err := record(ctx, q, families, rows, k.ID, k.Response); err != nil {
			return err
		}
	}
	return nil
}

func record(ctx context.Context, q RecordQueries, families currency.Families, rows map[types.MarketTicker]gen.Ticker, key uuid.UUID, r market.IdentifierFactResult) error {
	row := func(t types.MarketTicker) (gen.Ticker, error) {
		out, ok := rows[t]
		if !ok {
			return gen.Ticker{}, fmt.Errorf("ticker %s has no row", t.Name())
		}
		return out, nil
	}
	for _, a := range r.Assertions {
		t, err := row(a.Ticker)
		if err != nil {
			return err
		}
		if _, err := Assert(ctx, q, families, key, t, a); err != nil {
			return err
		}
	}
	for _, p := range r.PeriodAssertions {
		arg := gen.CreateIdentifierPeriodAssertionParams{
			ID: db.NewID(), FetchKeyID: key, Market: p.Market,
			AssertedFrom: p.Period.From, AssertedBefore: p.Period.Before,
		}
		if p.Ticker != nil {
			t, err := row(*p.Ticker)
			if err != nil {
				return err
			}
			arg.TickerID = &t.ID
		}
		if err := q.CreateIdentifierPeriodAssertion(ctx, arg); err != nil {
			return fmt.Errorf("record period assertion: %w", err)
		}
	}
	return nil
}

// Assert writes the assertion a of ticker t under fetch key key, dated by
// a.At. A positive assertion names the instrument of the first stored stable
// identifier it states, as Record describes. Assert returns false without
// writing where it can name no instrument.
func Assert(ctx context.Context, q RecordQueries, families currency.Families, key uuid.UUID, t gen.Ticker, a market.Assertion) (bool, error) {
	var instr *uuid.UUID
	if !a.Negative {
		var err error
		if instr, err = instrOf(ctx, q, families, key, a); err != nil {
			return false, err
		}
		if instr == nil {
			return false, nil
		}
	}
	return true, assert(ctx, q, key, t, instr, a.At, a.Edge)
}

// Asserter writes an assertion.
type Asserter interface {
	CreateIdentifierAssertion(ctx context.Context, arg gen.CreateIdentifierAssertionParams) error
}

// AssertInstr writes, under fetch key key, the assertion that t names
// instrument on the day of at. A nil instrument asserts that t names
// nothing.
func AssertInstr(ctx context.Context, q Asserter, key uuid.UUID, t gen.Ticker, instr *uuid.UUID, at time.Time) error {
	return assert(ctx, q, key, t, instr, at, "")
}

// assert writes the assertion AssertInstr writes, with an edge. An empty
// edge writes a point.
func assert(ctx context.Context, q Asserter, key uuid.UUID, t gen.Ticker, instr *uuid.UUID, at time.Time, edge gen.IdentifierEdge) error {
	arg := gen.CreateIdentifierAssertionParams{
		ID: db.NewID(), FetchKeyID: &key, TickerID: t.ID, Market: t.Market,
		InstrID: instr, AssertedOn: types.Day(at),
	}
	if edge != "" {
		arg.Edge = &edge
	}
	if err := q.CreateIdentifierAssertion(ctx, arg); err != nil {
		return fmt.Errorf("record assertion: %w", err)
	}
	return nil
}

// Finder finds a stored identifier.
type Finder interface {
	FindIdentifier(ctx context.Context, arg gen.FindIdentifierParams) (gen.FindIdentifierRow, error)
}

// StoredInstr returns the instrument of the first stable identifier of
// ids that is stored, nil where none is.
func StoredInstr(ctx context.Context, q Finder, ids []types.Identifier) (*uuid.UUID, error) {
	for _, id := range ids {
		if !id.Stable() {
			continue
		}
		row, err := q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: id.Type, Domain: id.Domain, Value: id.Value})
		if errors.Is(err, db.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("find %s: %w", id.Name(), err)
		}
		return &row.Instr.ID, nil
	}
	return nil, nil
}

// instrOf returns the instrument the first stored stable identifier of
// a names, creating one where none is stored. It returns nil where a states
// no stable identifier.
func instrOf(ctx context.Context, q RecordQueries, families currency.Families, key uuid.UUID, a market.Assertion) (*uuid.UUID, error) {
	var stable []types.Identifier
	for _, id := range a.Of.Identifiers {
		if id.Stable() {
			stable = append(stable, id)
		}
	}
	if len(stable) == 0 {
		return nil, nil
	}
	if err := db.LockIdentifiers(ctx, q, stable); err != nil {
		return nil, err
	}
	instr, err := StoredInstr(ctx, q, stable)
	if err != nil || instr != nil {
		return instr, err
	}
	return create(ctx, q, families.Family(a.Of.Currency), key, a, stable)
}

// create writes the instrument a names with its stable identifiers. A listing
// identifier goes on the listing in family, the family of the currency that
// a.Of states. Where family is empty, create leaves the listing identifiers
// out, and returns nil where that leaves the instrument with no identifier.
func create(ctx context.Context, q RecordQueries, family string, key uuid.UUID, a market.Assertion, stable []types.Identifier) (*uuid.UUID, error) {
	var kept []types.Identifier
	for _, id := range stable {
		if id.Grain() == types.IdentifierGrainInstr || family != "" {
			kept = append(kept, id)
		}
	}
	if len(kept) == 0 {
		return nil, nil
	}
	class := a.Of.Class
	if class == "" {
		class = types.AssetClassUnknown
	}
	instr, err := q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: class, FetchKeyID: &key})
	if err != nil {
		return nil, fmt.Errorf("create instrument: %w", err)
	}
	var listing *uuid.UUID
	if family != "" {
		row, err := q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrID: instr.ID, Currency: family, FetchKeyID: &key})
		if err != nil {
			return nil, fmt.Errorf("create listing in %s: %w", family, err)
		}
		listing = &row.ID
	}
	for _, id := range kept {
		arg := gen.CreateIdentifierParams{ID: db.NewID(), InstrID: instr.ID, Type: id.Type, Domain: id.Domain, Value: id.Value, FetchKeyID: &key}
		if id.Grain() == types.IdentifierGrainListing {
			arg.ListingID = listing
		}
		if _, err := q.CreateIdentifier(ctx, arg); err != nil {
			return nil, fmt.Errorf("create %s: %w", id.Name(), err)
		}
	}
	return &instr.ID, nil
}

// AttestQueries is what Attest queries.
type AttestQueries interface {
	CreateIdentifierAttestation(ctx context.Context, arg gen.CreateIdentifierAttestationParams) error
}

var _ AttestQueries = (*gen.Queries)(nil)

// CheckAttest reports why an identifier attestation of t on day on cannot be
// written: tickers.ErrSpelling when t is not a ticker in the system's
// spelling, and ErrFuture when on is after today.
func CheckAttest(mics mic.MICMap, t types.MarketTicker, on, today time.Time) error {
	if err := tickers.Spelled(mics, t); err != nil {
		return err
	}
	if types.Day(on).After(types.Day(today)) {
		return fmt.Errorf("%s: %w", types.Day(on).Format(time.DateOnly), ErrFuture)
	}
	return nil
}

// Attest writes an administrator's assertion that t names instrument on the day
// on, beneath run, which must be a system run of kind attest identifier. A nil
// instrument asserts that t names nothing. A later identifier attestation of a
// day replaces the earlier one.
func Attest(ctx context.Context, q AttestQueries, mics mic.MICMap, run gen.Run, admin uuid.UUID, t gen.Ticker, instr *uuid.UUID, on, today time.Time) error {
	if run.Kind != gen.RunKindAttestIdentifier || run.UserID != nil {
		return fmt.Errorf("run %s is not a system attest identifier run", run.ID)
	}
	if err := CheckAttest(mics, tickers.Of(t), on, today); err != nil {
		return err
	}
	arg := gen.CreateIdentifierAttestationParams{
		ID: db.NewID(), RunID: run.ID, AttestedBy: admin, TickerID: t.ID, Market: t.Market,
		InstrID: instr, AssertedOn: types.Day(on),
	}
	if err := q.CreateIdentifierAttestation(ctx, arg); err != nil {
		return fmt.Errorf("record identifier attestation: %w", err)
	}
	return nil
}
