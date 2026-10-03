package openfigi

import "github.com/leedenison/stonks/server/internal/mic"

// venue returns the operating MIC an exchange code names. It reports false for
// a code spanning several venues, such as a country's composite, and for a
// MIC mics does not hold.
func venue(code string, mics mic.Table) (string, bool) {
	m := codes[code]
	if len(m) != 1 {
		return "", false
	}
	return mics.Operating(m[0])
}
