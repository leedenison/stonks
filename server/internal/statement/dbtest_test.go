//go:build dbtest

package statement

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/run"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	url := os.Getenv("STONKS_TEST_DATABASE_URL")
	if url == "" {
		log.Fatal("STONKS_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	var err error
	pool, err = db.Open(ctx, url)
	if err != nil {
		log.Fatalf("open pool: %v", err)
	}
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// syncRunner runs each run inline over the test's transaction, moving the
// row through the states the real runner would.
type syncRunner struct {
	q *gen.Queries
}

func (r syncRunner) Start(ctx context.Context, spec run.Spec, work run.Work) (gen.Run, error) {
	row, err := r.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: spec.UserID, Kind: spec.Kind, Trigger: gen.RunTriggerUser})
	if err != nil {
		return row, err
	}
	if spec.Prepare != nil {
		if err := spec.Prepare(ctx, row); err != nil {
			return row, err
		}
	}
	return row, r.execute(ctx, row, work)
}

func (r syncRunner) Child(ctx context.Context, parent gen.Run, kind gen.RunKind, work run.Work) (gen.Run, error) {
	row, err := r.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: parent.UserID, Kind: kind, Trigger: gen.RunTriggerRun, ParentID: &parent.ID})
	if err != nil {
		return row, err
	}
	return row, r.execute(ctx, row, work)
}

func (r syncRunner) execute(ctx context.Context, row gen.Run, work run.Work) error {
	if _, err := r.q.StartRun(ctx, row.ID); err != nil {
		return err
	}
	if err := work(ctx, row); err != nil {
		return errors.Join(err, r.q.FailRun(ctx, gen.FailRunParams{ID: row.ID, Error: err.Error()}))
	}
	return r.q.CompleteRun(ctx, row.ID)
}

type stack struct {
	q    *gen.Queries
	svc  *Service
	user gen.User
}

// newStack returns a service over a transaction rolled back when the test
// ends, and a user to ingest as.
func newStack(t *testing.T) stack {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback: %v", err)
		}
	})
	q := gen.New(tx)
	user, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: fmt.Sprintf("%s@example.com", uuid.NewString()), Role: gen.UserRoleUser})
	require.NoError(t, err)
	return stack{q: q, svc: New(db.New[Queries](tx), syncRunner{q: q}, clock), user: user}
}

func (s stack) ingest(t *testing.T, msg *statementv1.Statement) gen.Run {
	t.Helper()
	ctx := context.Background()
	row, err := s.svc.Create(ctx, s.user.ID, msg)
	require.NoError(t, err)
	got, err := s.q.GetRun(ctx, gen.GetRunParams{ID: row.ID, UserID: s.user.ID})
	require.NoError(t, err)
	if got.State != gen.RunStateCompleted {
		t.Fatalf("statement run = %+v, want completed", got)
	}
	return got
}

func (s stack) transactions(t *testing.T) []string {
	t.Helper()
	rows, err := s.q.ListTransactions(context.Background(), s.user.ID)
	require.NoError(t, err)
	var out []string
	for _, r := range rows {
		cur := "-"
		if r.Currency != nil {
			cur = *r.Currency
		}
		out = append(out, fmt.Sprintf("%s %s %s %s", r.OrderDate.Format(time.DateOnly), r.Broker, r.Quantity, cur))
	}
	return out
}

// TestIngest ingests one statement of the shape an IBKR export takes and
// reads back everything it leaves.
func TestIngest(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	usd, eur := "USD", "EUR"
	acme := securityKey("ACME CORP", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US0000000001", nil))
	transfer := securityKey("ACME CORP", typev1.AssetClass_ASSET_CLASS_EQUITY, nil, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US0000000001", nil))
	msg := &statementv1.Statement{
		Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
		Rows: []*statementv1.Row{
			rowMsg(acme, "2026-03-05", "10"),
			rowMsg(cashKey("USD"), "2026-03-05", "-1000"),
			rowMsg(transfer, "2026-03-02", "3"),
			rowMsg(acme, "2026-04-20", "1"),
			rowMsg(securityKey("ACME CORP", typev1.AssetClass_ASSET_CLASS_EQUITY, &eur), "2026-03-06", "1"),
			rowMsg(securityKey("ACME CORP", typev1.AssetClass_ASSET_CLASS_OPTION, &usd), "2026-03-06", "1"),
		},
		Splits: []*statementv1.StatedSplit{{Key: securityKey("SPLIT CO", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), EffectiveDate: "2026-03-10", Quantity: "9", Ratio: &statementv1.SplitRatio{From: "1", To: "10"}}},
	}
	parent := s.ingest(t, msg)

	st, err := s.q.GetStatement(ctx, gen.GetStatementParams{ID: parent.ID, UserID: s.user.ID})
	require.NoError(t, err)
	if st.Statement.Broker != gen.BrokerIbkr || st.Statement.RowCount != 6 || st.Rejected != 3 {
		t.Errorf("GetStatement = %+v, want ibkr with 6 rows and 3 rejected", st)
	}
	keys, err := s.q.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: parent.ID, UserID: s.user.ID})
	require.NoError(t, err)
	if len(keys) != 6 {
		t.Errorf("ListStatedKeys = %d keys, want 6: the trade, cash, the transfer, EUR, the option and the split", len(keys))
	}
	items, err := s.q.ListStatementItems(ctx, gen.ListStatementItemsParams{StatementID: parent.ID, UserID: s.user.ID})
	require.NoError(t, err)
	var gotItems []string
	for _, it := range items {
		gotItems = append(gotItems, fmt.Sprintf("%d %s", it.Ordinal, it.Reason))
	}
	wantItems := []string{
		"3 order date outside the claimed period",
		"4 currency EUR contradicts the listing named, quoted in USD",
		"5 asset class option contradicts the listing's equity",
	}
	if diff := cmp.Diff(wantItems, gotItems); diff != "" {
		t.Errorf("items mismatch (-want +got):\n%s", diff)
	}
	want := []string{"2026-03-02 ibkr 3 -", "2026-03-05 ibkr 10 USD", "2026-03-05 ibkr -1000 USD"}
	if diff := cmp.Diff(want, s.transactions(t)); diff != "" {
		t.Errorf("transactions mismatch (-want +got):\n%s", diff)
	}

	// The transfer, stating no currency, landed on the listing the trade created.
	txs, err := s.q.ListTransactions(ctx, s.user.ID)
	require.NoError(t, err)
	if txs[0].ListingID == nil || txs[1].ListingID == nil || *txs[0].ListingID != *txs[1].ListingID || txs[0].InstrumentID != txs[1].InstrumentID {
		t.Errorf("transfer landed on %v of %s, trade on %v of %s, want the same listing", txs[0].ListingID, txs[0].InstrumentID, txs[1].ListingID, txs[1].InstrumentID)
	}
	dom := "ibkr/upload"
	named, err := s.q.GetListingByIdentifier(ctx, gen.GetListingByIdentifierParams{OwnerID: &s.user.ID, Type: gen.IdentifierTypeBrokerDescription, Domain: &dom, Value: "ACME CORP"})
	require.NoError(t, err)
	if named.Listing.ID != *txs[0].ListingID || named.AssetClass != gen.AssetClassEquity || named.Listing.OwnerID == nil {
		t.Errorf("the description names %+v, want the user owned equity listing the trade landed on", named)
	}
	ids, err := s.q.ListIdentifiers(ctx, gen.ListIdentifiersParams{InstrumentID: named.Listing.InstrumentID, UserID: s.user.ID})
	require.NoError(t, err)
	if len(ids) != 1 {
		t.Errorf("ListIdentifiers = %+v, want only the broker description admitted", ids)
	}

	children, err := s.q.ListChildRuns(ctx, gen.ListChildRunsParams{ParentID: &parent.ID, UserID: s.user.ID})
	require.NoError(t, err)
	if len(children) != 1 || children[0].Kind != gen.RunKindResolution || children[0].State != gen.RunStateCompleted {
		t.Fatalf("ListChildRuns = %+v, want one completed resolution", children)
	}
	resolved, err := s.q.ListResolutionKeys(ctx, gen.ListResolutionKeysParams{RunID: children[0].ID, UserID: s.user.ID})
	require.NoError(t, err)
	outcomes := map[gen.ResolutionOutcome]int{}
	for _, r := range resolved {
		outcomes[r.Outcome]++
	}
	wantOutcomes := map[gen.ResolutionOutcome]int{gen.ResolutionOutcomeCreated: 2, gen.ResolutionOutcomeMatched: 2, gen.ResolutionOutcomeRejected: 2}
	if diff := cmp.Diff(wantOutcomes, outcomes); diff != "" {
		t.Errorf("resolution outcomes mismatch (-want +got):\n%s", diff)
	}
}

// TestReplace checks that a statement replaces the period it claims and
// nothing else, and that a claim with no rows deletes.
func TestReplace(t *testing.T) {
	s := newStack(t)
	usd := "USD"
	acme := securityKey("ACME CORP", typev1.AssetClass_ASSET_CLASS_SECURITY, &usd)
	s.ingest(t, &statementv1.Statement{Broker: typev1.Broker_BROKER_SCHWAB, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
		Rows: []*statementv1.Row{rowMsg(acme, "2026-03-05", "1"), rowMsg(acme, "2026-03-20", "2")}})
	s.ingest(t, &statementv1.Statement{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
		Rows: []*statementv1.Row{rowMsg(acme, "2026-03-20", "7")}})
	s.ingest(t, &statementv1.Statement{Broker: typev1.Broker_BROKER_SCHWAB, OrderFrom: "2026-03-15", OrderBefore: "2026-04-01",
		Rows: []*statementv1.Row{rowMsg(acme, "2026-03-20", "3")}})
	want := []string{"2026-03-05 schwab 1 USD", "2026-03-20 ibkr 7 USD", "2026-03-20 schwab 3 USD"}
	if diff := cmp.Diff(want, s.transactions(t)); diff != "" {
		t.Errorf("after the overlapping statement (-want +got):\n%s", diff)
	}
	s.ingest(t, &statementv1.Statement{Broker: typev1.Broker_BROKER_SCHWAB, OrderFrom: "2026-03-15", OrderBefore: "2026-04-01"})
	want = []string{"2026-03-05 schwab 1 USD", "2026-03-20 ibkr 7 USD"}
	if diff := cmp.Diff(want, s.transactions(t)); diff != "" {
		t.Errorf("after the empty statement (-want +got):\n%s", diff)
	}
}

// TestTree checks that the class tree resolution consults is the one the
// database holds.
func TestTree(t *testing.T) {
	s := newStack(t)
	rows, err := s.q.ListAssetClassTree(context.Background())
	require.NoError(t, err)
	got := map[gen.AssetClass]gen.AssetClass{}
	for _, r := range rows {
		var parent gen.AssetClass
		if r.Parent != nil {
			parent = *r.Parent
		}
		got[r.Class] = parent
	}
	if diff := cmp.Diff(parents, got); diff != "" {
		t.Errorf("asset class tree mismatch (-code +table):\n%s", diff)
	}
}
