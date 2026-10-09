// Package market fetches market data from external datasources.
//
// A fetch asks one datasource for one kind of data about a set of requests,
// and records what was requested and what came back. A fetch is a child run
// of the work that needed the data; see [run.go](../run/run.go).
//
// Each kind of data is a Kind, which names the request an integration
// receives and the response it gives.  An integration serves a kind by
// implementing its Server.
//
// An integration reads a provider's venue codes through a table generated
// from the provider's own list. Every venue a table names normalises to an
// operating MIC.
//
// An integration declines a key before asking when the provider does not
// cover the key's class or venue. Its conversion keeps only the classes and
// markets it serves.
//
// When a provider refuses every call for a while, as when a daily quota is
// spent, the framework pauses the datasource for the whole delay the provider
// gives. While it lasts, each of the datasource's keys that misses the cache
// fails temporarily without a call. The pause lives in the process.
//
// A datasource's answer is cached in Redis for the lifetime the service
// configures. An answer is a system fact, so the cache is shared by every
// user. A fetch reads the cache before it checks blocks, the pause and the
// rate, and calls the datasource only for a key that misses. Only a served
// answer is cached, an empty one included. A hit records a served fetch key
// with zero attempts. A cache error fails the fetch: Redis is a hard
// dependency of the request path already, and a failed run is replayed as
// any other. Answers are cached by request, so a reorder of the datasources
// leaves them in place.
//
// The cache key is the request as the integration sends it. Its format is a
// contract the e2e suite holds; see [cache.go](cache.go).
package market

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// Failure is how an integration reads a call that failed. RetryAfter is the
// delay the provider requested, and zero uses the framework's own schedule.
type Failure struct {
	Temporary  bool
	Scope      gen.BlockScope
	Reason     string
	RetryAfter time.Duration
}

// Integration adapts one datasource to the fetch framework. It serves the
// kinds of data whose interfaces it implements.
type Integration interface {
	// Classify reads an error a fetch returned. Only the integration knows the
	// provider's codes.
	Classify(err error) Failure
	// Limit is the provider's call rate and the burst it tolerates.
	Limit() (rate.Limit, int)
	// Batch is the maximum number of keys per batch.
	Batch() int
	// Endpoint is the address the integration calls. It is the provider's
	// default unless the datasource sets one.
	Endpoint() string
}

// Config for a datasource.
type Config struct {
	Name       string
	Credential string
	Endpoint   string
	// JSON holds the integration's own settings, from the row's config
	// column.
	JSON json.RawMessage
}

// Factory builds an integration from its configuration.
type Factory func(Config) (Integration, error)

// Request is one request as sent: the request, and the identifier Serves
// chose to carry it.
type Request[Q any] struct {
	Value Q
	Sent  types.Identifier
}

// Response is an integration's response to one request. Err is set when
// the provider failed for that request alone.
type Response[P any] struct {
	Value P
	Err   error
}

// Server is an integration's side of one kind of data.
type Server[Q, P any] interface {
	// Serves reports the identifier to send for req. An error means the
	// integration serves nothing for it and the error's text is the reason.
	//
	// The identifier sent may differ from every identifier req states, as when
	// a venue is normalised to its operating MIC.  A request states what its
	// source said, and the fetch records what was sent.
	Serves(req Q) (types.Identifier, error)
	// Fetch calls the provider and marshalls the results, one response per
	// request in the order given.
	//
	// Fetch is called with at most Batch requests, as one call.
	Fetch(ctx context.Context, reqs []Request[Q]) ([]Response[P], error)
}

// Parameterised is a server whose call takes parameters from the request
// beyond the identifier sent, such as a currency filter. Two requests sent
// under one identifier with equal params get one answer, so the params are
// part of the cache key.
type Parameterised[Q any] interface {
	Params(req Q) []string
}

// Kind is one kind of data: its label in the fetch records, and the server
// an entry has for it.
type Kind[Q, P any] struct {
	name gen.FetchKind
	// server returns nil where the entry does not serve the kind.
	server func(*Entry) Server[Q, P]
	// subject is what the fetch_keys row records a request as.
	subject func(Q) uuid.UUID
	// count is how many results a served response offered, which the
	// fetch_keys row records.
	count func(P) int
}
