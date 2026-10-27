//go:build dbtest

package refreshcorpacts

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/types"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

// now is the moment of every test. Every period ends on the day after it.
var now = time.Date(2026, time.March, 2, 15, 30, 0, 0, time.UTC)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// fetched is one call of the recorder, by the kind and user of its parent.
type fetched struct {
	Kind gen.RunKind
	User *uuid.UUID
	Reqs []asked
}

// asked is a request with its ticker at its market.
type asked struct {
	Ticker types.MarketTicker
	Period types.Period
}

// recorder records each call of Fetch.
type recorder struct {
	mics  mic.MICMap
	calls []fetched
}

func (r *recorder) Fetch(_ context.Context, parent gen.Run, reqs []market.CorpActRequest) error {
	f := fetched{Kind: parent.Kind, User: parent.UserID}
	for _, q := range reqs {
		t, ok := r.mics.MarketTicker(q.Ticker)
		if !ok {
			return nil
		}
		f.Reqs = append(f.Reqs, asked{Ticker: t, Period: q.Period})
	}
	r.calls = append(r.calls, f)
	return nil
}

// stack is a Service and a Starter over a rolled-back transaction.
type stack struct {
	q        *gen.Queries
	tx       pgx.Tx
	admin    uuid.UUID
	recorder *recorder
	svc      *Service
	starter  *Starter
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	q := gen.New(tx)
	mics, err := mic.Load(ctx, q)
	require.NoError(t, err)
	s := &stack{q: q, tx: tx, recorder: &recorder{mics: mics}}
	s.admin = s.user(t)
	clock := func() time.Time { return now }
	log := slog.New(slog.DiscardHandler)
	runs := runtest.Runner{Q: q}
	s.svc = New(q, runs, s.recorder, mics, clock, log)
	s.starter = NewStarter(q, runs, s.recorder, mics, clock, log)
	return s
}

func (s *stack) user(t *testing.T) uuid.UUID {
	t.Helper()
	u, err := s.q.CreateUser(context.Background(), gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	return u.ID
}

func (s *stack) instrument(t *testing.T) uuid.UUID {
	t.Helper()
	inst, err := s.q.CreateInstrument(context.Background(), gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
	require.NoError(t, err)
	return inst.ID
}

// value returns a ticker value unique to the test, so that rows another test
// left behind do not match.
func value(t *testing.T, prefix string) string {
	t.Helper()
	return prefix + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:8])
}

// names attests that US:v names inst on day on. inst is nil for an
// assertion that v names nothing.
func (s *stack) names(t *testing.T, v string, inst *uuid.UUID, on time.Time) {
	t.Helper()
	ctx := context.Background()
	run, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindAttestIdentifier, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	require.NoError(t, s.q.CreateIdentifierAssertion(ctx, gen.CreateIdentifierAssertionParams{
		ID: db.NewID(), RunID: &run.ID, AttestedBy: &s.admin, Type: types.IdentifierTypeMicTicker, Domain: "US", Value: v, InstrumentID: inst, AssertedOn: on,
	}))
}

// fixedKey creates a key of user with a fixed association to inst.
func (s *stack) fixedKey(t *testing.T, user, inst uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	via, key := db.NewID(), db.NewID()
	isin := uuid.NewString()
	_, err := s.tx.Exec(ctx, `INSERT INTO identifiers (id, instrument_id, type, value) VALUES ($1, $2, 'isin', $3)`, via, inst, isin)
	require.NoError(t, err)
	_, err = s.tx.Exec(ctx, `INSERT INTO stated_keys (id, user_id, identifiers, instrument_id, via_id, arbiter)
		VALUES ($1, $2, jsonb_build_array(jsonb_build_object('type', 'isin', 'value', $3::text)), $4, $5, 'stated')`, key, user, isin, inst, via)
	require.NoError(t, err)
	return key
}

// datedKey creates a key of user dated through XNAS:v.
func (s *stack) datedKey(t *testing.T, user uuid.UUID, v string) uuid.UUID {
	t.Helper()
	key := db.NewID()
	_, err := s.tx.Exec(context.Background(), `INSERT INTO stated_keys (id, user_id, identifiers, via_type, via_domain, via_value, arbiter)
		VALUES ($1, $2, jsonb_build_array(jsonb_build_object('type', 'mic_ticker', 'domain', 'XNAS', 'value', $3::text)), 'mic_ticker', 'XNAS', $3, 'datasource')`,
		key, user, v)
	require.NoError(t, err)
	return key
}

// buys records a statement of user with a buy of key as at each of days.
func (s *stack) buys(t *testing.T, user, key uuid.UUID, days ...time.Time) gen.Run {
	t.Helper()
	ctx := context.Background()
	run, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user, Kind: gen.RunKindIngestStmt, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	_, err = s.q.CreateStatement(ctx, gen.CreateStatementParams{ID: run.ID, UserID: user, Broker: gen.BrokerIbkr, OrderFrom: day(2000, 1, 1), OrderBefore: day(2026, 1, 1)})
	require.NoError(t, err)
	for _, d := range days {
		_, err := s.q.CreateTransaction(ctx, gen.CreateTransactionParams{
			ID: db.NewID(), UserID: user, Broker: gen.BrokerIbkr, StatementID: run.ID, StatedKeyID: key,
			OrderDate: d, SettlementDate: d, AsAt: d, Quantity: decimal.NewFromInt(10),
		})
		require.NoError(t, err)
	}
	return run
}

// mine keeps the calls' requests for values, and drops a call left empty.
func mine(calls []fetched, values ...string) []fetched {
	var out []fetched
	for _, c := range calls {
		var reqs []asked
		for _, q := range c.Reqs {
			for _, v := range values {
				if q.Ticker.Value == v {
					reqs = append(reqs, q)
				}
			}
		}
		if len(reqs) > 0 {
			c.Reqs = reqs
			out = append(out, c)
		}
	}
	return out
}

// shared is a ticker that named V's instrument and then U's.
type shared struct {
	u, v      uuid.UUID
	statement gen.Run
	ticker    string
	// earlier names V's instrument before ticker named it, and nothing after.
	earlier string
}

// newShared builds the shared ticker:
//   - ticker names instrument a from 2010-01-04 and b from 2020-01-02;
//   - V holds a through a fixed key from 2011-05-02;
//   - U holds b through a key dated by ticker from 2021-06-01;
//   - earlier names a from 2005-01-03 and nothing from 2008-01-02, before
//     V's transactions.
func newShared(t *testing.T, s *stack) shared {
	t.Helper()
	sh := shared{u: s.user(t), v: s.user(t), ticker: value(t, "T"), earlier: value(t, "E")}
	a, b := s.instrument(t), s.instrument(t)
	s.names(t, sh.ticker, &a, day(2010, 1, 4))
	s.names(t, sh.ticker, &b, day(2020, 1, 2))
	s.names(t, sh.earlier, &a, day(2005, 1, 3))
	s.names(t, sh.earlier, nil, day(2008, 1, 2))
	s.buys(t, sh.v, s.fixedKey(t, sh.v, a), day(2012, 2, 1), day(2011, 5, 2))
	sh.statement = s.buys(t, sh.u, s.datedKey(t, sh.u, sh.ticker), day(2021, 6, 1))
	return sh
}

// TestAfterStatementShared checks that a ticker that also named another
// user's instrument is asked about over every user's holding, beneath a
// system child of the statement, and that a ticker whose validity ends before
// the holding is not asked about.
func TestAfterStatementShared(t *testing.T) {
	s := newStack(t)
	sh := newShared(t, s)
	s.svc.AfterStatement(context.Background(), sh.statement)
	want := []fetched{{
		Kind: gen.RunKindRefreshCorpActs,
		Reqs: []asked{{Ticker: types.MarketTicker{Market: "US", Value: sh.ticker}, Period: types.Period{From: day(2011, 5, 2), Before: day(2026, 3, 3)}}},
	}}
	if diff := cmp.Diff(want, mine(s.recorder.calls, sh.ticker, sh.earlier)); diff != "" {
		t.Errorf("fetches mismatch (-want +got):\n%s", diff)
	}
}

// TestFetchAfterIdentifierFactsShared checks that new facts of the shared
// ticker ask for the same request as the statement does.
func TestFetchAfterIdentifierFactsShared(t *testing.T) {
	s := newStack(t)
	sh := newShared(t, s)
	written := []resolve.WrittenIdentifierFacts{{Run: sh.statement, Tickers: []types.MarketTicker{{Market: "US", Value: sh.ticker}}}}
	s.svc.FetchAfterIdentifierFacts(context.Background(), sh.statement, written)
	want := []fetched{{
		Kind: gen.RunKindRefreshCorpActs,
		Reqs: []asked{{Ticker: types.MarketTicker{Market: "US", Value: sh.ticker}, Period: types.Period{From: day(2011, 5, 2), Before: day(2026, 3, 3)}}},
	}}
	if diff := cmp.Diff(want, mine(s.recorder.calls, sh.ticker, sh.earlier)); diff != "" {
		t.Errorf("fetches mismatch (-want +got):\n%s", diff)
	}
}

// TestAfterStatementOwn checks that a ticker only the statement's user holds
// is fetched beneath the statement, from the earliest transaction, and that a
// dated transaction on a day when its ticker names nothing holds nothing.
func TestAfterStatementOwn(t *testing.T) {
	s := newStack(t)
	u := s.user(t)
	own, unnamed := value(t, "O"), value(t, "N")
	inst := s.instrument(t)
	s.names(t, own, &inst, day(2015, 1, 2))
	s.names(t, unnamed, nil, day(2015, 1, 2))
	statement := s.buys(t, u, s.datedKey(t, u, own), day(2016, 3, 1), day(2015, 7, 1))
	s.buys(t, u, s.datedKey(t, u, unnamed), day(2016, 3, 1))
	s.svc.AfterStatement(context.Background(), statement)
	want := []fetched{{
		Kind: gen.RunKindIngestStmt, User: &u,
		Reqs: []asked{{Ticker: types.MarketTicker{Market: "US", Value: own}, Period: types.Period{From: day(2015, 7, 1), Before: day(2026, 3, 3)}}},
	}}
	if diff := cmp.Diff(want, mine(s.recorder.calls, own, unnamed)); diff != "" {
		t.Errorf("fetches mismatch (-want +got):\n%s", diff)
	}
}

// TestStart checks that a refresh asks about the tickers of every held
// instrument, beneath a system run of its own.
func TestStart(t *testing.T) {
	s := newStack(t)
	sh := newShared(t, s)
	row, err := s.starter.Start(context.Background())
	require.NoError(t, err)
	if row.UserID != nil || row.Kind != gen.RunKindRefreshCorpActs || row.Trigger != gen.RunTriggerAdministrator {
		t.Errorf("Start() run = %+v, want a system refresh corporate actions run started by an administrator", row)
	}
	want := []fetched{{
		Kind: gen.RunKindRefreshCorpActs,
		Reqs: []asked{{Ticker: types.MarketTicker{Market: "US", Value: sh.ticker}, Period: types.Period{From: day(2011, 5, 2), Before: day(2026, 3, 3)}}},
	}}
	if diff := cmp.Diff(want, mine(s.recorder.calls, sh.ticker, sh.earlier)); diff != "" {
		t.Errorf("fetches mismatch (-want +got):\n%s", diff)
	}
}

// TestHeldQueries checks the rows of the two queries across two users.
func TestHeldQueries(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	u, v := s.user(t), s.user(t)
	inst := s.instrument(t)
	s.buys(t, u, s.fixedKey(t, u, inst), day(2020, 3, 1), day(2019, 3, 1))
	s.buys(t, v, s.fixedKey(t, v, inst), day(2021, 3, 1))
	tk := value(t, "D")
	s.buys(t, u, s.datedKey(t, u, tk), day(2022, 3, 1), day(2022, 3, 1), day(2022, 4, 1))

	instruments, err := s.q.ListHeldInstruments(ctx)
	require.NoError(t, err)
	var gotInst []gen.ListHeldInstrumentsRow
	for _, r := range instruments {
		if r.InstrumentID == inst {
			gotInst = append(gotInst, r)
		}
	}
	wantInst := []gen.ListHeldInstrumentsRow{{InstrumentID: inst, UserID: u, FirstAsAt: day(2019, 3, 1)}, {InstrumentID: inst, UserID: v, FirstAsAt: day(2021, 3, 1)}}
	if u.String() > v.String() {
		wantInst[0], wantInst[1] = wantInst[1], wantInst[0]
	}
	if diff := cmp.Diff(wantInst, gotInst); diff != "" {
		t.Errorf("ListHeldInstruments mismatch (-want +got):\n%s", diff)
	}

	dates, err := s.q.ListHeldTickerDates(ctx)
	require.NoError(t, err)
	var gotDates []gen.ListHeldTickerDatesRow
	for _, r := range dates {
		if r.Value == tk {
			gotDates = append(gotDates, r)
		}
	}
	wantDates := []gen.ListHeldTickerDatesRow{{Domain: "XNAS", Value: tk, UserID: u, AsAt: day(2022, 3, 1)}, {Domain: "XNAS", Value: tk, UserID: u, AsAt: day(2022, 4, 1)}}
	if diff := cmp.Diff(wantDates, gotDates); diff != "" {
		t.Errorf("ListHeldTickerDates mismatch (-want +got):\n%s", diff)
	}
}
