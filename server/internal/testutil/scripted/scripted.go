// Package scripted holds integrations whose every answer a test scripts: one
// each for identity, identifier events and corporate actions, and one serving
// identity and identifier events.
package scripted

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

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
	// Declines lists the venues whose tickers the server refuses to send.
	Declines []string
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
// its venue. It declines a key whose first usable identifier is a ticker on a
// venue in Declines.
func (s *Identity) Serves(k gen.StatedKey) (types.Identifier, error) {
	for _, id := range k.Identifiers {
		if id.Type == types.IdentifierTypeMicTicker && slices.Contains(s.Declines, id.Domain) {
			return types.Identifier{}, declined(id.Domain)
		}
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
	// Declines lists the venues whose tickers the server refuses to send.
	Declines []string
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

// Serves sends the identifier the request names, and declines a ticker on a
// venue in Declines.
func (s *IdentifierEvents) Serves(r market.IdentifierEventRequest) (types.Identifier, error) {
	if r.Identifier.Type == types.IdentifierTypeMicTicker && slices.Contains(s.Declines, r.Identifier.Domain) {
		return types.Identifier{}, declined(r.Identifier.Domain)
	}
	return r.Identifier, nil
}

// declined is the error a scripted server returns for a venue it declines.
func declined(venue string) error {
	return fmt.Errorf("venue '%s' rejected", venue)
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

// CorpActs is a market.CorpActs whose answers a test scripts.
type CorpActs struct {
	// Down fails every request.
	Down bool
	// Responses maps each identifier sent to its answer. An identifier it
	// lacks is answered with an empty result.
	Responses map[types.Identifier]market.CorpActResult
	// Sent is every request the server received, in order.
	Sent []market.CorpActRequest
	// Declines lists the venues whose tickers the server refuses to send.
	Declines []string
}

var _ market.CorpActs = (*CorpActs)(nil)

// NewCorpActs returns a CorpActs with no responses scripted.
func NewCorpActs() *CorpActs {
	return &CorpActs{Responses: map[types.Identifier]market.CorpActResult{}}
}

// Classify reads every failure as temporary and about the identifier sent.
func (s *CorpActs) Classify(error) market.Failure {
	return market.Failure{Temporary: true, Scope: market.ScopeKey}
}

// Limit leaves calls unpaced.
func (s *CorpActs) Limit() (rate.Limit, int) { return rate.Inf, 1 }

// Batch is the most requests one call carries.
func (s *CorpActs) Batch() int { return 10 }

// Endpoint is the address of a provider that is never called.
func (s *CorpActs) Endpoint() string { return "https://scripted.test" }

// CorpActs is the server itself.
func (s *CorpActs) CorpActs() market.Server[market.CorpActRequest, market.CorpActResult] {
	return s
}

// Serves sends the ticker the request names, and declines one on a venue in
// Declines.
func (s *CorpActs) Serves(r market.CorpActRequest) (types.Identifier, error) {
	if slices.Contains(s.Declines, r.Ticker.Domain) {
		return types.Identifier{}, declined(r.Ticker.Domain)
	}
	return r.Ticker, nil
}

// Fetch answers each request from Responses.
func (s *CorpActs) Fetch(_ context.Context, reqs []market.Request[market.CorpActRequest]) ([]market.Response[market.CorpActResult], error) {
	out := make([]market.Response[market.CorpActResult], len(reqs))
	for i, r := range reqs {
		s.Sent = append(s.Sent, r.Value)
		if s.Down {
			out[i] = market.Response[market.CorpActResult]{Err: errors.New("the provider is down")}
			continue
		}
		out[i] = market.Response[market.CorpActResult]{Value: s.Responses[r.Sent]}
	}
	return out, nil
}

// Datasource serves identity from Identity and identifier events from Facts,
// with a window that starts at Since.
type Datasource struct {
	*Identity
	Facts *IdentifierEvents
	Since time.Time
}

var (
	_ market.Identity         = (*Datasource)(nil)
	_ market.IdentifierEvents = (*Datasource)(nil)
	_ market.Windowed         = (*Datasource)(nil)
)

// Events serves the identifier events of Facts.
func (d *Datasource) Events() market.Server[market.IdentifierEventRequest, market.IdentifierEventResult] {
	return d.Facts
}

// Window starts at Since whatever the day.
func (d *Datasource) Window(time.Time) time.Time { return d.Since }
