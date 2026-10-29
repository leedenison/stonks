package db_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/types"
)

// locker records the names of each LockAdvisory call.
type locker struct{ calls [][]string }

func (l *locker) LockAdvisory(_ context.Context, names []string) error {
	l.calls = append(l.calls, names)
	return nil
}

// TestLockNames checks the name each helper builds, and that the names of
// one call reach the query sorted and once each.
func TestLockNames(t *testing.T) {
	ctx := context.Background()
	a := uuid.MustParse("00000000-0000-7000-8000-000000000001")
	b := uuid.MustParse("00000000-0000-7000-8000-000000000002")
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0000000001"}
	ticker := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "AAA"}
	cases := []struct {
		name string
		lock func(db.AdvisoryLocker) error
		want [][]string
	}{
		{
			name: "identifiers",
			lock: func(q db.AdvisoryLocker) error {
				return db.LockIdentifiers(ctx, q, []types.Identifier{ticker, isin, ticker})
			},
			want: [][]string{{"identifier:isin::US0000000001", "identifier:mic_ticker:XNAS:AAA"}},
		},
		{
			name: "instruments",
			lock: func(q db.AdvisoryLocker) error { return db.LockInstrs(ctx, q, []uuid.UUID{b, a, b}) },
			want: [][]string{{"instr:" + a.String(), "instr:" + b.String()}},
		},
		{
			name: "none",
			lock: func(q db.AdvisoryLocker) error { return db.LockInstrs(ctx, q, nil) },
		},
		{
			name: "a user's keys",
			lock: func(q db.AdvisoryLocker) error { return db.LockUserKeys(ctx, q, a) },
			want: [][]string{{"user_keys:" + a.String()}},
		},
		{
			name: "a corporate action answer",
			lock: func(q db.AdvisoryLocker) error { return db.LockCorpActAnswer(ctx, q, "massive", b) },
			want: [][]string{{"corpact:massive:" + b.String()}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var l locker
			if err := tc.lock(&l); err != nil {
				t.Fatalf("lock: %v", err)
			}
			if diff := cmp.Diff(tc.want, l.calls); diff != "" {
				t.Errorf("names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
