package statement

import (
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
)

// parseDate reads an ISO 8601 date as midnight UTC.
func parseDate(s string) (time.Time, error) {
	return time.ParseInLocation(time.DateOnly, s, time.UTC)
}

// validate reads r into a row. A row that fails a check carries the first
// failing check as its reason; any other row carries its interned key.
func (g *ingestion) validate(ordinal int32, r *statementv1.Row, today time.Time, currencies map[string]bool) row {
	out := row{ordinal: ordinal, stated: r}
	reject := func(format string, args ...any) row {
		out.reason = fmt.Sprintf(format, args...)
		return out
	}
	if r.GetKey() == nil {
		return reject("no key")
	}
	k, err := keyOf(r.GetKey())
	if err != nil {
		return reject("%v", err)
	}
	if !k.statable() {
		return reject("no identifier")
	}
	dates := []struct {
		name string
		s    string
		dst  *time.Time
	}{{"order date", r.GetOrderDate(), &out.order}, {"settlement date", r.GetSettlementDate(), &out.settlement}, {"as at", r.GetAsAt(), &out.asAt}}
	for _, d := range dates {
		if *d.dst, err = parseDate(d.s); err != nil {
			return reject("malformed %s %q", d.name, d.s)
		}
	}
	if out.quantity, err = decimal.NewFromString(r.GetQuantity()); err != nil {
		return reject("malformed quantity %q", r.GetQuantity())
	}
	if out.order.Before(g.from) || !out.order.Before(g.before) {
		return reject("order date outside the claimed period")
	}
	if out.order.After(today) {
		return reject("order date after today")
	}
	if k.currency != nil && !currencies[*k.currency] {
		return reject("unknown currency %q", *k.currency)
	}
	out.key = g.intern(k)
	return out
}

// split reads sp, whose key must pass the checks a row's does.
func (g *ingestion) split(ordinal int32, sp *statementv1.StatedSplit, currencies map[string]bool) (split, error) {
	out := split{ordinal: ordinal}
	if sp.GetKey() == nil {
		return out, fmt.Errorf("no key")
	}
	k, err := keyOf(sp.GetKey())
	if err != nil {
		return out, err
	}
	if !k.statable() {
		return out, fmt.Errorf("no identifier")
	}
	if k.currency != nil && !currencies[*k.currency] {
		return out, fmt.Errorf("unknown currency %q", *k.currency)
	}
	if out.effective, err = parseDate(sp.GetEffectiveDate()); err != nil {
		return out, fmt.Errorf("malformed effective date %q", sp.GetEffectiveDate())
	}
	if out.quantity, err = decimal.NewFromString(sp.GetQuantity()); err != nil {
		return out, fmt.Errorf("malformed quantity %q", sp.GetQuantity())
	}
	if r := sp.GetRatio(); r != nil {
		from, err := decimal.NewFromString(r.GetFrom())
		if err != nil {
			return out, fmt.Errorf("malformed ratio from %q", r.GetFrom())
		}
		to, err := decimal.NewFromString(r.GetTo())
		if err != nil {
			return out, fmt.Errorf("malformed ratio to %q", r.GetTo())
		}
		out.from, out.to = &from, &to
	}
	out.key = g.intern(k)
	return out, nil
}

// keyOf reads a stated key into its canonical form, so that two keys stating
// the same thing compare equal: the identifiers sorted by type, domain and
// value with an absent domain first. Two values of an exclusive type in one
// domain contradict each other and are refused.
func keyOf(sk *typev1.StatedKey) (*key, error) {
	class, ok := types.FromProto[gen.AssetClass](sk.GetAssetClass())
	if !ok {
		return nil, fmt.Errorf("asset class %d outside the vocabulary", sk.GetAssetClass())
	}
	k := &key{currency: sk.Currency, identifiers: []types.Identifier{}}
	if class != "" {
		k.class = &class
	}
	for _, id := range sk.GetIdentifiers() {
		t, ok := types.FromProto[types.IdentifierType](id.GetType())
		if !ok || t == "" {
			return nil, fmt.Errorf("identifier type %d outside the vocabulary", id.GetType())
		}
		for _, held := range k.identifiers {
			if held.Type == t && held.Domain == id.GetDomain() && market.Trait(t).Exclusive {
				return nil, fmt.Errorf("two %s identifiers, %s and %s", t, held.Value, id.GetValue())
			}
		}
		k.identifiers = append(k.identifiers, types.Identifier{Type: t, Domain: id.GetDomain(), Value: id.GetValue()})
	}
	sort.Slice(k.identifiers, func(i, j int) bool {
		a, b := k.identifiers[i], k.identifiers[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Domain != b.Domain {
			return a.Domain < b.Domain
		}
		return a.Value < b.Value
	})
	return k, nil
}
