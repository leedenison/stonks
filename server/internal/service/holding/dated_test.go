package holding

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/testing/protocmp"

	holdingv1 "github.com/leedenison/stonks/proto/holding/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
	"github.com/leedenison/stonks/server/internal/types"
)

// The ticker FOO at Nasdaq named instrument a, then b. Coverage runs from
// day 1 to day 20 of March 2026.
var (
	aID, bID        = uuid.MustParse("00000000-0000-0000-0000-000000000071"), uuid.MustParse("00000000-0000-0000-0000-000000000072")
	aUSD, aGBP      = uuid.MustParse("00000000-0000-0000-0000-000000000081"), uuid.MustParse("00000000-0000-0000-0000-000000000082")
	bUSD, bGBP      = uuid.MustParse("00000000-0000-0000-0000-000000000083"), uuid.MustParse("00000000-0000-0000-0000-000000000084")
	fixedKeyID      = uuid.MustParse("00000000-0000-0000-0000-000000000090")
	datedKeyID      = uuid.MustParse("00000000-0000-0000-0000-000000000091")
	foo             = types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "FOO"}
	aISIN           = types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0000000071"}
	protoFoo        = foo.ToProto()
	protoAISIN      = aISIN.ToProto()
	fooAssertions   = gen.ListIdentifierAssertionsParams{Markets: []string{"US"}, Values: []string{"FOO"}}
	fooEvents       = gen.ListIdentifierEventsParams{Markets: []string{"US"}, Values: []string{"FOO"}}
	fooCoverage     = gen.ListIdentifierCoverageParams{Markets: []string{"US"}, Values: []string{"FOO"}}
	coverage        = []gen.ListIdentifierCoverageRow{{Market: "US", Value: ptr.To("FOO"), CoveredFrom: day(1), CoveredBefore: day(20), Datasource: "massive"}}
	straddle        = []gen.ListIdentifierAssertionsRow{assertion(2, &aID), assertion(10, &bID)}
	retired         = []gen.ListIdentifierAssertionsRow{assertion(2, &aID), assertion(12, nil)}
	instrs          = []gen.Instr{{ID: aID, AssetClass: types.AssetClassStock}, {ID: bID, AssetClass: types.AssetClassStock}}
	fooOnB          = gen.Identifier{InstrID: bID, ListingID: &bUSD, Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "FOO"}
	idents          = []gen.Identifier{{InstrID: aID, Type: types.IdentifierTypeIsin, Value: "US0000000071"}, fooOnB}
	aListings       = []gen.ListListingNamesRow{{ListingID: aGBP, InstrID: aID, Currency: "GBP"}, {ListingID: aUSD, InstrID: aID, Currency: "USD"}}
	bListing        = gen.ListListingNamesRow{ListingID: bUSD, InstrID: bID, Currency: "USD", TickerDomain: ptr.To("XNAS"), TickerValue: ptr.To("FOO"), Venue: "Nasdaq"}
	listings        = append(append([]gen.ListListingNamesRow{}, aListings...), bListing)
	names           = []gen.InstrName{{InstrID: aID, Type: types.IdentifierTypeIsin, Value: "US0000000071"}, {InstrID: bID, Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "FOO"}}
	protoAListings  = []*holdingv1.HoldingListing{{Id: aGBP.String(), Currency: "GBP"}, {Id: aUSD.String(), Currency: "USD"}}
	protoBListings  = []*holdingv1.HoldingListing{{Id: bUSD.String(), Currency: "USD", Venue: "Nasdaq", Ticker: protoFoo}}
	protoAName      = &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "US0000000071"}
	protoIdentsOfA  = []*typev1.Identifier{protoAISIN}
	protoIdentsOfB  = []*typev1.Identifier{protoFoo}
	assetClassStock = typev1.AssetClass_ASSET_CLASS_STOCK
	errBoom         = errors.New("boom")
)

func day(n int) time.Time { return time.Date(2026, time.March, n, 0, 0, 0, 0, time.UTC) }

// assertion is Massive's assertion on day n that FOO names instr, or nothing
// where instr is nil.
func assertion(n int, instr *uuid.UUID) gen.ListIdentifierAssertionsRow {
	return gen.ListIdentifierAssertionsRow{
		IdentifierAssertion: gen.IdentifierAssertion{Type: types.IdentifierTypeMicTicker, Domain: "US", Value: "FOO", AssertedOn: day(n), InstrID: instr, FetchKeyID: ptr.To(uuid.New())},
		Datasource:          ptr.To("massive"), Precedence: ptr.To(int32(2)),
	}
}

// datedKey is the key dated through FOO, stating currency, with the sum of
// every transaction as its quantity.
func datedKey(currency *string, group *uuid.UUID, quantity string) gen.ListHoldingKeysRow {
	return gen.ListHoldingKeysRow{
		StatedKey: gen.StatedKey{
			ID: datedKeyID, Currency: currency, Identifiers: []types.Identifier{foo},
			ViaType: ptr.To(types.IdentifierTypeMicTicker), ViaDomain: ptr.To("XNAS"), ViaValue: ptr.To("FOO"),
			Arbiter: ptr.To(gen.ArbiterDatasource), GroupID: group,
		},
		Quantity: decimal.RequireFromString(quantity),
	}
}

func txn(n int, quantity string) gen.ListDatedTxnsRow {
	return gen.ListDatedTxnsRow{StatedKeyID: datedKeyID, AsAt: day(n), Quantity: decimal.RequireFromString(quantity)}
}

// datedHoldingKey is the dated key as a holding carries it: with the sum of
// the transactions the holding sums, on listing.
func datedHoldingKey(currency *string, listing uuid.UUID, quantity string) *holdingv1.HoldingKey {
	hk := &holdingv1.HoldingKey{StatedKey: &typev1.StatedKey{Currency: currency, Identifiers: []*typev1.Identifier{protoFoo}}, Quantity: quantity}
	if listing != uuid.Nil {
		hk.ListingId = listing.String()
	}
	return hk
}

func TestListHoldingsDated(t *testing.T) {
	usd, gbx := ptr.To("USD"), ptr.To("GBX")
	fixedKey := gen.ListHoldingKeysRow{
		StatedKey: gen.StatedKey{ID: fixedKeyID, Currency: usd, Identifiers: []types.Identifier{aISIN}, InstrID: &aID, ListingID: &aUSD, Arbiter: ptr.To(gen.ArbiterDatasource)},
		Quantity:  decimal.RequireFromString("10"),
	}
	fixedHoldingKey := &holdingv1.HoldingKey{StatedKey: &typev1.StatedKey{Currency: usd, Identifiers: []*typev1.Identifier{protoAISIN}}, ListingId: aUSD.String(), Quantity: "10"}
	holdingA := func(quantity string, keys ...*holdingv1.HoldingKey) *holdingv1.InstrumentHolding {
		return &holdingv1.InstrumentHolding{InstrumentId: aID.String(), AssetClass: assetClassStock, Name: protoAName, Identifiers: protoIdentsOfA, Quantity: quantity, Listings: protoAListings, Keys: keys}
	}
	holdingB := func(quantity string, listings []*holdingv1.HoldingListing, keys ...*holdingv1.HoldingKey) *holdingv1.InstrumentHolding {
		return &holdingv1.InstrumentHolding{InstrumentId: bID.String(), AssetClass: assetClassStock, Name: protoFoo, Identifiers: protoIdentsOfB, Quantity: quantity, Listings: listings, Keys: keys}
	}
	group := func(currency *string, quantity string) *holdingv1.GroupHolding {
		return &holdingv1.GroupHolding{GroupId: datedKeyID.String(), Quantity: quantity, Identifiers: []*typev1.Identifier{protoFoo}, Keys: []*holdingv1.HoldingKey{datedHoldingKey(currency, uuid.Nil, quantity)}}
	}
	withBGBP := append(append([]gen.ListListingNamesRow{}, listings...), gen.ListListingNamesRow{ListingID: bGBP, InstrID: bID, Currency: "GBP"})
	protoBListingsWithGBP := append(append([]*holdingv1.HoldingListing{}, protoBListings...), &holdingv1.HoldingListing{Id: bGBP.String(), Currency: "GBP"})

	tests := []struct {
		name          string
		keys          []gen.ListHoldingKeysRow
		txns          []gen.ListDatedTxnsRow
		txnsErr       error
		assertions    []gen.ListIdentifierAssertionsRow
		assertionsErr error
		ids           []uuid.UUID
		listings      []gen.ListListingNamesRow
		want          []*holdingv1.InstrumentHolding
		wantGroups    []*holdingv1.GroupHolding
		wantCode      connect.Code
	}{
		{
			// Days 0 and 25 lie outside coverage, where ticker validity is
			// provisional, and still name an instrument. A has no ticker
			// row, so the key's listing on A is A's listing in the stated
			// family, and A keeps its own name.
			name:       "a key whose transactions straddle a rename",
			keys:       []gen.ListHoldingKeysRow{datedKey(usd, &datedKeyID, "15")},
			txns:       []gen.ListDatedTxnsRow{txn(0, "1"), txn(5, "2"), txn(15, "4"), txn(25, "8")},
			assertions: straddle, ids: []uuid.UUID{aID, bID}, listings: listings,
			want: []*holdingv1.InstrumentHolding{
				holdingA("3", datedHoldingKey(usd, aUSD, "3")),
				holdingB("12", protoBListings, datedHoldingKey(usd, bUSD, "12")),
			},
		},
		{
			name:       "a retired ticker leaves the later transaction in the group",
			keys:       []gen.ListHoldingKeysRow{datedKey(usd, &datedKeyID, "6")},
			txns:       []gen.ListDatedTxnsRow{txn(5, "2"), txn(15, "4")},
			assertions: retired, ids: []uuid.UUID{aID}, listings: listings,
			want:       []*holdingv1.InstrumentHolding{holdingA("2", datedHoldingKey(usd, aUSD, "2"))},
			wantGroups: []*holdingv1.GroupHolding{group(usd, "4")},
		},
		{
			name:       "a dated key beside a fixed key of the same instrument",
			keys:       []gen.ListHoldingKeysRow{fixedKey, datedKey(usd, &datedKeyID, "2")},
			txns:       []gen.ListDatedTxnsRow{txn(5, "2")},
			assertions: straddle, ids: []uuid.UUID{aID}, listings: listings,
			want: []*holdingv1.InstrumentHolding{holdingA("12", fixedHoldingKey, datedHoldingKey(usd, aUSD, "2"))},
		},
		{
			name:       "a total of zero is no holding",
			keys:       []gen.ListHoldingKeysRow{fixedKey, datedKey(usd, &datedKeyID, "-10")},
			txns:       []gen.ListDatedTxnsRow{txn(5, "-10"), txn(15, "5"), txn(16, "-5")},
			assertions: retired,
		},
		{
			name:       "the listing in the stated family",
			keys:       []gen.ListHoldingKeysRow{datedKey(gbx, &datedKeyID, "4")},
			txns:       []gen.ListDatedTxnsRow{txn(15, "4")},
			assertions: straddle, ids: []uuid.UUID{bID}, listings: withBGBP,
			want: []*holdingv1.InstrumentHolding{holdingB("4", protoBListingsWithGBP, datedHoldingKey(gbx, bGBP, "4"))},
		},
		{
			name:       "the ticker's listing where the stated family has none",
			keys:       []gen.ListHoldingKeysRow{datedKey(gbx, &datedKeyID, "4")},
			txns:       []gen.ListDatedTxnsRow{txn(15, "4")},
			assertions: straddle, ids: []uuid.UUID{bID}, listings: listings,
			want: []*holdingv1.InstrumentHolding{holdingB("4", protoBListings, datedHoldingKey(gbx, bUSD, "4"))},
		},
		{
			name:       "the ticker's listing where no currency is stated",
			keys:       []gen.ListHoldingKeysRow{datedKey(nil, &datedKeyID, "4")},
			txns:       []gen.ListDatedTxnsRow{txn(15, "4")},
			assertions: straddle, ids: []uuid.UUID{bID}, listings: withBGBP,
			want: []*holdingv1.InstrumentHolding{holdingB("4", protoBListingsWithGBP, datedHoldingKey(nil, bUSD, "4"))},
		},
		{
			name:       "a dated key with an unresolved transaction and no group",
			keys:       []gen.ListHoldingKeysRow{datedKey(usd, nil, "4")},
			txns:       []gen.ListDatedTxnsRow{txn(15, "4")},
			assertions: retired, wantCode: connect.CodeInternal,
		},
		{
			name:    "transactions failure",
			keys:    []gen.ListHoldingKeysRow{datedKey(usd, &datedKeyID, "4")},
			txnsErr: errBoom, wantCode: connect.CodeInternal,
		},
		{
			name:          "facts failure",
			keys:          []gen.ListHoldingKeysRow{datedKey(usd, &datedKeyID, "4")},
			txns:          []gen.ListDatedTxnsRow{txn(15, "4")},
			assertionsErr: errBoom, wantCode: connect.CodeInternal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, nil)
			f.reader.EXPECT().ListHoldingKeys(gomock.Any(), userID).Return(tc.keys, nil)
			f.reader.EXPECT().ListDatedTxns(gomock.Any(), userID).Return(tc.txns, tc.txnsErr)
			// The facts of FOO are read once, however many transactions
			// rest on it.
			f.reader.EXPECT().ListIdentifierAssertions(gomock.Any(), fooAssertions).Return(tc.assertions, tc.assertionsErr).MaxTimes(1)
			f.reader.EXPECT().ListIdentifierEvents(gomock.Any(), fooEvents).Return(nil, nil).MaxTimes(1)
			f.reader.EXPECT().ListIdentifierCoverage(gomock.Any(), fooCoverage).Return(coverage, nil).MaxTimes(1)
			f.reader.EXPECT().ListInstrsByID(gomock.Any(), gomock.Any()).Return(instrs, nil).MaxTimes(1)
			if len(tc.ids) > 0 {
				f.reader.EXPECT().ListIdentifiersOf(gomock.Any(), tc.ids).Return(idents, nil)
				f.reader.EXPECT().ListListingNames(gomock.Any(), tc.ids).Return(tc.listings, nil)
				f.reader.EXPECT().ListInstrNames(gomock.Any(), tc.ids).Return(names, nil)
			}
			res, err := f.client.ListHoldings(context.Background(), connect.NewRequest(&holdingv1.ListHoldingsRequest{}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("ListHoldings() code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			want := &holdingv1.ListHoldingsResponse{Instruments: tc.want, Groups: tc.wantGroups}
			if diff := cmp.Diff(want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ListHoldings() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
