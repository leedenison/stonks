// Package instrument serves stonks.instrument.v1.
package instrument

import (
	"context"

	"connectrpc.com/connect"

	instrumentv1 "github.com/leedenison/stonks/proto/instrument/v1"
	"github.com/leedenison/stonks/proto/instrument/v1/instrumentv1connect"
)

// Server implements InstrumentService.
type Server struct{}

var _ instrumentv1connect.InstrumentServiceHandler = (*Server)(nil)

// New returns a Server.
func New() *Server {
	return &Server{}
}

// ListInstruments returns no instruments.
func (*Server) ListInstruments(context.Context, *connect.Request[instrumentv1.ListInstrumentsRequest]) (*connect.Response[instrumentv1.ListInstrumentsResponse], error) {
	return connect.NewResponse(&instrumentv1.ListInstrumentsResponse{}), nil
}
