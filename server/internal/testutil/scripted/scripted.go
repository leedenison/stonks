// Package scripted is an identity integration whose every answer a test
// scripts.
package scripted

import (
	"context"
	"errors"

	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
)

// Identity is a market.Identity whose answers a test scripts.
type Identity struct {
	// Down fails every key of every request.
	Down bool
	// Limited marks every answer as from a provider that lists only some of
	// an instrument's listings.
	Limited bool
	// Responses maps each identifier sent to its answer. An identifier it
	// lacks is answered with an empty result.
	Responses map[types.Identifier]market.IdentityResult
}

var _ market.Identity = (*Identity)(nil)

// New returns an Identity with no responses scripted.
func New() *Identity {
	return &Identity{Responses: map[types.Identifier]market.IdentityResult{}}
}

// Classify reads every failure as temporary and about the identifier.
func (s *Identity) Classify(error) market.Failure {
	return market.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier}
}

// Limit leaves calls unpaced.
func (s *Identity) Limit() (rate.Limit, int) { return rate.Inf, 1 }

// Batch is the most keys one call carries.
func (s *Identity) Batch() int { return 10 }

// Endpoint is the address of a provider that is never called.
func (s *Identity) Endpoint() string { return "https://scripted.test" }

// Serves sends the first GUID the key states, or a ticker without its venue,
// as the OpenFIGI integration does.
func (s *Identity) Serves(k gen.StatedKey) (types.Identifier, error) {
	for _, id := range k.Identifiers {
		if market.IsGUID(id) || id.Type == types.IdentifierTypeMicTicker {
			return id, nil
		}
	}
	return types.Identifier{}, errors.New("no global identifier")
}

// Fetch answers each request from Responses.
func (s *Identity) Fetch(_ context.Context, reqs []market.Request[gen.StatedKey]) ([]market.Response[market.IdentityResult], error) {
	out := make([]market.Response[market.IdentityResult], len(reqs))
	for i, r := range reqs {
		if s.Down {
			out[i] = market.Response[market.IdentityResult]{Err: errors.New("the provider is down")}
			continue
		}
		v := s.Responses[r.Sent]
		v.Limited = s.Limited
		out[i] = market.Response[market.IdentityResult]{Value: v}
	}
	return out, nil
}
