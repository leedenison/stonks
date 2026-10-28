package tickervalidity

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
)

// TestFacts checks that only the identifier event source of the market keeps
// its edges and its assertion coverage, and that an assertion with no
// datasource is an identifier attestation.
func TestFacts(t *testing.T) {
	asserted := func(datasource *string, eventSource bool) gen.ListIdentifierAssertionsRow {
		return gen.ListIdentifierAssertionsRow{
			IdentifierAssertion: gen.IdentifierAssertion{AssertedFrom: d(5), AssertedBefore: d(6), InstrID: &a, EdgeFrom: true, EdgeBefore: true},
			Datasource:          datasource, Precedence: ptr.To(int32(1)), IsEventSource: eventSource,
		}
	}
	covered := func(datasource string, eventSource bool) gen.ListIdentifierAssertionCoverageRow {
		return gen.ListIdentifierAssertionCoverageRow{CoveredFrom: d(1), CoveredBefore: d(20), Datasource: datasource, Precedence: 1, IsEventSource: eventSource}
	}
	rows := Rows{
		Assertions: []gen.ListIdentifierAssertionsRow{
			asserted(ptr.To("alpha"), true), asserted(ptr.To("beta"), false), asserted(nil, false),
		},
		AssertionCoverage: []gen.ListIdentifierAssertionCoverageRow{covered("alpha", true), covered("beta", false)},
	}
	f := rows.facts()
	type got struct {
		Datasource           string
		Standing             standing
		EdgeFrom, EdgeBefore bool
	}
	var gotAssertions []got
	for _, x := range f.Assertions {
		gotAssertions = append(gotAssertions, got{x.Source.Datasource, x.Source.standing, x.EdgeFrom, x.EdgeBefore})
	}
	want := []got{{"alpha", eventSource, true, true}, {"beta", other, false, false}, {"", attested, false, false}}
	if diff := cmp.Diff(want, gotAssertions); diff != "" {
		t.Errorf("assertions mismatch (-want +got):\n%s", diff)
	}
	if len(f.AssertionCoverage) != 1 || f.AssertionCoverage[0].Source.Datasource != "alpha" || f.AssertionCoverage[0].Source.standing != eventSource {
		t.Errorf("assertion coverage = %+v, want only alpha's, as the identifier event source", f.AssertionCoverage)
	}
}
