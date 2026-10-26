// Package to converts generated rows to the types of [types] and to proto
// messages. A conversion is a method on its source type, defined beside it;
// a generated type cannot carry one, so its conversions live here. Each is
// a field copy: a conversion that reads anything else stays with its caller.
package to

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	adminblockv1 "github.com/leedenison/stonks/proto/admin/block/v1"
	admincoveragev1 "github.com/leedenison/stonks/proto/admin/coverage/v1"
	admininstrumentv1 "github.com/leedenison/stonks/proto/admin/instrument/v1"
	adminrunv1 "github.com/leedenison/stonks/proto/admin/run/v1"
	authv1 "github.com/leedenison/stonks/proto/auth/v1"
	holdingv1 "github.com/leedenison/stonks/proto/holding/v1"
	runv1 "github.com/leedenison/stonks/proto/run/v1"
	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
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

// AssociatedTicker returns the MIC_TICKER of k's dated association, and
// false where k has none.
func AssociatedTicker(k gen.StatedKey) (types.Identifier, bool) {
	if k.ViaType == nil || k.ViaDomain == nil || k.ViaValue == nil {
		return types.Identifier{}, false
	}
	return types.Identifier{Type: *k.ViaType, Domain: *k.ViaDomain, Value: *k.ViaValue}, true
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
	if v, ok := AssociatedTicker(k); ok {
		out.Ticker = v.ToProto()
	}
	return out
}

// InstrumentName returns the identifier that names an instrument.
func InstrumentName(n gen.InstrumentName) types.Identifier {
	return types.Identifier{Type: n.Type, Domain: n.Domain, Value: n.Value}
}

// ProtoHoldingListing writes l as a named listing.
func ProtoHoldingListing(l gen.ListListingNamesRow) *holdingv1.HoldingListing {
	out := &holdingv1.HoldingListing{Id: l.ListingID.String(), Currency: l.Currency, Venue: l.Venue}
	if l.TickerDomain != nil && l.TickerValue != nil {
		out.Ticker = types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: *l.TickerDomain, Value: *l.TickerValue}.ToProto()
	}
	return out
}

// ProtoHoldingKey writes k as a key of a holding.
func ProtoHoldingKey(k gen.ListHoldingKeysRow) *holdingv1.HoldingKey {
	out := &holdingv1.HoldingKey{StatedKey: ProtoStatedKey(k.StatedKey), Quantity: k.Quantity.String()}
	if k.StatedKey.ListingID != nil {
		out.ListingId = k.StatedKey.ListingID.String()
	}
	return out
}

// ProtoTransaction writes a transaction row with its mark.
func ProtoTransaction(tx gen.Transaction, m statementv1.TransactionMark) *statementv1.Transaction {
	return &statementv1.Transaction{
		Id:          tx.ID.String(),
		StatedKeyId: tx.StatedKeyID.String(),
		OrderDate:   tx.OrderDate.Format(time.DateOnly),
		Quantity:    tx.Quantity.String(),
		Mark:        m,
	}
}

// ProtoStatementSummary writes a statement, its run and its count of
// rejected rows as a summary.
func ProtoStatementSummary(st gen.Statement, run gen.Run, rejected int32) *statementv1.StatementSummary {
	return &statementv1.StatementSummary{
		Run:         ProtoRun(run),
		Broker:      types.ToProto[typev1.Broker](st.Broker),
		OrderFrom:   st.OrderFrom.Format("2006-01-02"),
		OrderBefore: st.OrderBefore.Format("2006-01-02"),
		Rows:        st.RowCount,
		Rejected:    rejected,
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

// ProtoAssertionItem converts an assertion with its source: the datasource
// of the fetch that made it, or the administrator who attested it.
func ProtoAssertionItem(a gen.IdentifierAssertion, datasource, attestedBy *string) *adminrunv1.AssertionItem {
	out := &adminrunv1.AssertionItem{
		Id:         a.ID.String(),
		Ticker:     types.Identifier{Type: a.Type, Domain: a.Domain, Value: a.Value}.ToProto(),
		AssertedOn: a.AssertedOn.Format(time.DateOnly),
	}
	if a.InstrumentID != nil {
		id := a.InstrumentID.String()
		out.InstrumentId = &id
	}
	switch {
	case datasource != nil:
		out.Source = &adminrunv1.AssertionItem_Datasource{Datasource: *datasource}
	case attestedBy != nil:
		out.Source = &adminrunv1.AssertionItem_AttestedBy{AttestedBy: *attestedBy}
	}
	return out
}

// ProtoCoverage converts a coverage row of a ticker or of its domain.
func ProtoCoverage(c gen.ListIdentifierCoverageRow) *admincoveragev1.Coverage {
	covers := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: c.Domain}
	if c.Value != nil {
		covers.Value = *c.Value
	}
	return &admincoveragev1.Coverage{
		Datasource: c.Datasource, Covers: covers.ToProto(),
		CoveredFrom: c.CoveredFrom.Format(time.DateOnly), CoveredBefore: c.CoveredBefore.Format(time.DateOnly),
	}
}

// ProtoIdentifierEvent converts an event of a ticker.
func ProtoIdentifierEvent(e gen.ListIdentifierEventsRow) *admincoveragev1.IdentifierEvent {
	out := &admincoveragev1.IdentifierEvent{
		EffectiveOn: e.EffectiveOn.Format(time.DateOnly),
		Action:      types.ToProto[admincoveragev1.IdentifierEventAction](e.Action),
		Value:       e.Value,
	}
	if e.ToValue != nil {
		out.ToValue = *e.ToValue
	}
	return out
}

// ProtoInstrumentTicker converts a ticker of an instrument.
func ProtoInstrumentTicker(r gen.ListInstrumentTickersRow) *admincoveragev1.InstrumentTicker {
	v := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: r.Domain, Value: r.Value}
	return &admincoveragev1.InstrumentTicker{Ticker: v.ToProto(), Current: r.Current, Transactions: r.Transactions}
}

// ProtoInstrument writes an instrument with the identifier that names it,
// and leaves its listings and identifiers to the caller.
func ProtoInstrument(r gen.ListInstrumentsRow) *admininstrumentv1.Instrument {
	out := &admininstrumentv1.Instrument{Id: r.ID.String(), AssetClass: types.ToProto[typev1.AssetClass](r.AssetClass)}
	if r.NameType != "" {
		out.Name = types.Identifier{Type: types.IdentifierType(r.NameType), Domain: r.NameDomain, Value: r.Name}.ToProto()
	}
	return out
}

// ProtoListing writes a listing, and leaves its identifiers to the caller.
func ProtoListing(l gen.Listing) *admininstrumentv1.Listing {
	out := &admininstrumentv1.Listing{Id: l.ID.String(), Currency: l.Currency}
	if l.PrimaryMic != nil {
		out.PrimaryMic = *l.PrimaryMic
	}
	return out
}
