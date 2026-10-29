// Package to converts generated rows to the types of [types] and to proto
// messages. A conversion is a method on its source type, defined beside it;
// a generated type cannot carry one, so its conversions live here. Each is
// a field copy: a conversion that reads anything else stays with its caller.
package to

import (
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminblockv1 "github.com/leedenison/stonks/proto/admin/block/v1"
	admincoveragev1 "github.com/leedenison/stonks/proto/admin/coverage/v1"
	admininstrv1 "github.com/leedenison/stonks/proto/admin/instr/v1"
	adminrunv1 "github.com/leedenison/stonks/proto/admin/run/v1"
	authv1 "github.com/leedenison/stonks/proto/auth/v1"
	holdingv1 "github.com/leedenison/stonks/proto/holding/v1"
	runv1 "github.com/leedenison/stonks/proto/run/v1"
	stmtv1 "github.com/leedenison/stonks/proto/stmt/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Identifier returns the triple an identifier row names.
func Identifier(row gen.Identifier) types.Identifier {
	return types.Identifier{Type: row.Type, Domain: row.Domain, Value: row.Value}
}

// Sent returns the identifier under which a fetch key was sent, false where
// the key was not sent.
func Sent(k gen.FetchKey) (types.Identifier, bool) {
	return sent(k.SentType, k.SentDomain, k.SentValue)
}

// BlockSent returns the identifier under which a block was raised, false
// where the block is of the whole datasource.
func BlockSent(b gen.DatasourceBlock) (types.Identifier, bool) {
	return sent(b.SentType, b.SentDomain, b.SentValue)
}

// sent reads the identifier columns of a row. A domain has a type and a
// domain and no value.
func sent(typ *types.IdentifierType, domain string, value *string) (types.Identifier, bool) {
	if typ == nil {
		return types.Identifier{}, false
	}
	out := types.Identifier{Type: *typ, Domain: domain}
	if value != nil {
		out.Value = *value
	}
	return out, true
}

// ProtoRun writes a run row as its proto message.
func ProtoRun(r gen.Run) *runv1.Run {
	out := &runv1.Run{
		Id:        r.ID.String(),
		Kind:      types.ToProto[runv1.RunKind](r.Kind),
		Trigger:   types.ToProto[runv1.RunTrigger](r.Trigger),
		State:     types.ToProto[runv1.RunState](r.State),
		Error:     r.Error,
		CreatedAt: timestamppb.New(r.CreatedAt),
	}
	if r.ParentID != nil {
		parent := r.ParentID.String()
		out.ParentId = &parent
	}
	if r.StartedAt != nil {
		out.StartedAt = timestamppb.New(*r.StartedAt)
	}
	if r.FinishedAt != nil {
		out.FinishedAt = timestamppb.New(*r.FinishedAt)
	}
	return out
}

// ProtoUser writes a user row as its proto message.
func ProtoUser(u gen.User) *authv1.User {
	return &authv1.User{Id: u.ID.String(), Email: u.Email, Name: u.Name, Role: types.ToProto[authv1.Role](u.Role)}
}

// ProtoStatedKey writes a stated key row as the key its source stated.
func ProtoStatedKey(k gen.StatedKey) *typev1.StatedKey {
	out := &typev1.StatedKey{Currency: k.Currency}
	if k.AssetClass != nil {
		out.AssetClass = types.ToProto[typev1.AssetClass](*k.AssetClass)
	}
	for _, i := range k.Identifiers {
		out.Identifiers = append(out.Identifiers, i.ToProto())
	}
	return out
}

// ProtoResolutionItem writes k with what a resolution made of it. A nil r,
// for a key no resolution has reached, leaves the outcome unset.
func ProtoResolutionItem(k gen.StatedKey, r *gen.ResolutionKey) *typev1.ResolutionItem {
	out := &typev1.ResolutionItem{StatedKey: ProtoStatedKey(k), StatedKeyId: k.ID.String()}
	if r != nil {
		out.Outcome = types.ToProto[typev1.ResolutionOutcome](r.Outcome)
		out.Reasons = r.Reasons
		out.Offered = r.Offered
	}
	if k.Arbiter != nil {
		out.Arbiter = types.ToProto[typev1.Arbiter](*k.Arbiter)
	}
	return out
}

// InstrName returns the identifier that names an instrument.
func InstrName(n gen.InstrName) types.Identifier {
	return types.Identifier{Type: n.Type, Domain: n.Domain, Value: n.Value}
}

// ProtoHoldingListing writes l, the row that ranks first as a listing's
// name, as a named listing. The venue is its common name, or its MIC where it
// has none.
func ProtoHoldingListing(l gen.ListListingNamesRow) *holdingv1.HoldingListing {
	out := &holdingv1.HoldingListing{Id: l.ListingID.String(), Currency: l.Currency}
	if l.Market == nil || l.TickerValue == nil || l.Relation == nil {
		return out
	}
	out.Ticker = types.MarketTicker{Market: *l.Market, Value: *l.TickerValue}.ToProto()
	out.Relation = types.ToProto[typev1.TickerRelation](*l.Relation)
	out.Venue = l.VenueName
	if out.Venue == nil {
		out.Venue = l.Venue
	}
	return out
}

// ProtoHoldingKey writes k as a key of a holding, with the quantity the
// holding sums from it.
func ProtoHoldingKey(k gen.StatedKey, quantity decimal.Decimal) *holdingv1.HoldingKey {
	out := &holdingv1.HoldingKey{StatedKey: ProtoStatedKey(k), Quantity: quantity.String()}
	if k.ListingID != nil {
		out.ListingId = k.ListingID.String()
	}
	return out
}

// ProtoTxn writes a transaction row with its mark.
func ProtoTxn(txn gen.Txn, m stmtv1.TxnMark) *stmtv1.Txn {
	return &stmtv1.Txn{
		Id:          txn.ID.String(),
		StatedKeyId: txn.StatedKeyID.String(),
		OrderDate:   txn.OrderDate.Format(time.DateOnly),
		Quantity:    txn.Quantity.String(),
		Mark:        m,
	}
}

// ProtoStmtSummary writes a statement as a summary.
func ProtoStmtSummary(stmt gen.Stmt, run gen.Run, rejected int32, live bool) *stmtv1.StmtSummary {
	return &stmtv1.StmtSummary{
		Run:      ProtoRun(run),
		Broker:   types.ToProto[typev1.Broker](stmt.Broker),
		Period:   types.Period{From: stmt.OrderFrom, Before: stmt.OrderBefore}.ToProto(),
		Rows:     stmt.RowCount,
		Rejected: rejected,
		Live:     live,
	}
}

// ProtoBlock writes a block as its message. Its run is the fetch that
// raised it.
func ProtoBlock(r gen.ListBlocksRow) *adminblockv1.Block {
	b := r.DatasourceBlock
	out := &adminblockv1.Block{
		Id:         b.ID.String(),
		Datasource: b.Datasource,
		Kind:       types.ToProto[adminblockv1.FetchKind](b.Kind),
		Scope:      types.ToProto[adminblockv1.BlockScope](b.Scope),
		Reason:     b.Reason,
		RunId:      r.FetchID.String(),
		CreatedAt:  timestamppb.New(b.CreatedAt),
	}
	if sent, ok := BlockSent(b); ok {
		out.Sent = sent.ToProto()
	}
	if b.ClearedAt != nil {
		out.ClearedAt = timestamppb.New(*b.ClearedAt)
	}
	return out
}

// ProtoFetchItem writes a fetch key with the stated key it concerns, where
// one asked.
func ProtoFetchItem(it gen.ListFetchItemsRow) *adminrunv1.FetchItem {
	k := it.FetchKey
	out := &adminrunv1.FetchItem{
		Outcome:  types.ToProto[adminrunv1.FetchOutcome](k.Outcome),
		Attempts: int32(k.Attempts),
		Reason:   k.Reason,
	}
	if k.StatedKeyID != nil {
		id := k.StatedKeyID.String()
		out.StatedKeyId = &id
		out.StatedKey = ProtoStatedKey(gen.StatedKey{
			Identifiers: it.KeyIdentifiers, AssetClass: it.KeyAssetClass, Currency: it.KeyCurrency,
		})
	}
	if sent, ok := Sent(k); ok {
		out.Sent = sent.ToProto()
	}
	return out
}

// ProtoAssertionItem converts a ticker's assertion with its source: the
// datasource of the fetch that made it, or the administrator who attested it.
// value is the value of the assertion's ticker.
func ProtoAssertionItem(a gen.IdentifierAssertion, value string, datasource, attestedBy *string) *adminrunv1.AssertionItem {
	out := &adminrunv1.AssertionItem{
		Id:         a.ID.String(),
		Ticker:     types.MarketTicker{Market: a.Market, Value: value}.ToProto(),
		Period:     types.Period{From: a.AssertedFrom, Before: a.AssertedBefore}.ToProto(),
		EdgeFrom:   a.EdgeFrom,
		EdgeBefore: a.EdgeBefore,
	}
	if a.InstrID != nil {
		id := a.InstrID.String()
		out.InstrId = &id
	}
	switch {
	case datasource != nil:
		out.Source = &adminrunv1.AssertionItem_Datasource{Datasource: *datasource}
	case attestedBy != nil:
		out.Source = &adminrunv1.AssertionItem_AttestedBy{AttestedBy: *attestedBy}
	}
	return out
}

// ProtoAssertionCoverage converts a period of assertion coverage.
func ProtoAssertionCoverage(p gen.ListIdentifierAssertionCoverageRow) *admincoveragev1.AssertionCoverage {
	return &admincoveragev1.AssertionCoverage{
		Datasource: p.Datasource,
		Period:     types.Period{From: p.CoveredFrom, Before: p.CoveredBefore}.ToProto(),
	}
}

// ProtoInstr writes an instrument with the identifier that names it,
// and leaves its listings and identifiers to the caller.
func ProtoInstr(r gen.ListInstrsRow) *admininstrv1.Instr {
	out := &admininstrv1.Instr{Id: r.ID.String(), AssetClass: types.ToProto[typev1.AssetClass](r.AssetClass)}
	if r.NameType != "" {
		out.Name = types.Identifier{Type: types.IdentifierType(r.NameType), Domain: r.NameDomain, Value: r.Name}.ToProto()
	}
	return out
}

// ProtoListingTicker writes the ticker columns of l as a listing's ticker.
func ProtoListingTicker(l gen.ListListingNamesRow) *admininstrv1.Listing_Ticker {
	out := &admininstrv1.Listing_Ticker{Mic: l.Venue}
	if l.Market != nil && l.TickerValue != nil {
		out.Ticker = types.MarketTicker{Market: *l.Market, Value: *l.TickerValue}.ToProto()
	}
	if l.Relation != nil {
		out.Relation = types.ToProto[typev1.TickerRelation](*l.Relation)
	}
	return out
}

// ProtoListing writes the listing columns of l, and leaves its identifiers
// and tickers to the caller.
func ProtoListing(l gen.ListListingNamesRow) *admininstrv1.Listing {
	out := &admininstrv1.Listing{Id: l.ListingID.String(), Currency: l.Currency}
	if l.PrimaryMic != nil {
		out.PrimaryMic = *l.PrimaryMic
	}
	return out
}
