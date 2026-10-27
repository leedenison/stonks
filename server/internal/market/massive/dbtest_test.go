//go:build dbtest

package massive

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/corpact"
	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/market/markettest"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/testutil/vcr"
	"github.com/leedenison/stonks/server/internal/types"
)

// TestVenuesAreMICs checks that every venue the table names is in the MIC
// reference table.
func TestVenuesAreMICs(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Open()
	t.Cleanup(pool.Close)
	tbl, err := mic.Load(ctx, gen.New(pool))
	require.NoError(t, err)
	markettest.VenuesAreMICs(t, tbl, slices.Collect(maps.Keys(venues))...)
}

// TestEventsRecord checks that recording Meta's ticker history writes each
// fact once, at the US market, and confirms FB for Meta until the rename and
// META after it, read through a ticker at any US venue.
func TestEventsRecord(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Open()
	t.Cleanup(pool.Close)
	tx := dbtest.Begin(t, pool)
	q := gen.New(tx)
	tbl, err := mic.Load(ctx, q)
	require.NoError(t, err)

	const cassette = "massive_events_ticker_change"
	key := vcr.Credential(t, "MASSIVE_API_KEY", "testdata/"+cassette)
	c, err := New(market.Config{Credential: key, JSON: json.RawMessage(conf)}, tbl, vcr.New(t, "testdata/"+cassette, scrub))
	require.NoError(t, err)

	user, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	parent, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindResolveStmtKeys, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	name := "massive-" + uuid.NewString()
	_, err = q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: name, Enabled: true, Precedence: 1000})
	require.NoError(t, err)
	log := slog.New(slog.DiscardHandler)
	sources, err := market.New(ctx, q, map[string]market.Factory{name: {New: func(market.Config) (market.Integration, error) { return c, nil }}}, log)
	require.NoError(t, err)
	entry := sources.Enabled()[slices.IndexFunc(sources.Enabled(), func(e *market.Entry) bool { return e.Name == name })]

	instr, err := q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
	require.NoError(t, err)
	listing, err := q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrID: instr.ID, Currency: "USD"})
	require.NoError(t, err)
	_, err = q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrID: instr.ID, ListingID: &listing.ID, Type: meta.Type, Value: meta.Value})
	require.NoError(t, err)

	answered := time.Date(2026, time.October, 22, 15, 0, 0, 0, time.UTC)
	fetcher := market.NewFetcher(db.New[market.Queries](tx), runtest.Runner{Q: q}, log, market.WithClock(func() time.Time { return answered }, func(context.Context, time.Duration) error { return nil }))
	keys, err := fetcher.IdentifierEvents(ctx, parent, entry, []market.IdentifierEventRequest{{Identifier: meta, Period: market.History(c, answered)}})
	require.NoError(t, err)
	require.Equal(t, gen.FetchOutcomeServed, keys[0].Outcome)
	families, _, err := currency.Load(ctx, q)
	require.NoError(t, err)
	require.NoError(t, tickervalidity.Record(ctx, q, tbl, families, keys))
	for table, want := range map[string]int{"identifier_assertions": 3, "identifier_events": 2, "identifier_event_coverage": 2} {
		var n int
		require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE fetch_key_id = $1", keys[0].ID).Scan(&n))
		if n != want {
			t.Errorf("%s rows = %d, want %d", table, n, want)
		}
	}

	tests := []struct {
		ticker string
		on     time.Time
		want   bool
	}{
		{ticker: "FB", on: day(2015, 1, 2), want: true},
		{ticker: "FB", on: day(2024, 1, 2)},
		{ticker: "META", on: day(2024, 1, 2), want: true},
		{ticker: "META", on: day(2015, 1, 2)},
	}
	for _, dom := range []string{"XNAS", "XNYS", "BATS"} {
		for _, tc := range tests {
			mt, ok := tbl.MarketTicker(ticker(dom, tc.ticker))
			require.True(t, ok)
			ivs, err := tickervalidity.Of(ctx, q, mt)
			require.NoError(t, err)
			if got := confirmed(ivs, instr.ID, tc.on); got != tc.want {
				t.Errorf("%s:%s confirmed for Meta on %s = %v, want %v", dom, tc.ticker, tc.on.Format(time.DateOnly), got, tc.want)
			}
		}
	}
}

// confirmed reports whether ivs confirm instrument on day d.
func confirmed(ivs []tickervalidity.Interval, instr uuid.UUID, d time.Time) bool {
	for _, iv := range ivs {
		if iv.Instr == instr && iv.Validity == tickervalidity.Confirmed &&
			(iv.From == nil || !d.Before(*iv.From)) && (iv.Before == nil || d.Before(*iv.Before)) {
			return true
		}
	}
	return false
}

// TestCorpActRecord checks the corporate actions recorded from Massive's
// answers. A spin-off factor is stored unhandled, coverage starts on since,
// and a split with a future ex-date is stored.
func TestCorpActRecord(t *testing.T) {
	answered := time.Date(2026, time.October, 26, 15, 0, 0, 0, time.UTC)
	always := types.Period{From: day(1970, 1, 1), Before: day(2100, 1, 1)}
	// before is the day after the answer, where every coverage ends.
	before := day(2026, 10, 27)
	tests := []struct {
		cassette string
		ticker   types.Identifier
		// actions states each stored action as "ex-date from:to", with
		// "unhandled" appended for an unhandled action.
		acts []string
	}{
		{
			cassette: "massive_splits_spinoff",
			ticker:   ticker("XNYS", "GE"),
			acts:     []string{"2021-08-02 8:1", "2023-01-04 1000:1281 unhandled", "2024-04-02 1000:1253 unhandled"},
		},
		{
			cassette: "massive_splits_announced",
			ticker:   ticker("XNAS", "SOXX"),
			acts:     []string{"2024-03-07 1:3", "2026-11-05 1:3"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.cassette, func(t *testing.T) {
			ctx := context.Background()
			pool := dbtest.Open()
			t.Cleanup(pool.Close)
			tx := dbtest.Begin(t, pool)
			q := gen.New(tx)
			tbl, err := mic.Load(ctx, q)
			require.NoError(t, err)
			key := vcr.Credential(t, "MASSIVE_API_KEY", "testdata/"+tc.cassette)
			c, err := New(market.Config{Credential: key, JSON: json.RawMessage(conf)}, tbl, vcr.New(t, "testdata/"+tc.cassette, scrub))
			require.NoError(t, err)

			user, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
			require.NoError(t, err)
			parent, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindIngestStmt, Trigger: gen.RunTriggerUser})
			require.NoError(t, err)
			name := "massive-" + uuid.NewString()
			_, err = q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: name, Enabled: true, Precedence: 1000})
			require.NoError(t, err)
			log := slog.New(slog.DiscardHandler)
			sources, err := market.New(ctx, q, map[string]market.Factory{name: {New: func(market.Config) (market.Integration, error) { return c, nil }}}, log)
			require.NoError(t, err)
			fetcher := market.NewFetcher(db.New[market.Queries](tx), runtest.Runner{Q: q}, log, market.WithClock(func() time.Time { return answered }, func(context.Context, time.Duration) error { return nil }))
			r := corpact.New(db.New[corpact.Queries](tx), fetcher, sources, tbl, log)
			require.NoError(t, r.Fetch(ctx, parent, []market.CorpActRequest{{Ticker: tc.ticker, Period: asked}}))

			mt, ok := tbl.MarketTicker(tc.ticker)
			require.True(t, ok)
			got, err := corpact.Read(ctx, q, []types.MarketTicker{mt}, always)
			require.NoError(t, err)
			i := slices.IndexFunc(got[mt], func(a corpact.Answer) bool { return a.Datasource == name })
			require.GreaterOrEqual(t, i, 0, "no answer from %s", name)
			a := got[mt][i]
			var acts []string
			for _, e := range a.Acts {
				d := e.ExDate.Format(time.DateOnly) + " " + e.Ratio.From.String() + ":" + e.Ratio.To.String()
				if !e.Handled {
					d += " unhandled"
				}
				acts = append(acts, d)
			}
			slices.Sort(acts)
			if diff := cmp.Diff(tc.acts, acts); diff != "" {
				t.Errorf("actions of %s mismatch (-want +got):\n%s", tc.ticker.Value, diff)
			}
			if len(a.Coverage) == 0 || !a.Coverage[0].From.Equal(since) || !a.Coverage[len(a.Coverage)-1].Before.Equal(before) {
				t.Errorf("coverage of %s = %v, want from %s before %s", tc.ticker.Value, a.Coverage, since.Format(time.DateOnly), before.Format(time.DateOnly))
			}
		})
	}
}
