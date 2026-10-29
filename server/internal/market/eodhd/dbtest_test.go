//go:build dbtest

package eodhd

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/corpact"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/market/markettest"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/testutil/vcr"
	"github.com/leedenison/stonks/server/internal/types"
)

// TestVenuesAreMICs checks that every MIC the tables name is in the MIC
// reference table.
func TestVenuesAreMICs(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Open()
	t.Cleanup(pool.Close)
	tbl, err := mic.Load(ctx, gen.New(pool))
	require.NoError(t, err)
	var venues []string
	for _, ms := range codes {
		venues = append(venues, ms...)
	}
	markettest.VenuesAreMICs(t, tbl, slices.Concat(venues, slices.Collect(maps.Values(usVenues)))...)
}

// recorded fetches and records the corporate actions of ticker from the
// cassette.
func recorded(t *testing.T, cassette string, ticker types.Identifier) (*gen.Queries, string, types.MarketTicker) {
	t.Helper()
	ctx := context.Background()
	pool := dbtest.Open()
	t.Cleanup(pool.Close)
	tx := dbtest.Begin(t, pool)
	q := gen.New(tx)
	tbl, err := mic.Load(ctx, q)
	require.NoError(t, err)
	key := vcr.Credential(t, "EODHD_API_KEY", "testdata/"+cassette)
	c, err := New(market.Config{Credential: key}, tbl, vcr.New(t, "testdata/"+cassette, scrub))
	require.NoError(t, err)

	user, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	parent, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindIngestStmt, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	name := "eodhd-" + uuid.NewString()
	_, err = q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: name, Enabled: true, Precedence: 1000})
	require.NoError(t, err)
	mt, ok := tbl.MarketTicker(ticker)
	require.True(t, ok)
	dbtest.CorpActSource(t, tx, mt.Market, name)
	log := slog.New(slog.DiscardHandler)
	sources, err := market.New(ctx, q, map[string]market.Factory{name: {New: func(market.Config) (market.Integration, error) { return c, nil }}}, log)
	require.NoError(t, err)
	answered := time.Date(2026, time.October, 26, 21, 0, 0, 0, time.UTC)
	fetcher := market.NewFetcher(db.New[market.Queries](tx), runtest.Runner{Q: q}, log, market.WithClock(func() time.Time { return answered }, func(context.Context, time.Duration) error { return nil }))
	r := corpact.New(db.New[corpact.Queries](tx), fetcher, sources, tbl, log)
	require.NoError(t, r.Fetch(ctx, parent, []market.CorpActRequest{{Ticker: ticker, Period: asked}}))
	return q, name, mt
}

// TestCorpActRecord checks the corporate actions recorded from EODHD's
// answers. A price factor is stored unhandled, except for the GE spin-off of
// 2019, which reads as a 26 for 25 split. Coverage runs from the first day asked to
// the day of the answer.
func TestCorpActRecord(t *testing.T) {
	// before is the day after the answer, where every coverage ends.
	before := day(2026, 10, 27)
	tests := []struct {
		cassette string
		ticker   types.Identifier
		acts     []string
	}{
		{
			cassette: "eodhd_splits_lse",
			ticker:   ticker("XLON", "VOD"),
			acts:     []string{"1994-07-21 319897:959692 unhandled", "1999-10-01 1:5", "2006-07-31 8:7", "2014-02-24 11:6"},
		},
		{
			cassette: "eodhd_splits_spinoff",
			ticker:   ticker("XNYS", "GE"),
			acts: []string{
				"1971-06-08 1:2", "1983-06-02 1:2", "1987-05-26 1:2", "1994-05-16 1:2", "1997-05-12 1:2", "2000-05-08 1:3",
				"2019-02-26 100:104", "2021-08-02 8:1", "2023-01-04 1000:1281 unhandled", "2024-04-02 1000:1253 unhandled",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.cassette, func(t *testing.T) {
			q, name, mt := recorded(t, tc.cassette, tc.ticker)
			got, err := corpact.Read(context.Background(), q, []types.MarketTicker{mt}, asked)
			require.NoError(t, err)
			a, ok := got[mt]
			require.True(t, ok && a.Datasource == name, "no answer from %s", name)
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
			if len(a.Coverage) == 0 || !a.Coverage[0].From.Equal(asked.From) || !a.Coverage[len(a.Coverage)-1].Before.Equal(before) {
				t.Errorf("coverage of %s = %v, want from %s before %s", tc.ticker.Value, a.Coverage, asked.From.Format(time.DateOnly), before.Format(time.DateOnly))
			}
		})
	}
}

// TestCorpActRecordUnknown checks that a symbol EODHD does not know is
// stored as an answer without coverage, and blocks nothing.
func TestCorpActRecordUnknown(t *testing.T) {
	ctx := context.Background()
	q, name, mt := recorded(t, "eodhd_splits_unknown", ticker("XNAS", "ZZZZQX"))
	rows, err := q.ListCorpActAnswers(ctx, gen.ListCorpActAnswersParams{Markets: []string{mt.Market}, Values: []string{mt.Value}})
	require.NoError(t, err)
	i := slices.IndexFunc(rows, func(r gen.ListCorpActAnswersRow) bool { return r.CorpActAnswer.Datasource == name })
	require.GreaterOrEqual(t, i, 0, "no answer from %s", name)
	if a := rows[i].CorpActAnswer; a.CoveredFrom != nil || a.CoveredBefore != nil {
		t.Errorf("coverage of ZZZZQX = %v to %v, want none", a.CoveredFrom, a.CoveredBefore)
	}
	blocks, err := q.ListOpenBlocks(ctx, gen.ListOpenBlocksParams{Datasource: name, Kind: gen.FetchKindCorpActs})
	require.NoError(t, err)
	if len(blocks) != 0 {
		t.Errorf("open blocks = %+v, want none", blocks)
	}
}
