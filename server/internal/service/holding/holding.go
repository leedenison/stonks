// Package holding serves stonks.holding.v1.
//
// Holdings are summed by the queries from the caller's own transactions
// through the caller's own keys, so another user's never contribute, and a
// caller with none is answered with empty lists rather than not found. A
// resolved key is summed into its instrument, and an unresolved one into the
// group sharing its stated data.
package holding

import (
	"context"
	"slices"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	holdingv1 "github.com/leedenison/stonks/proto/holding/v1"
	"github.com/leedenison/stonks/proto/holding/v1/holdingv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// Reader is this package's view of the holdings queries.
type Reader interface {
	ListInstrumentHoldings(ctx context.Context, userID uuid.UUID) ([]gen.ListInstrumentHoldingsRow, error)
	ListIdentifiersOf(ctx context.Context, ids []uuid.UUID) ([]gen.Identifier, error)
	ListGroupHoldings(ctx context.Context, userID uuid.UUID) ([]gen.ListGroupHoldingsRow, error)
	ListStatedKeysOfGroups(ctx context.Context, arg gen.ListStatedKeysOfGroupsParams) ([]gen.StatedKey, error)
}

var _ Reader = (*gen.Queries)(nil)

//go:generate go tool mockgen -source=holding.go -destination=mock/holding_mock.go -package=mock

// Server implements HoldingService.
type Server struct {
	reader Reader
}

var _ holdingv1connect.HoldingServiceHandler = (*Server)(nil)

// New returns a Server.
func New(reader Reader) *Server {
	return &Server{reader: reader}
}

// ListHoldings lists the caller's holdings of both kinds.
func (s *Server) ListHoldings(ctx context.Context, _ *connect.Request[holdingv1.ListHoldingsRequest]) (*connect.Response[holdingv1.ListHoldingsResponse], error) {
	p, err := auth.User(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	res := &holdingv1.ListHoldingsResponse{}
	if res.Instruments, err = s.instruments(ctx, p.User.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if res.Groups, err = s.groups(ctx, p.User.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(res), nil
}

// instruments answers the holdings of the instruments resolved from the
// user's keys, each named by every identifier naming it.
func (s *Server) instruments(ctx context.Context, user uuid.UUID) ([]*holdingv1.InstrumentHolding, error) {
	rows, err := s.reader.ListInstrumentHoldings(ctx, user)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.InstrumentID
	}
	idents, err := s.reader.ListIdentifiersOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	named := make(map[uuid.UUID][]*typev1.Identifier, len(rows))
	for _, i := range idents {
		named[i.InstrumentID] = append(named[i.InstrumentID], to.Identifier(i).ToProto())
	}
	out := make([]*holdingv1.InstrumentHolding, 0, len(rows))
	for _, r := range rows {
		out = append(out, &holdingv1.InstrumentHolding{
			InstrumentId: r.InstrumentID.String(),
			AssetClass:   types.ToProto[typev1.AssetClass](r.AssetClass),
			Identifiers:  named[r.InstrumentID],
			Quantity:     r.Quantity.String(),
		})
	}
	return out, nil
}

// groups answers the holdings of the user's groups, each carrying what its
// keys state: the classes and the identifiers, once each.
func (s *Server) groups(ctx context.Context, user uuid.UUID) ([]*holdingv1.GroupHolding, error) {
	rows, err := s.reader.ListGroupHoldings(ctx, user)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.GroupID
	}
	keys, err := s.reader.ListStatedKeysOfGroups(ctx, gen.ListStatedKeysOfGroupsParams{UserID: user, GroupIds: ids})
	if err != nil {
		return nil, err
	}
	stated := make(map[uuid.UUID]*stating, len(rows))
	for _, k := range keys {
		st, ok := stated[*k.GroupID]
		if !ok {
			st = &stating{h: &holdingv1.GroupHolding{}, classes: map[gen.AssetClass]bool{}, ids: map[types.Identifier]bool{}}
			stated[*k.GroupID] = st
		}
		st.fold(k)
	}
	out := make([]*holdingv1.GroupHolding, 0, len(rows))
	for _, r := range rows {
		h := &holdingv1.GroupHolding{}
		if st := stated[r.GroupID]; st != nil {
			h = st.h
		}
		h.GroupId = r.GroupID.String()
		h.Quantity = r.Quantity.String()
		slices.Sort(h.AssetClasses)
		out = append(out, h)
	}
	return out, nil
}

// stating is what the keys of one group state, each class and identifier
// once.
type stating struct {
	h       *holdingv1.GroupHolding
	classes map[gen.AssetClass]bool
	ids     map[types.Identifier]bool
}

// fold adds the asset class and identifiers of k that the group has not yet
// stated.
func (st *stating) fold(k gen.StatedKey) {
	if k.AssetClass != nil && !st.classes[*k.AssetClass] {
		st.classes[*k.AssetClass] = true
		st.h.AssetClasses = append(st.h.AssetClasses, types.ToProto[typev1.AssetClass](*k.AssetClass))
	}
	for _, id := range k.Identifiers {
		if !st.ids[id] {
			st.ids[id] = true
			st.h.Identifiers = append(st.h.Identifiers, id.ToProto())
		}
	}
}
