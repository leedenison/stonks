// Package scripted holds integrations whose every answer a test scripts: one
// for identity and one for identifier events.
package scripted

import (
	"context"
	"errors"

	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
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
	return market.Failure{Temporary: true, Scope: market.ScopeKey}
}

// Limit leaves calls unpaced.
func (s *Identity) Limit() (rate.Limit, int) { return rate.Inf, 1 }

// Batch is the most keys one call carries.
func (s *Identity) Batch() int { return 10 }

// Endpoint is the address of a provider that is never called.
func (s *Identity) Endpoint() string { return "https://scripted.test" }

// Serves sends the first global identifier the key states, or a ticker without
// its venue.
func (s *Identity) Serves(k gen.StatedKey) (types.Identifier, error) {
	for _, id := range k.Identifiers {
		if id.IsGlobal() || id.Type == types.IdentifierTypeMicTicker {
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

// IdentifierEvents is a market.IdentifierEvents whose answers a test scripts.
type IdentifierEvents struct {
	// Down fails every request.
	Down bool
	// Responses maps each identifier sent to its answer. An identifier it
	// lacks is answered with an empty result.
	Responses map[types.Identifier]market.IdentifierEventResult
	// Sent is every request the server received, in order.
	Sent []market.IdentifierEventRequest
}

var _ market.IdentifierEvents = (*IdentifierEvents)(nil)

// NewIdentifierEvents returns an IdentifierEvents with no responses scripted.
func NewIdentifierEvents() *IdentifierEvents {
	return &IdentifierEvents{Responses: map[types.Identifier]market.IdentifierEventResult{}}
}

// Classify reads every failure as temporary and about the identifier sent.
func (s *IdentifierEvents) Classify(error) market.Failure {
	return market.Failure{Temporary: true, Scope: market.ScopeKey}
}

// Limit leaves calls unpaced.
func (s *IdentifierEvents) Limit() (rate.Limit, int) { return rate.Inf, 1 }

// Batch is the most requests one call carries.
func (s *IdentifierEvents) Batch() int { return 10 }

// Endpoint is the address of a provider that is never called.
func (s *IdentifierEvents) Endpoint() string { return "https://scripted.test" }

// Events is the server itself.
func (s *IdentifierEvents) Events() market.Server[market.IdentifierEventRequest, market.IdentifierEventResult] {
	return s
}

// Serves sends the identifier the request names.
func (s *IdentifierEvents) Serves(r market.IdentifierEventRequest) (types.Identifier, error) {
	return r.Identifier, nil
}

// Fetch answers each request from Responses.
func (s *IdentifierEvents) Fetch(_ context.Context, reqs []market.Request[market.IdentifierEventRequest]) ([]market.Response[market.IdentifierEventResult], error) {
	out := make([]market.Response[market.IdentifierEventResult], len(reqs))
	for i, r := range reqs {
		s.Sent = append(s.Sent, r.Value)
		if s.Down {
			out[i] = market.Response[market.IdentifierEventResult]{Err: errors.New("the provider is down")}
			continue
		}
		out[i] = market.Response[market.IdentifierEventResult]{Value: s.Responses[r.Sent]}
	}
	return out, nil
}
