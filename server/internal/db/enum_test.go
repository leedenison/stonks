package db_test

import (
	"testing"

	runv1 "github.com/leedenison/stonks/proto/run/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db"
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
			got, ok := db.FromProto[gen.AssetClass](tc.in)
			if got != tc.want || ok != tc.ok {
				t.Errorf("FromProto(%v) = %q, %v, want %q, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
	if got, ok := db.FromProto[types.IdentifierType](typev1.IdentifierType_IDENTIFIER_TYPE_OPENFIGI_SHARE_CLASS); got != types.IdentifierTypeOpenfigiShareClass || !ok {
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
			if got := db.ToProto[runv1.RunState](tc.in); got != tc.want {
				t.Errorf("ToProto(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
	if got := db.ToProto[typev1.Broker](gen.BrokerFidelityUk); got != typev1.Broker_BROKER_FIDELITY_UK {
		t.Errorf("ToProto(fidelity_uk) = %v", got)
	}
}
