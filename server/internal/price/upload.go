package price

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// ErrListing is a refusal of a series whose listing the system lacks.
var ErrListing = errors.New("unknown listing")

// Close is the price of one day in an uploaded series.
type Close struct {
	Day   time.Time
	Price decimal.Decimal
}

// Series is the prices of one listing that an administrator uploads.
type Series struct {
	Listing uuid.UUID
	// Symbol is the name under which the source answered, such as
	// "NASDAQ:AAPL".
	Symbol string
	// Currency is the code the source quoted.
	Currency string
	// Coverage holds the periods that the source covered. A covered day
	// without a close had no session.
	Coverage []types.Period
	Closes   []Close
}

// Refusal is a series that was not stored. Err wraps ErrCurrency, ErrInvalid
// or ErrListing.
type Refusal struct {
	Listing uuid.UUID
	Err     error
}

// Uploader stores the prices that an administrator uploads for a datasource
// whose data the service never fetches.
type Uploader struct {
	store    UploadStore
	runs     Runner
	families currency.Families
	scales   currency.Scales
}

// NewUploader returns an Uploader.
func NewUploader(store UploadStore, runs Runner, families currency.Families, scales currency.Scales) *Uploader {
	return &Uploader{store: store, runs: runs, families: families, scales: scales}
}

// Upload stores series as answers of e, evaluated on the day asAt, and
// returns its run with each series it refused. The run is a system run, so
// the prices outlive the administrator. A refused series stores nothing and
// the others are stored. Any other error fails the run.
//
// The price of a security is stored as at asAt, because the source adjusts
// its history for every split before that day. A rate is stored as at its
// own day. Coverage ends before asAt, since the close of that day may be
// unpublished.
func (u *Uploader) Upload(ctx context.Context, e *market.Entry, asAt time.Time, series []Series) (gen.Run, []Refusal, error) {
	asAt = types.Day(asAt)
	var refused []Refusal
	parent, err := u.runs.Sync(ctx, nil, gen.RunKindUploadPrices, gen.RunTriggerAdministrator, func(ctx context.Context, parent gen.Run) error {
		_, err := u.runs.ChildAs(ctx, parent, nil, gen.RunKindDatasourceFetch, func(ctx context.Context, run gen.Run) error {
			var err error
			refused, err = u.fetch(ctx, run, e, asAt, series)
			return err
		})
		return err
	})
	if err != nil {
		return parent, nil, err
	}
	row, err := u.store.GetRunByID(ctx, parent.ID)
	if err != nil {
		return parent, nil, fmt.Errorf("get run: %w", err)
	}
	return row, refused, nil
}

// fetch records the fetch of one upload and stores each series in a
// transaction of its own.
func (u *Uploader) fetch(ctx context.Context, run gen.Run, e *market.Entry, asAt time.Time, series []Series) ([]Refusal, error) {
	if _, err := u.store.CreateFetch(ctx, gen.CreateFetchParams{
		ID: run.ID, UserID: run.UserID, Datasource: e.Name, Endpoint: e.Integration.Endpoint(), Kind: gen.FetchKindPrices,
	}); err != nil {
		return nil, fmt.Errorf("create fetch: %w", err)
	}
	rows, err := u.store.ListRateListings(ctx)
	if err != nil {
		return nil, fmt.Errorf("list rate listings: %w", err)
	}
	rates := map[uuid.UUID]string{}
	for _, r := range rows {
		rates[r.ID] = r.Code
	}
	var refused []Refusal
	for _, s := range series {
		code, rate := rates[s.Listing]
		err := u.store.Tx(ctx, func(q UploadQueries) error {
			return u.record(ctx, q, run, e.Name, asAt, s, code, rate)
		})
		switch {
		case errors.Is(err, ErrCurrency), errors.Is(err, ErrInvalid), errors.Is(err, ErrListing):
			refused = append(refused, Refusal{Listing: s.Listing, Err: err})
		case err != nil:
			return nil, fmt.Errorf("record the prices of %s: %w", s.Symbol, err)
		}
	}
	return refused, nil
}

// record writes the fetch key of s and its prices. A rate names its currency
// in code.
func (u *Uploader) record(ctx context.Context, q UploadQueries, run gen.Run, datasource string, asAt time.Time, s Series, code string, rate bool) error {
	if _, err := q.GetListingByID(ctx, s.Listing); errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("%w: %s", ErrListing, s.Listing)
	} else if err != nil {
		return fmt.Errorf("get listing: %w", err)
	}
	sent := types.Identifier{Type: types.IdentifierTypeDatasourceTicker, Domain: datasource, Value: s.Symbol}
	if rate {
		sent = types.Identifier{Type: types.IdentifierTypeCurrency, Value: code}
	}
	key := gen.CreateFetchKeyParams{
		ID: db.NewID(), FetchID: run.ID, UserID: run.UserID, Outcome: gen.FetchOutcomeServed, Attempts: 1,
		SentType: &sent.Type, SentDomain: sent.Domain, SentValue: &sent.Value, AnsweredAt: &asAt,
	}
	if hull, ok := hullOf(s.Coverage); ok {
		key.AskedFrom, key.AskedBefore = &hull.From, &hull.Before
	}
	if err := q.CreateFetchKey(ctx, key); err != nil {
		return fmt.Errorf("create fetch key: %w", err)
	}
	for _, p := range s.Coverage {
		a := Answer{Listing: s.Listing, Datasource: datasource, FetchKeyID: key.ID, AnsweredAt: asAt}
		a.Currency, a.Coverage = s.Currency, ptr.To(p)
		for _, c := range s.Closes {
			if !p.Contains(types.Day(c.Day)) {
				continue
			}
			at := asAt
			if rate {
				at = c.Day
			}
			a.Prices = append(a.Prices, market.DayPrice{Day: c.Day, Price: c.Price, AsAt: at})
		}
		if _, err := Record(ctx, q, u.families, u.scales, a); err != nil {
			return err
		}
	}
	return nil
}

// hullOf returns the period from the earliest start to the latest end of ps.
func hullOf(ps []types.Period) (types.Period, bool) {
	if len(ps) == 0 {
		return types.Period{}, false
	}
	hull := ps[0]
	for _, p := range ps[1:] {
		if p.From.Before(hull.From) {
			hull.From = p.From
		}
		if p.Before.After(hull.Before) {
			hull.Before = p.Before
		}
	}
	return hull, true
}
