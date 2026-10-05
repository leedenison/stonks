package types_test

import (
	"testing"

	adminv1 "github.com/leedenison/stonks/proto/admin/v1"
	authv1 "github.com/leedenison/stonks/proto/auth/v1"
	runv1 "github.com/leedenison/stonks/proto/run/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

func TestFromProto(t *testing.T) {
	tests := []struct {
		name string
		in   typev1.AssetClass
		want gen.AssetClass
		ok   bool
	}{
		{name: "one word", in: typev1.AssetClass_ASSET_CLASS_CASH, want: gen.AssetClassCash, ok: true},
		{name: "two words", in: typev1.AssetClass_ASSET_CLASS_MUTUAL_FUND, want: gen.AssetClassMutualFund, ok: true},
		{name: "unspecified", in: typev1.AssetClass_ASSET_CLASS_UNSPECIFIED, want: "", ok: true},
		{name: "undefined", in: 99, want: "", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := types.FromProto[gen.AssetClass](tc.in)
			if got != tc.want || ok != tc.ok {
				t.Errorf("FromProto(%v) = %q, %v, want %q, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
	if got, ok := types.FromProto[types.IdentifierType](typev1.IdentifierType_IDENTIFIER_TYPE_OPENFIGI_SHARE_CLASS); got != types.IdentifierTypeOpenfigiShareClass || !ok {
		t.Errorf("FromProto(OPENFIGI_SHARE_CLASS) = %q, %v", got, ok)
	}
}

func TestToProto(t *testing.T) {
	tests := []struct {
		name string
		in   gen.RunState
		want runv1.RunState
	}{
		{name: "named", in: gen.RunStateInterrupted, want: runv1.RunState_RUN_STATE_INTERRUPTED},
		{name: "empty", in: "", want: runv1.RunState_RUN_STATE_UNSPECIFIED},
		{name: "unknown", in: "paused", want: runv1.RunState_RUN_STATE_UNSPECIFIED},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := types.ToProto[runv1.RunState](tc.in); got != tc.want {
				t.Errorf("ToProto(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
	if got := types.ToProto[typev1.Broker](gen.BrokerFidelityUk); got != typev1.Broker_BROKER_FIDELITY_UK {
		t.Errorf("ToProto(fidelity_uk) = %v", got)
	}
}

// crosses checks that every value of a database enum converts to its proto
// counterpart and back, and that the proto names no value the database lacks.
func crosses[T types.Enum, P types.ProtoEnum](t *testing.T, all []T) {
	t.Helper()
	var zero P
	desc := zero.Descriptor()
	for _, v := range all {
		p := types.ToProto[P](v)
		if p == zero {
			t.Errorf("ToProto(%q) = UNSPECIFIED, want a %s value", v, desc.Name())
			continue
		}
		if back, ok := types.FromProto[T](p); !ok || back != v {
			t.Errorf("FromProto(ToProto(%q)) = %q, %v, want it back", v, back, ok)
		}
	}
	if n := desc.Values().Len() - 1; n != len(all) {
		t.Errorf("%s names %d values beside UNSPECIFIED, the database %d", desc.Name(), n, len(all))
	}
}

// TestEnumsCross holds every database enum that crosses the API equal to
// its proto counterpart.
func TestEnumsCross(t *testing.T) {
	identifierTypes := make([]types.IdentifierType, 0, len(gen.AllIdentifierTypeValues()))
	for _, v := range gen.AllIdentifierTypeValues() {
		identifierTypes = append(identifierTypes, types.IdentifierType(v))
	}
	t.Run("asset class", func(t *testing.T) { crosses[gen.AssetClass, typev1.AssetClass](t, gen.AllAssetClassValues()) })
	t.Run("broker", func(t *testing.T) { crosses[gen.Broker, typev1.Broker](t, gen.AllBrokerValues()) })
	t.Run("identifier type", func(t *testing.T) { crosses[types.IdentifierType, typev1.IdentifierType](t, identifierTypes) })
	t.Run("resolution outcome", func(t *testing.T) {
		crosses[gen.ResolutionOutcome, typev1.ResolutionOutcome](t, gen.AllResolutionOutcomeValues())
	})
	t.Run("run kind", func(t *testing.T) { crosses[gen.RunKind, runv1.RunKind](t, gen.AllRunKindValues()) })
	t.Run("run trigger", func(t *testing.T) { crosses[gen.RunTrigger, runv1.RunTrigger](t, gen.AllRunTriggerValues()) })
	t.Run("run state", func(t *testing.T) { crosses[gen.RunState, runv1.RunState](t, gen.AllRunStateValues()) })
	t.Run("fetch outcome", func(t *testing.T) { crosses[gen.FetchOutcome, adminv1.FetchOutcome](t, gen.AllFetchOutcomeValues()) })
	t.Run("fetch kind", func(t *testing.T) { crosses[gen.FetchKind, adminv1.FetchKind](t, gen.AllFetchKindValues()) })
	t.Run("finding kind", func(t *testing.T) { crosses[gen.FindingKind, adminv1.FindingKind](t, gen.AllFindingKindValues()) })
	t.Run("drop step", func(t *testing.T) { crosses[gen.DropStep, adminv1.DropStep](t, gen.AllDropStepValues()) })
	t.Run("block scope", func(t *testing.T) { crosses[gen.BlockScope, adminv1.BlockScope](t, gen.AllBlockScopeValues()) })
	t.Run("user role", func(t *testing.T) { crosses[gen.UserRole, authv1.Role](t, gen.AllUserRoleValues()) })
}
