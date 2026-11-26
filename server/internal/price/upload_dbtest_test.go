//go:build dbtest

package price

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/types"
)

// evaluated is the day on which the uploaded spreadsheet evaluated.
var evaluated = day(2026, 3, 10)

func (s *stack) uploader() *Uploader {
	return NewUploader(db.New[UploadQueries](s.tx), runtest.Runner{Q: s.q}, s.families, s.scales)
}

// upload uploads series as answers of alpha and fails the test on an error.
func (s *stack) upload(t *testing.T, series ...Series) (gen.Run, []Refusal) {
	t.Helper()
	run, refused, err := s.uploader().Upload(context.Background(), s.sources.Entry(s.alpha), evaluated, series)
	require.NoError(t, err)
	return run, refused
}

// rate returns the USD listing of the currency code, which it creates.
func (s *stack) rate(t *testing.T, code string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.q.CreateRateListings(ctx, gen.CreateRateListingsParams{Ids: []uuid.UUID{db.NewID()}, Codes: []string{code}}))
	rows, err := s.q.ListRateListings(ctx)
	require.NoError(t, err)
	for _, r := range rows {
		if r.Code == code && r.Currency == base {
			return r.ID
		}
	}
	t.Fatalf("the %s rate listing was not created", code)
	return uuid.Nil
}

func closeOn(d time.Time, price string) Close {
	return Close{Day: d, Price: decimal.RequireFromString(price)}
}

// sentKey is what a fetch key of an upload records.
type sentKey struct {
	Type, Domain, Value string
	Outcome             string
	AnsweredAt          string
	From, Before        string
}

// sent returns the fetch keys of the fetch run, by value.
func (s *stack) sent(t *testing.T, fetch uuid.UUID) []sentKey {
	t.Helper()
	rows, err := s.tx.Query(context.Background(), `SELECT sent_type::text, sent_domain, sent_value, outcome::text, answered_at, asked_from, asked_before
		FROM fetch_keys WHERE fetch_id = $1 AND user_id IS NULL ORDER BY sent_value`, fetch)
	require.NoError(t, err)
	defer rows.Close()
	var out []sentKey
	for rows.Next() {
		var k sentKey
		var at, from, before time.Time
		require.NoError(t, rows.Scan(&k.Type, &k.Domain, &k.Value, &k.Outcome, &at, &from, &before))
		k.AnsweredAt, k.From, k.Before = at.UTC().Format(time.RFC3339), from.Format(time.DateOnly), before.Format(time.DateOnly)
		out = append(out, k)
	}
	require.NoError(t, rows.Err())
	return out
}

// TestUpload checks what an upload stores for a security quoted in a scaled
// code and for a rate, and that a refused series leaves the others stored.
func TestUpload(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	gbp, usd, zero, eur := s.listing(t, "GBP"), s.listing(t, "USD"), s.listing(t, "USD"), s.rate(t, "EUR")
	missing := db.NewID()
	week := span(day(2026, 3, 2), day(2026, 3, 7))
	run, refused := s.upload(t,
		Series{Listing: gbp, Symbol: "LON:VOD", Currency: "GBX", Coverage: []types.Period{week}, Closes: []Close{
			closeOn(day(2026, 2, 27), "101"), closeOn(day(2026, 3, 2), "102.9"), closeOn(day(2026, 3, 3), "104.5"),
		}},
		Series{Listing: usd, Symbol: "NASDAQ:ABC", Currency: "EUR", Coverage: []types.Period{week}, Closes: []Close{closeOn(day(2026, 3, 2), "10")}},
		Series{Listing: missing, Symbol: "NASDAQ:GONE", Currency: "USD", Coverage: []types.Period{week}},
		Series{Listing: zero, Symbol: "NASDAQ:NIL", Currency: "USD", Coverage: []types.Period{week}, Closes: []Close{closeOn(day(2026, 3, 2), "0")}},
		Series{Listing: eur, Symbol: "CURRENCY:EURUSD", Currency: "USD", Coverage: []types.Period{week}, Closes: []Close{closeOn(day(2026, 3, 2), "1.0945")}},
	)

	wantRefused := map[uuid.UUID]error{usd: ErrCurrency, missing: ErrListing, zero: ErrInvalid}
	if len(refused) != len(wantRefused) {
		t.Errorf("%d series refused, want %d: %v", len(refused), len(wantRefused), refused)
	}
	for _, r := range refused {
		if !errors.Is(r.Err, wantRefused[r.Listing]) {
			t.Errorf("listing %s refused with %v, want %v", r.Listing, r.Err, wantRefused[r.Listing])
		}
	}
	// The pence become pounds, and each price is as at the evaluation. The
	// close before the coverage is left out.
	wantPrices := []stored{
		{Datasource: s.alpha, Day: "2026-03-02", Price: "1.029", AsAt: "2026-03-10"},
		{Datasource: s.alpha, Day: "2026-03-03", Price: "1.045", AsAt: "2026-03-10"},
	}
	if diff := cmp.Diff(wantPrices, s.prices(t, gbp)); diff != "" {
		t.Errorf("prices of the security mismatch (-want +got):\n%s", diff)
	}
	wantRate := []stored{{Datasource: s.alpha, Day: "2026-03-02", Price: "1.0945", AsAt: "2026-03-02"}}
	if diff := cmp.Diff(wantRate, s.prices(t, eur)); diff != "" {
		t.Errorf("prices of the rate mismatch (-want +got):\n%s", diff)
	}
	for _, l := range []uuid.UUID{usd, zero} {
		if got := s.coverage(t, s.alpha, l); len(got) != 0 {
			t.Errorf("coverage of the refused listing %s = %v, want none", l, got)
		}
	}

	want := gen.Run{ID: run.ID, Kind: gen.RunKindUploadPrices, Trigger: gen.RunTriggerAdministrator, State: gen.RunStateCompleted}
	if diff := cmp.Diff(want, gen.Run{ID: run.ID, UserID: run.UserID, Kind: run.Kind, Trigger: run.Trigger, State: run.State}); diff != "" {
		t.Errorf("run mismatch (-want +got):\n%s", diff)
	}
	var fetch uuid.UUID
	var kind, datasource, endpoint string
	require.NoError(t, s.tx.QueryRow(ctx, `SELECT runs.id, runs.kind::text, fetches.datasource, fetches.endpoint
		FROM runs JOIN fetches ON fetches.id = runs.id
		WHERE runs.parent_id = $1 AND runs.user_id IS NULL AND fetches.kind = 'prices'`, run.ID).Scan(&fetch, &kind, &datasource, &endpoint))
	if kind != string(gen.RunKindDatasourceFetch) || datasource != s.alpha || endpoint != "https://scripted.test" {
		t.Errorf("child run = %s of %s at %s, want a datasource fetch of %s at https://scripted.test", kind, datasource, endpoint, s.alpha)
	}
	// A refused series leaves no fetch key.
	wantSent := []sentKey{
		{Type: "currency", Value: "EUR", Outcome: "served", AnsweredAt: "2026-03-10T00:00:00Z", From: "2026-03-02", Before: "2026-03-07"},
		{Type: "datasource_ticker", Domain: s.alpha, Value: "LON:VOD", Outcome: "served", AnsweredAt: "2026-03-10T00:00:00Z", From: "2026-03-02", Before: "2026-03-07"},
	}
	if diff := cmp.Diff(wantSent, s.sent(t, fetch)); diff != "" {
		t.Errorf("fetch keys mismatch (-want +got):\n%s", diff)
	}
}

// TestUploadCoverage checks that coverage ends before the day of the
// evaluation, that a series keeps each of its periods apart, and that a
// second upload of covered days stores nothing.
func TestUploadCoverage(t *testing.T) {
	s := newStack(t)
	l := s.listing(t, "USD")
	series := Series{
		Listing: l, Symbol: "NASDAQ:ABC", Currency: "USD",
		Coverage: []types.Period{span(day(2026, 2, 2), day(2026, 2, 7)), span(day(2026, 3, 2), day(2026, 3, 14))},
		Closes: []Close{
			closeOn(day(2026, 2, 2), "10"), closeOn(day(2026, 2, 20), "11"), closeOn(day(2026, 3, 9), "12"),
		},
	}
	_, refused := s.upload(t, series)
	if len(refused) != 0 {
		t.Fatalf("Upload() refused %v, want nothing", refused)
	}
	wantCoverage := []types.Period{span(day(2026, 2, 2), day(2026, 2, 7)), span(day(2026, 3, 2), evaluated)}
	if diff := cmp.Diff(wantCoverage, s.coverage(t, s.alpha, l)); diff != "" {
		t.Errorf("coverage mismatch (-want +got):\n%s", diff)
	}
	// The close between the two periods is left out.
	wantPrices := []stored{
		{Datasource: s.alpha, Day: "2026-02-02", Price: "10", AsAt: "2026-03-10"},
		{Datasource: s.alpha, Day: "2026-03-09", Price: "12", AsAt: "2026-03-10"},
	}
	if diff := cmp.Diff(wantPrices, s.prices(t, l)); diff != "" {
		t.Errorf("prices mismatch (-want +got):\n%s", diff)
	}

	series.Closes = []Close{closeOn(day(2026, 2, 2), "99"), closeOn(day(2026, 2, 3), "99")}
	s.upload(t, series)
	if diff := cmp.Diff(wantPrices, s.prices(t, l)); diff != "" {
		t.Errorf("prices after a second upload mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantCoverage, s.coverage(t, s.alpha, l)); diff != "" {
		t.Errorf("coverage after a second upload mismatch (-want +got):\n%s", diff)
	}
}

// TestUploadAfterEvaluation checks that a series is refused when it holds a
// price of a security dated after the evaluation, which could not be stated
// as at that day.
func TestUploadAfterEvaluation(t *testing.T) {
	s := newStack(t)
	l := s.listing(t, "USD")
	_, refused := s.upload(t, Series{
		Listing: l, Symbol: "NASDAQ:ABC", Currency: "USD", Coverage: []types.Period{span(day(2026, 3, 2), day(2026, 3, 14))},
		Closes: []Close{closeOn(day(2026, 3, 2), "10"), closeOn(day(2026, 3, 11), "11")},
	})
	if len(refused) != 1 || !errors.Is(refused[0].Err, ErrInvalid) {
		t.Errorf("Upload() refused %v, want the series with ErrInvalid", refused)
	}
	if got := s.prices(t, l); len(got) != 0 {
		t.Errorf("prices = %v, want none", got)
	}
}
