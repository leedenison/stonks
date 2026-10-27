package resolve

import (
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// unresolved is the outcome of a resolution no claim won:
// unavailable where a datasource failed or was blocked for it, unrecognised
// otherwise.
func unresolved(res *resolution) gen.ResolutionOutcome {
	for _, fk := range res.fetchKeys {
		switch fk.Outcome {
		case gen.FetchOutcomeBlocked, gen.FetchOutcomeFailedTemporary, gen.FetchOutcomeFailedPermanent:
			return gen.ResolutionOutcomeUnavailable
		}
	}
	return gen.ResolutionOutcomeUnrecognised
}

// unserved words each outcome other than served for a reason.
var unserved = map[gen.FetchOutcome]string{
	gen.FetchOutcomeNotServed:       "skipped",
	gen.FetchOutcomeBlocked:         "blocked",
	gen.FetchOutcomeFailedTemporary: "failed",
	gen.FetchOutcomeFailedPermanent: "failed",
}

// unservedReason says why fk served nothing: the datasource, the outcome
// and the reason recorded, as in "a: failed: 503".
func unservedReason(fk *fetchKey) string {
	s := fk.Source + ": " + unserved[fk.Outcome]
	if fk.Reason != "" {
		s += ": " + fk.Reason
	}
	return s
}

// reasons returns reason as a one-entry list, or nil if reason is empty.
func reasons(reason string) []string {
	if reason == "" {
		return nil
	}
	return []string{reason}
}

// summary says, for each datasource, what it served for res and what became
// of it.
func summary(res *resolution, verdict claimVerdict) []string {
	if len(res.fetchKeys) == 0 {
		return []string{"no datasource enabled"}
	}
	parts := make([]string, 0, len(res.fetchKeys))
	for _, fk := range res.fetchKeys {
		if fk.Outcome != gen.FetchOutcomeServed {
			parts = append(parts, unservedReason(fk))
			continue
		}
		s := fk.Source + ": " + plural(len(fk.Response.Listings), "listing")
		if n := verdict.counts[fk.Source]; n > 0 {
			s += " in " + sets(n)
		}
		if n := verdict.notNaming[fk.Source]; n > 0 {
			s += fmt.Sprintf(", %s not naming %s", sets(n), fk.SentID.Name())
		}
		dropped := 0
		for _, f := range verdict.findings {
			if f.Kind == gen.FindingKindDropped && f.FetchKeyID != nil && *f.FetchKeyID == fk.ID {
				dropped++
			}
		}
		if dropped > 0 {
			s += ", " + sets(dropped) + " dropped"
		}
		parts = append(parts, s)
	}
	return parts
}

// plural counts n of noun: "1 listing", "2 listings".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// sets counts n sets of identifiers: "1 set of identifiers", "2 sets of
// identifiers".
func sets(n int) string {
	if n == 1 {
		return "1 set of identifiers"
	}
	return fmt.Sprintf("%d sets of identifiers", n)
}
