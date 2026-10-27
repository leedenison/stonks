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
	CreateIdentifierEvent(ctx context.Context, arg gen.CreateIdentifierEventParams) error
	CreateIdentifierEventCoverage(ctx context.Context, arg gen.CreateIdentifierEventCoverageParams) error
	FindIdentifier(ctx context.Context, arg gen.FindIdentifierParams) (gen.FindIdentifierRow, error)
	LockIdentifiers(ctx context.Context, keys []string) error
	CreateInstr(ctx context.Context, arg gen.CreateInstrParams) (gen.Instr, error)
	CreateListing(ctx context.Context, arg gen.CreateListingParams) (gen.Listing, error)
	CreateIdentifier(ctx context.Context, arg gen.CreateIdentifierParams) (gen.Identifier, error)
}

var _ RecordQueries = (*gen.Queries)(nil)

// ErrFuture reports an identifier attestation of a day after the day it is
// made.
var ErrFuture = errors.New("an identifier attestation cannot reach past the day it is made")

// Record writes the events, assertions and coverage of each served key. It
// checks every key first, so a key in the wrong spelling fails the whole
// call. A negative assertion is always written.
//
// A positive assertion names the instrument of the first stable identifier
// it states that is stored. A ticker row names the instrument its ticker
// identifies today, which is the wrong instrument for an assertion of a past
// day, so Record never maps through one. Where no stable identifier of the
// assertion is stored, Record creates the instrument with them, without a
// ticker row, and with the fetch key as provenance. A later answer that
// states one of them finds that instrument. Record skips an assertion that
// leaves no identifier to create the instrument with.
func Record(ctx context.Context, q RecordQueries, mics mic.MICMap, families currency.Families, keys []market.FetchKey[market.IdentifierEventRequest, market.IdentifierEventResult]) error {
	for _, k := range keys {
		if k.Outcome != gen.FetchOutcomeServed {
			continue
		}
		if err := check(mics, k.Response); err != nil {
			return err
		}
	}
	for _, k := range keys {
		if k.Outcome != gen.FetchOutcomeServed {
			continue
		}
		if err := record(ctx, q, families, k.ID, k.Response); err != nil {
			return err
		}
	}
	return nil
}

func check(mics mic.MICMap, r market.IdentifierEventResult) error {
	var ts []types.MarketTicker
	for _, e := range r.Events {
		ts = append(ts, e.Ticker)
		if e.ToValue != "" {
			ts = append(ts, types.MarketTicker{Market: e.Ticker.Market, Value: e.ToValue})
		}
	}
	for _, a := range r.Assertions {
		ts = append(ts, a.Ticker)
	}
	for _, c := range r.Coverage {
		if c.Ticker != nil {
			ts = append(ts, *c.Ticker)
		} else if err := tickers.SpelledMarket(mics, c.Market); err != nil {
			return err
		}
	}
	for _, t := range ts {
		if err := tickers.Spelled(mics, t); err != nil {
			return err
		}
	}
	return nil
}

func record(ctx context.Context, q RecordQueries, families currency.Families, key uuid.UUID, r market.IdentifierEventResult) error {
	for _, e := range r.Events {
		arg := gen.CreateIdentifierEventParams{
			ID: db.NewID(), FetchKeyID: key, Market: e.Ticker.Market, Value: e.Ticker.Value,
			Action: e.Action, EffectiveOn: types.Day(e.On),
		}
		if e.ToValue != "" {
			arg.ToValue = &e.ToValue
		}
		if err := q.CreateIdentifierEvent(ctx, arg); err != nil {
			return fmt.Errorf("record event: %w", err)
		}
	}
	for _, a := range r.Assertions {
		if _, err := Assert(ctx, q, families, key, a); err != nil {
			return err
		}
	}
	for _, c := range r.Coverage {
		arg := gen.CreateIdentifierEventCoverageParams{
			ID: db.NewID(), FetchKeyID: key, Market: c.Market,
			CoveredFrom: c.Period.From, CoveredBefore: c.Period.Before,
		}
		if c.Ticker != nil {
			arg.Value = &c.Ticker.Value
		}
		if err := q.CreateIdentifierEventCoverage(ctx, arg); err != nil {
			return fmt.Errorf("record coverage: %w", err)
		}
	}
	return nil
}

// Assert writes the assertion a under fetch key key, dated by a.At. A
// positive assertion names the instrument of the first stored stable
// identifier it states, as Record describes. Assert returns false without
// writing where it can name no instrument.
func Assert(ctx context.Context, q RecordQueries, families currency.Families, key uuid.UUID, a market.Assertion) (bool, error) {
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
	return true, AssertInstr(ctx, q, key, a.Ticker, instr, a.At)
}

// Asserter writes an assertion.
type Asserter interface {
	CreateIdentifierAssertion(ctx context.Context, arg gen.CreateIdentifierAssertionParams) error
}

// AssertInstr writes, under fetch key key, the assertion that t names
// instrument on the day of at. A nil instrument asserts that t names
// nothing.
func AssertInstr(ctx context.Context, q Asserter, key uuid.UUID, t types.MarketTicker, instr *uuid.UUID, at time.Time) error {
	return assert(ctx, q, key, types.IdentifierTypeMicTicker, t.Market, t.Value, instr, at)
}

// AssertIdentifier writes, under fetch key key, the assertion that v names
// instrument on the day of at. A MIC_TICKER at a venue is asserted at its
// market. Any other identifier, a bare ticker included, is written as stated.
func AssertIdentifier(ctx context.Context, q Asserter, mics mic.MICMap, key uuid.UUID, v types.Identifier, instr *uuid.UUID, at time.Time) error {
	if t, ok := mics.MarketTicker(v); ok {
		return AssertInstr(ctx, q, key, t, instr, at)
	}
	return assert(ctx, q, key, v.Type, v.Domain, v.Value, instr, at)
}

func assert(ctx context.Context, q Asserter, key uuid.UUID, typ types.IdentifierType, domain, value string, instr *uuid.UUID, at time.Time) error {
	arg := gen.CreateIdentifierAssertionParams{
		ID: db.NewID(), FetchKeyID: &key, Type: typ, Domain: domain, Value: value,
		InstrID: instr, AssertedOn: types.Day(at),
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
// written: tickers.ErrSpelling when t is not a ticker in the system's spelling, and
// ErrFuture when on is after today.
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
func Attest(ctx context.Context, q AttestQueries, mics mic.MICMap, run gen.Run, admin uuid.UUID, t types.MarketTicker, instr *uuid.UUID, on, today time.Time) error {
	if run.Kind != gen.RunKindAttestIdentifier || run.UserID != nil {
		return fmt.Errorf("run %s is not a system attest identifier run", run.ID)
	}
	if err := CheckAttest(mics, t, on, today); err != nil {
		return err
	}
	arg := gen.CreateIdentifierAttestationParams{
		ID: db.NewID(), RunID: run.ID, AttestedBy: admin, Type: types.IdentifierTypeMicTicker, Domain: t.Market, Value: t.Value,
		InstrID: instr, AssertedOn: types.Day(on),
	}
	if err := q.CreateIdentifierAttestation(ctx, arg); err != nil {
		return fmt.Errorf("record identifier attestation: %w", err)
	}
	return nil
}
