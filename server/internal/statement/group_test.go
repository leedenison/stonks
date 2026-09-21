package statement

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
)

// id returns the nth key id, ordered so that a smaller n is the earlier key.
func id(n int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-7000-8000-%012d", n))
}

// keyRow states what one key of broker says about an instrument.
func keyRow(n int, broker gen.Broker, description string, ids ...types.StatedIdentifier) gen.ListGroupableKeysRow {
	k := gen.StatedKey{ID: id(n), Identifiers: ids}
	if description != "" {
		k.Description = &description
	}
	return gen.ListGroupableKeysRow{StatedKey: k, Broker: broker}
}

func stated(t gen.IdentifierType, value string, domain *string) types.StatedIdentifier {
	return types.StatedIdentifier{Type: string(t), Domain: domain, Value: value}
}

func TestGroups(t *testing.T) {
	isin := func(v string) types.StatedIdentifier { return stated(gen.IdentifierTypeIsin, v, nil) }
	ticker := func(v string, domain *string) types.StatedIdentifier {
		return stated(gen.IdentifierTypeMicTicker, v, domain)
	}
	tests := []struct {
		name string
		keys []gen.ListGroupableKeysRow
		want map[int]int
	}{
		{
			name: "a shared identifier joins two brokers",
			keys: []gen.ListGroupableKeysRow{
				keyRow(1, gen.BrokerIbkr, "ACME CORP", isin("US0000000001")),
				keyRow(2, gen.BrokerSchwab, "ACME CORPORATION", isin("US0000000001")),
			},
			want: map[int]int{1: 1, 2: 1},
		},
		{
			name: "a shared description joins within one broker",
			keys: []gen.ListGroupableKeysRow{
				keyRow(1, gen.BrokerIbkr, "ACME CORP"),
				keyRow(2, gen.BrokerIbkr, "ACME CORP"),
			},
			want: map[int]int{1: 1, 2: 1},
		},
		{
			name: "one description at two brokers stays apart",
			keys: []gen.ListGroupableKeysRow{
				keyRow(1, gen.BrokerIbkr, "ACME CORP"),
				keyRow(2, gen.BrokerSchwab, "ACME CORP"),
			},
			want: map[int]int{1: 1, 2: 2},
		},
		{
			name: "a ticker without its venue names nothing",
			keys: []gen.ListGroupableKeysRow{
				keyRow(1, gen.BrokerSchwab, "ACME CORP", ticker("ACME", nil)),
				keyRow(2, gen.BrokerFidelityUk, "ACME PLC", ticker("ACME", nil)),
			},
			want: map[int]int{1: 1, 2: 2},
		},
		{
			name: "a ticker at one venue joins",
			keys: []gen.ListGroupableKeysRow{
				keyRow(1, gen.BrokerSchwab, "ACME CORP", ticker("ACME", ptr.To("XNAS"))),
				keyRow(2, gen.BrokerFidelityUk, "ACME PLC", ticker("ACME", ptr.To("XNAS"))),
			},
			want: map[int]int{1: 1, 2: 1},
		},
		{
			name: "a ticker at two venues stays apart",
			keys: []gen.ListGroupableKeysRow{
				keyRow(1, gen.BrokerSchwab, "ACME CORP", ticker("ACME", ptr.To("XNAS"))),
				keyRow(2, gen.BrokerFidelityUk, "ACME PLC", ticker("ACME", ptr.To("XLON"))),
			},
			want: map[int]int{1: 1, 2: 2},
		},
		{
			name: "one value under two types stays apart",
			keys: []gen.ListGroupableKeysRow{
				keyRow(1, gen.BrokerIbkr, "ACME CORP", isin("US0000000001")),
				keyRow(2, gen.BrokerSchwab, "ACME PLC", stated(gen.IdentifierTypeCusip, "US0000000001", nil)),
			},
			want: map[int]int{1: 1, 2: 2},
		},
		{
			name: "the chain runs through the key that links it",
			keys: []gen.ListGroupableKeysRow{
				keyRow(1, gen.BrokerIbkr, "ACME CORP", isin("US0000000001")),
				keyRow(2, gen.BrokerIbkr, "ACME CORP", isin("US0000000002")),
				keyRow(3, gen.BrokerSchwab, "ACME PLC", isin("US0000000002")),
			},
			want: map[int]int{1: 1, 2: 1, 3: 1},
		},
		{
			name: "the earliest key names the group whichever order they arrive",
			keys: []gen.ListGroupableKeysRow{
				keyRow(3, gen.BrokerIbkr, "ACME CORP", isin("US0000000001")),
				keyRow(1, gen.BrokerSchwab, "ACME PLC", isin("US0000000001")),
			},
			want: map[int]int{3: 1, 1: 1},
		},
		{
			name: "a key sharing nothing is its own group",
			keys: []gen.ListGroupableKeysRow{keyRow(1, gen.BrokerIbkr, "ACME CORP")},
			want: map[int]int{1: 1},
		},
		{
			name: "a key stating nothing shares nothing",
			keys: []gen.ListGroupableKeysRow{
				{StatedKey: gen.StatedKey{ID: id(1)}, Broker: gen.BrokerIbkr},
				{StatedKey: gen.StatedKey{ID: id(2)}, Broker: gen.BrokerIbkr},
			},
			want: map[int]int{1: 1, 2: 2},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := make(map[uuid.UUID]uuid.UUID, len(tc.want))
			for k, v := range tc.want {
				want[id(k)] = id(v)
			}
			if diff := cmp.Diff(want, groups(tc.keys)); diff != "" {
				t.Errorf("groups mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
