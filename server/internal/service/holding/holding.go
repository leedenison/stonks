// Package holding serves stonks.holding.v1.
//
// Holdings are summed by the queries from the caller's own transactions
// through the caller's own keys, so another user's never contribute, and a
// caller with none is answered with empty lists rather than not found. A
// resolved key is summed into its instrument, and an unresolved one into the
// group it shares what it states with.
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
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

// Reader is the view of the holdings queries this package depends on.
type Reader interface {
	ListInstrumentHoldings(ctx context.Context, userID uuid.UUID) ([]gen.ListInstrumentHoldingsRow, error)
	ListHeldIdentifiers(ctx context.Context, userID uuid.UUID) ([]gen.Identifier, error)
	ListGroupHoldings(ctx context.Context, userID uuid.UUID) ([]gen.ListGroupHoldingsRow, error)
	ListHeldGroupKeys(ctx context.Context, userID uuid.UUID) ([]gen.ListHeldGroupKeysRow, error)
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

// instruments answers the holdings of the instruments the user's keys
// resolved to, each named by every identifier naming it.
func (s *Server) instruments(ctx context.Context, user uuid.UUID) ([]*holdingv1.InstrumentHolding, error) {
	rows, err := s.reader.ListInstrumentHoldings(ctx, user)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	idents, err := s.reader.ListHeldIdentifiers(ctx, user)
	if err != nil {
		return nil, err
	}
	named := make(map[uuid.UUID][]*typev1.Identifier, len(rows))
	for _, i := range idents {
		named[i.InstrumentID] = append(named[i.InstrumentID], toProto(i))
	}
	out := make([]*holdingv1.InstrumentHolding, 0, len(rows))
	for _, r := range rows {
		out = append(out, &holdingv1.InstrumentHolding{
			InstrumentId: r.InstrumentID.String(),
			AssetClass:   db.ToProto[typev1.AssetClass](r.AssetClass),
			Identifiers:  named[r.InstrumentID],
			Quantity:     r.Quantity.String(),
		})
	}
	return out, nil
}

// groups answers the holdings of the user's groups, each carrying what its
// keys state: the classes, the identifiers and the descriptions, once each.
func (s *Server) groups(ctx context.Context, user uuid.UUID) ([]*holdingv1.GroupHolding, error) {
	rows, err := s.reader.ListGroupHoldings(ctx, user)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	keys, err := s.reader.ListHeldGroupKeys(ctx, user)
	if err != nil {
		return nil, err
	}
	stated := make(map[uuid.UUID]*holdingv1.GroupHolding, len(rows))
	for _, k := range keys {
		if k.StatedKey.GroupID == nil {
			continue
		}
		h, ok := stated[*k.StatedKey.GroupID]
		if !ok {
			h = &holdingv1.GroupHolding{}
			stated[*k.StatedKey.GroupID] = h
		}
		fold(h, k)
	}
	out := make([]*holdingv1.GroupHolding, 0, len(rows))
	for _, r := range rows {
		h := stated[r.GroupID]
		if h == nil {
			h = &holdingv1.GroupHolding{}
		}
		h.GroupId = r.GroupID.String()
		h.Quantity = r.Quantity.String()
		slices.Sort(h.AssetClasses)
		out = append(out, h)
	}
	return out, nil
}

// fold adds what k states to h, leaving out what another key of the group
// has already stated.
func fold(h *holdingv1.GroupHolding, k gen.ListHeldGroupKeysRow) {
	if k.StatedKey.AssetClass != nil {
		class := db.ToProto[typev1.AssetClass](*k.StatedKey.AssetClass)
		if !slices.Contains(h.AssetClasses, class) {
			h.AssetClasses = append(h.AssetClasses, class)
		}
	}
	for _, i := range k.StatedKey.Identifiers {
		id := &typev1.Identifier{Type: db.ToProto[typev1.IdentifierType](i.Type), Domain: i.Domain, Value: i.Value}
		if !slices.ContainsFunc(h.Identifiers, func(o *typev1.Identifier) bool {
			return o.GetType() == id.GetType() && o.GetDomain() == id.GetDomain() && o.GetValue() == id.GetValue()
		}) {
			h.Identifiers = append(h.Identifiers, id)
		}
	}
	if k.StatedKey.Description != nil {
		d := &holdingv1.Description{Broker: db.ToProto[typev1.Broker](k.Broker), Text: *k.StatedKey.Description}
		if !slices.ContainsFunc(h.Descriptions, func(o *holdingv1.Description) bool {
			return o.GetBroker() == d.GetBroker() && o.GetText() == d.GetText()
		}) {
			h.Descriptions = append(h.Descriptions, d)
		}
	}
}

func toProto(i gen.Identifier) *typev1.Identifier {
	return &typev1.Identifier{Type: db.ToProto[typev1.IdentifierType](i.Type), Domain: i.Domain, Value: i.Value}
}
