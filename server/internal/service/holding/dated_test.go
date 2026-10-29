package holding

import (
	"context"
	"errors"
	"testing"

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

// The ticker FOO at Nasdaq named instrument a, then b.
var (
	aID, bID        = uuid.MustParse("00000000-0000-0000-0000-000000000071"), uuid.MustParse("00000000-0000-0000-0000-000000000072")
	aUSD, aGBP      = uuid.MustParse("00000000-0000-0000-0000-000000000081"), uuid.MustParse("00000000-0000-0000-0000-000000000082")
	bUSD, bGBP      = uuid.MustParse("00000000-0000-0000-0000-000000000083"), uuid.MustParse("00000000-0000-0000-0000-000000000084")
	fixedKeyID      = uuid.MustParse("00000000-0000-0000-0000-000000000090")
	datedKeyID      = uuid.MustParse("00000000-0000-0000-0000-000000000091")
	fooRow          = gen.Ticker{ID: uuid.MustParse("00000000-0000-0000-0000-000000000092"), Market: "US", Value: "FOO"}
	foo             = types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "FOO"}
	aISIN           = types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0000000071"}
	protoFoo        = foo.ToProto()
	protoAISIN      = aISIN.ToProto()
	instrs          = []gen.Instr{{ID: aID, AssetClass: types.AssetClassStock}, {ID: bID, AssetClass: types.AssetClassStock}}
	idents          = []gen.Identifier{{InstrID: aID, Type: types.IdentifierTypeIsin, Value: "US0000000071"}, {InstrID: bID, Type: types.IdentifierTypeIsin, Value: "US0000000072"}}
	aListings       = []gen.ListListingNamesRow{{ListingID: aGBP, InstrID: aID, Currency: "GBP"}, {ListingID: aUSD, InstrID: aID, Currency: "USD"}}
	bListing        = gen.ListListingNamesRow{ListingID: bUSD, InstrID: bID, Currency: "USD", TickerID: &fooRow.ID, Market: ptr.To("US"), TickerValue: ptr.To("FOO"), Venue: ptr.To("XNAS"), VenueName: ptr.To("Nasdaq"), Relation: ptr.To(gen.TickerRelationCurrent), Rank: ptr.To(int64(1))}
	listings        = append(append([]gen.ListListingNamesRow{}, aListings...), bListing)
	names           = []gen.InstrName{{InstrID: aID, Type: types.IdentifierTypeIsin, Value: "US0000000071"}, {InstrID: bID, Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "FOO"}}
	protoAListings  = []*holdingv1.HoldingListing{{Id: aGBP.String(), Currency: "GBP"}, {Id: aUSD.String(), Currency: "USD"}}
	protoBListings  = []*holdingv1.HoldingListing{{Id: bUSD.String(), Currency: "USD", Venue: ptr.To("Nasdaq"), Ticker: &typev1.MarketTicker{Market: "US", Value: "FOO"}, Relation: typev1.TickerRelation_TICKER_RELATION_CURRENT}}
	protoAName      = &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "US0000000071"}
	protoIdentsOfA  = []*typev1.Identifier{protoAISIN}
	protoIdentsOfB  = []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "US0000000072"}}
	assetClassStock = typev1.AssetClass_ASSET_CLASS_STOCK
	// adjusted is the mark of a holding whose transactions are all dated
	// today, so no corporate action restates them.
	adjusted = holdingv1.HoldingMark_HOLDING_MARK_ADJUSTED
	errBoom  = errors.New("boom")
)

// datedKey is the key dated through FOO, stating currency, with the sum of
// its transactions on instr as its quantity. A nil instr sums the
// transactions on days when FOO names nothing.
func datedKey(currency *string, group, instr *uuid.UUID, quantity string) gen.ListHoldingKeysRow {
	return gen.ListHoldingKeysRow{
		StatedKey: gen.StatedKey{
			ID: datedKeyID, Currency: currency, Identifiers: []types.Identifier{foo},
			ViaTickerID: &fooRow.ID, ViaMarket: ptr.To("US"), ViaVenue: ptr.To("XNAS"),
			Arbiter: ptr.To(gen.ArbiterDatasource), GroupID: group,
		},
		InstrID:  instr,
		AsAt:     types.Day(now),
		Quantity: decimal.RequireFromString(quantity),
	}
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
		InstrID:   &aID,
		AsAt:      types.Day(now),
		Quantity:  decimal.RequireFromString("10"),
	}
	fixedHoldingKey := &holdingv1.HoldingKey{StatedKey: &typev1.StatedKey{Currency: usd, Identifiers: []*typev1.Identifier{protoAISIN}}, ListingId: aUSD.String(), Quantity: "10"}
	holdingA := func(quantity string, keys ...*holdingv1.HoldingKey) *holdingv1.InstrHolding {
		return &holdingv1.InstrHolding{InstrId: aID.String(), AssetClass: assetClassStock, Name: protoAName, Identifiers: protoIdentsOfA, Quantity: quantity, Listings: protoAListings, Keys: keys, Mark: adjusted}
	}
	holdingB := func(quantity string, listings []*holdingv1.HoldingListing, keys ...*holdingv1.HoldingKey) *holdingv1.InstrHolding {
		return &holdingv1.InstrHolding{InstrId: bID.String(), AssetClass: assetClassStock, Name: protoFoo, Identifiers: protoIdentsOfB, Quantity: quantity, Listings: listings, Keys: keys, Mark: adjusted}
	}
	group := func(currency *string, quantity string) *holdingv1.GroupHolding {
		return &holdingv1.GroupHolding{GroupId: datedKeyID.String(), Quantity: quantity, Identifiers: []*typev1.Identifier{protoFoo}, Keys: []*holdingv1.HoldingKey{datedHoldingKey(currency, uuid.Nil, quantity)}}
	}
	withBGBP := append(append([]gen.ListListingNamesRow{}, listings...), gen.ListListingNamesRow{ListingID: bGBP, InstrID: bID, Currency: "GBP"})
	protoBListingsWithGBP := append(append([]*holdingv1.HoldingListing{}, protoBListings...), &holdingv1.HoldingListing{Id: bGBP.String(), Currency: "GBP"})

	g := &datedKeyID
	tests := []struct {
		name       string
		keys       []gen.ListHoldingKeysRow
		keysErr    error
		ids        []uuid.UUID
		listings   []gen.ListListingNamesRow
		want       []*holdingv1.InstrHolding
		wantGroups []*holdingv1.GroupHolding
		wantCode   connect.Code
	}{
		{
			// A has no ticker row. The key's listing on A is A's listing in
			// the stated family. A keeps its own name.
			name: "a key whose transactions straddle a rename",
			keys: []gen.ListHoldingKeysRow{datedKey(usd, g, &aID, "3"), datedKey(usd, g, &bID, "12")},
			ids:  []uuid.UUID{aID, bID}, listings: listings,
			want: []*holdingv1.InstrHolding{
				holdingA("3", datedHoldingKey(usd, aUSD, "3")),
				holdingB("12", protoBListings, datedHoldingKey(usd, bUSD, "12")),
			},
		},
		{
			name: "a retired ticker leaves the later transaction in the group",
			keys: []gen.ListHoldingKeysRow{datedKey(usd, g, nil, "4"), datedKey(usd, g, &aID, "2")},
			ids:  []uuid.UUID{aID}, listings: listings,
			want:       []*holdingv1.InstrHolding{holdingA("2", datedHoldingKey(usd, aUSD, "2"))},
			wantGroups: []*holdingv1.GroupHolding{group(usd, "4")},
		},
		{
			name: "a dated key beside a fixed key of the same instrument",
			keys: []gen.ListHoldingKeysRow{fixedKey, datedKey(usd, g, &aID, "2")},
			ids:  []uuid.UUID{aID}, listings: listings,
			want: []*holdingv1.InstrHolding{holdingA("12", fixedHoldingKey, datedHoldingKey(usd, aUSD, "2"))},
		},
		{
			name: "a total of zero is no holding",
			keys: []gen.ListHoldingKeysRow{fixedKey, datedKey(usd, g, nil, "0"), datedKey(usd, g, &aID, "-10")},
		},
		{
			name: "the listing in the stated family",
			keys: []gen.ListHoldingKeysRow{datedKey(gbx, g, &bID, "4")},
			ids:  []uuid.UUID{bID}, listings: withBGBP,
			want: []*holdingv1.InstrHolding{holdingB("4", protoBListingsWithGBP, datedHoldingKey(gbx, bGBP, "4"))},
		},
		{
			name: "the ticker's listing where the stated family has none",
			keys: []gen.ListHoldingKeysRow{datedKey(gbx, g, &bID, "4")},
			ids:  []uuid.UUID{bID}, listings: listings,
			want: []*holdingv1.InstrHolding{holdingB("4", protoBListings, datedHoldingKey(gbx, bUSD, "4"))},
		},
		{
			name: "the ticker's listing where no currency is stated",
			keys: []gen.ListHoldingKeysRow{datedKey(nil, g, &bID, "4")},
			ids:  []uuid.UUID{bID}, listings: withBGBP,
			want: []*holdingv1.InstrHolding{holdingB("4", protoBListingsWithGBP, datedHoldingKey(nil, bUSD, "4"))},
		},
		{
			name:     "a dated key with an unresolved transaction and no group",
			keys:     []gen.ListHoldingKeysRow{datedKey(usd, nil, nil, "4")},
			wantCode: connect.CodeInternal,
		},
		{
			name:    "keys failure",
			keysErr: errBoom, wantCode: connect.CodeInternal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, nil)
			f.reader.EXPECT().ListHoldingKeys(gomock.Any(), userID).Return(tc.keys, tc.keysErr)
			f.reader.EXPECT().ListInstrsByID(gomock.Any(), gomock.Any()).Return(instrs, nil).MaxTimes(1)
			f.reader.EXPECT().ListInstrMarkets(gomock.Any(), gomock.Any()).Return(nil, nil).MaxTimes(1)
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
			want := &holdingv1.ListHoldingsResponse{Instrs: tc.want, Groups: tc.wantGroups}
			if diff := cmp.Diff(want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ListHoldings() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
