package to

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminblockv1 "github.com/leedenison/stonks/proto/admin/block/v1"
	admininstrumentv1 "github.com/leedenison/stonks/proto/admin/instrument/v1"
	adminrunv1 "github.com/leedenison/stonks/proto/admin/run/v1"
	authv1 "github.com/leedenison/stonks/proto/auth/v1"
	holdingv1 "github.com/leedenison/stonks/proto/holding/v1"
	runv1 "github.com/leedenison/stonks/proto/run/v1"
	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

func TestIdentifier(t *testing.T) {
	row := gen.Identifier{ID: uuid.New(), InstrumentID: uuid.New(), Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"}
	want := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"}
	if got := Identifier(row); got != want {
		t.Errorf("Identifier(%+v) = %+v, want %+v", row, got, want)
	}
}

func TestSent(t *testing.T) {
	typ, value := types.IdentifierTypeIsin, "GB00BH4HKS39"
	if got, ok := Sent(gen.FetchKey{SentType: &typ, SentValue: &value}); !ok || got != (types.Identifier{Type: typ, Value: value}) {
		t.Errorf("Sent(served) = %+v, %v, want the ISIN", got, ok)
	}
	if _, ok := Sent(gen.FetchKey{}); ok {
		t.Error("Sent(not served) = ok, want false")
	}
	if got, ok := BlockSent(gen.DatasourceBlock{SentType: &typ, SentDomain: "XLON", SentValue: &value}); !ok || got.Domain != "XLON" {
		t.Errorf("BlockSent = %+v, %v, want the venue kept", got, ok)
	}
	ticker := types.IdentifierTypeMicTicker
	domain := types.Identifier{Type: ticker, Domain: "XNYS"}
	if got, ok := Sent(gen.FetchKey{SentType: &ticker, SentDomain: "XNYS"}); !ok || got != domain || !got.IsDomain() {
		t.Errorf("Sent(domain) = %+v, %v, want %+v", got, ok, domain)
	}
}

func TestProtoRun(t *testing.T) {
	id, parent := uuid.New(), uuid.New()
	created := time.Date(2026, time.September, 24, 10, 0, 0, 0, time.UTC)
	started := created.Add(time.Second)
	row := gen.Run{ID: id, ParentID: &parent, Kind: gen.RunKindDatasourceFetch, Trigger: gen.RunTriggerRun, State: gen.RunStateRunning, CreatedAt: created, StartedAt: &started}
	want := &runv1.Run{
		Id: id.String(), ParentId: ptr.To(parent.String()), Kind: runv1.RunKind_RUN_KIND_DATASOURCE_FETCH, Trigger: runv1.RunTrigger_RUN_TRIGGER_RUN,
		State: runv1.RunState_RUN_STATE_RUNNING, CreatedAt: timestamppb.New(created), StartedAt: timestamppb.New(started),
	}
	if diff := cmp.Diff(want, ProtoRun(row), protocmp.Transform()); diff != "" {
		t.Errorf("ProtoRun mismatch (-want +got):\n%s", diff)
	}
}

func TestProtoUser(t *testing.T) {
	id := uuid.New()
	row := gen.User{ID: id, Email: "one@example.com", Name: "One", Role: gen.UserRoleAdmin}
	want := &authv1.User{Id: id.String(), Email: "one@example.com", Name: "One", Role: authv1.Role_ROLE_ADMIN}
	if diff := cmp.Diff(want, ProtoUser(row), protocmp.Transform()); diff != "" {
		t.Errorf("ProtoUser mismatch (-want +got):\n%s", diff)
	}
}

func TestProtoStatedKey(t *testing.T) {
	row := gen.StatedKey{AssetClass: ptr.To(types.AssetClassStock), Currency: ptr.To("GBP"), Identifiers: []types.Identifier{{Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"}}}
	want := &typev1.StatedKey{
		AssetClass: typev1.AssetClass_ASSET_CLASS_STOCK, Currency: ptr.To("GBP"),
		Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB00BH4HKS39"}},
	}
	if diff := cmp.Diff(want, ProtoStatedKey(row), protocmp.Transform()); diff != "" {
		t.Errorf("ProtoStatedKey mismatch (-want +got):\n%s", diff)
	}
}

// TestProtoResolutionItem checks that only a dated key carries a ticker.
func TestProtoResolutionItem(t *testing.T) {
	instrument := uuid.New()
	tests := []struct {
		name string
		key  gen.StatedKey
		want *typev1.Identifier
	}{
		{name: "dated", key: gen.StatedKey{ViaType: ptr.To(types.IdentifierTypeMicTicker), ViaDomain: ptr.To("XNAS"), ViaValue: ptr.To("ACME")}, want: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: "XNAS", Value: "ACME"}},
		{name: "fixed", key: gen.StatedKey{InstrumentID: &instrument}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ProtoResolutionItem(tc.key, nil)
			if diff := cmp.Diff(tc.want, got.GetTicker(), protocmp.Transform()); diff != "" {
				t.Errorf("ProtoResolutionItem ticker mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestInstrumentName(t *testing.T) {
	row := gen.InstrumentName{InstrumentID: uuid.New(), Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"}
	want := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"}
	if got := InstrumentName(row); got != want {
		t.Errorf("InstrumentName(%+v) = %+v, want %+v", row, got, want)
	}
}

func TestProtoHoldingListing(t *testing.T) {
	id := uuid.New()
	ticker := &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: "XLON", Value: "VOD"}
	tests := []struct {
		name string
		row  gen.ListListingNamesRow
		want *holdingv1.HoldingListing
	}{
		{
			name: "named by a ticker",
			row:  gen.ListListingNamesRow{ListingID: id, Currency: "GBP", Venue: "LSE", TickerDomain: ptr.To("XLON"), TickerValue: ptr.To("VOD")},
			want: &holdingv1.HoldingListing{Id: id.String(), Currency: "GBP", Venue: "LSE", Ticker: ticker},
		},
		{
			name: "without a ticker",
			row:  gen.ListListingNamesRow{ListingID: id, Currency: "GBP", Venue: "XLON"},
			want: &holdingv1.HoldingListing{Id: id.String(), Currency: "GBP", Venue: "XLON"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, ProtoHoldingListing(tc.row), protocmp.Transform()); diff != "" {
				t.Errorf("ProtoHoldingListing mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestProtoHoldingKey(t *testing.T) {
	listing := uuid.New()
	row := gen.ListHoldingKeysRow{StatedKey: gen.StatedKey{ListingID: &listing, Currency: ptr.To("GBP")}, Quantity: decimal.RequireFromString("12.5")}
	want := &holdingv1.HoldingKey{StatedKey: &typev1.StatedKey{Currency: ptr.To("GBP")}, Quantity: "12.5", ListingId: listing.String()}
	if diff := cmp.Diff(want, ProtoHoldingKey(row), protocmp.Transform()); diff != "" {
		t.Errorf("ProtoHoldingKey mismatch (-want +got):\n%s", diff)
	}
}

func TestProtoTxn(t *testing.T) {
	id, key := uuid.New(), uuid.New()
	row := gen.Txn{ID: id, StatedKeyID: key, OrderDate: time.Date(2024, time.January, 16, 0, 0, 0, 0, time.UTC), Quantity: decimal.RequireFromString("-0.5")}
	want := &statementv1.Txn{Id: id.String(), StatedKeyId: key.String(), OrderDate: "2024-01-16", Quantity: "-0.5", Mark: statementv1.TxnMark_TXN_MARK_PROVISIONAL}
	if diff := cmp.Diff(want, ProtoTxn(row, statementv1.TxnMark_TXN_MARK_PROVISIONAL), protocmp.Transform()); diff != "" {
		t.Errorf("ProtoTxn mismatch (-want +got):\n%s", diff)
	}
}

func TestProtoStmtSummary(t *testing.T) {
	id := uuid.New()
	created := time.Date(2026, time.September, 24, 10, 0, 0, 0, time.UTC)
	stmt := gen.Stmt{Broker: gen.BrokerIbkr, OrderFrom: created, OrderBefore: created.AddDate(0, 1, 0), RowCount: 40}
	run := gen.Run{ID: id, Kind: gen.RunKindIngestStmt, Trigger: gen.RunTriggerUser, State: gen.RunStateCompleted, CreatedAt: created}
	want := &statementv1.StatementSummary{
		Run: ProtoRun(run), Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-09-24", OrderBefore: "2026-10-24", Rows: 40, Rejected: 2,
	}
	if diff := cmp.Diff(want, ProtoStmtSummary(stmt, run, 2), protocmp.Transform()); diff != "" {
		t.Errorf("ProtoStmtSummary mismatch (-want +got):\n%s", diff)
	}
}

func TestProtoBlock(t *testing.T) {
	id, fetch := uuid.New(), uuid.New()
	created := time.Date(2026, time.September, 24, 10, 0, 0, 0, time.UTC)
	typ, value := types.IdentifierTypeIsin, "GB00BH4HKS39"
	row := gen.ListBlocksRow{
		DatasourceBlock: gen.DatasourceBlock{ID: id, Datasource: "openfigi", Kind: gen.FetchKindIdentity, Scope: gen.BlockScopeIdentifier, Reason: "refused", SentType: &typ, SentValue: &value, CreatedAt: created, ClearedAt: &created},
		FetchID:         fetch,
	}
	want := &adminblockv1.Block{
		Id: id.String(), Datasource: "openfigi", Kind: adminblockv1.FetchKind_FETCH_KIND_IDENTITY, Scope: adminblockv1.BlockScope_BLOCK_SCOPE_IDENTIFIER,
		Reason: "refused", RunId: fetch.String(), CreatedAt: timestamppb.New(created), ClearedAt: timestamppb.New(created),
		Sent: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: value},
	}
	if diff := cmp.Diff(want, ProtoBlock(row), protocmp.Transform()); diff != "" {
		t.Errorf("ProtoBlock mismatch (-want +got):\n%s", diff)
	}
}

func TestProtoFetchItem(t *testing.T) {
	key := uuid.New()
	typ, value := types.IdentifierTypeIsin, "GB00BH4HKS39"
	ticker := types.IdentifierTypeMicTicker
	tests := []struct {
		name string
		row  gen.ListFetchItemsRow
		want *adminrunv1.FetchItem
	}{
		{
			name: "a stated key's identifier",
			row: gen.ListFetchItemsRow{
				FetchKey:    gen.FetchKey{StatedKeyID: &key, Outcome: gen.FetchOutcomeServed, Attempts: 2, Reason: ptr.To("ok"), SentType: &typ, SentValue: &value},
				KeyCurrency: ptr.To("GBP"),
			},
			want: &adminrunv1.FetchItem{
				StatedKey: &typev1.StatedKey{Currency: ptr.To("GBP")}, StatedKeyId: ptr.To(key.String()), Outcome: adminrunv1.FetchOutcome_FETCH_OUTCOME_SERVED,
				Attempts: 2, Reason: ptr.To("ok"), Sent: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: value},
			},
		},
		{
			name: "a domain no stated key asked for",
			row:  gen.ListFetchItemsRow{FetchKey: gen.FetchKey{Outcome: gen.FetchOutcomeServed, Attempts: 1, SentType: &ticker, SentDomain: "XNYS"}},
			want: &adminrunv1.FetchItem{
				Outcome: adminrunv1.FetchOutcome_FETCH_OUTCOME_SERVED, Attempts: 1,
				Sent: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: "XNYS"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, ProtoFetchItem(tc.row), protocmp.Transform()); diff != "" {
				t.Errorf("ProtoFetchItem mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestProtoInstrument(t *testing.T) {
	id := uuid.New()
	tests := []struct {
		name string
		row  gen.ListInstrumentsRow
		want *admininstrumentv1.Instrument
	}{
		{
			name: "named",
			row:  gen.ListInstrumentsRow{ID: id, AssetClass: types.AssetClassStock, Name: "VOD", NameType: "MIC_TICKER", NameDomain: "XLON"},
			want: &admininstrumentv1.Instrument{
				Id: id.String(), AssetClass: typev1.AssetClass_ASSET_CLASS_STOCK,
				Name: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: "XLON", Value: "VOD"},
			},
		},
		{
			name: "without a name",
			row:  gen.ListInstrumentsRow{ID: id, AssetClass: types.AssetClassStock},
			want: &admininstrumentv1.Instrument{Id: id.String(), AssetClass: typev1.AssetClass_ASSET_CLASS_STOCK},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, ProtoInstrument(tc.row), protocmp.Transform()); diff != "" {
				t.Errorf("ProtoInstrument mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestProtoListing(t *testing.T) {
	id := uuid.New()
	row := gen.Listing{ID: id, Currency: "GBP", PrimaryMic: ptr.To("XLON")}
	want := &admininstrumentv1.Listing{Id: id.String(), Currency: "GBP", PrimaryMic: "XLON"}
	if diff := cmp.Diff(want, ProtoListing(row), protocmp.Transform()); diff != "" {
		t.Errorf("ProtoListing mismatch (-want +got):\n%s", diff)
	}
}
