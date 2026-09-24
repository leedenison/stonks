// Package datasource defines the interface for external datasources.
// Datasources fetch one kind of data about a set of keys from one
// external provider, and records what was requested and what came back.
// A fetch is a child run of the work that needed the data.
// See [run.go](../run/run.go).
//
// The registry is built at startup from the datasources table.
//
// Integrations interpret datasource errors and report them.  Fetches that
// fail durably are blocked.
package datasource

import (
	"time"

	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db/gen"
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
}

// Config for a datasource.
type Config struct {
	Name       string
	Credential string
	Endpoint   string
}

// Factory builds an integration from its configuration.
type Factory func(Config) (Integration, error)

// Integrations returns the factory of every integration.
func Integrations() map[string]Factory { return map[string]Factory{} }
