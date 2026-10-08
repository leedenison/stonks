package eodhd

import (
	"slices"

	"github.com/leedenison/stonks/server/internal/mic"
)

// usCode is EODHD's exchange code for every US venue.
const usCode = "US"

// usVenues maps the US venue names in EODHD's exchange symbol list to MICs,
// which EODHD does not publish for them. The list's other names, OTC markets
// and NMFQS (the quotation service of US mutual funds), are left out.
var usVenues = map[string]string{
	"NASDAQ":    "XNAS",
	"NYSE":      "XNYS",
	"NYSE ARCA": "ARCX",
	"NYSE MKT":  "XASE",
	"AMEX":      "XASE",
	"BATS":      "BATS",
}

// exchanges maps the operating MIC of each venue EODHD lists to the exchange
// codes that list it, sorted. Several codes reach one operating MIC where
// EODHD splits an exchange by segment, as KO and KQ split XKRX. A MIC that
// mics does not list is skipped.
func exchanges(mics mic.Table) map[string][]string {
	out := map[string][]string{}
	add := func(m, code string) {
		op, ok := mics.Operating(m)
		if ok && !slices.Contains(out[op], code) {
			out[op] = append(out[op], code)
		}
	}
	for code, ms := range codes {
		for _, m := range ms {
			add(m, code)
		}
	}
	for _, m := range usVenues {
		add(m, usCode)
	}
	for _, cs := range out {
		slices.Sort(cs)
	}
	return out
}
