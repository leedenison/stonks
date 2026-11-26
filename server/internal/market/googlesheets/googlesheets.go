// Package googlesheets is the Google Sheets datasource. An administrator
// uploads its prices through a command line tool. Its integration implements
// none of the fetch interfaces, so the framework never asks it for data.
package googlesheets

import (
	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/market"
)

// Name is the datasources row that selects this integration.
const Name = "google_sheets"

// endpoint is the address of the Sheets API, which the tool calls.
const endpoint = "https://sheets.googleapis.com"

type integration struct{ endpoint string }

// Factory returns a market.Factory for the datasource.
func Factory() market.Factory {
	return market.Factory{
		Endpoint: endpoint,
		New: func(cfg market.Config) (market.Integration, error) {
			ep, err := market.Endpoint(cfg, endpoint)
			if err != nil {
				return nil, err
			}
			return integration{endpoint: ep}, nil
		},
	}
}

// Classify, Limit and Batch satisfy market.Integration. The framework reads
// them only around a fetch, and this datasource has none.

func (integration) Classify(error) market.Failure { return market.Failure{Scope: market.ScopeKey} }

func (integration) Limit() (rate.Limit, int) { return rate.Inf, 1 }

func (integration) Batch() int { return 1 }

func (i integration) Endpoint() string { return i.endpoint }
