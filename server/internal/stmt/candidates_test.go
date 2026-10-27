package stmt

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

// syncs has the runner execute a synchronous run inline and record its row.
func (f *fixture) syncs() *gen.Run {
	var row gen.Run
	f.runs.EXPECT().Sync(gomock.Any(), userID, gen.RunKindResolveStmtKeys, gen.RunTriggerUser, gomock.Any()).DoAndReturn(func(ctx context.Context, user uuid.UUID, kind gen.RunKind, trigger gen.RunTrigger, work run.Work) (gen.Run, error) {
		row = gen.Run{ID: db.NewID(), UserID: &user, Kind: kind, Trigger: trigger}
		return row, work(ctx, row)
	}).AnyTimes()
	return &row
}

func TestCandidates(t *testing.T) {
	key := gen.StatedKey{ID: db.NewID(), UserID: userID, Identifiers: []types.Identifier{{Type: types.IdentifierTypeMicTicker, Value: "INTC"}}}
	offer := resolve.Offer{Candidates: []resolve.Candidate{{Datasource: "alpha", Strongest: isinID("US0000000001")}}}
	boom := errors.New("boom")
	t.Run("lists the candidates as a synchronous run", func(t *testing.T) {
		f := newFixture(t)
		row := f.syncs()
		f.store.EXPECT().GetStatedKey(gomock.Any(), gen.GetStatedKeyParams{ID: key.ID, UserID: userID}).Return(key, nil)
		f.resolver.EXPECT().Candidates(gomock.Any(), gomock.Any(), key).DoAndReturn(func(_ context.Context, res gen.Run, _ gen.StatedKey) (resolve.Offer, error) {
			if res.ID != row.ID {
				t.Errorf("the resolver ran under %s, want the synchronous run %s", res.ID, row.ID)
			}
			return offer, nil
		})
		got, err := f.svc.Candidates(context.Background(), userID, key.ID)
		if err != nil {
			t.Fatalf("Candidates() error = %v", err)
		}
		if diff := cmp.Diff(offer, got); diff != "" {
			t.Errorf("Candidates() mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("a key of another user is not found", func(t *testing.T) {
		f := newFixture(t)
		f.store.EXPECT().GetStatedKey(gomock.Any(), gomock.Any()).Return(gen.StatedKey{}, db.ErrNotFound)
		if _, err := f.svc.Candidates(context.Background(), userID, key.ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("Candidates() error = %v, want ErrNotFound", err)
		}
	})
	t.Run("a key with an association is refused", func(t *testing.T) {
		f := newFixture(t)
		associated := key
		associated.InstrID = &userID
		f.store.EXPECT().GetStatedKey(gomock.Any(), gomock.Any()).Return(associated, nil)
		if _, err := f.svc.Candidates(context.Background(), userID, key.ID); !errors.Is(err, ErrAssociated) {
			t.Errorf("Candidates() error = %v, want ErrAssociated", err)
		}
	})
	t.Run("a dated key is refused", func(t *testing.T) {
		f := newFixture(t)
		dated := key
		dated.ViaTickerID, dated.ViaMarket, dated.ViaVenue = ptr.To(uuid.New()), ptr.To("US"), ptr.To("XNAS")
		f.store.EXPECT().GetStatedKey(gomock.Any(), gomock.Any()).Return(dated, nil)
		if _, err := f.svc.Candidates(context.Background(), userID, key.ID); !errors.Is(err, ErrAssociated) {
			t.Errorf("Candidates() error = %v, want ErrAssociated", err)
		}
	})
	t.Run("the resolver's error fails the run and is returned", func(t *testing.T) {
		f := newFixture(t)
		f.syncs()
		f.store.EXPECT().GetStatedKey(gomock.Any(), gomock.Any()).Return(key, nil)
		f.resolver.EXPECT().Candidates(gomock.Any(), gomock.Any(), key).Return(resolve.Offer{}, boom)
		if _, err := f.svc.Candidates(context.Background(), userID, key.ID); !errors.Is(err, boom) {
			t.Errorf("Candidates() error = %v, want %v", err, boom)
		}
	})
}

func TestConfirm(t *testing.T) {
	key := gen.StatedKey{ID: db.NewID(), UserID: userID, Identifiers: []types.Identifier{{Type: types.IdentifierTypeMicTicker, Value: "INTC"}}}
	pick := resolve.Pick{Datasource: "alpha", Identifier: isinID("US0000000001")}
	t.Run("confirms as a synchronous run and replays its new facts", func(t *testing.T) {
		f := newFixture(t)
		row := f.syncs()
		written := []resolve.WrittenIdentifierFacts{{Run: gen.Run{ID: db.NewID()}, Tickers: []types.MarketTicker{{Market: "US", Value: "INTC"}}}}
		f.store.EXPECT().GetStatedKey(gomock.Any(), gen.GetStatedKeyParams{ID: key.ID, UserID: userID}).Return(key, nil)
		f.resolver.EXPECT().Confirm(gomock.Any(), gomock.Any(), key, pick).DoAndReturn(func(_ context.Context, res gen.Run, _ gen.StatedKey, _ resolve.Pick) (gen.ResolutionKey, []resolve.WrittenIdentifierFacts, error) {
			return gen.ResolutionKey{RunID: res.ID, StatedKeyID: key.ID, Outcome: gen.ResolutionOutcomeMatched}, written, nil
		})
		got, err := f.svc.Confirm(context.Background(), userID, key.ID, pick)
		if err != nil {
			t.Fatalf("Confirm() error = %v, want the confirmation to stand", err)
		}
		if got.RunID != row.ID || got.Outcome != gen.ResolutionOutcomeMatched {
			t.Errorf("Confirm() = %+v, want matched under the synchronous run %s", got, row.ID)
		}
		if len(f.replayed) != 1 || f.replayed[0].except.ID != row.ID || !cmp.Equal(f.replayed[0].written, written) {
			t.Errorf("replays = %+v, want one beneath the confirmation's run with its facts", f.replayed)
		}
	})
	t.Run("a key with an association is refused", func(t *testing.T) {
		f := newFixture(t)
		associated := key
		associated.InstrID = &userID
		f.store.EXPECT().GetStatedKey(gomock.Any(), gomock.Any()).Return(associated, nil)
		if _, err := f.svc.Confirm(context.Background(), userID, key.ID, pick); !errors.Is(err, ErrAssociated) {
			t.Errorf("Confirm() error = %v, want ErrAssociated", err)
		}
	})
	t.Run("a pick the answer lacks is refused", func(t *testing.T) {
		f := newFixture(t)
		f.syncs()
		f.store.EXPECT().GetStatedKey(gomock.Any(), gomock.Any()).Return(key, nil)
		f.resolver.EXPECT().Confirm(gomock.Any(), gomock.Any(), key, pick).Return(gen.ResolutionKey{}, nil, resolve.ErrNoCandidate)
		if _, err := f.svc.Confirm(context.Background(), userID, key.ID, pick); !errors.Is(err, resolve.ErrNoCandidate) {
			t.Errorf("Confirm() error = %v, want ErrNoCandidate", err)
		}
	})
}

func isinID(value string) types.Identifier {
	return types.Identifier{Type: types.IdentifierTypeIsin, Value: value}
}
