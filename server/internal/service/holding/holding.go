// Package holding serves stonks.holding.v1.
//
// Holdings are summed by the query from the caller's own transactions, so
// another user's never contribute, and a caller with none is answered with
// an empty list rather than not found.
package holding

import (
	"context"

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
	ListHoldings(ctx context.Context, userID uuid.UUID) ([]gen.ListHoldingsRow, error)
	ListHeldIdentifiers(ctx context.Context, userID uuid.UUID) ([]gen.Identifier, error)
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

// ListHoldings lists the caller's holdings.
func (s *Server) ListHoldings(ctx context.Context, _ *connect.Request[holdingv1.ListHoldingsRequest]) (*connect.Response[holdingv1.ListHoldingsResponse], error) {
	p, err := auth.User(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	rows, err := s.reader.ListHoldings(ctx, p.User.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	res := &holdingv1.ListHoldingsResponse{}
	if len(rows) == 0 {
		return connect.NewResponse(res), nil
	}
	idents, err := s.reader.ListHeldIdentifiers(ctx, p.User.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	named := make(map[uuid.UUID][]*typev1.Identifier, len(rows))
	for _, i := range idents {
		named[i.InstrumentID] = append(named[i.InstrumentID], toProto(i))
	}
	for _, r := range rows {
		res.Holdings = append(res.Holdings, &holdingv1.Holding{
			InstrumentId: r.InstrumentID.String(),
			AssetClass:   db.ToProto[typev1.AssetClass](r.AssetClass),
			Identifiers:  named[r.InstrumentID],
			Quantity:     r.Quantity.String(),
		})
	}
	return connect.NewResponse(res), nil
}

func toProto(i gen.Identifier) *typev1.Identifier {
	return &typev1.Identifier{Type: db.ToProto[typev1.IdentifierType](i.Type), Domain: i.Domain, Value: i.Value}
}
