//go:build dbtest

package price

import (
	"bytes"
	"context"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/cli/google"
	"github.com/leedenison/stonks/proto/admin/price/v1/adminpricev1connect"
	"github.com/leedenison/stonks/proto/auth/v1/authv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	googleauth "github.com/leedenison/stonks/server/internal/auth/google"
	"github.com/leedenison/stonks/server/internal/auth/session"
	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/market/googlesheets"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/platform/rdb"
	"github.com/leedenison/stonks/server/internal/price"
	"github.com/leedenison/stonks/server/internal/service"
	authsvc "github.com/leedenison/stonks/server/internal/service/auth"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/googletest"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/types"
)

var (
	pool *pgxpool.Pool
	rc   *redis.Client
)

func TestMain(m *testing.M) {
	url := os.Getenv("STONKS_TEST_REDIS_URL")
	if url == "" {
		log.Fatal("STONKS_TEST_REDIS_URL is not set")
	}
	var err error
	rc, err = rdb.Open(context.Background(), url)
	if err != nil {
		log.Fatalf("open redis: %v", err)
	}
	pool = dbtest.Open()
	code := m.Run()
	pool.Close()
	if err := rc.Close(); err != nil {
		log.Fatalf("close redis: %v", err)
	}
	os.Exit(code)
}

// The OAuth clients whose tokens the service accepts: the front end's and
// the tool's.
const (
	webClient = "web-client-id.apps.googleusercontent.com"
	cliClient = "cli-client-id.apps.googleusercontent.com"
)

// The listings that the tool's fixture of a pasted values tab names.
var (
	amzn    = uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	ibm     = uuid.MustParse("00000000-0000-0000-0000-0000000000a2")
	vod     = uuid.MustParse("00000000-0000-0000-0000-0000000000a3")
	gbpRate = uuid.MustParse("00000000-0000-0000-0000-0000000000a4")
)

// evaluated is a moment on the day the fixture was evaluated.
var evaluated = time.Date(2026, time.November, 26, 15, 0, 0, 0, time.UTC)

// TestCodesAreOperatingMICs checks that the tool keys its exchange codes by
// venues the service can send: each key is its own operating MIC.
func TestCodesAreOperatingMICs(t *testing.T) {
	mics, err := mic.Load(context.Background(), gen.New(pool))
	require.NoError(t, err)
	for venue := range google.Codes {
		if op, ok := mics.Operating(venue); !ok || op != venue {
			t.Errorf("google.Codes is keyed by %s, whose operating MIC is %q", venue, op)
		}
	}
}

// stack is the service as the tool reaches it, over a rolled-back
// transaction, and a Sheets API that serves the fixture as the values tab.
type stack struct {
	q      *gen.Queries
	server *httptest.Server
	sheets *httptest.Server
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	q := gen.New(tx)
	clock := func() time.Time { return evaluated }
	discard := slog.New(slog.DiscardHandler)

	_, err := q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: googlesheets.Name, Enabled: true, Precedence: 9000})
	require.NoError(t, err)
	sources, err := market.New(ctx, q, map[string]market.Factory{googlesheets.Name: googlesheets.Factory()}, discard)
	require.NoError(t, err)
	families, scales, err := currency.Load(ctx, q)
	require.NoError(t, err)
	for id, family := range map[uuid.UUID]string{amzn: "USD", ibm: "USD", vod: "GBP"} {
		instr, err := q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
		require.NoError(t, err)
		_, err = q.CreateListing(ctx, gen.CreateListingParams{ID: id, InstrID: instr.ID, Currency: family})
		require.NoError(t, err)
	}
	require.NoError(t, q.CreateRateListings(ctx, gen.CreateRateListingsParams{Ids: []uuid.UUID{gbpRate}, Codes: []string{"GBP"}}))

	jwks := googletest.NewJWKS(t)
	authn := auth.New(auth.Options{
		Verifier: googleauth.New([]string{webClient, cliClient}, googleauth.WithHTTPClient(jwks.Client()), googleauth.WithJWKSURL(jwks.URL), googleauth.WithClock(clock)),
		Users:    q,
		Sessions: session.New(rc, clock),
	})
	opts, err := service.HandlerOptions(discard, authn)
	require.NoError(t, err)
	uploader := price.NewUploader(db.New[price.UploadQueries](tx), runtest.Runner{Q: q}, families, scales)
	mux := http.NewServeMux()
	mux.Handle(authv1connect.NewAuthServiceHandler(authsvc.New(authn, false), opts...))
	mux.Handle(adminpricev1connect.NewPriceServiceHandler(New(q, price.NewDemand(q, families, clock), sources, uploader, clock), opts...))
	s := &stack{q: q, server: httptest.NewServer(mux)}
	t.Cleanup(s.server.Close)

	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "cli", "google", "testdata", "values.json"))
	require.NoError(t, err)
	s.sheets = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(w, `{"values": `+string(fixture)+`}`)
		require.NoError(t, err)
	}))
	t.Cleanup(s.sheets.Close)
	return s
}

// user creates an account with role and returns a Google ID token for it,
// issued to the tool's OAuth client.
func (s *stack) user(t *testing.T, role gen.UserRole) string {
	t.Helper()
	email := uuid.NewString() + "@example.com"
	_, err := s.q.CreateUser(context.Background(), gen.CreateUserParams{ID: db.NewID(), Email: email, Role: role})
	require.NoError(t, err)
	return googletest.Valid(cliClient, email, evaluated).Sign(t)
}

// stored is a row of the prices table as the test reads it back.
type stored struct {
	Day, Price, AsAt string
}

func (s *stack) prices(t *testing.T, listing uuid.UUID) []stored {
	t.Helper()
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	rows, err := s.q.ListPrices(context.Background(), gen.ListPricesParams{ListingIds: []uuid.UUID{listing}, FromDay: day(2026, 11, 1), Before: day(2026, 12, 1)})
	require.NoError(t, err)
	var out []stored
	for _, r := range rows {
		out = append(out, stored{Day: r.Day.Format(time.DateOnly), Price: r.Price.String(), AsAt: r.AsAt.Format(time.DateOnly)})
	}
	return out
}

// TestToolReads runs the read step of the tool against the service, signed
// in with a token of the tool's OAuth client, and checks the stored prices.
func TestToolReads(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	prices, err := google.SignIn(ctx, s.server.Client(), s.server.URL, s.user(t, gen.UserRoleAdmin))
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "google.json"), []byte(`{"spreadsheet_id": "sheet-1"}`), 0o600))
	out := &bytes.Buffer{}
	tool := &google.Tool{Sheets: google.NewSheets(s.sheets.Client(), s.sheets.URL), Prices: prices, Dir: dir, Clock: func() time.Time { return evaluated }, Out: out}

	// The fixture holds one symbol that Google does not know, so the step
	// reports it after it has uploaded the others.
	if err := tool.Read(ctx); err == nil || !strings.Contains(err.Error(), "1 series were refused or skipped") {
		t.Fatalf("Read() error = %v, want the one skipped series:\n%s", err, out)
	}
	if !strings.Contains(out.String(), "uploaded 4 series as at 2026-11-26, 0 refused, 1 skipped\n") {
		t.Errorf("report = %q, want four series uploaded", out)
	}
	// The pence of London become pounds, as at the day of the evaluation.
	got := s.prices(t, vod)
	if len(got) != 10 {
		t.Fatalf("%d prices of the London listing, want 10: %v", len(got), got)
	}
	if diff := cmp.Diff(stored{Day: "2026-11-02", Price: "1.1685", AsAt: "2026-11-26"}, got[0]); diff != "" {
		t.Errorf("first price of the London listing mismatch (-want +got):\n%s", diff)
	}
	// A rate is stored as at its own day, weekend days included.
	rate := s.prices(t, gbpRate)
	if len(rate) != 12 {
		t.Fatalf("%d prices of the rate, want 12: %v", len(rate), rate)
	}
	if diff := cmp.Diff(stored{Day: "2026-11-02", Price: "1.34088", AsAt: "2026-11-02"}, rate[0]); diff != "" {
		t.Errorf("first price of the rate mismatch (-want +got):\n%s", diff)
	}
	for listing, want := range map[uuid.UUID]int{amzn: 5, ibm: 10} {
		if got := s.prices(t, listing); len(got) != want {
			t.Errorf("%d prices of listing %s, want %d", len(got), listing, want)
		}
	}
}

// TestToolSignIn checks who the tool can sign in as.
func TestToolSignIn(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	t.Run("an account without the admin role is refused by the tool", func(t *testing.T) {
		if _, err := google.SignIn(ctx, s.server.Client(), s.server.URL, s.user(t, gen.UserRoleUser)); err == nil || !strings.Contains(err.Error(), "not an administrator") {
			t.Errorf("SignIn() error = %v, want a refusal of the role", err)
		}
	})
	t.Run("a token of an OAuth client the service does not accept is refused", func(t *testing.T) {
		token := googletest.Valid("other-client-id.apps.googleusercontent.com", "one@example.com", evaluated).Sign(t)
		if _, err := google.SignIn(ctx, s.server.Client(), s.server.URL, token); err == nil || !strings.Contains(err.Error(), "unauthenticated") {
			t.Errorf("SignIn() error = %v, want unauthenticated", err)
		}
	})
}
