// Package datasource defines the interface for external datasources.
// Datasources fetch one kind of data about a set of requests from one
// external provider, and records what was requested and what came back.
// A fetch is a child run of the work that needed the data.
// See [run.go](../run/run.go).
//
// Each kind of data is a Kind, which names the request an integration is
// asked and the response it gives.  An integration serves a kind by
// implementing its Server.
//
// The registry is built at startup from the datasources table.
//
// A fetch splits its requests into calls no larger than the integration's
// batch.  Each call waits on the datasource's rate limit and is retried
// separately.
//
// Integrations interpret datasource errors and report them.  Fetches that
// fail durably are blocked.
package datasource

import (
	"context"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// Failure is how an integration reads a call that failed. RetryAfter is the
// delay the provider asked for, and zero uses the framework's own schedule.
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
	// Limit is the rate the provider is called at, and the burst it tolerates.
	Limit() (rate.Limit, int)
	// Batch is the maximum number of keys per batch.
	Batch() int
}

// Config for a datasource.
type Config struct {
	Name       string
	Credential string
	Endpoint   string
}

// Factory builds an integration from its configuration.
type Factory func(Config) (Integration, error)

// FetchRequest is one request as sent: the request, and the identifier
// Serves chose to send it under.
type FetchRequest[Q any] struct {
	Value Q
	Sent  types.Identifier
}

// FetchResponse is an integration's response to one request. Err is set when
// the provider failed for that request alone.
type FetchResponse[P any] struct {
	Value P
	Err   error
}

// Server is an integration's side of one kind of data.
type Server[Q, P any] interface {
	// Serves reports the identifier to send for req. An error means the
	// integration serves nothing for it and the error's text is the reason.
	Serves(req Q) (types.Identifier, error)
	// Fetch calls the provider and marshalls the results, one response per
	// request in the order given.
	//
	// Fetch is called with at most Batch requests, as one call. Rate limits
	// are applied per datasource for the life of the process, so concurrent
	// resolutions share the quota.
	Fetch(ctx context.Context, reqs []FetchRequest[Q]) ([]FetchResponse[P], error)
}

// Kind is one kind of data: its label in the fetch records, and the server
// an entry has for it.
type Kind[Q, P any] struct {
	name gen.FetchKind
	// server is nil where the entry does not serve the kind.
	server func(*Entry) Server[Q, P]
	// subject is what the fetch_keys row records a request as.
	subject func(Q) uuid.UUID
}
