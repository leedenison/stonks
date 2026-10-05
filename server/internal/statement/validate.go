package statement

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
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
	k, err := g.keyFor(r.GetKey(), currencies)
	if err != nil {
		return reject("%v", err)
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
	out.key = g.intern(k)
	return out
}

// split reads sp, whose key must pass the checks a row's does.
func (g *ingestion) split(ordinal int32, sp *statementv1.StatedSplit, currencies map[string]bool) (split, error) {
	out := split{ordinal: ordinal}
	k, err := g.keyFor(sp.GetKey(), currencies)
	if err != nil {
		return out, err
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

// keyFor reads a key stated by a row or a split. It refuses a key that names
// no instrument or an unknown currency.
func (g *ingestion) keyFor(sk *typev1.StatedKey, currencies map[string]bool) (*key, error) {
	if sk == nil {
		return nil, errors.New("no key")
	}
	k, err := keyOf(sk, g.broker)
	if err != nil {
		return nil, err
	}
	if !k.statable() {
		return nil, errors.New("no identifier")
	}
	if k.currency != nil && !currencies[*k.currency] {
		return nil, fmt.Errorf("unknown currency %q", *k.currency)
	}
	return k, nil
}

// keyOf reads a stated key into its canonical form, so that two keys stating
// the same thing compare equal: the identifiers sorted by type, domain and
// value with an absent domain first, and stated once each. Two values of an
// exclusive type in one domain contradict each other and are refused. A
// statement states an issuer's identifier only in its own broker's domain.
func keyOf(sk *typev1.StatedKey, broker gen.Broker) (*key, error) {
	class, ok := types.FromProto[gen.AssetClass](sk.GetAssetClass())
	if !ok {
		return nil, fmt.Errorf("asset class %d outside the vocabulary", sk.GetAssetClass())
	}
	k := &key{currency: sk.Currency, identifiers: []types.Identifier{}}
	if class != "" {
		k.class = &class
	}
	for _, msg := range sk.GetIdentifiers() {
		t, ok := types.FromProto[types.IdentifierType](msg.GetType())
		if !ok || t == "" {
			return nil, fmt.Errorf("identifier type %d outside the vocabulary", msg.GetType())
		}
		id := types.Identifier{Type: t, Domain: msg.GetDomain(), Value: msg.GetValue()}
		if id.Value == "" {
			return nil, fmt.Errorf("%s identifier with no value", t)
		}
		if (t == types.IdentifierTypeBrokerID || t == types.IdentifierTypeBrokerDescription) && id.Domain != string(broker) {
			return nil, fmt.Errorf("%s identifier of broker %q in a statement of %s", t, id.Domain, broker)
		}
		if slices.Contains(k.identifiers, id) {
			continue
		}
		for _, held := range k.identifiers {
			if held.Type == t && held.Domain == id.Domain && market.Trait(t).Exclusive {
				return nil, fmt.Errorf("two %s identifiers, %s and %s", t, held.Value, id.Value)
			}
		}
		k.identifiers = append(k.identifiers, id)
	}
	slices.SortFunc(k.identifiers, func(a, b types.Identifier) int {
		return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.Domain, b.Domain), cmp.Compare(a.Value, b.Value))
	})
	return k, nil
}
