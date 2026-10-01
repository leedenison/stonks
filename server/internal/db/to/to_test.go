package to

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"

	authv1 "github.com/leedenison/stonks/proto/auth/v1"
	runv1 "github.com/leedenison/stonks/proto/run/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
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
}

func TestProtoRun(t *testing.T) {
	id, parent := uuid.New(), uuid.New()
	created := time.Date(2026, time.September, 24, 10, 0, 0, 0, time.UTC)
	started := created.Add(time.Second)
	row := gen.Run{ID: id, ParentID: &parent, Kind: gen.RunKindFetch, Trigger: gen.RunTriggerRun, State: gen.RunStateRunning, CreatedAt: created, StartedAt: &started}
	want := &runv1.Run{
		Id: id.String(), ParentId: ptr.To(parent.String()), Kind: runv1.RunKind_RUN_KIND_FETCH, Trigger: runv1.RunTrigger_RUN_TRIGGER_RUN,
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
	row := gen.StatedKey{AssetClass: ptr.To(gen.AssetClassStock), Currency: ptr.To("GBP"), Identifiers: []types.Identifier{{Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"}}}
	want := &typev1.StatedKey{
		AssetClass: typev1.AssetClass_ASSET_CLASS_STOCK, Currency: ptr.To("GBP"),
		Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB00BH4HKS39"}},
	}
	if diff := cmp.Diff(want, ProtoStatedKey(row), protocmp.Transform()); diff != "" {
		t.Errorf("ProtoStatedKey mismatch (-want +got):\n%s", diff)
	}
}
