// Package openfigi is the identity integration with the OpenFIGI mapping API.
//
// A stated key is sent under its strongest identifier OpenFIGI accepts, and
// every listing OpenFIGI maps it to is a candidate.  A candidate carries the
// share class and composite FIGIs, the ticker under OpenFIGI's exchange code,
// and the ticker under the operating MIC where the exchange code names exactly
// one venue.  A composite exchange code names a market rather than a venue, so
// its listings carry no MIC_TICKER.  OpenFIGI states no currency.
//
// A ticker is sent in OpenFIGI's form, with a share class separated by a
// slash, and a MIC_TICKER is answered with the class separated by a dot.
package openfigi

import (
	"github.com/leedenison/stonks/server/internal/mic"
)

// Client is the OpenFIGI integration.
type Client struct {
	mics mic.Table
}

// result is one listing of a mapping job's answer.
type result struct {
	Ticker         string  `json:"ticker"`
	ExchCode       string  `json:"exchCode"`
	SecurityType   string  `json:"securityType"`
	SecurityType2  string  `json:"securityType2"`
	MarketSector   string  `json:"marketSector"`
	ShareClassFIGI *string `json:"shareClassFIGI"`
	CompositeFIGI  *string `json:"compositeFIGI"`
}
