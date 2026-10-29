package types

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
)

func TestProtoPeriod(t *testing.T) {
	from := time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		from, before *time.Time
		want         *typev1.Period
	}{
		{name: "bounded", from: &from, before: &before, want: &typev1.Period{From: ptr.To("2024-02-01"), Before: ptr.To("2024-03-01")}},
		{name: "open start", before: &before, want: &typev1.Period{Before: ptr.To("2024-03-01")}},
		{name: "open end", from: &from, want: &typev1.Period{From: ptr.To("2024-02-01")}},
		{name: "unbounded", want: &typev1.Period{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, ProtoPeriod(tc.from, tc.before), protocmp.Transform()); diff != "" {
				t.Errorf("ProtoPeriod(%v, %v) mismatch (-want +got):\n%s", tc.from, tc.before, diff)
			}
		})
	}
	got := Period{From: from, Before: before}.ToProto()
	if diff := cmp.Diff(&typev1.Period{From: ptr.To("2024-02-01"), Before: ptr.To("2024-03-01")}, got, protocmp.Transform()); diff != "" {
		t.Errorf("Period.ToProto mismatch (-want +got):\n%s", diff)
	}
}
