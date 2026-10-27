package resolve

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/join"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// resolution is one stated key's progress from lookup to outcome.
type resolution struct {
	row gen.StatedKey
	// globalAndDescriptionIDs caches globalAndDescriptionIDs of row.
	globalAndDescriptionIDs []types.Identifier
	// family is the family of the currency the key states.
	family          string
	knownInstrument *join.InstrumentWithCoverage
	// mics maps the venue of each ticker to its market.
	mics      mic.MICMap
	fetchKeys []*fetchKey
	findings  []gen.CreateFindingParams
	outcome   gen.ResolutionOutcome
	reason    string
	// via is the MIC_TICKER the key may be dated through: the one its dated
	// association names, or the one datedTicker finds. dating is the ticker
	// validity of its market ticker. It is nil for a key that is fixed whatever
	// it resolves to.
	via    types.Identifier
	dating *tickervalidity.Ticker
	// dates are the as-at dates of the key's transactions, stored and
	// stated.
	dates []time.Time
	// datedFromFacts reports that prepare dated a key through the facts of
	// its ticker, so the write gives it the dated association.
	datedFromFacts bool
	// written lists the MIC_TICKER assertions the key's write made, each with
	// the fetch key that made it. Each attempt of the write resets it.
	written []writtenFact
	// firstVerdict caches first for a resolution without a user's choice.
	firstVerdict *claimVerdict
}

// first returns the verdict among res's claims without the database's
// claim. Without a user's choice it is computed once, since the claims do
// not change after the fetch.
func (res *resolution) first(userChoice consistentClaims) claimVerdict {
	if userChoice != nil {
		return choose(res, nil, userChoice)
	}
	if res.firstVerdict == nil {
		v := choose(res, nil, nil)
		res.firstVerdict = &v
	}
	return *res.firstVerdict
}

// writtenFact is an identifier fact of id that the fetch key fk answered.
type writtenFact struct {
	fk *fetchKey
	id types.Identifier
}

// wrote records that the write asserted id from fk, where id is a venue
// ticker.
func (res *resolution) wrote(fk *fetchKey, id types.Identifier) {
	if id.Type == types.IdentifierTypeMicTicker && id.Domain != "" && id.Value != "" {
		res.written = append(res.written, writtenFact{fk: fk, id: id})
	}
}

// globalAndDescriptionIDs returns every global identifier and every broker
// description k states, strongest first. A lookup hit on one of them names the
// instrument.
func globalAndDescriptionIDs(k gen.StatedKey) []types.Identifier {
	var out []types.Identifier
	for _, id := range k.Identifiers {
		if id.IsGlobal() || id.Type == types.IdentifierTypeBrokerDescription {
			out = append(out, id)
		}
	}
	slices.SortStableFunc(out, func(a, b types.Identifier) int { return a.Strength() - b.Strength() })
	return out
}

// statesBareTicker reports whether k states a ticker without its venue.
func statesBareTicker(k gen.StatedKey) bool {
	for _, id := range k.Identifiers {
		if id.Type == types.IdentifierTypeMicTicker && id.Domain == "" {
			return true
		}
	}
	return false
}

// classConflict returns why the class k states contradicts an instrument of
// class, if it does.
func classConflict(k gen.StatedKey, class types.AssetClass) (string, bool) {
	if k.AssetClass == nil || !k.AssetClass.Disjoint(class) {
		return "", false
	}
	return fmt.Sprintf("asset class %s contradicts the instrument's %s", *k.AssetClass, class), true
}

func (res *resolution) decide(outcome gen.ResolutionOutcome, format string, args ...any) {
	res.outcome, res.reason = outcome, fmt.Sprintf(format, args...)
}

// lookupIDs returns the identifiers of ids that a lookup in the database may
// match: every one but a ticker. A ticker row names the instrument its ticker
// identifies today. What the ticker named on another day is read from its
// ticker validity, so resolution does not match through a ticker row. An option
// symbol still matches through its row until corporate actions are built.
func lookupIDs(ids []types.Identifier) []types.Identifier {
	var out []types.Identifier
	for _, id := range ids {
		if !id.Ticker() {
			out = append(out, id)
		}
	}
	return out
}

// findResolutionInstrument finds the instrument the database names for each
// of rs and decides the resolutions that never reach a datasource. Every
// key's identifiers are read in one query.
func (r *Resolver) findResolutionInstrument(ctx context.Context, rs []*resolution) error {
	var ids []types.Identifier
	for _, res := range rs {
		if !statesCurrency(res.row) {
			ids = append(ids, lookupIDs(res.globalAndDescriptionIDs)...)
		}
	}
	matches, err := findInstrumentsByIdentifier(ctx, r.store, ids)
	if err != nil {
		return err
	}
	byID := map[types.Identifier]*join.InstrumentWithCoverage{}
	for _, h := range matches {
		for _, id := range h.matched {
			byID[id] = h.instrument
		}
	}
	for _, res := range rs {
		if statesCurrency(res.row) {
			if err := r.findResolutionCurrency(ctx, res); err != nil {
				return err
			}
			continue
		}
		res.match(byID)
	}
	return nil
}

// statesCurrency reports whether k states a currency, which names money in it.
func statesCurrency(k gen.StatedKey) bool {
	return slices.ContainsFunc(k.Identifiers, func(id types.Identifier) bool { return id.Type == types.IdentifierTypeCurrency })
}

// match attaches to res the instrument byID names for it. It decides res
// when its identifiers name different instruments, when the class
// conflicts, or when the key names nothing a datasource could be asked.
func (res *resolution) match(byID map[types.Identifier]*join.InstrumentWithCoverage) {
	var first types.Identifier
	var f *join.InstrumentWithCoverage
	for _, id := range lookupIDs(res.globalAndDescriptionIDs) {
		hit, ok := byID[id]
		switch {
		case !ok:
			continue
		case f == nil:
			first, f = id, hit
		case hit.Row.ID != f.Row.ID:
			detail := fmt.Sprintf("%s and %s name different instruments", first.Name(), id.Name())
			res.findings = append(res.findings, gen.CreateFindingParams{Kind: gen.FindingKindContradiction, Detail: ptr.To(detail)})
			res.decide(gen.ResolutionOutcomeUnrecognised, "%s", detail)
			return
		}
	}
	if f == nil {
		if !slices.ContainsFunc(res.globalAndDescriptionIDs, types.Identifier.IsGlobal) && !statesBareTicker(res.row) {
			res.decide(gen.ResolutionOutcomeUnrecognised, "%s", unnamed(res.globalAndDescriptionIDs))
		}
		return
	}
	res.knownInstrument = f
	if reason, ok := classConflict(res.row, f.Row.AssetClass); ok {
		res.decide(gen.ResolutionOutcomeUnrecognised, "%s", reason)
	}
}

// unnamed returns the reason for a key that states no global identifier.
// Such a key looks up only a broker description, so the reason names that. The
// key shows the description's value beside the reason.
func unnamed(ids []types.Identifier) string {
	if len(ids) == 0 {
		return "no global identifier"
	}
	return "failed to match broker description"
}

// findResolutionCurrency resolves a currency key against the seed; a
// currency key never reaches a datasource.
func (r *Resolver) findResolutionCurrency(ctx context.Context, res *resolution) error {
	var code string
	for _, id := range res.row.Identifiers {
		if id.Type == types.IdentifierTypeCurrency {
			code = id.Value
			break
		}
	}
	hit, err := r.store.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: code})
	switch {
	case errors.Is(err, db.ErrNotFound):
		res.decide(gen.ResolutionOutcomeRejected, "no currency %s", code)
		return nil
	case err != nil:
		return fmt.Errorf("look up currency %s: %w", code, err)
	}
	if reason, ok := classConflict(res.row, hit.Instr.AssetClass); ok {
		res.decide(gen.ResolutionOutcomeRejected, "%s", reason)
		return nil
	}
	loaded, err := join.InstrumentsWithCoverage(ctx, r.store, []gen.Instr{hit.Instr})
	if err != nil {
		return err
	}
	f := loaded[hit.Instr.ID]
	if res.family != noFamily && f.Listing(res.family) == nil {
		res.decide(gen.ResolutionOutcomeRejected, "no listing of %s in %s", code, *res.row.Currency)
		return nil
	}
	res.knownInstrument = f
	return nil
}
